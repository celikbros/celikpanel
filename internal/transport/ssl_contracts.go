package transport

import "time"

// IssueLetsEncryptRequest is the exact panel-agent wire contract for ACME
// certificate issuance. Sensitive EAB material is request-scoped and is never
// persisted by this transport package.
type IssueLetsEncryptRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	Domain              string   `json:"domain"`
	Aliases             []string `json:"aliases"`
	Email               string   `json:"email"`
	SubscriptionID      int      `json:"subscription_id"`
	DomainID            int      `json:"domain_id"`
	AutoRenew           bool     `json:"auto_renew"`
	ACMEServer          string   `json:"acme_server,omitempty"`
	EABKeyID            string   `json:"eab_key_id,omitempty"`
	EABHMACKey          string   `json:"eab_hmac_key,omitempty"`
	ForceRenewal        bool     `json:"force_renewal,omitempty"`
	StageLineage        bool     `json:"stage_lineage,omitempty"`
	FreshLineage        bool     `json:"fresh_lineage,omitempty"`
	CurrentCertPath     string   `json:"current_cert_path,omitempty"`
	CurrentLineageName  string   `json:"current_lineage_name,omitempty"`
}

type IssueLetsEncryptResponse struct {
	Success     bool      `json:"success"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	ChainPath   string    `json:"chain_path"`
	ExpiresAt   time.Time `json:"expires_at"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	LineageName string    `json:"lineage_name"`
	Error       string    `json:"error,omitempty"`
}

type RenewCertRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Domain              string `json:"domain"`
	CurrentCertPath     string `json:"current_cert_path,omitempty"`
	LineageName         string `json:"lineage_name,omitempty"`
	SubscriptionID      int    `json:"subscription_id"`
	DomainID            int    `json:"domain_id"`
}

type RenewCertResponse struct {
	Success     bool      `json:"success"`
	CertPath    string    `json:"cert_path"`
	KeyPath     string    `json:"key_path"`
	ChainPath   string    `json:"chain_path"`
	ExpiresAt   time.Time `json:"expires_at"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	LineageName string    `json:"lineage_name"`
	Error       string    `json:"error,omitempty"`
}

type ValidateCertRequest struct {
	CertContent  string `json:"cert_content"`
	KeyContent   string `json:"key_content"`
	ChainContent string `json:"chain_content,omitempty"`
	Domain       string `json:"domain"`
}

type ValidateCertResponse struct {
	Valid        bool      `json:"valid"`
	Trusted      bool      `json:"trusted"`
	TrustChecked bool      `json:"trust_checked"`
	TrustError   string    `json:"trust_error,omitempty"`
	Issuer       string    `json:"issuer"`
	Subject      string    `json:"subject"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	DNSNames     []string  `json:"dns_names,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type InstallCertRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Domain              string `json:"domain"`
	CertContent         string `json:"cert_content"`
	KeyContent          string `json:"key_content"`
	ChainContent        string `json:"chain_content,omitempty"`
}

type InstallCertResponse struct {
	Success   bool   `json:"success"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	ChainPath string `json:"chain_path,omitempty"`
	Error     string `json:"error,omitempty"`
}

type InspectCertificateRequest struct {
	Domain    string `json:"domain,omitempty"`
	CertPath  string `json:"cert_path"`
	KeyPath   string `json:"key_path"`
	ChainPath string `json:"chain_path,omitempty"`
}

type ReconcileSiteCertLineagesRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	ReferencedLineages  []string `json:"referenced_lineages"`
	// ActiveLineages is retained for safe rolling upgrades with agents that
	// predate the expanded certificate ledger.
	ActiveLineages []string `json:"active_lineages"`
}

type ReconcileSiteCertLineagesResponse struct {
	Deleted int    `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

type DeleteCertLineageRequest struct {
	Domain              string   `json:"domain"`
	DeleteCanonical     bool     `json:"delete_canonical,omitempty"`
	LineageNames        []string `json:"lineage_names,omitempty"`
	SnapshotPath        string   `json:"snapshot_path,omitempty"`
	ExpectedBuildCommit string   `json:"expected_build_commit,omitempty"`
}

type DeleteCertLineageResponse struct {
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}
