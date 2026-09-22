package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const vpnPeerSyncCommitPhasePrefix = servicemutationledger.VpnPeerSyncCommitPhasePrefix
const vpnPeerSyncCommitIntent = servicemutationledger.VpnPeerSyncCommitIntent
const vpnPeerSyncCommitPublished = servicemutationledger.VpnPeerSyncCommitPublished

func formatVPNPeerSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	return servicemutationledger.FormatVPNPeerSyncCommitPhase(state, requestID, qualifier)
}

func parseVPNPeerSyncCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	return servicemutationledger.ParseVPNPeerSyncCommitPhase(value)
}
