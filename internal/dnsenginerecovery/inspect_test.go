package dnsenginerecovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func inspectionFixture(t *testing.T) (dnsengineartifact.JournalPolicy, dnsengineartifact.SwitchJournalV1, servicemutationledger.Ledger, time.Time) {
	t.Helper()
	policy := dnsengineartifact.JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json",
		StateUID:  0, StateGID: 0, RequireOwner: true,
		PDNSMainPath:     "/etc/powerdns/pdns.conf",
		PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
		PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "dnsengineartifact", "testdata", "switch-journal", "alpha81-bind.json"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	job := &transport.ServiceMutationJob{
		RequestID:      journal.MutationRequestID,
		OwnerID:        journal.MutationOwnerID,
		Kind:           "dns_engine_switch",
		Target:         string(journal.TargetEngine),
		PackageName:    journal.ManifestQualifier,
		Status:         servicemutationledger.StatusRunning,
		Phase:          "leased",
		Attempt:        1,
		StartedAt:      now,
		UpdatedAt:      now,
		LeaseExpiresAt: now.Add(time.Minute),
		DeadlineAt:     now.Add(time.Hour),
	}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, ActiveRequestID: job.RequestID, Jobs: map[string]*transport.ServiceMutationJob{job.RequestID: job}}
	return policy, journal, ledger, now
}

func TestInspectEvidenceBindsExactAcceptedJob(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	got, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil || got.Status != EvidenceActive || got.RequestID != journal.MutationRequestID || got.Phase != journal.Phase || got.SourceEngine != string(journal.SourceEngine) || got.TargetEngine != string(journal.TargetEngine) || got.TargetGeneration != journal.TargetGeneration || got.TargetEpoch != journal.TargetEpoch || got.InverseKind != NativeInverseBINDSwitch {
		t.Fatalf("valid pair: %+v, %v", got, err)
	}
	for name, mutate := range map[string]func(*servicemutationledger.Ledger){
		"different-owner": func(l *servicemutationledger.Ledger) {
			l.Jobs[journal.MutationRequestID].OwnerID = strings.Repeat("f", 32)
		},
		"different-target":    func(l *servicemutationledger.Ledger) { l.Jobs[journal.MutationRequestID].Target = "pdns" },
		"no-active-authority": func(l *servicemutationledger.Ledger) { l.ActiveRequestID = "" },
		"unrecognized-phase":  func(l *servicemutationledger.Ledger) { l.Jobs[journal.MutationRequestID].Phase = "waiting" },
		"missing-job":         func(l *servicemutationledger.Ledger) { delete(l.Jobs, journal.MutationRequestID) },
	} {
		t.Run(name, func(t *testing.T) {
			_, _, altered, _ := inspectionFixture(t)
			mutate(&altered)
			if observation, err := InspectEvidence(policy, journal, altered, now); err == nil {
				t.Fatalf("unexpectedly accepted %+v", observation)
			}
		})
	}
}

func TestInspectEvidenceExpiredActiveLeaseIsNotReportedActive(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	lease := ledger.Jobs[journal.MutationRequestID].LeaseExpiresAt
	got, err := InspectEvidence(policy, journal, ledger, lease)
	if err != nil || got.Status != EvidenceLeaseExpired {
		t.Fatalf("stale active lease: %+v, %v", got, err)
	}
	if _, err := InspectEvidence(policy, journal, ledger, now.Add(-time.Nanosecond)); err == nil {
		t.Fatal("observer clock preceding operation accepted")
	}
}
func TestInspectEvidenceWorkerAndCancellationRemainObservations(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	job := ledger.Jobs[journal.MutationRequestID]
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 123, "clock-token", "apt-get"
	got, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil || got.Status != EvidenceWorkerRecorded {
		t.Fatalf("recorded worker: %+v, %v", got, err)
	}
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 0, "", ""
	job.Status = servicemutationledger.StatusCancelling
	job.Phase = servicemutationledger.PhaseCancellingExpiredLease
	job.ErrorCode = servicemutationledger.ErrorLeaseExpired
	job.ErrorMessage = servicemutationledger.MessageLeaseExpired
	job.UpdatedAt = job.LeaseExpiresAt
	if _, err := InspectEvidence(policy, journal, ledger, job.LeaseExpiresAt.Add(-time.Nanosecond)); err == nil {
		t.Fatal("unexpired cancellation accepted")
	}
	got, err = InspectEvidence(policy, journal, ledger, job.LeaseExpiresAt)
	if err != nil || got.Status != EvidenceExpiredCancellation {
		t.Fatalf("expired cancellation: %+v, %v", got, err)
	}
}

func TestInspectEvidenceFinalizedAndCorruptJournal(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	job := ledger.Jobs[journal.MutationRequestID]
	ledger.ActiveRequestID = ""
	job.Status = servicemutationledger.StatusSucceeded
	job.Phase, _ = dnsengineartifact.FormatSwitchFinalizedPhase(job.RequestID, job.PackageName)
	job.FinishedAt = job.UpdatedAt
	job.LeaseExpiresAt = time.Time{}
	got, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil || got.Status != EvidenceFinalized {
		t.Fatalf("finalized ledger with retained journal: %+v, %v", got, err)
	}
	journal.StateBefore.Path = "/tmp/other"
	if _, err := InspectEvidence(policy, journal, ledger, now); err == nil {
		t.Fatal("corrupt journal accepted")
	}
}

func TestInspectEvidenceRecognizesExactOrphanedDNSWorker(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	job := ledger.Jobs[journal.MutationRequestID]
	job.Status = servicemutationledger.StatusOrphaned
	job.Phase = "waiting_for_orphaned_process"
	job.ErrorCode = "agent_restart_worker_alive"
	job.ErrorMessage = "The previous DNS engine switch worker is still alive."
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 123, "456", "apt-get"
	job.UpdatedAt = job.LeaseExpiresAt.Add(time.Second)
	got, err := InspectEvidence(policy, journal, ledger, now.Add(2*time.Minute))
	if err != nil || got.Status != EvidenceOrphanedWorker ||
		got.WorkerPID != 123 || got.WorkerStarted != "456" {
		t.Fatalf("exact orphan wait not observed: %+v, %v", got, err)
	}
	job.ErrorCode = "other"
	if got, err := InspectEvidence(policy, journal, ledger, now.Add(2*time.Minute)); err == nil {
		t.Fatalf("foreign orphan wait accepted: %+v", got)
	}
}

func TestInspectEvidenceRecognizesReleasedUndecidedSwitch(t *testing.T) {
	policy, journal, ledger, now := inspectionFixture(t)
	job := ledger.Jobs[journal.MutationRequestID]
	ledger.ActiveRequestID = ""
	job.Status = servicemutationledger.StatusFailed
	job.Phase = "interrupted"
	job.ErrorCode = dnsengineartifact.ReleasedUnsupportedHostCode
	job.ErrorMessage = "Host could not be inspected; switch journal remains."
	job.FinishedAt = job.UpdatedAt
	job.LeaseExpiresAt = time.Time{}
	observed, err := InspectEvidence(policy, journal, ledger, now)
	if err != nil || observed.Status != EvidenceReleasedUndecided || observed.ReleaseReason != dnsengineartifact.ReleasedUnsupportedHostCode {
		t.Fatalf("released switch not observable: %+v, %v", observed, err)
	}
	job.ErrorCode = "other_failure"
	if observed, err := InspectEvidence(policy, journal, ledger, now); err == nil {
		t.Fatalf("unrelated terminal failure accepted: %+v", observed)
	}
}
