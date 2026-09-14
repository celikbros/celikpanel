package main

import (
	"errors"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

const dnsClusterConfigCommitPhasePrefix = "commit/dns-cluster-config/v1/"

const dnsClusterConfigCommitIntent = "intent"

const dnsClusterConfigCommitPublished = "published"

func formatDNSClusterConfigCommitPhase(state, requestID, qualifier string) (string, error) {
	if (state != dnsClusterConfigCommitIntent && state != dnsClusterConfigCommitPublished) ||
		!validMutationIdentity(requestID) ||
		!mutationpayload.ValidDNSClusterConfigQualifier(qualifier) {
		return "", errors.New("invalid DNS cluster commit phase identity")
	}
	return dnsClusterConfigCommitPhasePrefix + state + "/" + requestID + "/" + qualifier, nil
}

func parseDNSClusterConfigCommitPhase(value string) (state, requestID, qualifier string, err error) {
	if !strings.HasPrefix(value, dnsClusterConfigCommitPhasePrefix) {
		return "", "", "", errors.New("not a DNS cluster commit phase")
	}
	remainder := strings.TrimPrefix(value, dnsClusterConfigCommitPhasePrefix)
	state, remainder, found := strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	requestID, qualifier, found = strings.Cut(remainder, "/")
	if !found {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	canonical, formatErr := formatDNSClusterConfigCommitPhase(state, requestID, qualifier)
	if formatErr != nil || canonical != value {
		return "", "", "", errors.New("invalid DNS cluster commit phase")
	}
	return state, requestID, qualifier, nil
}
