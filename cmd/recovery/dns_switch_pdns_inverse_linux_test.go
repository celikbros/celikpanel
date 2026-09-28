//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func adoptionWorkerEvidence() dnsenginerecovery.SwitchEvidence {
	request := strings.Repeat("a", 32)
	owner := strings.Repeat("b", 32)
	qualifier := "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64)
	now := time.Now().UTC()
	return dnsenginerecovery.SwitchEvidence{
		Journal: dnsengineartifact.SwitchJournalV1{
			MutationRequestID: request, MutationOwnerID: owner,
			TargetEngine: transport.DNSEnginePowerDNS, ManifestQualifier: qualifier,
			Phase: dnsengineartifact.SwitchPhaseRollingBack,
		},
		Observation: dnsenginerecovery.EvidenceObservation{
			RequestID: request, Status: dnsenginerecovery.EvidenceActive,
		},
		AcceptedJob: transport.ServiceMutationJob{
			RequestID: request, OwnerID: owner, Kind: "dns_engine_switch",
			Target: string(transport.DNSEnginePowerDNS), PackageName: qualifier,
			Status: servicemutationledger.StatusRunning, Phase: "leased", Attempt: 1,
			StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Second),
			LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(2 * time.Minute),
		},
	}
}

func TestInstalledPDNSInverseWorkerExclusionRejectsForeignAndRecordedWorker(t *testing.T) {
	evidence := adoptionWorkerEvidence()
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err != nil {
		t.Fatalf("accepted worker-free job rejected: %v", err)
	}
	foreign := evidence
	foreign.AcceptedJob.OwnerID = strings.Repeat("d", 32)
	if err := excludeInstalledDNSInverseWorker(context.Background(), foreign); err == nil {
		t.Fatal("foreign accepted job was admitted")
	}
	recorded := evidence
	recorded.AcceptedJob.WorkerPID = 1
	recorded.AcceptedJob.WorkerStarted = "invalid"
	recorded.AcceptedJob.WorkerCommand = "agent"
	if err := excludeInstalledDNSInverseWorker(context.Background(), recorded); err == nil {
		t.Fatal("unknown recorded worker was admitted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := excludeInstalledDNSInverseWorker(cancelled, evidence); err == nil {
		t.Fatal("cancelled exclusion was admitted")
	}
}

func TestInstalledPDNSInverseWorkerExclusionRequiresTerminalCheckpoint(t *testing.T) {
	evidence := adoptionWorkerEvidence()
	evidence.Journal.Phase = dnsengineartifact.SwitchPhaseRolledBack
	evidence.Observation.Status = dnsenginerecovery.EvidenceTerminalRolledBack
	evidence.AcceptedJob.WorkerPID = 0
	evidence.AcceptedJob.WorkerStarted = ""
	evidence.AcceptedJob.WorkerCommand = ""
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err != nil {
		t.Fatalf("exact terminal job rejected: %v", err)
	}
	evidence.AcceptedJob.WorkerPID = 42
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err == nil {
		t.Fatal("terminal job with a recorded worker was admitted")
	}
	evidence.AcceptedJob.WorkerPID = 0
	evidence.Journal.Phase = dnsengineartifact.SwitchPhaseRollingBack
	if err := excludeInstalledDNSInverseWorker(context.Background(), evidence); err == nil {
		t.Fatal("terminal job before rollback checkpoint was admitted")
	}
}

func TestInstalledPDNSInverseRejectsInvalidRequestBeforeHostAccess(t *testing.T) {
	if err := completeInstalledPDNSAdoptionInverse(context.Background(), "invalid"); err == nil {
		t.Fatal("invalid request ID reached host access")
	}
}

func TestInstalledPDNSInverseCancellationStopsBeforeHostAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := completeInstalledPDNSAdoptionInverse(ctx, strings.Repeat("a", 32))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inverse reached installed paths: %v", err)
	}
}

