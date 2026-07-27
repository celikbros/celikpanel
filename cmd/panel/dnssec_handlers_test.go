package main

import (
	"context"
	"strings"
	"testing"
)

func TestDANEAutomationFeatureStateIsSafelyDisabled(t *testing.T) {
	state := currentDANEAutomationState()
	if state.Enabled {
		t.Fatal("automatic DANE mutation is enabled without a durable rollover job")
	}
	if daneMutationSafetyPrerequisitesAvailable() {
		t.Fatal("DANE mutation safety prerequisites unexpectedly report available")
	}
	reason := strings.ToLower(state.Reason)
	if !strings.Contains(reason, "rollover") || !strings.Contains(reason, "ownership") {
		t.Fatalf("disabled DANE state does not explain both safety blockers: %+v", state)
	}
}

func TestRefreshTLSARecordsIsNoOpWhileGateDisabled(t *testing.T) {
	// A zero-value Panel would panic if refresh tried to reach the database,
	// agent, or DNS publisher. Success proves certificate/mail flows cannot
	// mutate any TLSA record while the release gate is disabled.
	if err := (&Panel{}).refreshTLSARecords(context.Background(), 42); err != nil {
		t.Fatalf("disabled DANE refresh should not block core TLS operations: %v", err)
	}
}

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
