package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const dnsZoneSyncV3CommitPhasePrefix = "commit/dns-zone-sync/v3/"

const dnsZoneSyncV3Applied = "applied"

const dnsZoneSyncV3PropagationPending = "propagation-pending"

const dnsZoneSyncV3Recovering = "recovering"

const dnsZoneSyncV3Published = "published"

func formatDNSZoneSyncV3Phase(state, requestID, domain, qualifier string) (string, error) {
	if (state != dnsZoneSyncV3Applied && state != dnsZoneSyncV3PropagationPending &&
		state != dnsZoneSyncV3Recovering && state != dnsZoneSyncV3Published) ||
		!validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(qualifier) {
		return "", errors.New("invalid DNS zone V3 phase identity")
	}
	return dnsZoneSyncV3CommitPhasePrefix + state + "/" + requestID +
		"/" + domain + "/" + qualifier, nil
}

func parseDNSZoneSyncV3Phase(value string) (state, requestID, domain, qualifier string, err error) {
	if !strings.HasPrefix(value, dnsZoneSyncV3CommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone V3 phase")
	}
	remainder := strings.TrimPrefix(value, dnsZoneSyncV3CommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	canonical, formatErr := formatDNSZoneSyncV3Phase(state, requestID, domain, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	return state, requestID, domain, qualifier, nil
}

func formatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier string) (string, error) {
	return formatDNSZoneSyncV3Phase(
		dnsZoneSyncV3Published, requestID, domain, qualifier,
	)
}
