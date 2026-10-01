package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// upd8 F1: a mail profile's sub-step and the setup firewall step were refused
// by the Agent because a package task was running, and the owner saw only
// mail_profile_install_failed / server_setup_firewall_failed with the cause in
// the panel log. Both now keep HOST_MUTATION_BUSY and the reason's sentence.
func TestSetupFailuresKeepTheHostBusyCause(t *testing.T) {
	packageBusy := &hostMutationBusyError{reason: transport.HostMutationReasonPackageManager}
	want := hostMutationBusyMessages[transport.HostMutationReasonPackageManager]

	profile := mailProfileInstallFailure(fmt.Errorf("mail TLS synchronization: %w", packageBusy))
	if profile.Code != transport.HostMutationBusy || profile.Message != want {
		t.Fatalf("mail profile failure = %+v", profile)
	}
	for _, kind := range []string{"firewall", "panel_certificate", "infrastructure_dns", "verify"} {
		step := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: kind}}
		failure := serverSetupFailureForStep(step, packageBusy)
		if failure.Code != transport.HostMutationBusy || failure.Message != want {
			t.Fatalf("%s step failure = %+v", kind, failure)
		}
	}

	// A refusal from an Agent that names no reason keeps the generic sentence.
	unnamed := serverSetupFailureForStep(
		serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "firewall"}},
		fmt.Errorf("apply: %w", &hostMutationBusyError{}),
	)
	if unnamed.Code != transport.HostMutationBusy || unnamed.Message != hostMutationBusyGenericMessage {
		t.Fatalf("unnamed busy failure = %+v", unnamed)
	}

	// A busy refusal joined with another failure is not a pure host-busy cause
	// and keeps the step's own code; so does any other error.
	joined := errors.Join(packageBusy, errors.New("compensation failed"))
	if got := mailProfileInstallFailure(joined); got.Code != "mail_profile_install_failed" {
		t.Fatalf("joined mail profile failure = %+v", got)
	}
	firewall := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "firewall"}}
	if got := serverSetupFailureForStep(firewall, joined); got.Code != "server_setup_firewall_failed" {
		t.Fatalf("joined firewall failure = %+v", got)
	}
	if got := serverSetupFailureForStep(firewall, errors.New("firewall application was not verified")); got.Code != "server_setup_firewall_failed" {
		t.Fatalf("plain firewall failure = %+v", got)
	}
}
