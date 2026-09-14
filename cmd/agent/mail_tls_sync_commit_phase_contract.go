package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const mailTLSSyncCommitPhasePrefix = "commit/mail-tls-sync/v1/"

const mailTLSSyncCommitIntent = "intent"

const mailTLSSyncCommitPublished = "published"

func formatMailTLSSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != mailTLSSyncCommitIntent && state != mailTLSSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(qualifier) {
		return "", errors.New("invalid mail TLS sync commit phase identity")
	}
	return mailTLSSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseMailTLSSyncCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, mailTLSSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a mail TLS sync commit phase")
	}
	remainder := strings.TrimPrefix(value, mailTLSSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	canonical, formatErr := formatMailTLSSyncCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	return state, requestID, qualifier, nil
}
