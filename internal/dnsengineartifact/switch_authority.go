package dnsengineartifact

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	SwitchPublishedPhasePrefix = "commit/dns-engine-switch/v1/published/"
	SwitchFinalizedPhasePrefix = "commit/dns-engine-switch/v2/finalized/"
)

// SwitchIdentity ties a journal to its accepted operation. These pure comparisons
// do not authorize mutation, probe worker liveness or acquire a host lock. A live
// worker remains valid evidence during its owning operation but must be excluded
// separately before recovery. A terminal ledger proves historical completion,
// not current DNS service health.
type SwitchIdentity struct {
	RequestID, OwnerID string
	Target             transport.DNSEngine
	Qualifier          string
}

func (id SwitchIdentity) Validate() error {
	if !servicemutationledger.ValidIdentity(id.RequestID) || !servicemutationledger.ValidIdentity(id.OwnerID) || !transport.ValidDNSEngine(id.Target) || !mutationpayload.ValidDNSEngineSwitchQualifier(id.Qualifier) {
		return errors.New("invalid DNS switch operation identity")
	}
	return nil
}
func FormatSwitchPublishedPhase(requestID, qualifier string) (string, error) {
	if !servicemutationledger.ValidIdentity(requestID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return "", errors.New("invalid DNS engine switch terminal receipt identity")
	}
	return SwitchPublishedPhasePrefix + requestID + "/" + qualifier, nil
}

func FormatSwitchFinalizedPhase(requestID, qualifier string) (string, error) {
	if !servicemutationledger.ValidIdentity(requestID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return "", errors.New("invalid finalized DNS engine switch receipt identity")
	}
	return SwitchFinalizedPhasePrefix + requestID + "/" + qualifier, nil
}

func (id SwitchIdentity) ActiveJob(job *transport.ServiceMutationJob) bool {
	if id.Validate() != nil {
		return false
	}

	return job != nil &&
		job.RequestID == id.RequestID &&
		job.OwnerID == id.OwnerID &&
		job.Kind == "dns_engine_switch" &&
		job.Target == string(id.Target) &&
		job.PackageName == id.Qualifier &&
		job.Status == servicemutationledger.StatusRunning &&
		job.Phase == "leased" &&
		job.Attempt > 0 &&
		!job.StartedAt.IsZero() &&
		!job.UpdatedAt.IsZero() &&
		!job.LeaseExpiresAt.IsZero() &&
		!job.DeadlineAt.IsZero() &&
		job.FinishedAt.IsZero() &&
		!job.UpdatedAt.Before(job.StartedAt) &&
		!job.LeaseExpiresAt.Before(job.UpdatedAt) &&
		!job.DeadlineAt.Before(job.LeaseExpiresAt) &&
		job.WorkerPID == 0 &&
		strings.TrimSpace(job.WorkerStarted) == "" &&
		strings.TrimSpace(job.WorkerCommand) == "" &&
		strings.TrimSpace(job.ErrorCode) == "" &&
		strings.TrimSpace(job.ErrorMessage) == ""
}

func (id SwitchIdentity) ActiveJobWithRegisteredWorker(job *transport.ServiceMutationJob) bool {
	if id.Validate() != nil {
		return false
	}

	if job == nil || job.WorkerPID <= 0 {
		return false
	}
	started := strings.TrimSpace(job.WorkerStarted)
	command := strings.TrimSpace(job.WorkerCommand)
	if started == "" || job.WorkerStarted != started ||
		command == "" || job.WorkerCommand != command ||
		len(command) > 64 || filepath.Base(command) != command {
		return false
	}
	workerFree := *job
	workerFree.WorkerPID = 0
	workerFree.WorkerStarted = ""
	workerFree.WorkerCommand = ""
	return id.ActiveJob(&workerFree)
}

