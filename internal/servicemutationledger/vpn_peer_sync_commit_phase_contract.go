package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const VpnPeerSyncCommitPhasePrefix = "commit/vpn-peer-sync/v1/"

const VpnPeerSyncCommitIntent = "intent"

const VpnPeerSyncCommitPublished = "published"

func FormatVPNPeerSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != VpnPeerSyncCommitIntent && state != VpnPeerSyncCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mutationpayload.ValidVPNPeerSyncQualifier(qualifier) {
		return "", errors.New("invalid VPN peer sync commit phase identity")
	}
	return VpnPeerSyncCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func ParseVPNPeerSyncCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, VpnPeerSyncCommitPhasePrefix) {
		return "", "", "", errors.New("not a VPN peer sync commit phase")
	}
	remainder := strings.TrimPrefix(value, VpnPeerSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	canonical, err := FormatVPNPeerSyncCommitPhase(state, requestID, qualifier)
	if err != nil || canonical != value {
		return "", "", "", errors.New("invalid VPN peer sync commit phase")
	}
	return state, requestID, qualifier, nil
}
