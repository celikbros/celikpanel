//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func firewallApplyTestCommitment(t *testing.T) mutationpayload.FirewallApplyCommitment {
	t.Helper()
	commitment, err := mutationpayload.CanonicalFirewallApply(
		true,
		true,
		[]int{443, 80},
		[]int{53},
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func firewallApplyTestJournal(t *testing.T) *firewallApplyJournal {
	t.Helper()
	commitment := firewallApplyTestCommitment(t)
	return &firewallApplyJournal{
		Version:   firewallApplyJournalVersion,
		RequestID: testMutationRequestID,
		Qualifier: commitment.Qualifier,
		Enabled:   commitment.Enabled,
		Persist:   commitment.Persist,
		TCPPorts:  commitment.TCPPorts,
		UDPPorts:  commitment.UDPPorts,
		SSHPorts:  []int{22, 2222},
	}
}

func TestFirewallApplyCommitPhaseRoundTripKeepsSlashQualifier(t *testing.T) {
	qualifier := firewallApplyTestCommitment(t).Qualifier
	for _, state := range []string{
		firewallApplyCommitIntent,
		firewallApplyCommitPublished,
	} {
		phase, err := formatFirewallApplyCommitPhase(
			state,
			testMutationRequestID,
			qualifier,
		)
		if err != nil {
			t.Fatal(err)
		}
		gotState, requestID, gotQualifier, err := parseFirewallApplyCommitPhase(phase)
		if err != nil {
			t.Fatal(err)
		}
		if gotState != state || requestID != testMutationRequestID ||
			gotQualifier != qualifier {
			t.Fatalf(
				"phase roundtrip=(%q,%q,%q), want=(%q,%q,%q)",
				gotState,
				requestID,
				gotQualifier,
				state,
				testMutationRequestID,
				qualifier,
			)
		}
	}
}

func TestCanonicalFirewallRulesetReadbackAcceptsRealNFTLoopbackRendering(t *testing.T) {
	expected := buildFirewallRuleset(false, []int{22, 53, 80, 443}, []int{53})
	actual := `table inet celikpanel_fw {
	chain input {
		type filter hook input priority filter; policy drop;
		iif "lo" accept
		ct state established,related accept
		ct state invalid drop
		meta l4proto icmp accept
		meta l4proto ipv6-icmp accept
		tcp dport { 22, 53, 80, 443 } accept
		udp dport 53 accept
	}
}
`
	want, err := canonicalFirewallRulesetReadback([]byte(expected))
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonicalFirewallRulesetReadback([]byte(actual))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("canonical real nft readback:\n%s\nwant:\n%s", got, want)
	}
}

func TestCanonicalFirewallRulesetReadbackRejectsDuplicateOrUnexpectedInterface(t *testing.T) {
	for name, rule := range map[string]string{
		"duplicate loopback":   "iif lo accept\niif \"lo\" accept",
		"unexpected interface": "iif eth0 accept",
	} {
		t.Run(name, func(t *testing.T) {
			rules := strings.Replace(
				buildFirewallRuleset(false, []int{22}, nil),
				"iif lo accept",
				rule,
				1,
			)
			if _, err := canonicalFirewallRulesetReadback([]byte(rules)); err == nil {
				t.Fatal("unsafe input-interface readback was accepted")
			}
		})
	}
}

func TestFirewallApplyJournalRoundTripAndStrictValidation(t *testing.T) {
	journal := firewallApplyTestJournal(t)
	raw, err := encodeFirewallApplyJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFirewallApplyJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !equalFirewallApplyJournals(decoded, journal) {
		t.Fatalf("journal roundtrip got=%+v want=%+v", decoded, journal)
	}
	for name, mutate := range map[string]func(*firewallApplyJournal){
		"wrong request": func(value *firewallApplyJournal) {
			value.RequestID = "not-an-id"
		},
		"unsorted TCP": func(value *firewallApplyJournal) {
			value.TCPPorts = []int{443, 80}
		},
		"unsorted SSH": func(value *firewallApplyJournal) {
			value.SSHPorts = []int{2222, 22}
		},
		"qualifier mismatch": func(value *firewallApplyJournal) {
			value.TCPPorts = []int{81, 443}
		},
		"disabled hidden ports": func(value *firewallApplyJournal) {
			value.Enabled = false
		},
		"disabled prior snapshot": func(value *firewallApplyJournal) {
			disabled, disabledErr := mutationpayload.CanonicalFirewallApply(
				false,
				true,
				nil,
				nil,
			)
			if disabledErr != nil {
				t.Fatal(disabledErr)
			}
			value.Enabled = false
			value.Persist = true
			value.TCPPorts = nil
			value.UDPPorts = nil
			value.SSHPorts = nil
			value.Qualifier = disabled.Qualifier
			value.PriorSnapshotExists = true
			value.PriorSnapshot = encodeFirewallSnapshot(
				[]int{80},
				nil,
				[]int{22},
			)
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneFirewallApplyJournal(journal)
			mutate(changed)
			if _, err := encodeFirewallApplyJournal(changed); err == nil {
				t.Fatal("invalid firewall journal was accepted")
			}
		})
	}
	if _, err := decodeFirewallApplyJournal(append(raw, '\n')); err == nil {
		t.Fatal("noncanonical firewall journal bytes were accepted")
	}
}

