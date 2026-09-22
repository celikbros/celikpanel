package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const dnsZoneSyncV3CommitPhasePrefix = servicemutationledger.DnsZoneSyncV3CommitPhasePrefix
const dnsZoneSyncV3Applied = servicemutationledger.DnsZoneSyncV3Applied
const dnsZoneSyncV3PropagationPending = servicemutationledger.DnsZoneSyncV3PropagationPending
const dnsZoneSyncV3Recovering = servicemutationledger.DnsZoneSyncV3Recovering
const dnsZoneSyncV3Published = servicemutationledger.DnsZoneSyncV3Published

func formatDNSZoneSyncV3Phase(state, requestID, domain, qualifier string) (string, error) {
	return servicemutationledger.FormatDNSZoneSyncV3Phase(state, requestID, domain, qualifier)
}

func parseDNSZoneSyncV3Phase(value string) (state, requestID, domain, qualifier string, err error) {
	return servicemutationledger.ParseDNSZoneSyncV3Phase(value)
}

func formatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier string) (string, error) {
	return servicemutationledger.FormatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier)
}
