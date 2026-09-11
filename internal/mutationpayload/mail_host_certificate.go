package mutationpayload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const MailHostCertificateDirectory = "/etc/ssl/celikpanel/_mail/host"

type MailHostCertificateCommitment struct {
	Domain              string `json:"domain"`
	Email               string `json:"email"`
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Qualifier           string `json:"qualifier"`
}

var mailHostCertificateQualifier = regexp.MustCompile(`^mhc1:[0-9a-f]{64}$`)

func CanonicalMailHostCertificate(domain, email, build string) (MailHostCertificateCommitment, error) {
	canonical, err := hostname.CanonicalFQDN(domain)
	if err != nil || canonical != domain {
		return MailHostCertificateCommitment{}, errors.New("invalid mail host certificate name")
	}
	// Share the existing reviewed email/build validation, but use a distinct
	// purpose and fixed destination in the digest. A panel-certificate lease
	// cannot authorize mail-host publication or vice versa.
	panel, err := CanonicalPanelCertificateIssue(domain, email, "/var/lib/celikpanel/tls", build)
	if err != nil {
		return MailHostCertificateCommitment{}, err
	}
	digest := sha256.New()
	for _, part := range []string{"mail_host_certificate/v1", "Agent.IssueMailHostCertificateV1", MailHostCertificateDirectory, canonical, panel.Email, panel.ExpectedBuildCommit} {
		writePanelCertificateIssueDigestFrame(digest, []byte(part))
	}
	return MailHostCertificateCommitment{Domain: canonical, Email: panel.Email, ExpectedBuildCommit: panel.ExpectedBuildCommit, Qualifier: "mhc1:" + hex.EncodeToString(digest.Sum(nil))}, nil
}

func ValidMailHostCertificateQualifier(value string) bool {
	return mailHostCertificateQualifier.MatchString(value)
}
