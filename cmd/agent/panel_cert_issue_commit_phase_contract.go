package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const panelCertificateIssueCommitPhasePrefix = "commit/panel-certificate-issue/v1/"

const panelCertificateIssueCommitIntent = "intent"

const panelCertificateIssueCommitPublished = "published"

func formatPanelCertificateIssueCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != panelCertificateIssueCommitIntent &&
		state != panelCertificateIssueCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!validPanelCertDomain.MatchString(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidPanelCertificateIssueQualifier(qualifier) {
		return "", errors.New("invalid panel certificate issue commit phase identity")
	}
	return panelCertificateIssueCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func parsePanelCertificateIssueCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, panelCertificateIssueCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a panel certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, panelCertificateIssueCommitPhasePrefix)
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
	canonical, phaseErr := formatPanelCertificateIssueCommitPhase(
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
