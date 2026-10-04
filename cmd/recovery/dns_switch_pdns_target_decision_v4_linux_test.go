//go:build linux

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsDecisionFixtureV4 struct {
	e                    dnsenginerecovery.SwitchEvidence
	workerErr, nativeErr error
	writeErr             error
	writeLands           bool
	workerChecks         int
	nativeChecks         int
	nativeState          dnsenginerecovery.PDNSTargetStageState
	writeCalls           int
	continued            int
	sequence             []string
}

func newPDNSDecisionFixtureV4() *pdnsDecisionFixtureV4 {
	j := dnsengineartifact.SwitchJournalV1{
		Schema: dnsengineartifact.SwitchJournalSchemaV4,
		Mode:   transport.DNSEngineSwitchModeSwitch, SourceEngine: transport.DNSEngineBIND,
		TargetEngine: transport.DNSEnginePowerDNS, Topology: transport.DNSTopologyStandalone,
		Phase: dnsengineartifact.SwitchPhaseTargetStaged, MutationRequestID: strings.Repeat("a", 32),
		StateBefore:       dnsengineartifact.FileSnapshot{Exists: true},
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "bind9.service", ActiveState: "active"}, {Name: "named.service", ActiveState: "active"}},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}},
		PDNSTargetPlan:    &dnsengineartifact.PDNSTargetInversePlanV4{Kind: dnsengineartifact.PDNSTargetInversePlanKindV4, Candidate: &dnsengineartifact.PDNSTargetCandidateProofV4{}},
	}
	o := dnsenginerecovery.EvidenceObservation{
		EvidenceSHA256: strings.Repeat("b", 64), Status: dnsenginerecovery.EvidenceActive,
		RequestID: j.MutationRequestID, Phase: j.Phase,
		SourceEngine: string(j.SourceEngine), TargetEngine: string(j.TargetEngine),
		TargetGeneration: j.TargetGeneration, TargetEpoch: j.TargetEpoch,
		InverseKind:   dnsenginerecovery.NativeInversePDNSSwitch,
		SourceReceipt: dnsenginerecovery.SourceReceiptExact, SourceOwnership: dnsenginerecovery.SourceOwnershipExact,
	}
	return &pdnsDecisionFixtureV4{e: dnsenginerecovery.SwitchEvidence{Journal: j, Observation: o}, writeLands: true, nativeState: dnsenginerecovery.PDNSTargetStageNeedsRestore}
}

func (f *pdnsDecisionFixtureV4) ops() pdnsTargetRollbackDecisionOpsV4 {
	return pdnsTargetRollbackDecisionOpsV4{
		Read: func(context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) { return f.e, true, nil },
		ExcludeWorker: func(context.Context, dnsenginerecovery.SwitchEvidence) error {
			f.workerChecks++
			f.sequence = append(f.sequence, "worker")
			return f.workerErr
		},
		VerifyTarget: func(dnsengineartifact.SwitchJournalV1) error {
			f.sequence = append(f.sequence, "candidate")
			return nil
		},
		AssessNative: func(_ context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
			f.nativeChecks++
			f.sequence = append(f.sequence, "native")
			expected := dnsengineartifact.SwitchPhaseRollingBack
			if f.e.Journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent || f.e.Journal.Phase == dnsengineartifact.SwitchPhaseRollingBackTargetEnable {
				expected = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
			}
			if j.Phase != expected {
				return dnsenginerecovery.PDNSTargetStageUnknown, errors.New("assessor did not receive synthetic rollback phase")
			}
			if f.nativeErr != nil {
				return dnsenginerecovery.PDNSTargetStageUnknown, f.nativeErr
			}
			return f.nativeState, nil
		},
		WritePhase: func(_ context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			f.writeCalls++
			f.sequence = append(f.sequence, "checkpoint")
			expected := dnsengineartifact.SwitchPhaseRollingBack
			if before.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent {
				expected = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
			}
			if !reflect.DeepEqual(before, f.e.Journal) || after.Phase != expected {
				return errors.New("checkpoint changed frozen journal")
			}
			if f.writeLands {
				f.e.Journal = after
				f.e.Observation.Phase = after.Phase
				f.e.Observation.EvidenceSHA256 = strings.Repeat("c", 64)
			}
			return f.writeErr
		},
		Continue: func(context.Context) error { f.continued++; f.sequence = append(f.sequence, "inverse"); return nil },
	}
}

func TestPDNSTargetDecisionV4AllowsOnlyPreActivationPhases(t *testing.T) {
	for _, phase := range []string{dnsengineartifact.SwitchPhaseIntent, dnsengineartifact.SwitchPhaseTargetStaged, dnsengineartifact.SwitchPhaseSourceStopped} {
		f := newPDNSDecisionFixtureV4()
		f.e.Journal.Phase, f.e.Observation.Phase = phase, phase
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil || f.writeCalls != 1 || f.continued != 1 {
			t.Fatalf("phase %s: err=%v writes=%d inverse=%d", phase, err, f.writeCalls, f.continued)
		}
	}
	for _, phase := range []string{dnsengineartifact.SwitchPhaseTargetStarted, dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted} {
		f := newPDNSDecisionFixtureV4()
		f.e.Journal.Phase, f.e.Observation.Phase = phase, phase
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err == nil || f.writeCalls != 0 || f.continued != 0 {
			t.Fatalf("unsafe phase %s was admitted: err=%v writes=%d inverse=%d", phase, err, f.writeCalls, f.continued)
		}
	}
}

