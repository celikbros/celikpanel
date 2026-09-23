package transport

import "time"

// Enrollment has a durable reservation, not an expiring ServiceMutationBinding.
// The authenticated caller supplies the exact previously accepted owner tuple.
// No RPC accepts executable paths, shell text, directions or native unit names.
type MailEnrollmentRequest struct {
	RequestID  string `json:"request_id"`
	OwnerID    string `json:"owner_id"`
	Generation string `json:"generation"`
}
type MailEnrollmentStartRequest struct {
	MailEnrollmentRequest
	ExpectedBuildCommit string `json:"expected_build_commit"`
}
type MailEnrollmentStartResponse struct {
	RequestID string `json:"request_id"`
	Handoff   string `json:"handoff"` // accepted or unknown; never completion
	Reason    string `json:"reason,omitempty"`
}
type MailEnrollmentStatusResponse struct {
	MailEnrollmentRequest
	State      string    `json:"state"` // unknown, not_recorded, forward, rollback, published, restored
	ObservedAt time.Time `json:"observed_at"`
	Reason     string    `json:"reason,omitempty"`
}
