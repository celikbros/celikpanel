//go:build linux

package dnsenginerecovery

import (
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func TestInspectAcceptedWorkerRequiresExactJobAndKernelVerdict(t *testing.T) {
	_, journal, ledger, now := inspectionFixture(t)
	id := dnsengineartifact.SwitchIdentity{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
	}
	job := ledger.Jobs[id.RequestID]
	probeCalls := 0
	probe := func(pid int, started string) (bool, error) {
		probeCalls++
		if pid != 123 || started != "456" {
			t.Fatal("wrong recorded worker probed")
		}
		return true, nil
	}
	if got, err := inspectAcceptedWorker(id, job, now, probe); err != nil || got != WorkerNotRecorded || probeCalls != 0 {
		t.Fatalf("no worker: %q, %v, calls=%d", got, err, probeCalls)
	}
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 123, "456", "apt-get"
	if got, err := inspectAcceptedWorker(id, job, now, probe); err != nil || got != WorkerGone || probeCalls != 1 {
		t.Fatalf("exited worker: %q, %v, calls=%d", got, err, probeCalls)
	}
	if got, err := inspectAcceptedWorker(id, job, now, func(int, string) (bool, error) { return false, nil }); err != nil || got != WorkerStillAlive {
		t.Fatalf("live worker: %q, %v", got, err)
	}
	if got, err := inspectAcceptedWorker(id, job, now, func(int, string) (bool, error) { return false, errors.New("procfs unavailable") }); err == nil || got != "" {
		t.Fatalf("unknown process state admitted: %q, %v", got, err)
	}
	job.OwnerID = "ffffffffffffffffffffffffffffffff"
	if got, err := inspectAcceptedWorker(id, job, now, probe); err == nil || got != "" || probeCalls != 1 {
		t.Fatalf("foreign job probed: %q, %v, calls=%d", got, err, probeCalls)
	}
}

func TestInspectAcceptedWorkerRecognizesExactOrphanOnly(t *testing.T) {
	_, journal, ledger, now := inspectionFixture(t)
	id := dnsengineartifact.SwitchIdentity{
		RequestID: journal.MutationRequestID, OwnerID: journal.MutationOwnerID,
		Target: journal.TargetEngine, Qualifier: journal.ManifestQualifier,
	}
	job := ledger.Jobs[id.RequestID]
	job.Status = servicemutationledger.StatusOrphaned
	job.Phase = "waiting_for_orphaned_process"
	job.ErrorCode = "agent_restart_worker_alive"
	job.ErrorMessage = "The previous DNS engine switch worker is still alive."
	job.WorkerPID, job.WorkerStarted, job.WorkerCommand = 123, "456", "apt-get"
	job.UpdatedAt = job.LeaseExpiresAt
	if got, err := inspectAcceptedWorker(id, job, now, func(int, string) (bool, error) { return true, nil }); err != nil || got != WorkerGone {
		t.Fatalf("exact orphan: %q, %v", got, err)
	}
	job.ErrorCode = "different"
	if got, err := inspectAcceptedWorker(id, job, now, func(int, string) (bool, error) { t.Fatal("foreign orphan probed"); return true, nil }); err == nil || got != "" {
		t.Fatalf("foreign orphan: %q, %v", got, err)
	}
}
