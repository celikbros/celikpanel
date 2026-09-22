package servicemutationledger

import (
	"errors"
	"strings"
	"time"
)

// Enrollment is a durable reservation, not a leased RPC worker. Expiry, a dead
// process or a status request cannot release it. Only its exact executor may
// publish a verified forward or inverse result under the native locks.
const MailEnrollmentKind = "mail_renewal_enrollment"
const MailEnrollmentTarget = "mail-renewal"
const MailEnrollmentPhasePrefix = "commit/mail-renewal-enrollment/v1/"
const MailEnrollmentQualifierPrefix = "mail-renewal-enrollment/v1:sha256:"

const MailEnrollmentForward = "forward"
const MailEnrollmentRollback = "rollback"
const MailEnrollmentPublished = "published"
const MailEnrollmentRestored = "restored"

var ErrMailEnrollment = errors.New("mail renewal enrollment evidence does not match the accepted operation; preserve it and resume or restore that exact operation")

// ScopeSHA256 binds the canonical enrollment scope (including capture, file
// plan and target generation). The caller must separately establish accepted
// owner intent and trusted source; a digest alone grants no authority.
type MailEnrollmentIdentity struct {
	RequestID, OwnerID, ScopeSHA256 string
}

func (id MailEnrollmentIdentity) valid() bool {
	if !ValidIdentity(id.RequestID) || !ValidIdentity(id.OwnerID) || len(id.ScopeSHA256) != 64 {
		return false
	}
	for _, c := range id.ScopeSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (id MailEnrollmentIdentity) phase(state string) string {
	return MailEnrollmentPhasePrefix + state + "/" + id.RequestID + "/" + id.ScopeSHA256
}

func mailEnrollmentState(job *ServiceMutationJob) (MailEnrollmentIdentity, string, error) {
	if job == nil || job.Kind != MailEnrollmentKind || job.Target != MailEnrollmentTarget ||
		!strings.HasPrefix(job.PackageName, MailEnrollmentQualifierPrefix) {
		return MailEnrollmentIdentity{}, "", ErrMailEnrollment
	}
	id := MailEnrollmentIdentity{job.RequestID, job.OwnerID, strings.TrimPrefix(job.PackageName, MailEnrollmentQualifierPrefix)}
	if !id.valid() || job.Attempt != 1 || job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" {
		return id, "", ErrMailEnrollment
	}
	for _, state := range []string{MailEnrollmentForward, MailEnrollmentRollback, MailEnrollmentPublished, MailEnrollmentRestored} {
		if job.Phase != id.phase(state) {
			continue
		}
		valid := false
		switch state {
		case MailEnrollmentForward, MailEnrollmentRollback:
			valid = job.Status == StatusOrphaned
		case MailEnrollmentPublished:
			valid = job.Status == StatusSucceeded && job.ErrorCode == "" && job.ErrorMessage == ""
		case MailEnrollmentRestored:
			valid = job.Status == StatusFailed && job.ErrorCode == "mail_enrollment_restored"
		}
		if valid {
			return id, state, nil
		}
	}
	return id, "", ErrMailEnrollment
}

// MailEnrollmentState checks the whole ledger, exact owner and active pointer.
// It is read-only and never interprets a past lease deadline as permission.
func MailEnrollmentState(ledger *Ledger, id MailEnrollmentIdentity) (string, error) {
	if !id.valid() || Validate(ledger) != nil {
		return "", ErrMailEnrollment
	}
	got, state, err := mailEnrollmentState(ledger.Jobs[id.RequestID])
	if err != nil || got != id {
		return "", ErrMailEnrollment
	}
	return state, nil
}

func copyEnrollmentLedger(ledger *Ledger) Ledger {
	copy := Ledger{Version: ledger.Version, ActiveRequestID: ledger.ActiveRequestID, Jobs: make(map[string]*ServiceMutationJob, len(ledger.Jobs))}
	for key, job := range ledger.Jobs {
		value := *job
		copy.Jobs[key] = &value
	}
	return copy
}

// AdmitMailEnrollment returns candidate bytes for the common durable writer.
// It neither writes nor acquires locks. The caller must hold release then host
// and publication locks, prove an idle host, accepted intent and source, and
// durably publish this reservation BEFORE the first native mutation.
func AdmitMailEnrollment(ledger *Ledger, id MailEnrollmentIdentity, now time.Time) (Ledger, error) {
	if !id.valid() || now.IsZero() || Validate(ledger) != nil || ledger.ActiveRequestID != "" || ledger.Jobs[id.RequestID] != nil {
		return Ledger{}, ErrMailEnrollment
	}
	out := copyEnrollmentLedger(ledger)
	out.ActiveRequestID = id.RequestID
	out.Jobs[id.RequestID] = &ServiceMutationJob{
		RequestID: id.RequestID, OwnerID: id.OwnerID, Kind: MailEnrollmentKind, Target: MailEnrollmentTarget,
		PackageName: MailEnrollmentQualifierPrefix + id.ScopeSHA256, Status: StatusOrphaned,
		Phase: id.phase(MailEnrollmentForward), Attempt: 1,
		StartedAt: now, UpdatedAt: now, LeaseExpiresAt: now, DeadlineAt: now,
	}
	return out, Validate(&out)
}

// AdvanceMailEnrollment only encodes a transition whose native proof the caller
// already established. Unknown/failure never becomes terminal. Inverse intent is
// monotonic; a released terminal reservation cannot be reopened or reused.
func AdvanceMailEnrollment(ledger *Ledger, id MailEnrollmentIdentity, next string, now time.Time) (Ledger, error) {
	state, err := MailEnrollmentState(ledger, id)
	if err != nil || now.IsZero() || now.Before(ledger.Jobs[id.RequestID].UpdatedAt) ||
		!(state == MailEnrollmentForward && (next == MailEnrollmentRollback || next == MailEnrollmentPublished) ||
			state == MailEnrollmentRollback && next == MailEnrollmentRestored) {
		return Ledger{}, ErrMailEnrollment
	}
	out := copyEnrollmentLedger(ledger)
	job := out.Jobs[id.RequestID]
	job.Phase = id.phase(next)
	job.UpdatedAt = now
	if next == MailEnrollmentPublished || next == MailEnrollmentRestored {
		out.ActiveRequestID = ""
		job.LeaseExpiresAt = time.Time{}
		job.FinishedAt = now
		job.Status = StatusSucceeded
		job.ErrorCode, job.ErrorMessage = "", ""
		if next == MailEnrollmentRestored {
			job.Status = StatusFailed
			job.ErrorCode = "mail_enrollment_restored"
			job.ErrorMessage = "Enrollment was not installed; the accepted native before-state was verified restored."
		}
	}
	return out, Validate(&out)
}

// RecordedMailEnrollment selects one exact accepted request, including a terminal
// result. It never searches for a likely owner, chooses the newest job, admits
// work or reinterprets an expired RPC lease as permission to retry.
func RecordedMailEnrollment(ledger *Ledger, requestID string) (MailEnrollmentIdentity, string, error) {
	if !ValidIdentity(requestID) || Validate(ledger) != nil {
		return MailEnrollmentIdentity{}, "", ErrMailEnrollment
	}
	return mailEnrollmentState(ledger.Jobs[requestID])
}
