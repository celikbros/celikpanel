package main

import (
	"strings"
	"testing"
)

func TestDNSSECResultErrorPreservesAgentFailure(t *testing.T) {
	const want = "rectify zone: backend refused the update"
	got := dnssecResultError(dnssecAgentResponse{Error: want}, true)
	if got != want {
		t.Fatalf("dnssecResultError() = %q, want exact agent error %q", got, want)
	}
}

func TestDNSSECResultErrorRejectsSuccessWithoutDS(t *testing.T) {
	got := dnssecResultError(dnssecAgentResponse{Secured: true}, true)
	if !strings.Contains(got, "no DS") {
		t.Fatalf("dnssecResultError() = %q, want missing-DS failure", got)
	}
}

func TestDNSSECResultErrorAllowsUnsignedStatus(t *testing.T) {
	if got := dnssecResultError(dnssecAgentResponse{}, false); got != "" {
		t.Fatalf("unsigned status returned error %q", got)
	}
}
