package dnsengineartifact

import (
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"reflect"
	"strings"
	"testing"
	"time"
)

func switchAuthorityFixture(t *testing.T) (SwitchIdentity, transport.ServiceMutationJob) {
	t.Helper()
	j, err := journalTestPolicy().DecodeSwitchJournal(journalFixture(t, "alpha81-bind"))
	if err != nil {
		t.Fatal(err)
	}
	id := SwitchIdentity{j.MutationRequestID, j.MutationOwnerID, j.TargetEngine, j.ManifestQualifier}
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	job := transport.ServiceMutationJob{RequestID: id.RequestID, OwnerID: id.OwnerID, Kind: "dns_engine_switch", Target: string(id.Target), PackageName: id.Qualifier, Status: servicemutationledger.StatusRunning, Phase: "leased", Attempt: 1, StartedAt: now, UpdatedAt: now, LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour)}
	return id, job
}
func TestSwitchAuthorityRequiresSameAcceptedJob(t *testing.T) {
	id, good := switchAuthorityFixture(t)
	if !id.ActiveJob(&good) {
		t.Fatal("valid active shape refused")
	}
	for name, mutate := range map[string]func(*transport.ServiceMutationJob){
		"request": func(j *transport.ServiceMutationJob) { j.RequestID = strings.Repeat("f", 32) },
		"owner":   func(j *transport.ServiceMutationJob) { j.OwnerID = strings.Repeat("f", 32) },
		"target":  func(j *transport.ServiceMutationJob) { j.Target = "pdns" },
		"scope": func(j *transport.ServiceMutationJob) {
			j.PackageName = "dns-engine-switch/v1:sha256:" + strings.Repeat("f", 64)
		},
		"kind":     func(j *transport.ServiceMutationJob) { j.Kind = "dns_zone_sync" },
		"phase":    func(j *transport.ServiceMutationJob) { j.Phase = "waiting" },
		"terminal": func(j *transport.ServiceMutationJob) { j.FinishedAt = j.StartedAt },
		"error":    func(j *transport.ServiceMutationJob) { j.ErrorCode = "failed" },
		"lease":    func(j *transport.ServiceMutationJob) { j.LeaseExpiresAt = j.StartedAt.Add(-time.Second) },
		"attempt":  func(j *transport.ServiceMutationJob) { j.Attempt = 0 },
		"worker": func(j *transport.ServiceMutationJob) {
			j.WorkerPID = 42
			j.WorkerStarted = "123"
			j.WorkerCommand = "apt-get"
		},
	} {
		t.Run(name, func(t *testing.T) {
			j := good
			mutate(&j)
			if id.ActiveJob(&j) {
				t.Fatal("different evidence accepted")
			}
		})
	}
	invalid := id
	invalid.RequestID = ""
	empty := good
	empty.RequestID = ""
	if invalid.ActiveJob(&empty) {
		t.Fatal("matching invalid IDs accepted")
	}
}
func TestSwitchAuthorityRegisteredWorkerShapeDoesNotAuthorizeRecovery(t *testing.T) {
	id, job := switchAuthorityFixture(t)
	job.WorkerPID = 123
	job.WorkerStarted = "clock-token"
	job.WorkerCommand = "apt-get"
	before := job
	if !id.ActiveJobWithRegisteredWorker(&job) || id.ActiveJob(&job) || !reflect.DeepEqual(before, job) {
		t.Fatal("worker shape altered the record or waived strict worker exclusion")
	}
	for _, command := range []string{"", " apt-get", "/usr/bin/apt-get", strings.Repeat("a", 65)} {
		j := job
		j.WorkerCommand = command
		if id.ActiveJobWithRegisteredWorker(&j) {
			t.Fatal("unsafe worker command accepted")
		}
	}
}
func TestSwitchAuthorityExpiredCancellationIsExactAndTimeBound(t *testing.T) {
	id, job := switchAuthorityFixture(t)
	job.Status = servicemutationledger.StatusCancelling
	job.Phase = servicemutationledger.PhaseCancellingExpiredLease
	job.ErrorCode = servicemutationledger.ErrorLeaseExpired
	job.ErrorMessage = servicemutationledger.MessageLeaseExpired
	job.UpdatedAt = job.LeaseExpiresAt
	if !id.ExpiredCancellingJob(&job, job.LeaseExpiresAt) {
		t.Fatal("exact expired job refused")
	}
	if id.ExpiredCancellingJob(&job, job.LeaseExpiresAt.Add(-time.Nanosecond)) {
		t.Fatal("unexpired lease accepted")
	}
	job.ErrorCode = "other"
	if id.ExpiredCancellingJob(&job, job.LeaseExpiresAt) {
		t.Fatal("different cancellation reason accepted")
	}
}
func TestSwitchAuthorityFinalizedLedgerCannotBorrowAnotherOutcome(t *testing.T) {
	id, job := switchAuthorityFixture(t)
	job.Status = servicemutationledger.StatusSucceeded
	job.Phase, _ = FormatSwitchFinalizedPhase(id.RequestID, id.Qualifier)
	job.FinishedAt = job.UpdatedAt
	job.LeaseExpiresAt = time.Time{}
	ledger := servicemutationledger.Ledger{Version: servicemutationledger.Version, Jobs: map[string]*transport.ServiceMutationJob{id.RequestID: &job}}
	if err := id.ValidateFinalizedLedger(ledger); err != nil {
		t.Fatal(err)
	}
	raw, err := servicemutationledger.Encode(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*servicemutationledger.Ledger){
		"other-owner": func(l *servicemutationledger.Ledger) { l.Jobs[id.RequestID].OwnerID = strings.Repeat("f", 32) },
		"published-not-finalized": func(l *servicemutationledger.Ledger) {
			l.Jobs[id.RequestID].Phase, _ = FormatSwitchPublishedPhase(id.RequestID, id.Qualifier)
		},
		"other-request-phase": func(l *servicemutationledger.Ledger) {
			l.Jobs[id.RequestID].Phase, _ = FormatSwitchFinalizedPhase(strings.Repeat("f", 32), id.Qualifier)
		},
		"false-success": func(l *servicemutationledger.Ledger) { l.Jobs[id.RequestID].ErrorCode = "failed" },
		"later-evidence": func(l *servicemutationledger.Ledger) {
			l.Jobs[id.RequestID].UpdatedAt = job.UpdatedAt.Add(-time.Second)
		},
		"still-active":      func(l *servicemutationledger.Ledger) { l.ActiveRequestID = id.RequestID },
		"other-invalid-job": func(l *servicemutationledger.Ledger) { l.Jobs[strings.Repeat("f", 32)] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			l, err := servicemutationledger.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&l)
			if id.ValidateFinalizedLedger(l) == nil {
				t.Fatal("ambiguous completion accepted")
			}
		})
	}
}

