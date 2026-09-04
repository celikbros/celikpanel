package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const nginxOnlyListener = "LISTEN 0 128 0.0.0.0:80 0.0.0.0:* users:((nginx,pid=8,fd=3))\n"

// R-035. Three different situations used to arrive as one sentence. Only a
// server proven to have no SSH service is a state the operator may accept; a
// probe that could not run is not a server without SSH, and neither is an SSH
// service that merely is not listening at this moment.
func TestSSHDiscoveryTellsNoSSHServiceApartFromAFailedProbe(t *testing.T) {
	tests := []struct {
		name        string
		runner      *fakeFirewallCommandRunner
		wantReason  string
		wantMessage string
	}{
		{
			name: "no SSH service on this host",
			runner: &fakeFirewallCommandRunner{
				ssOut: nginxOnlyListener, sshServicePresent: false,
			},
			wantReason:  transport.SSHDiscoveryNoService,
			wantMessage: "this server has no SSH service",
		},
		{
			name: "an SSH service that is not listening",
			runner: &fakeFirewallCommandRunner{
				ssOut: nginxOnlyListener, sshServicePresent: true,
			},
			wantReason:  transport.SSHDiscoveryNotListening,
			wantMessage: "no verified listening sshd port",
		},
		{
			name: "the listener probe could not run",
			runner: &fakeFirewallCommandRunner{
				ssErr: errors.New("ss permission denied"), sshServicePresent: false,
			},
			wantReason:  transport.SSHDiscoveryProbeFailed,
			wantMessage: "SSH listener discovery failed",
		},
		{
			name: "the presence probe could not run",
			runner: &fakeFirewallCommandRunner{
				ssOut:                nginxOnlyListener,
				sshServicePresentErr: errors.New("systemctl is unavailable"),
			},
			wantReason:  transport.SSHDiscoveryProbeFailed,
			wantMessage: "could not be determined",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := detectSSHPortsWithRunner(test.runner)
			if err == nil {
				t.Fatal("discovery unexpectedly found a listener")
			}
			refusal := classifySSHDiscovery(test.runner, err)
			if refusal.reason != test.wantReason {
				t.Fatalf("reason = %q, want %q", refusal.reason, test.wantReason)
			}
			if !strings.Contains(refusal.Error(), test.wantMessage) {
				t.Fatalf("message = %q, want it to contain %q", refusal.Error(), test.wantMessage)
			}
		})
	}
}

// A proven listener classifies as nothing at all, and the status response
// reports the ports rather than a reason.
func TestSSHDiscoveryReportsPortsWhenAListenerIsProven(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		ssOut:          "LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:((not-sshd,pid=73,fd=3))\n",
		trustedSSHPIDs: map[int]bool{73: true},
	}
	var response FirewallStatusResponse
	response.Enabled = true
	readFirewallSSHDiscovery(runner, &response)
	if response.SSHDiscoveryReason != "" || len(response.SSHPorts) != 1 ||
		response.SSHPorts[0] != 2222 || response.Error != "" {
		t.Fatalf("status = %+v", response)
	}
}

// A server with no SSH service is a state, not a fault: the live status names
// it without turning the firewall's own status into an error. The two refusals
// stay visible as errors on a firewall that is already live.
func TestSSHDiscoveryReasonIsNotAnErrorWhenTheHostSimplyHasNoSSH(t *testing.T) {
	noService := &fakeFirewallCommandRunner{ssOut: nginxOnlyListener}
	var live FirewallStatusResponse
	live.Enabled = true
	readFirewallSSHDiscovery(noService, &live)
	if live.SSHDiscoveryReason != transport.SSHDiscoveryNoService || live.Error != "" {
		t.Fatalf("live status on a server without SSH = %+v", live)
	}

	notListening := &fakeFirewallCommandRunner{ssOut: nginxOnlyListener, sshServicePresent: true}
	var degraded FirewallStatusResponse
	degraded.Enabled = true
	readFirewallSSHDiscovery(notListening, &degraded)
	if degraded.SSHDiscoveryReason != transport.SSHDiscoveryNotListening ||
		!strings.Contains(degraded.Error, "no verified listening sshd port") {
		t.Fatalf("live status with SSH down = %+v", degraded)
	}

	// While the firewall is off the reason is still reported, so the panel can
	// say what enabling it would run into, but nothing is called an error.
	var off FirewallStatusResponse
	readFirewallSSHDiscovery(notListening, &off)
	if off.SSHDiscoveryReason != transport.SSHDiscoveryNotListening || off.Error != "" {
		t.Fatalf("status while off = %+v", off)
	}
}

