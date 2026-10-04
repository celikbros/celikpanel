package servicemutationledger

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const DnsClusterConfigCommitPhasePrefix = "commit/dns-cluster-config/v1/"

const DnsClusterConfigCommitIntent = "intent"

const DnsClusterConfigCommitPublished = "published"

func FormatDNSClusterConfigCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != DnsClusterConfigCommitIntent && state != DnsClusterConfigCommitPublished) ||
		!ValidIdentity(requestID) ||
		!mutationpayload.ValidDNSClusterConfigQualifier(qualifier) {
		return "", errors.New("invalid DNS cluster commit phase identity")
	}
	return DnsClusterConfigCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func ParseDNSClusterConfigCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, DnsClusterConfigCommitPhasePrefix) {
		return "", "", "", errors.New("not a DNS cluster commit phase")
	}
	remainder := strings.TrimPrefix(value, DnsClusterConfigCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	canonical, formatErr := FormatDNSClusterConfigCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	return state, requestID, qualifier, nil
}