func TestPDNSTargetDecisionV4FailsClosedOnWorkerNativeOrCheckpointDrift(t *testing.T) {
	for _, mutate := range []func(*pdnsDecisionFixtureV4){
		func(f *pdnsDecisionFixtureV4) { f.workerErr = errors.New("worker still alive") },
		func(f *pdnsDecisionFixtureV4) { f.nativeErr = errors.New("owner changed BIND config") },
		func(f *pdnsDecisionFixtureV4) {
			f.writeLands = false
			f.writeErr = errors.New("interrupted before write")
		},
	} {
		f := newPDNSDecisionFixtureV4()
		mutate(f)
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err == nil || f.continued != 0 {
			t.Fatalf("unknown proof reached native inverse: err=%v calls=%v", err, f.sequence)
		}
	}
}

func TestPDNSTargetDecisionV4ReplaysLandedCheckpointAfterWriteError(t *testing.T) {
	f := newPDNSDecisionFixtureV4()
	f.writeErr = errors.New("interrupted after durable write")
	if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil || f.continued != 1 || f.nativeChecks < 3 || f.workerChecks < 3 {
		t.Fatalf("durable checkpoint not re-proved: err=%v calls=%v", err, f.sequence)
	}
	if f.sequence[len(f.sequence)-1] != "inverse" {
		t.Fatalf("inverse preceded durable proof: %v", f.sequence)
	}
	if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil || f.writeCalls != 1 || f.continued != 2 {
		t.Fatalf("replay wrote a second decision: err=%v writes=%d inverse=%d", err, f.writeCalls, f.continued)
	}
}

func TestPDNSTargetDecisionV4RejectsLedgerOrOwnerDriftBeforeCheckpoint(t *testing.T) {
	for _, change := range []func(*dnsenginerecovery.SwitchEvidence){
		func(e *dnsenginerecovery.SwitchEvidence) { e.AcceptedJob.OwnerID = strings.Repeat("d", 32) },
		func(e *dnsenginerecovery.SwitchEvidence) {
			e.Observation.SourceOwnership = dnsenginerecovery.SourceOwnershipDifferent
		},
	} {
		f := newPDNSDecisionFixtureV4()
		ops := f.ops()
		reads := 0
		ops.Read = func(context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
			reads++
			e := f.e
			if reads == 2 {
				change(&e)
			}
			return e, true, nil
		}
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), ops); err == nil || f.writeCalls != 0 || f.continued != 0 {
			t.Fatalf("owner or ledger drift reached checkpoint: err=%v calls=%v", err, f.sequence)
		}
	}
}

func TestPDNSTargetDecisionV4AcceptsRenamedOnlyAfterSourceStopped(t *testing.T) {
	f := newPDNSDecisionFixtureV4()
	f.nativeState = dnsenginerecovery.PDNSTargetStageRenamed
	f.e.Journal.Phase, f.e.Observation.Phase = dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseSourceStopped
	if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil || f.writeCalls != 1 || f.continued != 1 {
		t.Fatalf("exact pre-start rename did not reach durable inverse: %v, writes=%d inverse=%d", err, f.writeCalls, f.continued)
	}
	f = newPDNSDecisionFixtureV4()
	f.nativeState = dnsenginerecovery.PDNSTargetStageRenamed
	if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err == nil || f.writeCalls != 0 || f.continued != 0 {
		t.Fatalf("rename before source-stopped was admitted: %v", err)
	}
}

func TestPDNSTargetDecisionV4RejectsCandidateShapeDrift(t *testing.T) {
	for _, changeAt := range []int{2, 3} {
		f := newPDNSDecisionFixtureV4()
		f.e.Journal.Phase, f.e.Observation.Phase = dnsengineartifact.SwitchPhaseSourceStopped, dnsengineartifact.SwitchPhaseSourceStopped
		ops := f.ops()
		original := ops.AssessNative
		checks := 0
		ops.AssessNative = func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
			checks++
			if checks >= changeAt {
				return dnsenginerecovery.PDNSTargetStageRenamed, nil
			}
			return original(ctx, j)
		}
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), ops); err == nil || f.continued != 0 {
			t.Fatalf("shape drift at check %d reached inverse: %v, calls=%v", changeAt, err, f.sequence)
		}
		if changeAt == 2 && f.writeCalls != 0 {
			t.Fatal("shape drift before durable decision wrote a checkpoint")
		}
	}
}

func TestPDNSTargetDecisionV4EnableIntentRequiresRenamedAndDurableCheckpoint(t *testing.T) {
	for _, state := range []dnsenginerecovery.PDNSTargetStageState{
		dnsenginerecovery.PDNSTargetStageRenamed,
		dnsenginerecovery.PDNSTargetStageRenamedEnabled,
	} {
		f := newPDNSDecisionFixtureV4()
		f.e.Journal.Phase, f.e.Observation.Phase = dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseTargetEnableIntent
		f.nativeState = state
		f.writeErr = errors.New("interrupted after durable write")
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil ||
			f.e.Journal.Phase != dnsengineartifact.SwitchPhaseRollingBackTargetEnable ||
			f.writeCalls != 1 || f.continued != 1 {
			t.Fatalf("enable-intent cut did not reach exact checkpoint: %v, %+v", err, f)
		}
		if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err != nil || f.writeCalls != 1 || f.continued != 2 {
			t.Fatalf("landed enable-intent checkpoint did not replay: %v, %+v", err, f)
		}
	}
	f := newPDNSDecisionFixtureV4()
	f.e.Journal.Phase, f.e.Observation.Phase = dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseTargetEnableIntent
	if err := decideAndCompletePDNSTargetRollbackV4(context.Background(), f.ops()); err == nil || f.writeCalls != 0 {
		t.Fatalf("unrenamed enable intent admitted: %v", err)
	}
}
