package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"net/mail"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const (
	panelCertificateIssueSchema          = "panel-certificate-issue/v1"
	panelCertificateIssueQualifierPrefix = panelCertificateIssueSchema + ":sha256:"
	panelCertificateManagedTLSDir        = "/var/lib/celikpanel/tls"
	panelCertificateEmailMaxBytes        = 254
	panelCertificateEmailLocalMaxBytes   = 64
	panelCertificateBuildMaxBytes        = 128
)

var panelCertificateIssueDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(panelCertificateIssueSchema),
	[]byte("panel_certificate_issue"),
	[]byte("Agent.IssuePanelCertificateV2"),
	[]byte("issue"),
}

// PanelCertificateIssueCommitment is the canonical, detached certificate
// issuance request authorized by a durable service-mutation lease.
type PanelCertificateIssueCommitment struct {
	Domain              string
	Email               string
	TLSDir              string
	ExpectedBuildCommit string
	Qualifier           string
}

// CanonicalPanelCertificateIssue validates and freezes every caller-controlled
// value that can change certbot execution or the publication target.
func CanonicalPanelCertificateIssue(
	domain, email, tlsDir, expectedBuildCommit string,
) (PanelCertificateIssueCommitment, error) {
	canonicalDomain, err := hostname.CanonicalFQDN(domain)
	if err != nil {
		return PanelCertificateIssueCommitment{}, errors.New("invalid panel certificate domain")
	}
	canonicalEmail, err := canonicalPanelCertificateEmail(email)
	if err != nil {
		return PanelCertificateIssueCommitment{}, err
	}
	if tlsDir != panelCertificateManagedTLSDir {
		return PanelCertificateIssueCommitment{}, errors.New("invalid panel certificate TLS directory")
	}
	canonicalBuild, err := canonicalPanelCertificateBuild(expectedBuildCommit)
	if err != nil {
		return PanelCertificateIssueCommitment{}, err
	}

	digest := sha256.New()
	for _, frame := range panelCertificateIssueDigestFrames {
		writePanelCertificateIssueDigestFrame(digest, frame)
	}
	for _, value := range []string{
		canonicalDomain,
		canonicalEmail,
		tlsDir,
		canonicalBuild,
	} {
		writePanelCertificateIssueDigestFrame(digest, []byte(value))
	}

	return PanelCertificateIssueCommitment{
		Domain:              canonicalDomain,
		Email:               canonicalEmail,
		TLSDir:              tlsDir,
		ExpectedBuildCommit: canonicalBuild,
		Qualifier: panelCertificateIssueQualifierPrefix +
			hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func canonicalPanelCertificateEmail(raw string) (string, error) {
	address := strings.TrimSpace(raw)
	if address == "" || len(address) > panelCertificateEmailMaxBytes ||
		strings.Count(address, "@") != 1 {
		return "", errors.New("invalid panel certificate email address")
	}
	for index := 0; index < len(address); index++ {
		if address[index] < 0x21 || address[index] > 0x7e {
			return "", errors.New("invalid panel certificate email address")
		}
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Name != "" || parsed.Address != address {
		return "", errors.New("invalid panel certificate email address")
	}
	local, emailDomain, found := strings.Cut(address, "@")
	if !found || local == "" || len(local) > panelCertificateEmailLocalMaxBytes {
		return "", errors.New("invalid panel certificate email address")
	}
	canonicalDomain, err := hostname.CanonicalFQDN(emailDomain)
	if err != nil {
		return "", errors.New("invalid panel certificate email address")
	}
	canonical := local + "@" + canonicalDomain
	if len(canonical) > panelCertificateEmailMaxBytes {
		return "", errors.New("invalid panel certificate email address")
	}
	parsedCanonical, err := mail.ParseAddress(canonical)
	if err != nil || parsedCanonical.Name != "" || parsedCanonical.Address != canonical {
		return "", errors.New("invalid panel certificate email address")
	}
	return canonical, nil
}

func canonicalPanelCertificateBuild(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "unknown" {
		return "unknown", nil
	}
	if len(value) > panelCertificateBuildMaxBytes {
		return "", errors.New("invalid expected panel build commit")
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return "", errors.New("invalid expected panel build commit")
		}
	}
	return value, nil
}

func writePanelCertificateIssueDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

// ValidPanelCertificateIssueQualifier accepts only the canonical v1
// lowercase SHA-256 representation stored in ServiceMutationJob.PackageName.
func ValidPanelCertificateIssueQualifier(value string) bool {
	if len(value) != len(panelCertificateIssueQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, panelCertificateIssueQualifierPrefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, panelCertificateIssueQualifierPrefix) {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
