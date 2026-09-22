package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const MailHostCertificateCommitPhasePrefix = "commit/mail-host-certificate/v1/"

const MailHostCertificateCommitIntent = "intent"

const MailHostCertificateCommitPublished = "published"

func FormatMailHostCertificateCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != MailHostCertificateCommitIntent &&
		state != MailHostCertificateCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mailhostartifact.ValidReceiptDomain(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidMailHostCertificateQualifier(qualifier) {
		return "", errors.New("invalid mail host certificate issue commit phase identity")
	}
	return MailHostCertificateCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func ParseMailHostCertificateCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, MailHostCertificateCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a mail host certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, MailHostCertificateCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	canonical, phaseErr := FormatMailHostCertificateCommitPhase(
		state,
		requestID,
		domain,
		qualifier,
	)
	if phaseErr != nil || canonical != value {
		return "", "", "", "", errors.New(
			"invalid mail host certificate issue commit phase",
		)
	}
	return state, requestID, domain, qualifier, nil
}
