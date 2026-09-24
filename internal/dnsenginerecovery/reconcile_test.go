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
	exists, finalized, absent                  bool
	targetErr, absentErr, writeErr, inverseErr error
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
		ProveTargetAbsent: func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error) {
			tr.steps = append(tr.steps, "prove-absent")
			return tr.absent, tr.absentErr
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
		"precommit inverse":  {true, false, errors.New("unverified"), "", OutcomeRolledBack, []string{"read", "verify", "prove-absent", "write:rolling-back", "inverse", "write:rolled-back"}},
		"verified conflict":  {true, false, errors.New("changed"), dnsengineartifact.SwitchPhaseTargetVerified, OutcomeAbsent, []string{"read", "verify"}},
		"committed conflict": {true, false, errors.New("changed"), dnsengineartifact.SwitchPhaseCommitted, OutcomeAbsent, []string{"read", "verify"}},
	} {
		t.Run(name, func(t *testing.T) {
			current := j
			if tc.phase != "" {
				current.Phase = tc.phase
			}
			before := append([]byte(nil), current.StateBefore.Data...)
			tr := &trace{journal: current, exists: tc.exists, finalized: tc.finalized, targetErr: tc.targetErr, absent: true}
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
	} {
		t.Run(name, func(t *testing.T) {
			tr := &trace{journal: j, exists: true, targetErr: errors.New("target unverified"), absent: true}
			injected(tr)
			if got, err := Reconcile(context.Background(), p, id, tr.operations()); err == nil || got != OutcomeAbsent {
				t.Fatal(got, err)
			}
			if tr.journal.Phase != dnsengineartifact.SwitchPhaseRollingBack && tr.journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
				t.Fatal("lost durable rollback checkpoint")
			}
		})
	}
}

func TestReconcileUncertainTargetNeverBeginsInverse(t *testing.T) {
	p, j, id := switchFixture(t)
	for name, tc := range map[string]struct {
		absentErr error
		cancel    bool
	}{
		"target receipt or foreign source": {},
		"source observation failed":        {absentErr: errors.New("read failed")},
		"target observation timed out":     {cancel: true},
	} {
		t.Run(name, func(t *testing.T) {
			tr := &trace{journal: j, exists: true, targetErr: errors.New("runtime unknown")}
			tr.absentErr = tc.absentErr
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			got, err := Reconcile(ctx, p, id, tr.operations())
			if got != OutcomeAbsent || err == nil {
				t.Fatalf("uncertain result=%s err=%v", got, err)
			}
			for _, step := range tr.steps {
				if step == "inverse" || step == "write:rolling-back" || step == "remove" {
					t.Fatalf("uncertain observation mutated host: %v", tr.steps)
				}
			}
		})
	}
}

func TestReconcileDurableRollbackIntentResumes(t *testing.T) {
	p, j, id := switchFixture(t)
	j.Phase = dnsengineartifact.SwitchPhaseRollingBack
	tr := &trace{journal: j, exists: true, targetErr: errors.New("target not verified")}
	got, err := Reconcile(context.Background(), p, id, tr.operations())
	if err != nil || got != OutcomeRolledBack {
		t.Fatalf("durable rollback result=%s err=%v steps=%v", got, err, tr.steps)
	}
	for _, step := range tr.steps {
		if step == "prove-absent" {
			t.Fatalf("durable inverse intent was reclassified: %v", tr.steps)
		}
	}
}

func TestRollbackCancelledBeforeCheckpoint(t *testing.T) {
	_, j, _ := switchFixture(t)
	tr := &trace{journal: j}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Rollback(ctx, &j, tr.operations()); err == nil || len(tr.steps) != 0 {
		t.Fatalf("cancelled inverse began: err=%v steps=%v", err, tr.steps)
	}
}

func TestReconcileNeverRecommitsDurableInverse(t *testing.T) {
	p, fixture, id := switchFixture(t)
	for name, tc := range map[string]struct {
		phase string
		steps []string
	}{
		"rolling back": {
			phase: dnsengineartifact.SwitchPhaseRollingBack,
			steps: []string{"read", "inverse", "write:rolled-back"},
		},
		"rolled back": {
			phase: dnsengineartifact.SwitchPhaseRolledBack,
			steps: []string{"read", "inverse"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			j := fixture
			j.Phase = tc.phase
			// A later target observation would pass. It must not reverse an
			// already durable inverse decision.
			tr := &trace{journal: j, exists: true}
			got, err := Reconcile(context.Background(), p, id, tr.operations())
			if err != nil || got != OutcomeRolledBack || !reflect.DeepEqual(tr.steps, tc.steps) {
				t.Fatalf("inverse decision changed: got=%s err=%v steps=%v", got, err, tr.steps)
			}
		})
	}
}

func TestRollbackRefusesVerifiedTarget(t *testing.T) {
	_, j, _ := switchFixture(t)
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted} {
		j.Phase = phase
		tr := &trace{journal: j}
		if err := Rollback(context.Background(), &j, tr.operations()); err == nil || len(tr.steps) != 0 || j.Phase != phase {
			t.Fatalf("verified phase %s entered inverse: err=%v steps=%v", phase, err, tr.steps)
		}
	}
}

func TestRollbackRetainsExactRolledBackCheckpoint(t *testing.T) {
	policy, journal, id := switchFixture(t)
	tr := &trace{journal: journal, exists: true, targetErr: errors.New("target unverified"), absent: true}
	got, err := Reconcile(context.Background(), policy, id, tr.operations())
	if err != nil || got != OutcomeRolledBack || tr.journal.Phase != dnsengineartifact.SwitchPhaseRolledBack {
		t.Fatalf("rollback lost final checkpoint: outcome=%s phase=%s err=%v", got, tr.journal.Phase, err)
	}
}
