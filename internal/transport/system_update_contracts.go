package transport

const AgentCapabilitySystemUpdateV1 = "system_update_v1"

// V1 is exposed only through Agent.CheckSystemUpdate,
// Agent.StartSystemUpdate, and Agent.SystemUpdateStatus.
// There is intentionally no generic download or command RPC.
//
// Security-sensitive integers stay canonical decimal strings so browser
// clients never round them through IEEE-754 numbers.
type SystemUpdateCheckResponse struct {
	Supported           bool   `json:"supported"`
	Available           bool   `json:"available"`
	CurrentVersion      string `json:"current_version"`
	CurrentCommit       string `json:"current_commit"`
	TargetVersion       string `json:"target_version,omitempty"`
	TargetCommit        string `json:"target_commit,omitempty"`
	TargetSequence      string `json:"target_sequence,omitempty"`
	TargetOS            string `json:"target_os,omitempty"`
	TargetArch          string `json:"target_arch,omitempty"`
	TargetArchiveSHA256 string `json:"target_archive_sha256,omitempty"`
	TargetArchiveSize   string `json:"target_archive_size,omitempty"`
	PublishedAt         string `json:"published_at,omitempty"`
	Error               string `json:"error,omitempty"`
}

// The request intentionally has no URL, path, command, argument, or
// environment field. Every value must match a freshly verified manifest.
type SystemUpdateStartRequest struct {
	RequestID              string `json:"request_id"`
	TargetVersion          string `json:"target_version"`
	TargetCommit           string `json:"target_commit"`
	TargetSequence         string `json:"target_sequence"`
	TargetOS               string `json:"target_os"`
	TargetArch             string `json:"target_arch"`
	TargetArchiveSHA256    string `json:"target_archive_sha256"`
	TargetArchiveSize      string `json:"target_archive_size"`
	ExpectedCurrentVersion string `json:"expected_current_version"`
	ExpectedCurrentCommit  string `json:"expected_current_commit"`
}

type SystemUpdateStartResponse struct {
	Accepted bool   `json:"accepted"`
	Status   string `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
}

type SystemUpdateStatusRequest struct {
	RequestID string `json:"request_id"`
}

type SystemUpdateStatusResponse struct {
	Found               bool   `json:"found"`
	RequestID           string `json:"request_id,omitempty"`
	Status              string `json:"status,omitempty"`
	TargetVersion       string `json:"target_version,omitempty"`
	TargetCommit        string `json:"target_commit,omitempty"`
	TargetSequence      string `json:"target_sequence,omitempty"`
	TargetOS            string `json:"target_os,omitempty"`
	TargetArch          string `json:"target_arch,omitempty"`
	TargetArchiveSHA256 string `json:"target_archive_sha256,omitempty"`
	TargetArchiveSize   string `json:"target_archive_size,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
	Error               string `json:"error,omitempty"`
}