func TestLegacyApplyFirewallIsStableZeroTouchStub(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	before := cloneServiceMutationLedger(manager.ledger)
	var response FirewallStatusResponse
	if err := (&Agent{}).ApplyFirewall(
		&ApplyFirewallRequest{
			Enabled:  true,
			Persist:  true,
			TCPPorts: []int{80},
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "unsupported") {
		t.Fatalf("legacy response=%+v", response)
	}
	if manager.active != nil || manager.ledger.ActiveRequestID != "" ||
		len(manager.ledger.Jobs) != len(before.Jobs) {
		t.Fatal("legacy firewall RPC touched the durable manager")
	}
}

func TestFirewallApplyBeginRequiresCanonicalPayloadQualifierPreLock(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	valid := firewallApplyTestCommitment(t).Qualifier
	tests := []struct {
		name        string
		kind        string
		target      string
		packageName string
	}{
		{name: "legacy apply", kind: "firewall_apply", target: "nftables"},
		{name: "legacy sync", kind: "firewall_sync", target: "nftables"},
		{name: "wrong target", kind: "firewall_apply", target: "iptables", packageName: valid},
		{name: "malformed qualifier", kind: "firewall_apply", target: "nftables", packageName: valid[:len(valid)-1]},
		{name: "uppercase qualifier", kind: "firewall_apply", target: "nftables", packageName: strings.ToUpper(valid)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, err := manager.begin(&ServiceMutationBeginRequest{
				RequestID:   testMutationRequestID,
				OwnerID:     testMutationOwnerID,
				Kind:        test.kind,
				Target:      test.target,
				PackageName: test.packageName,
			})
			if err == nil || job != nil {
				t.Fatalf("unsafe begin job=%+v err=%v", job, err)
			}
			if manager.active != nil || manager.ledger.ActiveRequestID != "" ||
				len(manager.ledger.Jobs) != 0 {
				t.Fatal("invalid firewall qualifier occupied the durable lease")
			}
		})
	}
}

func TestFirewallApplyIntentCancelAndFinishCannotReportFailure(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"firewall_apply",
		"nftables",
		commitment.Qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		mutationTestBinding(),
		newServiceMutationStepClaim(
			serviceMutationStepApplyFirewall,
			"nftables",
			commitment.Qualifier,
			serviceMutationFirewallEnablePersisted,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()
	journal, err := commitStandaloneFirewallApplyIntent(
		ctx,
		&firewallApplyJournal{
			Version:   firewallApplyJournalVersion,
			Qualifier: commitment.Qualifier,
			Enabled:   true,
			Persist:   true,
			TCPPorts:  commitment.TCPPorts,
			UDPPorts:  commitment.UDPPorts,
			SSHPorts:  []int{22},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:     testMutationRequestID,
		ExpectedOwner: testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled == nil || cancelled.Status != serviceMutationStatusRunning ||
		!strings.HasPrefix(cancelled.Phase, firewallApplyCommitPhasePrefix) {
		t.Fatalf("cancel changed committed firewall job: %+v", cancelled)
	}
	finished, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	})
	if err == nil || finished == nil ||
		finished.Status != serviceMutationStatusRunning {
		t.Fatalf("finish(false) job=%+v err=%v", finished, err)
	}
	if err := publishStandaloneFirewallApply(ctx, journal); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("terminal job=%+v", terminal)
	}
}

