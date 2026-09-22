package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mailhostartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const PanelCertificateIssueCommitPhasePrefix = "commit/panel-certificate-issue/v1/"

const PanelCertificateIssueCommitIntent = "intent"

const PanelCertificateIssueCommitPublished = "published"

func FormatPanelCertificateIssueCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != PanelCertificateIssueCommitIntent &&
		state != PanelCertificateIssueCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mailhostartifact.ValidReceiptDomain(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidPanelCertificateIssueQualifier(qualifier) {
		return "", errors.New("invalid panel certificate issue commit phase identity")
	}
	return PanelCertificateIssueCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func ParsePanelCertificateIssueCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, PanelCertificateIssueCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a panel certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, PanelCertificateIssueCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	canonical, phaseErr := FormatPanelCertificateIssueCommitPhase(
		state,
		requestID,
		domain,
		qualifier,
	)
	if phaseErr != nil || canonical != value {
		return "", "", "", "", errors.New(
			"invalid panel certificate issue commit phase",
		)
	}
	return state, requestID, domain, qualifier, nil
}
