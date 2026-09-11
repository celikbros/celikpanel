package transport

import "time"

const AgentCapabilityMailHostCertificateV1 = "mail_host_certificate_v1"

type IssueMailHostCertificateRequest struct {
	ServiceMutationBinding
	Domain              string `json:"domain"`
	Email               string `json:"email"`
	ExpectedBuildCommit string `json:"expected_build_commit"`
}

type IssueMailHostCertificateResponse struct {
	Issued    bool      `json:"issued"`
	ExpiresAt time.Time `json:"expires_at"`
	Error     string    `json:"error,omitempty"`
}

type MailHostCertificateStatusRequest struct {
	Domain string `json:"domain"`
}

type MailHostCertificateStatusResponse struct {
	Ready        bool      `json:"ready"`
	Domain       string    `json:"domain"`
	ExpiresAt    time.Time `json:"expires_at"`
	RenewalReady bool      `json:"renewal_ready"`
	Error        string    `json:"error,omitempty"`
}
