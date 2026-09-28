//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestFreshPrimaryForwardV3InterruptedCheckpointsResumeSameRequest(t *testing.T) {
	for _, cut := range []string{
		dnsengineartifact.FreshPrimaryCheckpointStartedV3,
		dnsengineartifact.FreshPrimaryCheckpointNativeV3,
		dnsengineartifact.FreshPrimaryPublishStateV3,
		dnsengineartifact.FreshPrimaryCheckpointVerifiedV3,
		dnsengineartifact.FreshPrimaryCheckpointCommittedV3,
	} {
		t.Run(cut, func(t *testing.T) {
			stored := dnsEngineSwitchJournal{Phase: dnsengineartifact.SwitchPhaseTargetEnableIntent, PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{}}
			var state *dnsEngineStateReceipt
			desired := dnsEngineStateReceipt{EngineEpoch: 1}
			cutUsed := false
			writes := 0
			ops := freshPrimaryForwardOpsV3{
				read: func() (dnsEngineSwitchJournal, bool, error) { return stored, true, nil },
				observe: func(context.Context, dnsEngineSwitchJournal) (freshPrimaryForwardObservationV3, error) {
					return freshPrimaryForwardObservationV3{live: pdnsnative.Snapshot{Tables: map[string][][]any{"domains": {}}}, state: state, processStart: "same-process"}, nil
				},
				plan: func(j dnsEngineSwitchJournal, _ freshPrimaryForwardObservationV3) (dnsengineartifact.FreshPrimaryForwardPlanV3, error) {
					next := j
					switch {
					case j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent:
						next.Phase = dnsengineartifact.SwitchPhaseTargetStarted
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryCheckpointStartedV3, NextJournal: next}, nil
					case j.Phase == dnsengineartifact.SwitchPhaseTargetStarted && j.PDNSFreshPlan.Native == nil:
						plan := *j.PDNSFreshPlan
						plan.Native = &pdnsnative.RecordedTransition{}
						next.PDNSFreshPlan = &plan
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryCheckpointNativeV3, NextJournal: next}, nil
					case j.Phase == dnsengineartifact.SwitchPhaseTargetStarted && state == nil:
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryPublishStateV3, DesiredState: &desired}, nil
					case j.Phase == dnsengineartifact.SwitchPhaseTargetStarted:
						next.Phase = dnsengineartifact.SwitchPhaseTargetVerified
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryCheckpointVerifiedV3, NextJournal: next}, nil
					case j.Phase == dnsengineartifact.SwitchPhaseTargetVerified:
						next.Phase = dnsengineartifact.SwitchPhaseCommitted
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryCheckpointCommittedV3, NextJournal: next}, nil
					case j.Phase == dnsengineartifact.SwitchPhaseCommitted:
						return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryRetainCommittedV3, NextJournal: j}, nil
					}
					return dnsengineartifact.FreshPrimaryForwardPlanV3{}, errors.New("unexpected phase")
				},
				checkpoint: func(before, after dnsEngineSwitchJournal) error {
					if !reflect.DeepEqual(stored, before) {
						t.Fatal("checkpoint ignored exact journal preimage")
					}
					stored = after
					writes++
					if !cutUsed && (cut == dnsengineartifact.FreshPrimaryCheckpointStartedV3 && after.Phase == dnsengineartifact.SwitchPhaseTargetStarted && after.PDNSFreshPlan.Native == nil ||
						cut == dnsengineartifact.FreshPrimaryCheckpointNativeV3 && after.PDNSFreshPlan.Native != nil ||
						cut == dnsengineartifact.FreshPrimaryCheckpointVerifiedV3 && after.Phase == dnsengineartifact.SwitchPhaseTargetVerified ||
						cut == dnsengineartifact.FreshPrimaryCheckpointCommittedV3 && after.Phase == dnsengineartifact.SwitchPhaseCommitted) {
						cutUsed = true
						return errors.New("injected postpublication interruption")
					}
					return nil
				},
				publish: func(_ dnsEngineSwitchJournal, published dnsEngineStateReceipt) error {
					state = &published
					writes++
					if !cutUsed && cut == dnsengineartifact.FreshPrimaryPublishStateV3 {
						cutUsed = true
						return errors.New("injected state readback interruption")
					}
					return nil
				},
				verify: func(context.Context, dnsEngineSwitchJournal) error {
					if state == nil || stored.Phase != dnsengineartifact.SwitchPhaseCommitted {
						return errors.New("unverified terminal request")
					}
					return nil
				},
			}
			if outcome, err := runFreshPrimaryForwardV3(context.Background(), stored, ops); err == nil || outcome != dnsenginerecovery.OutcomeAbsent || !cutUsed {
				t.Fatalf("cut: outcome=%s err=%v cut=%v", outcome, err, cutUsed)
			}
			atCut := writes
			if outcome, err := runFreshPrimaryForwardV3(context.Background(), stored, ops); err != nil || outcome != dnsenginerecovery.OutcomeCommitted {
				t.Fatalf("resume: outcome=%s err=%v", outcome, err)
			}
			if stored.Phase != dnsengineartifact.SwitchPhaseCommitted || state == nil || writes > atCut+4 {
				t.Fatal("resume did not preserve monotonic checkpoints")
			}
		})
	}
}

