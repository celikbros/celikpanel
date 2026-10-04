package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const MailTLSSyncCommitPhasePrefix = "commit/mail-tls-sync/v1/"

const MailTLSSyncCommitIntent = "intent"

const MailTLSSyncCommitPublished = "published"

func FormatMailTLSSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != MailTLSSyncCommitIntent && state != MailTLSSyncCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mutationpayload.ValidMailTLSSyncQualifier(qualifier) {
		return "", errors.New("invalid mail TLS sync commit phase identity")
	}
	return MailTLSSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func ParseMailTLSSyncCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, MailTLSSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a mail TLS sync commit phase")
	}
	remainder := strings.TrimPrefix(value, MailTLSSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	canonical, formatErr := FormatMailTLSSyncCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid mail TLS sync commit phase")
	}
	return state, requestID, qualifier, nil
}
