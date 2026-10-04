//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/processidentity"
	"github.com/alicelik/celikpanel/internal/transport"
)

// WorkerExclusion is one predicate of recovery admission, never permission to
// change DNS. The caller must keep the host lock and prove native/owner state.
type WorkerExclusion string

const (
	WorkerNotRecorded WorkerExclusion = "no-recorded-worker"
	WorkerGone        WorkerExclusion = "recorded-worker-gone"
	WorkerStillAlive  WorkerExclusion = "recorded-worker-alive"
)

// InspectAcceptedWorker checks the exact accepted DNS operation before probing
// the recorded process. A missing or replaced PID is only a point-in-time fact.
func InspectAcceptedWorker(id dnsengineartifact.SwitchIdentity, job *transport.ServiceMutationJob, now time.Time) (WorkerExclusion, error) {
	return inspectAcceptedWorker(id, job, now, processidentity.RecordedWorkerGone)
}

func inspectAcceptedWorker(id dnsengineartifact.SwitchIdentity, job *transport.ServiceMutationJob, now time.Time, gone func(int, string) (bool, error)) (WorkerExclusion, error) {
	if gone == nil || id.Validate() != nil || job == nil ||
		!(id.ActiveJob(job) || id.ActiveJobWithRegisteredWorker(job) ||
			id.ExpiredCancellingJob(job, now) || id.OrphanedWorkerJob(job)) {
		return "", errors.New("DNS switch worker exclusion lacks an exact accepted job")
	}
	if job.WorkerPID == 0 {
		return WorkerNotRecorded, nil
	}
	absent, err := gone(job.WorkerPID, job.WorkerStarted)
	if err != nil {
		return "", fmt.Errorf("recorded DNS switch worker could not be excluded: %w", err)
	}
	if !absent {
		return WorkerStillAlive, nil
	}
	return WorkerGone, nil
}