func TestFreshPrimaryForwardV3RefusesDriftAroundEffect(t *testing.T) {
	for _, drift := range []string{"journal", "process", "sql", "owner-state"} {
		t.Run(drift, func(t *testing.T) {
			stored := dnsEngineSwitchJournal{Phase: dnsengineartifact.SwitchPhaseTargetEnableIntent}
			observation := 0
			writes := 0
			ops := freshPrimaryForwardOpsV3{
				read: func() (dnsEngineSwitchJournal, bool, error) {
					if drift == "journal" && observation > 0 {
						return dnsEngineSwitchJournal{Phase: "owner-edited"}, true, nil
					}
					return stored, true, nil
				},
				observe: func(context.Context, dnsEngineSwitchJournal) (freshPrimaryForwardObservationV3, error) {
					observation++
					if drift == "owner-state" && observation == 2 {
						return freshPrimaryForwardObservationV3{}, errors.New("owner state changed")
					}
					seen := freshPrimaryForwardObservationV3{processStart: "original", live: pdnsnative.Snapshot{Tables: map[string][][]any{"domains": {}}}}
					if observation == 2 && drift == "process" {
						seen.processStart = "restarted"
					}
					if observation == 2 && drift == "sql" {
						seen.live.Tables["domains"] = [][]any{{"changed"}}
					}
					return seen, nil
				},
				plan: func(j dnsEngineSwitchJournal, _ freshPrimaryForwardObservationV3) (dnsengineartifact.FreshPrimaryForwardPlanV3, error) {
					j.Phase = dnsengineartifact.SwitchPhaseTargetStarted
					return dnsengineartifact.FreshPrimaryForwardPlanV3{Action: dnsengineartifact.FreshPrimaryCheckpointStartedV3, NextJournal: j}, nil
				},
				checkpoint: func(_, after dnsEngineSwitchJournal) error { writes++; stored = after; return nil },
				publish: func(dnsEngineSwitchJournal, dnsEngineStateReceipt) error {
					t.Fatal("unexpected state write")
					return nil
				},
				verify: func(context.Context, dnsEngineSwitchJournal) error { t.Fatal("unexpected commit"); return nil },
			}
			outcome, err := runFreshPrimaryForwardV3(context.Background(), stored, ops)
			if err == nil || outcome != dnsenginerecovery.OutcomeAbsent {
				t.Fatalf("drift accepted: %s %v", outcome, err)
			}
			if drift == "journal" && writes != 0 {
				t.Fatal("changed journal permitted a checkpoint")
			}
			if drift != "journal" && writes != 1 {
				t.Fatal("drift occurred before checkpoint fixture")
			}
		})
	}
}
func TestFreshPrimaryNativeObservationViewV3PreservesDurableEnableIntent(t *testing.T) {
	original := dnsEngineSwitchJournal{
		Phase:         dnsengineartifact.SwitchPhaseTargetEnableIntent,
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{},
	}
	view := freshPrimaryNativeObservationViewV3(original)
	if view.Phase != dnsengineartifact.SwitchPhaseTargetStarted ||
		original.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent {
		t.Fatal("possibly started target was not observed through an isolated started view")
	}
	view.Phase = dnsengineartifact.SwitchPhaseCommitted
	if original.Phase != dnsengineartifact.SwitchPhaseTargetEnableIntent {
		t.Fatal("observation changed the durable journal phase")
	}
}
func TestFreshPrimaryProducerStateCASRefusesOwnerCreatedReceipt(t *testing.T) {
	_, root := newMutationTestManager(t)
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", filepath.Join(root, "state"))
	state := dnsEngineStateReceipt{
		Schema:      dnsengineartifact.StateSchemaV1,
		Mode:        transport.DNSEngineSwitchModeSwitch,
		Engine:      transport.DNSEnginePowerDNS,
		EngineEpoch: 1,
		PairRole:    transport.DNSPairRolePrimary,
		PairLocalIP: "192.0.2.10", PairPeerIP: "192.0.2.11",
		PrimaryCatalogSerial: 1790542951,
		ManifestQualifier:    canonicalSwitchRequest(t).ManifestQualifier,
		MutationRequestID:    testMutationRequestID,
		MutationOwnerID:      testMutationOwnerID,
		NativeCatalogV3:      dnsengineartifact.NativeCatalogDebian49V3,
	}
	j := dnsEngineSwitchJournal{
		Schema: dnsengineartifact.SwitchJournalSchemaV3,
		Mode:   state.Mode, TargetEngine: state.Engine, TargetEpoch: state.EngineEpoch,
		PairRole: state.PairRole, LocalIP: state.PairLocalIP, PeerIP: state.PairPeerIP,
		ManifestQualifier: state.ManifestQualifier, MutationRequestID: state.MutationRequestID,
		MutationOwnerID: state.MutationOwnerID,
		StateBefore:     dnsengineartifact.FileSnapshot{Path: dnsEngineStatePath()},
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{
			Native: &pdnsnative.RecordedTransition{Observed: pdnsnative.CatalogTransition{NativeSerial: state.PrimaryCatalogSerial}},
		},
	}
	raw, err := dnsengineartifact.CanonicalStateDocumentV3(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsEngineStatePath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishExactFreshPrimaryStateV3(j, state); err == nil {
		t.Fatal("owner-created state was treated as the frozen absent preimage")
	}
	after, err := os.ReadFile(dnsEngineStatePath())
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatalf("owner-created state changed: %v", err)
	}
}

