package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const firewallApplyCommitPhasePrefix = servicemutationledger.FirewallApplyCommitPhasePrefix
const firewallApplyCommitIntent = servicemutationledger.FirewallApplyCommitIntent
const firewallApplyCommitPublished = servicemutationledger.FirewallApplyCommitPublished

func formatFirewallApplyCommitPhase(state, requestID, qualifier string) (string, error) {
	return servicemutationledger.FormatFirewallApplyCommitPhase(state, requestID, qualifier)
}

func parseFirewallApplyCommitPhase(value string) (
	state, requestID, qualifier string,
	err error,
) {
	return servicemutationledger.ParseFirewallApplyCommitPhase(value)
}
