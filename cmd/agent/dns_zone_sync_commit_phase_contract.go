package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const dnsZoneSyncCommitPhasePrefix = servicemutationledger.DnsZoneSyncCommitPhasePrefix
const dnsZoneSyncCommitIntent = servicemutationledger.DnsZoneSyncCommitIntent
const dnsZoneSyncCommitApplied = servicemutationledger.DnsZoneSyncCommitApplied
const dnsZoneSyncCommitPublished = servicemutationledger.DnsZoneSyncCommitPublished

func formatDNSZoneSyncCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	return servicemutationledger.FormatDNSZoneSyncCommitPhase(state, requestID, domain, qualifier)
}

func parseDNSZoneSyncCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	return servicemutationledger.ParseDNSZoneSyncCommitPhase(value)
}