func TestFreshPrimaryPreparedConfigsV3AcceptsOnlyExactUnchangedOverlap(t *testing.T) {
	unchanged := dnsengineartifact.FileSnapshot{Path: "/etc/powerdns/pdns.conf", Exists: true, Mode: 0o640, SHA256: "same"}
	added := dnsengineartifact.FileSnapshot{Path: "/etc/powerdns/pdns.d/celikpanel.conf", Exists: true, Mode: 0o644, SHA256: "new"}
	journal := dnsEngineSwitchJournal{
		ConfigBefore:  []dnsengineartifact.FileSnapshot{unchanged, {Path: added.Path}},
		PDNSFreshPlan: &dnsengineartifact.PDNSFreshPrimaryPlanV3{ConfigAfter: []dnsengineartifact.FileSnapshot{unchanged, added}},
	}
	observed := []dnsenginerecovery.PDNSTargetConfigStateV4{
		dnsenginerecovery.PDNSTargetConfigBeforeV4, dnsenginerecovery.PDNSTargetConfigAfterV4,
	}
	if err := verifyFreshPrimaryPreparedConfigsV3(observed, journal); err != nil {
		t.Fatalf("unchanged file matched both frozen images but was refused: %v", err)
	}
	for _, changed := range []struct {
		name   string
		states []dnsenginerecovery.PDNSTargetConfigStateV4
		edit   func(*dnsEngineSwitchJournal)
	}{
		{name: "changed file still before", states: []dnsenginerecovery.PDNSTargetConfigStateV4{dnsenginerecovery.PDNSTargetConfigBeforeV4, dnsenginerecovery.PDNSTargetConfigBeforeV4}},
		{name: "owner edit", states: []dnsenginerecovery.PDNSTargetConfigStateV4{dnsenginerecovery.PDNSTargetConfigUnknownV4, dnsenginerecovery.PDNSTargetConfigAfterV4}},
		{name: "different frozen image", states: observed, edit: func(j *dnsEngineSwitchJournal) {
			j.PDNSFreshPlan = &dnsengineartifact.PDNSFreshPrimaryPlanV3{ConfigAfter: []dnsengineartifact.FileSnapshot{{Path: unchanged.Path, Exists: true, Mode: 0o600, SHA256: "changed"}, added}}
		}},
		{name: "missing path", states: observed[:1]},
	} {
		t.Run(changed.name, func(t *testing.T) {
			candidate := journal
			if changed.edit != nil {
				changed.edit(&candidate)
			}
			if err := verifyFreshPrimaryPreparedConfigsV3(changed.states, candidate); err == nil {
				t.Fatal("unprepared or unknown PowerDNS config was accepted")
			}
		})
	}
}
