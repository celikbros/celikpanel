package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const FirewallApplyCommitPhasePrefix = "commit/firewall-apply/v1/"

const FirewallApplyCommitIntent = "intent"

const FirewallApplyCommitPublished = "published"

func FormatFirewallApplyCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != FirewallApplyCommitIntent && state != FirewallApplyCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(qualifier) {
		return "", errors.New("invalid firewall apply commit phase identity")
	}
	return FirewallApplyCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func ParseFirewallApplyCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, FirewallApplyCommitPhasePrefix) {
		return "", "", "", errors.New("not a firewall apply commit phase")
	}
	remainder := strings.TrimPrefix(value, FirewallApplyCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	canonical, formatErr := FormatFirewallApplyCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	return state, requestID, qualifier, nil
}
