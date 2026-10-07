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

// 2026-10-08: the failed setup step carries the typed reason, so the wizard
// selects its headline from the reason and not from the English sentence. A
// child operation row stores only the code and the sentence; the reason is
// read back from that sentence when the row is loaded.
func TestSetupBusyFailureCarriesItsTypedReason(t *testing.T) {
	firewall := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "firewall"}}
	for reason, sentence := range hostMutationBusyMessages {
		direct := serverSetupFailureForStep(firewall, fmt.Errorf("apply: %w", &hostMutationBusyError{reason: reason}))
		if direct.Code != transport.HostMutationBusy || direct.Reason != reason || direct.Message != sentence {
			t.Fatalf("%s direct failure = %+v", reason, direct)
		}
		if got := hostMutationBusyReasonForMessage(sentence); got != reason {
			t.Fatalf("sentence for %s reads back as %q", reason, got)
		}
		// The child path: an install operation refused at its Agent lease.
		start := operationStartFailure(&hostMutationBusyError{reason: reason})
		child := &serverSetupChildFailure{Code: start.Code, Message: start.Message,
			Reason: hostMutationBusyReasonForMessage(start.Message)}
		service := serverSetupExecutionStep{serverSetupPlanStep: serverSetupPlanStep{Kind: "service", Target: "nginx"}}
		if got := serverSetupFailureForStep(service, child); got.Code != transport.HostMutationBusy || got.Reason != reason {
			t.Fatalf("%s child failure = %+v", reason, got)
		}
	}
	// The Panel's own short work runs as an Agent job; the Agent now names it.
	if hostMutationBusyReasonForMessage(hostMutationBusyMessages[transport.HostMutationReasonAgentMutation]) != transport.HostMutationReasonAgentMutation {
		t.Fatal("the Agent-job sentence lost its reason")
	}
	for _, message := range []string{hostMutationBusyGenericMessage, "", "something else"} {
		if got := hostMutationBusyReasonForMessage(message); got != "" {
			t.Fatalf("%q reads back as reason %q", message, got)
		}
	}
	unnamed := serverSetupFailureForStep(firewall, &hostMutationBusyError{})
	if unnamed.Reason != "" {
		t.Fatalf("unnamed busy failure invented reason %q", unnamed.Reason)
	}
}