func abandonFirewallApplyTestRuntime(
	t *testing.T,
	manager *serviceMutationManager,
) {
	t.Helper()
	manager.mu.Lock()
	runtime := manager.active
	if runtime == nil {
		manager.mu.Unlock()
		t.Fatal("test mutation has no active runtime")
	}
	runtime.cancel()
	manager.active = nil
	manager.mu.Unlock()
	if err := runtime.lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func releasePoisonedFirewallApplyTestManager(manager *serviceMutationManager) {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	var locks []*serviceMutationFileLock
	if manager.active != nil {
		manager.active.cancel()
		locks = append(locks, manager.active.lock)
		manager.active = nil
	}
	if manager.poisonLock != nil {
		locks = append(locks, manager.poisonLock)
		manager.poisonLock = nil
	}
	manager.mu.Unlock()
	seen := make(map[*serviceMutationFileLock]bool)
	for _, lock := range locks {
		if lock != nil && !seen[lock] {
			_ = lock.Close()
			seen[lock] = true
		}
	}
}

func persistFirewallApplyTestIntent(
	t *testing.T,
	manager *serviceMutationManager,
	journal *firewallApplyJournal,
) {
	t.Helper()
	if err := writeFirewallApplyJournal(
		firewallApplyJournalPath(manager),
		journal,
	); err != nil {
		t.Fatal(err)
	}
	phase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitIntent,
		journal.RequestID,
		journal.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.Phase = phase
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
}

func TestFirewallApplyStartupRecoveryCompletesExactIntent(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"firewall_apply",
		"nftables",
		commitment.Qualifier,
	)
	journal := firewallApplyTestJournal(t)
	persistFirewallApplyTestIntent(t, manager, journal)
	abandonFirewallApplyTestRuntime(t, manager)

	previousRecovery := recoverFirewallApplyHost
	calls := 0
	recoverFirewallApplyHost = func(
		ctx context.Context,
		got *firewallApplyJournal,
	) error {
		calls++
		if ctx.Err() != nil || !equalFirewallApplyJournals(got, journal) {
			return errors.New("recovery received the wrong journal")
		}
		tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
		if tracker == nil || !tracker.allowCancellingRecovery {
			return errors.New("recovery command context is not tracked")
		}
		return nil
	}
	t.Cleanup(func() { recoverFirewallApplyHost = previousRecovery })

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if calls != 1 || job == nil || job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("recovery calls=%d job=%+v", calls, job)
	}
	state, requestID, qualifier, err := parseFirewallApplyCommitPhase(job.Phase)
	if err != nil || state != firewallApplyCommitPublished ||
		requestID != testMutationRequestID ||
		qualifier != commitment.Qualifier {
		t.Fatalf("terminal receipt=(%q,%q,%q) err=%v", state, requestID, qualifier, err)
	}
}

func TestFirewallApplyStartupPreIntentDoesNotTouchHost(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"firewall_apply",
		"nftables",
		commitment.Qualifier,
	)
	abandonFirewallApplyTestRuntime(t, manager)
	previousRecovery := recoverFirewallApplyHost
	calls := 0
	recoverFirewallApplyHost = func(
		context.Context,
		*firewallApplyJournal,
	) error {
		calls++
		return nil
	}
	t.Cleanup(func() { recoverFirewallApplyHost = previousRecovery })

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if calls != 0 || job == nil || job.Status != serviceMutationStatusFailed {
		t.Fatalf("pre-intent calls=%d job=%+v", calls, job)
	}
}

func TestFirewallApplyStartupMissingJournalPoisonsAndRetainsLock(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"firewall_apply",
		"nftables",
		commitment.Qualifier,
	)
	journal := firewallApplyTestJournal(t)
	persistFirewallApplyTestIntent(t, manager, journal)
	if err := os.Remove(firewallApplyJournalPath(manager)); err != nil {
		t.Fatal(err)
	}
	abandonFirewallApplyTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("missing journal manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() { releasePoisonedFirewallApplyTestManager(reloaded) })
	second, secondErr := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if second != nil || !errors.Is(secondErr, errServiceMutationHostBusy) {
		t.Fatalf("retained lock manager=%v err=%v", second, secondErr)
	}
}

func TestFirewallApplyStartupLegacyActiveJobPoisonsAndRetainsLock(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"firewall_apply",
		"nftables",
		commitment.Qualifier,
	)
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.PackageName = ""
	manager.active.job.UpdatedAt = manager.now()
	if err := manager.persistLedgerMutationLocked(before); err != nil {
		manager.mu.Unlock()
		t.Fatal(err)
	}
	manager.mu.Unlock()
	abandonFirewallApplyTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err == nil || reloaded == nil || reloaded.poisoned == nil {
		t.Fatalf("legacy recovery manager=%v err=%v", reloaded, err)
	}
	t.Cleanup(func() { releasePoisonedFirewallApplyTestManager(reloaded) })
}
