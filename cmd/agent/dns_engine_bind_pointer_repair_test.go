package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

const testBINDPointerPath = "/var/cache/bind/celikpanel/current"

// fakeBINDPointerHost is the native state a pointer repair observes: the
// current pointer, the verified target tree, BIND's configuration and records.
type fakeBINDPointerHost struct {
	pointer       string
	pointerErr    error
	anchorInclude bool
	recordsErr    error
	treeErr       error
	configErr     error
	restoreErr    error
	restores      int
	logs          []string
	steps         []string
}

func (host *fakeBINDPointerHost) ops(journal dnsEngineSwitchJournal) bindTargetPointerRepairOps {
	return bindTargetPointerRepairOps{
		pointerPath: testBINDPointerPath,
		current: func() (string, bool, error) {
			host.steps = append(host.steps, "pointer")
			return host.pointer, host.pointer != "", host.pointerErr
		},
		anchorIncludesPointer: func() (bool, error) {
			host.steps = append(host.steps, "anchor")
			return host.anchorInclude, nil
		},
		verifyRecords: func() error {
			host.steps = append(host.steps, "records")
			return host.recordsErr
		},
		loadTarget: func() (binddns.Receipt, error) {
			host.steps = append(host.steps, "tree")
			if host.treeErr != nil {
				return binddns.Receipt{}, host.treeErr
			}
			return binddns.Receipt{Generation: journal.TargetGeneration, EngineEpoch: journal.TargetEpoch}, nil
		},
		verifyConfig: func(receipt binddns.Receipt) error {
			host.steps = append(host.steps, "config")
			if receipt.Generation != journal.TargetGeneration {
				return errors.New("config checked against another receipt")
			}
			return host.configErr
		},
		restore: func() error {
			host.steps = append(host.steps, "restore")
			host.restores++
			if host.restoreErr != nil {
				return host.restoreErr
			}
			host.pointer = journal.TargetGeneration
			return nil
		},
		logf: func(format string, arguments ...any) {
			host.logs = append(host.logs, fmt.Sprintf(format, arguments...))
		},
	}
}

func verifiedBINDPointerJournal(t *testing.T, phase string) dnsEngineSwitchJournal {
	t.Helper()
	journal := testBINDSwitchJournal(t)
	journal.Phase = phase
	return journal
}

// Pointer absent, exact tree, records and configuration: the pointer is
// restored once to the journal's generation, the verified target converges
// through Reconcile, and a second recovery changes nothing.
func TestBINDPointerRepairRestoresExactTargetAndConverges(t *testing.T) {
	for _, phase := range []string{dnsSwitchPhaseTargetVerified, dnsSwitchPhaseCommitted} {
		t.Run(phase, func(t *testing.T) {
			journal := verifiedBINDPointerJournal(t, phase)
			host := &fakeBINDPointerHost{anchorInclude: true}
			var writes []string
			ops := dnsenginerecovery.Operations{
				Read: func(context.Context) (dnsengineartifact.SwitchJournalV1, bool, error) {
					return journal, true, nil
				},
				ProveFinalized: func(context.Context, dnsengineartifact.SwitchIdentity) (bool, error) {
					return false, nil
				},
				VerifyTarget: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					host.steps = append(host.steps, "verify")
					if host.pointer != journal.TargetGeneration {
						return errors.New("open current: file does not exist")
					}
					return nil
				},
				ProveTargetAbsent: func(context.Context, dnsengineartifact.SwitchJournalV1) (bool, error) {
					t.Fatal("a verified target must never be proved absent")
					return false, nil
				},
				Write: func(_ context.Context, _, next dnsengineartifact.SwitchJournalV1) error {
					writes = append(writes, next.Phase)
					journal = next
					return nil
				},
				Inverse: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
					t.Fatal("a verified target must never roll back")
					return nil
				},
				RepairVerifiedTarget: func(_ context.Context, observed dnsengineartifact.SwitchJournalV1) (bool, error) {
					return repairMissingBINDTargetPointerWithOps(observed, host.ops(observed))
				},
			}
			id := dnsengineartifact.SwitchIdentity{
				RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
				Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
			}
			outcome, err := dnsenginerecovery.Reconcile(context.Background(), dnsJournalPolicy(), id, ops)
			if err != nil || outcome != dnsenginerecovery.OutcomeCommitted {
				t.Fatalf("outcome = %s, err = %v", outcome, err)
			}
			want := []string{"verify", "pointer", "anchor", "records", "tree", "config", "restore", "verify"}
			if !reflect.DeepEqual(host.steps, want) || host.restores != 1 ||
				!reflect.DeepEqual(writes, []string{dnsSwitchPhaseCommitted}) {
				t.Fatalf("steps = %v restores = %d writes = %v", host.steps, host.restores, writes)
			}
			if len(host.logs) != 1 || !strings.Contains(host.logs[0], journal.MutationRequestID) ||
				!strings.Contains(host.logs[0], journal.TargetGeneration) ||
				strings.Count(host.logs[0], ". ") != 0 {
				t.Fatalf("repair log = %q, want one plain sentence", host.logs)
			}

			// Idempotent: the pointer is now exact, so a second recovery
			// verifies directly and a direct repair call is not a repair.
			host.steps = nil
			outcome, err = dnsenginerecovery.Reconcile(context.Background(), dnsJournalPolicy(), id, ops)
			if err != nil || outcome != dnsenginerecovery.OutcomeCommitted || host.restores != 1 {
				t.Fatalf("second recovery outcome = %s err = %v restores = %d", outcome, err, host.restores)
			}
			repaired, err := repairMissingBINDTargetPointerWithOps(journal, host.ops(journal))
			if repaired || err != nil || host.restores != 1 {
				t.Fatalf("repeat repair = %v, %v, restores = %d", repaired, err, host.restores)
			}
		})
	}
}