func TestInstalledPDNSInverseDoesNotTreatDeletedZoneAsProvedAbsent(t *testing.T) {
	if err := requirePDNSAdoptionInverseNativeProof(pdnsAdoptionNativeProof{ActiveSOA: 1}); err != nil {
		t.Fatalf("fully counted active-zone proof rejected: %v", err)
	}
	if err := requirePDNSAdoptionInverseNativeProof(pdnsAdoptionNativeProof{ActiveSOA: 1, DeletedSOA: 1}); err == nil {
		t.Fatal("deleted-zone absence was inferred from SQL rather than native answers")
	}
	if err := requirePDNSAdoptionInverseNativeProof(pdnsAdoptionNativeProof{ActiveSOA: 1, DeletedSOA: 1, DeletedSOAVerified: 1}); err != nil {
		t.Fatalf("fully counted native negative answers rejected: %v", err)
	}
}
func TestInstalledPDNSInverseDeletedZoneStopsBeforeDurableEffects(t *testing.T) {
	request := strings.Repeat("a", 32)
	journal := dnsengineartifact.SwitchJournalV1{
		Mode:              transport.DNSEngineSwitchModeAdopt,
		SourceEngine:      "",
		TargetEngine:      transport.DNSEnginePowerDNS,
		Phase:             dnsengineartifact.SwitchPhaseRollingBack,
		MutationRequestID: request,
	}
	evidence := dnsenginerecovery.SwitchEvidence{
		Journal: journal,
		Observation: dnsenginerecovery.EvidenceObservation{
			EvidenceSHA256:  "exact-secured-bytes",
			RequestID:       request,
			Phase:           journal.Phase,
			TargetEngine:    string(journal.TargetEngine),
			InverseKind:     dnsenginerecovery.NativeInversePDNSAdoption,
			SourceOwnership: dnsenginerecovery.SourceOwnershipNotApplicable,
			TargetReceipt:   dnsenginerecovery.TargetReceiptExact,
			SourceReceipt:   dnsenginerecovery.SourceReceiptDifferent,
			Status:          dnsenginerecovery.EvidenceActive,
		},
	}
	effects := 0
	ops := dnsenginerecovery.PDNSAdoptionInverseOps{
		Read: func(context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
			return evidence, true, nil
		},
		ExcludeWorker: func(context.Context, dnsenginerecovery.SwitchEvidence) error { return nil },
		ProveNative: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			return requirePDNSAdoptionInverseNativeProof(pdnsAdoptionNativeProof{ActiveSOA: 1, DeletedSOA: 1})
		},
		RemoveState: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			effects++
			return nil
		},
		WritePhase: func(context.Context, dnsengineartifact.SwitchJournalV1, dnsengineartifact.SwitchJournalV1) error {
			effects++
			return nil
		},
		PublishFailed: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			effects++
			return nil
		},
		RemoveJournal: func(context.Context, dnsengineartifact.SwitchJournalV1) error {
			effects++
			return nil
		},
	}
	err := dnsenginerecovery.CompletePDNSAdoptionInverse(context.Background(), ops)
	if err == nil || !strings.Contains(err.Error(), "exact deleted-zone absence proof") {
		t.Fatalf("unproved deleted-zone authority was admitted: %v", err)
	}
	if effects != 0 {
		t.Fatalf("unproved deleted-zone authority reached %d durable effects", effects)
	}
}

