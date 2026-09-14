package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const firewallApplyCommitPhasePrefix = "commit/firewall-apply/v1/"

const firewallApplyCommitIntent = "intent"

const firewallApplyCommitPublished = "published"

func formatFirewallApplyCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != firewallApplyCommitIntent && state != firewallApplyCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidFirewallApplyQualifier(qualifier) {
		return "", errors.New("invalid firewall apply commit phase identity")
	}
	return firewallApplyCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseFirewallApplyCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, firewallApplyCommitPhasePrefix) {
		return "", "", "", errors.New("not a firewall apply commit phase")
	}
	remainder := strings.TrimPrefix(value, firewallApplyCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	canonical, formatErr := formatFirewallApplyCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid firewall apply commit phase")
	}
	return state, requestID, qualifier, nil
}
