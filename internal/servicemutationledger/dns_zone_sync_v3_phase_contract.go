package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const DnsZoneSyncV3CommitPhasePrefix = "commit/dns-zone-sync/v3/"

const DnsZoneSyncV3Applied = "applied"

const DnsZoneSyncV3PropagationPending = "propagation-pending"

const DnsZoneSyncV3Recovering = "recovering"

const DnsZoneSyncV3Published = "published"

func FormatDNSZoneSyncV3Phase(state, requestID, domain, qualifier string) (string, error) {
	if (state != DnsZoneSyncV3Applied && state != DnsZoneSyncV3PropagationPending &&
		state != DnsZoneSyncV3Recovering && state != DnsZoneSyncV3Published) ||
		!ValidIdentity(requestID) ||
		!ServiceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(qualifier) {
		return "", errors.New("invalid DNS zone V3 phase identity")
	}
	return DnsZoneSyncV3CommitPhasePrefix + state + "/" + requestID +
		"/" + domain + "/" + qualifier, nil
}

func ParseDNSZoneSyncV3Phase(value string) (state, requestID, domain, qualifier string, err error) {
	if !strings.HasPrefix(value, DnsZoneSyncV3CommitPhasePrefix) {
		return "", "", "", "", errors.New("not a DNS zone V3 phase")
	}
	remainder := strings.TrimPrefix(value, DnsZoneSyncV3CommitPhasePrefix)
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
	canonical, formatErr := FormatDNSZoneSyncV3Phase(state, requestID, domain, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", "", errors.New("invalid DNS zone V3 phase")
	}
	return state, requestID, domain, qualifier, nil
}

func FormatDNSZoneSyncV3PublishedPhase(requestID, domain, qualifier string) (string, error) {
	return FormatDNSZoneSyncV3Phase(
		DnsZoneSyncV3Published, requestID, domain, qualifier,
	)
}