func TestBINDPointerRepairRefusesAnythingThatIsNotExact(t *testing.T) {
	cause := errors.New("observed difference")
	other := strings.Repeat("d", 64)
	for _, test := range []struct {
		name        string
		host        fakeBINDPointerHost
		kind        bindTargetPointerRefusalKind
		steps       []string
		bootClaim   bool
		wantInError []string
	}{
		{
			name: "pointer selects another generation",
			host: fakeBINDPointerHost{pointer: other, anchorInclude: true},
			kind: bindTargetPointerSelectsOther, steps: []string{"pointer"},
			wantInError: []string{other, "left it unchanged"},
		},
		{
			name: "pointer unreadable",
			host: fakeBINDPointerHost{pointerErr: cause},
			kind: bindTargetPointerUnreadable, steps: []string{"pointer"},
			wantInError: []string{"could not be verified"},
		},
		{
			name: "records changed",
			host: fakeBINDPointerHost{anchorInclude: true, recordsErr: cause},
			kind: bindTargetPointerRecordsChanged, bootClaim: true,
			steps:       []string{"pointer", "anchor", "records"},
			wantInError: []string{"is missing", "records no longer match"},
		},
		{
			name: "tree missing",
			host: fakeBINDPointerHost{anchorInclude: true, treeErr: errors.New("open generation: file does not exist")},
			kind: bindTargetPointerGenerationUnverified, bootClaim: true,
			steps:       []string{"pointer", "anchor", "records", "tree"},
			wantInError: []string{"is missing or no longer verifies"},
		},
		{
			name: "tree modified",
			host: fakeBINDPointerHost{anchorInclude: true, treeErr: errors.New("BIND generation file is unsafe")},
			kind: bindTargetPointerGenerationUnverified, bootClaim: true,
			steps:       []string{"pointer", "anchor", "records", "tree"},
			wantInError: []string{"is missing or no longer verifies"},
		},
		{
			name:        "configuration changed without the include",
			host:        fakeBINDPointerHost{configErr: cause},
			kind:        bindTargetPointerConfigChanged,
			steps:       []string{"pointer", "anchor", "records", "tree", "config"},
			wantInError: []string{"not the one this operation wrote", "was not established"},
		},
		{
			name: "restore failed",
			host: fakeBINDPointerHost{anchorInclude: true, restoreErr: cause},
			kind: bindTargetPointerRestoreFailed, bootClaim: true,
			steps:       []string{"pointer", "anchor", "records", "tree", "config", "restore"},
			wantInError: []string{"restoring it to generation"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := verifiedBINDPointerJournal(t, dnsSwitchPhaseTargetVerified)
			host := test.host
			pointerBefore := host.pointer
			repaired, err := repairMissingBINDTargetPointerWithOps(journal, host.ops(journal))
			var refusal *bindTargetPointerRefusal
			if repaired || !errors.As(err, &refusal) || refusal.kind != test.kind {
				t.Fatalf("repair = %v, %v", repaired, err)
			}
			if !reflect.DeepEqual(host.steps, test.steps) {
				t.Fatalf("steps = %v, want %v", host.steps, test.steps)
			}
			if host.pointer != pointerBefore || len(host.logs) != 0 {
				t.Fatalf("refusal changed the pointer to %q or logged a repair %v", host.pointer, host.logs)
			}
			text := err.Error()
			for _, want := range append(test.wantInError,
				testBINDPointerPath, journal.MutationRequestID,
				"recovery dns-switch-status --quiesced", "restarting the Agent retries this same operation",
			) {
				if !strings.Contains(text, want) {
					t.Errorf("refusal %q lacks %q", text, want)
				}
			}
			bootText := "cannot start after a reboot"
			if strings.Contains(text, bootText) != test.bootClaim {
				t.Errorf("refusal reboot claim = %v, want %v: %q", !test.bootClaim, test.bootClaim, text)
			}
			if test.bootClaim && !strings.Contains(text, testBINDPointerPath+"/zones.conf") {
				t.Errorf("refusal does not name the missing include: %q", text)
			}
			message := releasedDNSSwitchUnknownMessage(
				fmt.Errorf("verified DNS engine target no longer matches its journal: %w; target check: x", err),
			)
			if message != refusal.ledgerMessage() || len(message) > 512 ||
				strings.Contains(message, cause.Error()) ||
				!strings.Contains(message, "recovery dns-switch-status --quiesced") ||
				strings.Contains(message, "cannot start after a reboot") != test.bootClaim {
				t.Fatalf("ledger message (%d bytes) = %q", len(message), message)
			}
		})
	}
}

