package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const DnsZoneSyncCommitPhasePrefix = "commit/dns-zone-sync/v1/"

const DnsZoneSyncCommitIntent = "intent"

const DnsZoneSyncCommitApplied = "applied"

const DnsZoneSyncCommitPublished = "published"

func FormatDNSZoneSyncCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	if (state != DnsZoneSyncCommitIntent &&
		state != DnsZoneSyncCommitApplied &&
		state != DnsZoneSyncCommitPublished) ||
		!ValidIdentity(requestID) ||
		!ServiceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncQualifier(qualifier) {
		return "", errors.New("invalid DNS zone sync commit phase identity")
	}
	return DnsZoneSyncCommitPhasePrefix + state + "/" + requestID + "/" +
		domain + "/" + qualifier, nil
}

func ParseDNSZoneSyncCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	if !strings.HasPrefix(value, DnsZoneSyncCommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone sync commit phase")
	}
	remainder := strings.TrimPrefix(value, DnsZoneSyncCommitPhasePrefix)
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
	canonical, formatErr := FormatDNSZoneSyncCommitPhase(
		state, requestID, domain, qualifier,
	)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone sync commit phase")
	}
	return state, requestID, domain, qualifier, nil
}
