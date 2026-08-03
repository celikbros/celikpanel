package transport

// EnableRepoRequest identifies a repository operation while carrying the
// durable mutation identity used to make privileged changes idempotent.
// Repository URLs and signing keys deliberately do not cross the RPC boundary;
// the agent resolves those values from its compiled catalogue.
type EnableRepoRequest struct {
	ServiceMutationBinding
	RepoID string `json:"repo_id"`
}

type RepoStatusResponse struct {
	Enabled         bool   `json:"enabled"`
	Repairable      bool   `json:"repairable,omitempty"`
	PartialSuccess  bool   `json:"partial_success,omitempty"`
	MutationApplied bool   `json:"mutation_applied,omitempty"`
	Source          string `json:"source,omitempty"`
	Error           string `json:"error,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
}

type RepoPackagesRequest struct {
	RepoID string `json:"repo_id"`
}

type RepoPackagesResponse struct {
	Packages  []string `json:"packages"`
	ErrorCode string   `json:"error_code,omitempty"`
	Error     string   `json:"error,omitempty"`
}