func (id SwitchIdentity) ExpiredCancellingJob(job *transport.ServiceMutationJob, now time.Time) bool {
	if id.Validate() != nil {
		return false
	}

	return job != nil &&
		job.RequestID == id.RequestID &&
		job.OwnerID == id.OwnerID &&
		job.Kind == "dns_engine_switch" &&
		job.Target == string(id.Target) &&
		job.PackageName == id.Qualifier &&
		job.Status == servicemutationledger.StatusCancelling &&
		job.Phase == servicemutationledger.PhaseCancellingExpiredLease &&
		job.Attempt > 0 &&
		!job.StartedAt.IsZero() &&
		!job.UpdatedAt.IsZero() &&
		!job.LeaseExpiresAt.IsZero() &&
		!job.DeadlineAt.IsZero() &&
		job.FinishedAt.IsZero() &&
		!job.UpdatedAt.Before(job.StartedAt) &&
		!job.LeaseExpiresAt.Before(job.StartedAt) &&
		!job.UpdatedAt.Before(job.LeaseExpiresAt) &&
		!job.DeadlineAt.Before(job.LeaseExpiresAt) &&
		!now.Before(job.LeaseExpiresAt) &&
		job.WorkerPID == 0 &&
		strings.TrimSpace(job.WorkerStarted) == "" &&
		strings.TrimSpace(job.WorkerCommand) == "" &&
		job.ErrorCode == servicemutationledger.ErrorLeaseExpired &&
		job.ErrorMessage == servicemutationledger.MessageLeaseExpired
}

func (id SwitchIdentity) ValidateFinalizedLedger(ledger servicemutationledger.Ledger) error {
	if err := id.Validate(); err != nil {
		return err
	}

	if err := servicemutationledger.Validate(&ledger); err != nil {
		return fmt.Errorf("validate finalized DNS engine ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" {
		return errors.New("finalized DNS engine ledger has an active request")
	}
	job := ledger.Jobs[id.RequestID]
	wantPhase, err := FormatSwitchFinalizedPhase(
		id.RequestID, id.Qualifier,
	)
	if err != nil {
		return err
	}
	if job == nil || job.RequestID != id.RequestID ||
		job.OwnerID != id.OwnerID ||
		job.Kind != "dns_engine_switch" ||
		job.Target != string(id.Target) ||
		job.PackageName != id.Qualifier ||
		job.Status != servicemutationledger.StatusSucceeded || job.Phase != wantPhase ||
		job.Attempt <= 0 || job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.DeadlineAt.IsZero() || job.FinishedAt.IsZero() ||
		job.UpdatedAt.Before(job.StartedAt) ||
		job.DeadlineAt.Before(job.StartedAt) ||
		job.FinishedAt.Before(job.StartedAt) ||
		!job.UpdatedAt.Equal(job.FinishedAt) ||
		!job.LeaseExpiresAt.IsZero() || job.WorkerPID != 0 ||
		strings.TrimSpace(job.WorkerStarted) != "" ||
		strings.TrimSpace(job.WorkerCommand) != "" ||
		strings.TrimSpace(job.ErrorCode) != "" ||
		strings.TrimSpace(job.ErrorMessage) != "" {
		return errors.New("DNS engine ledger lacks its exact finalized receipt")
	}
	return nil
}

// OrphanedWorkerJob accepts only the durable waiting state written after the
// original Agent observed this exact switch worker still alive. It is evidence
// of the same operation, never proof that the worker has since stopped.
func (id SwitchIdentity) OrphanedWorkerJob(job *transport.ServiceMutationJob) bool {
	if id.Validate() != nil || job == nil ||
		job.RequestID != id.RequestID || job.OwnerID != id.OwnerID ||
		job.Kind != "dns_engine_switch" || job.Target != string(id.Target) ||
		job.PackageName != id.Qualifier ||
		job.Status != servicemutationledger.StatusOrphaned ||
		job.Phase != "waiting_for_orphaned_process" ||
		job.ErrorCode != "agent_restart_worker_alive" ||
		job.ErrorMessage != "The previous DNS engine switch worker is still alive." ||
		job.Attempt <= 0 || job.WorkerPID <= 0 ||
		job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.LeaseExpiresAt.IsZero() || job.DeadlineAt.IsZero() ||
		!job.FinishedAt.IsZero() || job.UpdatedAt.Before(job.StartedAt) ||
		job.LeaseExpiresAt.Before(job.StartedAt) ||
		job.DeadlineAt.Before(job.LeaseExpiresAt) {
		return false
	}
	started := strings.TrimSpace(job.WorkerStarted)
	command := strings.TrimSpace(job.WorkerCommand)
	if started == "" || started != job.WorkerStarted ||
		command == "" || command != job.WorkerCommand ||
		len(command) > 64 || filepath.Base(command) != command {
		return false
	}
	_, err := strconv.ParseUint(started, 10, 64)
	return err == nil
}
