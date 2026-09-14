package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const vpnPeerSyncCommitPhasePrefix = "commit/vpn-peer-sync/v1/"

const vpnPeerSyncCommitIntent = "intent"

const vpnPeerSyncCommitPublished = "published"

func formatVPNPeerSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != vpnPeerSyncCommitIntent && state != vpnPeerSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync commit phase identity")
	}
	return vpnPeerSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseVPNPeerSyncCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, vpnPeerSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a VPN peer sync commit phase")
	}
	remainder := strings.TrimPrefix(value, vpnPeerSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	canonical, err := formatVPNPeerSyncCommitPhase(state, requestID, qualifier)
	if err != nil || canonical != value {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	return state, requestID, qualifier, nil
}
