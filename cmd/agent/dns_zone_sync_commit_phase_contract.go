package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const dnsZoneSyncCommitPhasePrefix = "commit/dns-zone-sync/v1/"

const dnsZoneSyncCommitIntent = "intent"

const dnsZoneSyncCommitApplied = "applied"

const dnsZoneSyncCommitPublished = "published"

func formatDNSZoneSyncCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != dnsZoneSyncCommitIntent &&
		state != dnsZoneSyncCommitApplied &&
		state != dnsZoneSyncCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!serviceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncQualifier(qualifier) {
		return "", errors.New("invalid DNS zone sync commit phase identity")
	}
	return dnsZoneSyncCommitPhasePrefix + state + "/" + requestID + "/" +
		domain + "/" + qualifier, nil
}

func parseDNSZoneSyncCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, dnsZoneSyncCommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone sync commit phase")
	}
	remainder := strings.TrimPrefix(value, dnsZoneSyncCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	requestID, remainder, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	domain, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	canonical, formatErr := formatDNSZoneSyncCommitPhase(
		state, requestID, domain, qualifier,
	)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	return state, requestID, domain, qualifier, nil
}
