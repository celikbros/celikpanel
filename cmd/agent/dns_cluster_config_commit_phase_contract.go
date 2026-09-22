package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const dnsClusterConfigCommitPhasePrefix = servicemutationledger.DnsClusterConfigCommitPhasePrefix
const dnsClusterConfigCommitIntent = servicemutationledger.DnsClusterConfigCommitIntent
const dnsClusterConfigCommitPublished = servicemutationledger.DnsClusterConfigCommitPublished

func formatDNSClusterConfigCommitPhase(state, requestID, qualifier string) (string, error) {
	return servicemutationledger.FormatDNSClusterConfigCommitPhase(state, requestID, qualifier)
}

func parseDNSClusterConfigCommitPhase(value string) (state, requestID, qualifier string, err error) {
	return servicemutationledger.ParseDNSClusterConfigCommitPhase(value)
}