func TestSwitchAuthorityOrphanedWorkerIsExactHistoricalWait(t *testing.T) {
	id, job := switchAuthorityFixture(t)
	job.Status = servicemutationledger.StatusOrphaned
	job.Phase = "waiting_for_orphaned_process"
	job.ErrorCode = "agent_restart_worker_alive"
	job.ErrorMessage = "The previous DNS engine switch worker is still alive."
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 123, "456", "apt-get"
	job.UpdatedAt = job.LeaseExpiresAt.Add(time.Second)
	if !id.OrphanedWorkerJob(&job) {
		t.Fatal("exact historical worker wait refused after lease expiry")
	}
	for name, mutate := range map[string]func(*transport.ServiceMutationJob){
		"other owner":        func(j *transport.ServiceMutationJob) { j.OwnerID = strings.Repeat("f", 32) },
		"other phase":        func(j *transport.ServiceMutationJob) { j.Phase = "host_state_unverified" },
		"other reason":       func(j *transport.ServiceMutationJob) { j.ErrorCode = "other" },
		"other message":      func(j *transport.ServiceMutationJob) { j.ErrorMessage = "other" },
		"missing worker":     func(j *transport.ServiceMutationJob) { j.WorkerPID = 0 },
		"unreadable token":   func(j *transport.ServiceMutationJob) { j.WorkerStarted = "clock-token" },
		"noncanonical token": func(j *transport.ServiceMutationJob) { j.WorkerStarted = "000456" },
		"unsafe command":     func(j *transport.ServiceMutationJob) { j.WorkerCommand = "/bin/apt-get" },
		"terminal":           func(j *transport.ServiceMutationJob) { j.FinishedAt = j.UpdatedAt },
	} {
		t.Run(name, func(t *testing.T) {
			changed := job
			mutate(&changed)
			if id.OrphanedWorkerJob(&changed) {
				t.Fatal("different orphan evidence accepted")
			}
		})
	}
}