func TestBINDPointerRepairAppliesOnlyToVerifiedBINDTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*dnsEngineSwitchJournal)
	}{
		{"before target-verified", func(j *dnsEngineSwitchJournal) { j.Phase = dnsSwitchPhaseTargetStarted }},
		{"rolling back", func(j *dnsEngineSwitchJournal) { j.Phase = dnsSwitchPhaseRollingBack }},
		{"PowerDNS target", func(j *dnsEngineSwitchJournal) { j.TargetEngine = transport.DNSEnginePowerDNS }},
		{"no exact generation", func(j *dnsEngineSwitchJournal) { j.TargetGeneration = "" }},
		{"V3 journal", func(j *dnsEngineSwitchJournal) { j.Schema = dnsengineartifact.SwitchJournalSchemaV3 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := verifiedBINDPointerJournal(t, dnsSwitchPhaseTargetVerified)
			test.mutate(&journal)
			host := &fakeBINDPointerHost{anchorInclude: true}
			repaired, err := repairMissingBINDTargetPointerWithOps(journal, host.ops(journal))
			if repaired || err != nil || len(host.steps) != 0 {
				t.Fatalf("repair = %v, %v, steps = %v", repaired, err, host.steps)
			}
		})
	}
}

func TestReleasedDNSSwitchUnknownMessageKeepsGenericTextForOtherCauses(t *testing.T) {
	message := releasedDNSSwitchUnknownMessage(errors.New("named is not active"))
	if !strings.HasPrefix(message, "The interrupted DNS switch could not be verified after the Agent restarted.") ||
		len(message) > 512 {
		t.Fatalf("generic message = %q", message)
	}
}

// A first generation's pointer is removed only after the unit is restored and
// the configuration no longer includes it; a prior generation is selected
// again before its unit is restored. The owner-aware proof always runs first.
func TestBINDSwitchInverseOrdersThePointerAroundTheUnit(t *testing.T) {
	for _, test := range []struct {
		name        string
		hadPrevious bool
		failAt      string
		want        []string
	}{
		{name: "first install", want: []string{"proof", "activation", "pointer"}},
		{name: "first install activation fails", failAt: "activation", want: []string{"proof", "activation"}},
		{name: "first install proof fails", failAt: "proof", want: []string{"proof"}},
		{name: "prior generation", hadPrevious: true, want: []string{"proof", "pointer", "activation"}},
		{name: "prior generation pointer fails", hadPrevious: true, failAt: "pointer", want: []string{"proof", "pointer"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var order []string
			step := func(name string) func() error {
				return func() error {
					order = append(order, name)
					if name == test.failAt {
						return errors.New(name + " failed")
					}
					return nil
				}
			}
			err := runBINDSwitchInverseInPointerOrder(
				test.hadPrevious, step("proof"), step("pointer"), step("activation"),
			)
			if (err != nil) != (test.failAt != "") || !reflect.DeepEqual(order, test.want) {
				t.Fatalf("order = %v, err = %v, want %v", order, err, test.want)
			}
		})
	}
}