// The V2 apply path is where the firewall is actually changed. It proceeds on
// a server proven to have no SSH service, recording that fact in the durable
// journal, and refuses every other reason before touching the host.
func TestFirewallApplyV2ProceedsOnlyOnAProvenAbsenceOfSSH(t *testing.T) {
	commitment, err := mutationpayload.CanonicalFirewallApply(true, true, []int{2083}, nil)
	if err != nil {
		t.Fatal(err)
	}

	noService := &fakeFirewallCommandRunner{ssOut: nginxOnlyListener}
	store := &fakeFirewallStateStore{}
	journal, err := prepareFirewallApplyJournal(
		context.Background(), commitment, noService, store,
	)
	if err != nil {
		t.Fatalf("prepare on a server without SSH: %v", err)
	}
	if !journal.NoSSHService || len(journal.SSHPorts) != 0 {
		t.Fatalf("journal = %+v", journal)
	}
	journal.RequestID = strings.Repeat("a", 32)
	if err := validateFirewallApplyJournal(journal); err != nil {
		t.Fatalf("validate the no-SSH journal: %v", err)
	}
	if len(noService.commands) != 0 || store.saves != 0 {
		t.Fatalf("preparation mutated the host: commands=%d saves=%d",
			len(noService.commands), store.saves)
	}

	// The marker is the only thing that makes an empty SSH port set valid, and
	// it can never widen a plan.
	journal.NoSSHService = false
	if err := validateFirewallApplyJournal(journal); err == nil {
		t.Fatal("an enabled journal with no SSH ports and no marker validated")
	}
	journal.NoSSHService = true
	journal.SSHPorts = []int{22}
	if err := validateFirewallApplyJournal(journal); err == nil {
		t.Fatal("a no-SSH marker validated alongside a protected SSH port")
	}

	for _, refused := range []struct {
		name   string
		runner *fakeFirewallCommandRunner
		reason string
	}{
		{
			name:   "an SSH service that is not listening",
			runner: &fakeFirewallCommandRunner{ssOut: nginxOnlyListener, sshServicePresent: true},
			reason: transport.SSHDiscoveryNotListening,
		},
		{
			name:   "a probe that could not run",
			runner: &fakeFirewallCommandRunner{ssErr: errors.New("ss permission denied")},
			reason: transport.SSHDiscoveryProbeFailed,
		},
	} {
		t.Run(refused.name, func(t *testing.T) {
			refusedStore := &fakeFirewallStateStore{}
			_, err := prepareFirewallApplyJournal(
				context.Background(), commitment, refused.runner, refusedStore,
			)
			var refusal *sshDiscoveryRefusal
			if !errors.As(err, &refusal) || refusal.reason != refused.reason {
				t.Fatalf("prepare error = %v", err)
			}
			if len(refused.runner.commands) != 0 || refusedStore.saves != 0 {
				t.Fatalf("refused preparation mutated the host: commands=%d saves=%d",
					len(refused.runner.commands), refusedStore.saves)
			}
		})
	}
}

// A firewall the operator knowingly enabled on a server without SSH has to
// come back after a reboot too. Only a proven absence restores; a presence
// probe that could not run still refuses.
func TestFirewallRestoreOnAServerWithoutSSH(t *testing.T) {
	snapshot := encodeFirewallSnapshot([]int{2083}, nil, nil)

	restored := &fakeFirewallCommandRunner{
		configuredSSHErr: errors.New("no trusted sshd executable was found"),
	}
	batch, exists, err := prepareFirewallRestoreBatch(
		restored, &fakeFirewallStateStore{data: snapshot, exists: true},
	)
	if err != nil || !exists {
		t.Fatalf("restore on a server without SSH: exists=%v err=%v", exists, err)
	}
	if !strings.Contains(batch, "tcp dport { 2083 }") {
		t.Fatalf("restored ruleset = %q", batch)
	}

	unprovable := &fakeFirewallCommandRunner{
		configuredSSHErr:     errors.New("sshd -T failed"),
		sshServicePresentErr: errors.New("systemctl is unavailable"),
	}
	if _, _, err := prepareFirewallRestoreBatch(
		unprovable, &fakeFirewallStateStore{data: snapshot, exists: true},
	); err == nil {
		t.Fatal("restore proceeded while SSH presence could not be proven")
	}
}

// An applied policy that protects no SSH port says why, so the panel can name
// the state and audit the change as the acknowledged one it was.
func TestAppliedNoSSHPolicyReportsItsReason(t *testing.T) {
	journal := &firewallApplyJournal{
		Version:      firewallApplyJournalVersion,
		RequestID:    strings.Repeat("b", 32),
		Enabled:      true,
		Persist:      true,
		TCPPorts:     []int{2083},
		NoSSHService: true,
	}
	var response FirewallStatusResponse
	populateFirewallApplyResponse(journal, &response)
	if response.SSHDiscoveryReason != transport.SSHDiscoveryNoService ||
		len(response.SSHPorts) != 0 || !response.Enabled {
		t.Fatalf("applied no-SSH policy response = %+v", response)
	}

	journal.NoSSHService = false
	journal.SSHPorts = []int{22}
	populateFirewallApplyResponse(journal, &response)
	if response.SSHDiscoveryReason != "" || len(response.SSHPorts) != 1 {
		t.Fatalf("applied policy with a proven listener = %+v", response)
	}
}
