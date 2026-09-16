// Package recoveryobs reads bounded, producer-written recovery observations.
// Observations never authorize mutations or assert current process liveness.
package recoveryobs

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const Root = "/var/lib/celikpanel-recovery-observations"
const RecordSchema = "celikpanel-recovery-observation/v1"
const StatusSchema = "celikpanel-recovery-status/v1"
const MaxRecordSize = 2048

var requestPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var ErrUnavailable = errors.New("recovery observation is unavailable")

func ValidRequestID(id string) bool { return requestPattern.MatchString(id) }

type Status struct {
	Schema          string `json:"schema"`
	RequestID       string `json:"request_id"`
	Observation     string `json:"observation"`
	Phase           string `json:"phase,omitempty"`
	TerminalProof   string `json:"terminal_proof"`
	Reason          string `json:"reason"`
	ObservedAt      string `json:"observed_at,omitempty"`
	PreviousFailure string `json:"previous_failure,omitempty"`
	WaitingFor      string `json:"waiting_for,omitempty"`
}

// ValidWaitingFor accepts optional guidance, never a phase or mutation authority.
func ValidWaitingFor(value string) bool {
	return value == "initializing" || value == "starting" || value == "stopping"
}

// Record is internal producer data, not an HTTP response. Its commit binds the
// native transaction to the exact reviewed worker. No raw diagnostics belong here.
type Record struct {
	RequestID, TargetCommit, Phase, TerminalProof, Reason, ObservedAt, PreviousFailure string
}

func unavailable(id string) Status {
	return Status{Schema: StatusSchema, RequestID: id, Observation: "unavailable", TerminalProof: "none", Reason: "observation_unavailable"}
}

func (r Record) Status() Status {
	if r.Validate() != nil {
		return unavailable(r.RequestID)
	}
	previous := r.PreviousFailure
	if previous == "none" {
		previous = ""
	}
	return Status{Schema: StatusSchema, RequestID: r.RequestID, Observation: "known", Phase: r.Phase, TerminalProof: r.TerminalProof, Reason: r.Reason, ObservedAt: r.ObservedAt, PreviousFailure: previous}
}

func (r Record) Validate() error {
	if !ValidRequestID(r.RequestID) || !commitPattern.MatchString(r.TargetCommit) {
		return ErrUnavailable
	}
	wantReason, wantProof := "", "none"
	switch r.Phase {
	case "accepted":
		wantReason = "operation_accepted"
	case "running":
		wantReason = "update_running"
	case "recovering":
		wantReason = "recovery_running"
	case "failed":
		wantReason = "update_failed"
	case "recovery_required":
		if r.Reason != "recovery_failed" && r.Reason != "recovery_incomplete" {
			return ErrUnavailable
		}
		wantReason = r.Reason
	case "succeeded":
		wantReason, wantProof = "update_verified", "update_verified"
	case "recovered":
		wantReason, wantProof = "rollback_verified", "rollback_verified"
	default:
		return ErrUnavailable
	}
	if r.Reason != wantReason || r.TerminalProof != wantProof {
		return ErrUnavailable
	}
	switch r.PreviousFailure {
	case "none", "update_failed", "recovery_failed", "recovery_incomplete":
	default:
		return ErrUnavailable
	}
	t, err := time.Parse(time.RFC3339, r.ObservedAt)
	if err != nil || t.UTC().Format("2006-01-02T15:04:05Z") != r.ObservedAt {
		return ErrUnavailable
	}
	return nil
}

func (r Record) Encode() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []byte("schema=" + RecordSchema + "\nrequest_id=" + r.RequestID + "\ntarget_commit=" + r.TargetCommit + "\nphase=" + r.Phase + "\nterminal_proof=" + r.TerminalProof + "\nreason=" + r.Reason + "\nobserved_at=" + r.ObservedAt + "\nprevious_failure=" + r.PreviousFailure + "\n"), nil
}

func Decode(raw []byte, id string) (Record, error) {
	if len(raw) == 0 || len(raw) > MaxRecordSize || !ValidRequestID(id) {
		return Record{}, ErrUnavailable
	}
	lines := strings.Split(string(raw), "\n")
	keys := []string{"schema", "request_id", "target_commit", "phase", "terminal_proof", "reason", "observed_at", "previous_failure"}
	if len(lines) != len(keys)+1 || lines[len(keys)] != "" {
		return Record{}, ErrUnavailable
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		prefix := key + "="
		if !strings.HasPrefix(lines[i], prefix) {
			return Record{}, ErrUnavailable
		}
		values[i] = strings.TrimPrefix(lines[i], prefix)
	}
	if values[0] != RecordSchema || values[1] != id {
		return Record{}, ErrUnavailable
	}
	r := Record{RequestID: values[1], TargetCommit: values[2], Phase: values[3], TerminalProof: values[4], Reason: values[5], ObservedAt: values[6], PreviousFailure: values[7]}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	return r, nil
}

func merge(old *Record, next Record) (Record, error) {
	if next.PreviousFailure == "" {
		next.PreviousFailure = "none"
	}
	if err := next.Validate(); err != nil {
		return Record{}, err
	}
	if old != nil {
		if old.Validate() != nil || old.RequestID != next.RequestID || old.TargetCommit != next.TargetCommit {
			return Record{}, ErrUnavailable
		}
		// A delayed worker error must not erase a newer native terminal proof.
		if old.TerminalProof != "none" {
			return *old, nil
		}
		if next.ObservedAt < old.ObservedAt {
			return Record{}, ErrUnavailable
		}
		if next.PreviousFailure == "none" {
			next.PreviousFailure = old.PreviousFailure
		}
	}
	if next.Phase == "failed" || next.Phase == "recovery_required" {
		next.PreviousFailure = next.Reason
	}
	return next, nil
}