func TestJournalAbsentPDNSInverseRequiresExactHistoricalOwnerVerdict(t *testing.T) {
	request := strings.Repeat("a", 32)
	now := time.Now().UTC().Truncate(time.Second)
	job := transport.ServiceMutationJob{
		RequestID: request, OwnerID: strings.Repeat("b", 32),
		Kind: "dns_engine_switch", Target: string(transport.DNSEnginePowerDNS),
		PackageName: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		Status:      servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Minute), UpdatedAt: now, DeadlineAt: now.Add(time.Hour),
		FinishedAt: now, ErrorCode: "dns_engine_switch_rolled_back_by_owner_recovery",
		ErrorMessage: "The interrupted DNS engine switch was rolled back to the verified previous state.",
	}
	ledger := servicemutationledger.Ledger{
		Version: servicemutationledger.Version,
		Jobs:    map[string]*transport.ServiceMutationJob{request: &job},
	}
	if err := classifyJournalAbsentPDNSInverseLedger(ledger, request); err != nil {
		t.Fatalf("exact terminal owner verdict rejected: %v", err)
	}
	if err := classifyJournalAbsentPDNSInverseLedger(ledger, strings.Repeat("d", 32)); err == nil {
		t.Fatal("foreign request accepted")
	}
	for name, mutate := range map[string]func(*transport.ServiceMutationJob){
		"other target":    func(j *transport.ServiceMutationJob) { j.Target = "bind" },
		"other kind":      func(j *transport.ServiceMutationJob) { j.Kind = "dns_zone_sync" },
		"wrong owner":     func(j *transport.ServiceMutationJob) { j.OwnerID = "invalid" },
		"other phase":     func(j *transport.ServiceMutationJob) { j.Phase = "failed" },
		"other code":      func(j *transport.ServiceMutationJob) { j.ErrorCode = "interrupted" },
		"other message":   func(j *transport.ServiceMutationJob) { j.ErrorMessage = "unknown" },
		"worker retained": func(j *transport.ServiceMutationJob) { j.WorkerPID = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := job
			mutate(&changed)
			other := servicemutationledger.Ledger{
				Version: servicemutationledger.Version,
				Jobs:    map[string]*transport.ServiceMutationJob{request: &changed},
			}
			if err := classifyJournalAbsentPDNSInverseLedger(other, request); err == nil {
				t.Fatal("unknown or foreign terminal job accepted")
			}
		})
	}
}

func TestJournalAbsentPDNSInverseOutcomeSeparatesTerminalHistoryFromUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := servicemutationledger.FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	request := strings.Repeat("a", 32)
	now := time.Now().UTC().Truncate(time.Second)
	job := &transport.ServiceMutationJob{
		RequestID: request, OwnerID: strings.Repeat("b", 32),
		Kind: "dns_engine_switch", Target: string(transport.DNSEnginePowerDNS),
		PackageName: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
		Status:      servicemutationledger.StatusFailed, Phase: "interrupted", Attempt: 1,
		StartedAt: now.Add(-time.Minute), UpdatedAt: now, DeadlineAt: now.Add(time.Hour),
		FinishedAt: now, ErrorCode: "dns_engine_switch_rolled_back_by_owner_recovery",
		ErrorMessage: "The interrupted DNS engine switch was rolled back to the verified previous state.",
	}
	ledger := &servicemutationledger.Ledger{
		Version: servicemutationledger.Version,
		Jobs:    map[string]*transport.ServiceMutationJob{request: job},
	}
	ledgerPath := filepath.Join(root, "service-mutations.json")
	write := func() {
		t.Helper()
		raw, err := servicemutationledger.Encode(ledger)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ledgerPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	err := journalAbsentPDNSInverseOutcome(context.Background(), root, owner, request)
	if !errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		t.Fatalf("exact retired-journal history was not distinguished: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "inspect native PowerDNS") {
		t.Fatalf("historical verdict was falsely reported as current service success: %v", err)
	}
	if err := journalAbsentPDNSInverseOutcome(context.Background(), root, owner, strings.Repeat("d", 32)); errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		t.Fatal("foreign request received the exact terminal classification")
	}
	job.ErrorCode = "generic_failure"
	write()
	if err := journalAbsentPDNSInverseOutcome(context.Background(), root, owner, request); errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		t.Fatal("generic failed job was mistaken for owner recovery")
	}
	job.ErrorCode = "dns_engine_switch_rolled_back_by_owner_recovery"
	write()
	if err := os.WriteFile(filepath.Join(root, "dns-engine-switch-journal.json"), []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := journalAbsentPDNSInverseOutcome(context.Background(), root, owner, request); errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		t.Fatal("present journal was treated as retired")
	}
}
