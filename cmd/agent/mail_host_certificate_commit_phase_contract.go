package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const mailHostCertificateCommitPhasePrefix = "commit/mail-host-certificate/v1/"

const mailHostCertificateCommitIntent = "intent"

const mailHostCertificateCommitPublished = "published"

func formatMailHostCertificateCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != mailHostCertificateCommitIntent &&
		state != mailHostCertificateCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!validPanelCertDomain.MatchString(domain) ||
		domain != strings.ToLower(strings.TrimSpace(domain)) ||
		!mutationpayload.ValidMailHostCertificateQualifier(qualifier) {
		return "", errors.New("invalid mail host certificate issue commit phase identity")
	}
	return mailHostCertificateCommitPhasePrefix + state + "/" +
		requestID + "/" + domain + "/" + qualifier, nil
}

func parseMailHostCertificateCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, mailHostCertificateCommitPhasePrefix) {
		return "", "", "", "", errors.New(
			"not a mail host certificate issue commit phase",
		)
	}
	remainder := strings.TrimPrefix(value, mailHostCertificateCommitPhasePrefix)
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
	canonical, phaseErr := formatMailHostCertificateCommitPhase(
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
