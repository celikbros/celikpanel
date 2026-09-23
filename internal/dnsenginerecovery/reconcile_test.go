package dnsenginerecovery

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func switchFixture(t *testing.T) (dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchIdentity) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := dnsengineartifact.JournalPolicy{StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json", RequireOwner: true, PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf", PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3"}
	j, err := p.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p, j, dnsengineartifact.SwitchIdentity{RequestID: j.MutationRequestID, OwnerID: j.MutationOwnerID, Target: j.TargetEngine, Qualifier: j.ManifestQualifier}
}

type trace struct {
	steps                                      []string
	journal                                    dnsengineartifact.SwitchJournalV1
	exists, finalized                          bool
	targetErr, writeErr, inverseErr, removeErr error
	writes                                     int
}

func (tr *trace) operations() Operations {
	return Operations{
		Read: func(context.Context) (dnsengineartifact.SwitchJournalV1, bool, error) {
			tr.steps = append(tr.steps, "read")
			return tr.journal, tr.exists, nil
		},
		ProveFinalized: func(context.Context, dnsengineartifact.SwitchIdentity) (bool, error) {
			tr.steps = append(tr.steps, "finalized")
			return tr.finalized, nil
		},
		VerifyTarget: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			tr.steps = append(tr.steps, "verify")
			return tr.targetErr
		},
		Write: func(_ context.Context, j dnsengineartifact.SwitchJournalV1) error {
			tr.steps = append(tr.steps, "write:"+j.Phase)
			tr.writes++
			if tr.writeErr != nil && tr.writes == 2 {
				return tr.writeErr
			}
			tr.journal = j
			return nil
		},
		Inverse: func(_ context.Context, j dnsengineartifact.SwitchJournalV1) error {
			tr.steps = append(tr.steps, "inverse")
			return tr.inverseErr
		},
		Remove: func(context.Context) error { tr.steps = append(tr.steps, "remove"); return tr.removeErr },
	}
}
func TestReconcileExactOperationOrder(t *testing.T) {
	p, j, id := switchFixture(t)
	for name, tc := range map[string]struct {
		exists, finalized bool
		targetErr         error
		phase             string
		outcome           Outcome
		steps             []string
	}{
		"no journal":         {false, false, nil, "", OutcomeAbsent, []string{"read", "finalized"}},
		"finalized":          {false, true, nil, "", OutcomeFinalized, []string{"read", "finalized"}},
		"target":             {true, false, nil, "", OutcomeCommitted, []string{"read", "verify", "write:committed"}},
		"precommit inverse":  {true, false, errors.New("unverified"), "", OutcomeRolledBack, []string{"read", "verify", "write:rolling-back", "inverse", "write:rolled-back", "remove"}},
		"verified conflict":  {true, false, errors.New("changed"), dnsengineartifact.SwitchPhaseTargetVerified, OutcomeAbsent, []string{"read", "verify"}},
		"committed conflict": {true, false, errors.New("changed"), dnsengineartifact.SwitchPhaseCommitted, OutcomeAbsent, []string{"read", "verify"}},
	} {
		t.Run(name, func(t *testing.T) {
			current := j
			if tc.phase != "" {
				current.Phase = tc.phase
			}
			before := append([]byte(nil), current.StateBefore.Data...)
			tr := &trace{journal: current, exists: tc.exists, finalized: tc.finalized, targetErr: tc.targetErr}
			got, err := Reconcile(context.Background(), p, id, tr.operations())
			if err != nil && tc.phase == "" {
				t.Fatal(err)
			}
			if got != tc.outcome || !reflect.DeepEqual(tr.steps, tc.steps) || !bytes.Equal(tr.journal.StateBefore.Data, before) {
				t.Fatalf("got %s, steps %v, err %v", got, tr.steps, err)
			}
			if tc.phase != "" && err == nil {
				t.Fatal("verified target conflict was not reported")
			}
		})
	}
}
func TestReconcileNeverExecutesForeignOrMalformedJournal(t *testing.T) {
	p, j, id := switchFixture(t)
	cases := map[string]func(*dnsengineartifact.SwitchJournalV1){
		"owner":   func(j *dnsengineartifact.SwitchJournalV1) { j.MutationOwnerID = "ffffffffffffffffffffffffffffffff" },
		"request": func(j *dnsengineartifact.SwitchJournalV1) { j.MutationRequestID = "ffffffffffffffffffffffffffffffff" },
		"target":  func(j *dnsengineartifact.SwitchJournalV1) { j.TargetEngine = "pdns" },
		"qualifier": func(j *dnsengineartifact.SwitchJournalV1) {
			j.ManifestQualifier = "dns-engine-switch/v1:sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		},
		"source": func(j *dnsengineartifact.SwitchJournalV1) { j.StateBefore.Data = []byte("hidden source") },
		"config": func(j *dnsengineartifact.SwitchJournalV1) { j.ConfigBefore[0].Path = "/etc/passwd" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cur := j
			change(&cur)
			tr := &trace{journal: cur, exists: true}
			if _, err := Reconcile(context.Background(), p, id, tr.operations()); err == nil {
				t.Fatal("foreign journal accepted")
			}
			if !reflect.DeepEqual(tr.steps, []string{"read"}) {
				t.Fatal("mutation callback reached", tr.steps)
			}
		})
	}
	bad := id
	bad.RequestID = ""
	tr := &trace{journal: j, exists: true}
	if _, err := Reconcile(context.Background(), p, bad, tr.operations()); err == nil || len(tr.steps) != 0 {
		t.Fatal("invalid expectation read evidence")
	}
}
func TestReconcileInterruptedInverseRetainsJournal(t *testing.T) {
	p, j, id := switchFixture(t)
	for name, injected := range map[string]func(*trace){
		"inverse fails":             func(tr *trace) { tr.inverseErr = errors.New("owner changed") },
		"terminal checkpoint fails": func(tr *trace) { tr.writeErr = errors.New("rename uncertain") },
		"removal fails":             func(tr *trace) { tr.removeErr = errors.New("unlink uncertain") },
	} {
		t.Run(name, func(t *testing.T) {
			tr := &trace{journal: j, exists: true, targetErr: errors.New("target unverified")}
			injected(tr)
			if got, err := Reconcile(context.Background(), p, id, tr.operations()); err == nil || got != OutcomeAbsent {
				t.Fatal(got, err)
			}
			if tr.steps[len(tr.steps)-1] == "remove" && tr.removeErr == nil {
				t.Fatal("discarded unresolved before image")
			}
			if tr.journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && tr.journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
				t.Fatal("lost durable rollback checkpoint")
			}
		})
	}
}
