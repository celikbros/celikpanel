//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// R-055. The VPN path is the third to ask what a failed privileged plan left
// on the host, and the first to inherit the answer instead of writing it
// again. These tests are the same four the firewall and mail paths have: a
// plan that cannot succeed reaches a terminal failure and releases the ledger,
// the reason survives a restart, an unproven host still poisons and still
// keeps the lock, and a plan that can still be finished is still finished.
//
// R-055. VPN yolu, basarisiz bir ayricalikli planin makinede ne biraktigini
// soran ucuncu yoldur ve yaniti yeniden yazmak yerine devralan ilk yoldur.

// stageCommittedVPNPeerSyncRecovery leaves the durable state an agent crash
// leaves behind: an active peer-sync job carrying its intent receipt, a
// managed durable configuration on disk, and no live worker - so the next
// manager start runs the committed recovery.
// stageCommittedVPNPeerSyncRecovery, bir agent cokmesinin biraktigi kalici
// durumu birakir.
func stageCommittedVPNPeerSyncRecovery(
	t *testing.T,
	host vpnRPCTestHost,
	durable []byte,
) (string, mutationpayload.VPNPeerSyncCommitment) {
	t.Helper()
	if err := os.WriteFile(host.configPath, durable, 0o600); err != nil {
		t.Fatal(err)
	}
	_, commitment := vpnPeerSyncRecoveryPayload(t, 55)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "vpn_peer_sync", "wireguard", commitment.Qualifier)
	intentPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitIntent, testMutationRequestID, commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	persistVPNPeerSyncTestPhase(t, manager, intentPhase)
	abandonVPNPeerSyncTestRuntime(t, manager)
	return root, commitment
}

func reloadVPNPeerSyncTestManager(t *testing.T, root string) *serviceMutationManager {
	t.Helper()
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return reloaded
}

// requireReleasedVPNHost proves the whole host is mutable again: nothing is
// poisoned, the ledger points at no active job, and an unrelated mutation can
// take the lease. That last one is the property this family of wedges takes
// away - one VPN request the machine cannot serve holding DNS, mail, sites and
// updates with it, across restarts.
// requireReleasedVPNHost, makinenin yeniden degistirilebilir oldugunu
// kanitlar: ilgisiz bir mutasyon kirayi alabilir.
func requireReleasedVPNHost(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	poisoned := manager.poisoned
	active := manager.ledger.ActiveRequestID
	manager.mu.Unlock()
	if poisoned != nil {
		t.Fatalf("VPN recovery left the manager poisoned: %v", poisoned)
	}
	if active != "" {
		t.Fatalf("VPN recovery kept the ledger active: %q", active)
	}
	unrelated, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("b", 32),
		OwnerID:   strings.Repeat("c", 32),
		Kind:      "package_install",
		Target:    "nginx",
	})
	if err != nil || unrelated == nil {
		t.Fatalf(
			"host refused an unrelated mutation after VPN recovery: job=%+v err=%v",
			unrelated, err,
		)
	}
	abandonVPNPeerSyncTestRuntime(t, manager)
}

func requireVPNPeerSyncCleanFailure(
	t *testing.T,
	job *ServiceMutationJob,
	wantCode, wantReason string,
	afterRestart bool,
) {
	t.Helper()
	if job == nil || job.Status != serviceMutationStatusFailed {
		t.Fatalf("unfinishable VPN plan did not reach a terminal failure: %+v", job)
	}
	if job.Phase != vpnPeerSyncFailedPhase {
		t.Fatalf("terminal phase = %q, want %q", job.Phase, vpnPeerSyncFailedPhase)
	}
	if strings.HasPrefix(job.Phase, vpnPeerSyncCommitPhasePrefix) {
		t.Fatalf("terminal failure kept a commit receipt: %q", job.Phase)
	}
	if job.ErrorCode != wantCode {
		t.Fatalf("terminal code = %q, want %q", job.ErrorCode, wantCode)
	}
	if !strings.Contains(job.ErrorMessage, wantReason) {
		t.Fatalf("terminal message %q does not carry the reason %q", job.ErrorMessage, wantReason)
	}
	if !strings.Contains(job.ErrorMessage, "The VPN was not otherwise changed") {
		t.Fatalf("terminal message %q does not say what was left behind", job.ErrorMessage)
	}
	warned := strings.Contains(job.ErrorMessage, "An earlier attempt was interrupted")
	if warned != afterRestart {
		t.Fatalf(
			"terminal message %q interrupted-attempt warning = %v, want %v",
			job.ErrorMessage, warned, afterRestart,
		)
	}
	if !job.LeaseExpiresAt.IsZero() || job.FinishedAt.IsZero() {
		t.Fatalf("terminal job did not release its lease: %+v", job)
	}
}

// TestVPNPeerSyncRecoveryFailsAnUntouchedPlanAndReleasesTheLedger is R-055:
// startup recovery cannot get an answer out of the host about which WireGuard
// interfaces exist, so it cannot finish the plan - but asking that question
// changed nothing, so the plan ends failed with its reason instead of being
// re-attempted at every agent start with the whole host held behind it.
func TestVPNPeerSyncRecoveryFailsAnUntouchedPlanAndReleasesTheLedger(t *testing.T) {
	host := newVPNRPCTestHost(t)
	durable := managedVPNTestConfig("original")
	root, _ := stageCommittedVPNPeerSyncRecovery(t, host, durable)
	t.Setenv("VPN_TEST_FAIL_INTERFACE_PROBE", "1")

	reloaded := reloadVPNPeerSyncTestManager(t, root)
	requireVPNPeerSyncCleanFailure(
		t,
		reloaded.status(testMutationRequestID),
		vpnPeerSyncFailedUntouchedCode,
		"WireGuard interface probe failed",
		true,
	)
	actual, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(durable) {
		t.Fatal("a recovery that reported an untouched host changed the durable configuration")
	}
	requireReleasedVPNHost(t, reloaded)
}

// TestVPNPeerSyncRecoveryFailureSurvivesAnotherRestart: the reason is durable,
// and the second boot neither re-attempts the plan nor holds the host.
func TestVPNPeerSyncRecoveryFailureSurvivesAnotherRestart(t *testing.T) {
	host := newVPNRPCTestHost(t)
	root, _ := stageCommittedVPNPeerSyncRecovery(t, host, managedVPNTestConfig("original"))
	t.Setenv("VPN_TEST_FAIL_INTERFACE_PROBE", "1")

	first := reloadVPNPeerSyncTestManager(t, root)
	requireVPNPeerSyncCleanFailure(
		t,
		first.status(testMutationRequestID),
		vpnPeerSyncFailedUntouchedCode,
		"WireGuard interface probe failed",
		true,
	)
	releasePoisonedVPNPeerSyncTestManager(first)

	second := reloadVPNPeerSyncTestManager(t, root)
	requireVPNPeerSyncCleanFailure(
		t,
		second.status(testMutationRequestID),
		vpnPeerSyncFailedUntouchedCode,
		"WireGuard interface probe failed",
		true,
	)
	requireReleasedVPNHost(t, second)
}

// TestVPNPeerSyncRecoveryStillHoldsAnUnprovenHost keeps the fail-closed rule.
// Here the recovery did reach the live interface and could not synchronise it
// to the durable configuration, so the interface may be half applied: that
// host still poisons and still retains the lock, exactly as before.
func TestVPNPeerSyncRecoveryStillHoldsAnUnprovenHost(t *testing.T) {
	host := newVPNRPCTestHost(t)
	root, _ := stageCommittedVPNPeerSyncRecovery(t, host, managedVPNTestConfig("original"))
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VPN_TEST_SYNCCONF_FAIL_ALWAYS", "1")

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if reloaded == nil || err == nil ||
		!errors.Is(err, errServiceMutationManagerPoisoned) {
		t.Fatalf("unproven host did not fail closed at startup: manager=%v err=%v", reloaded, err)
	}
	reloaded.mu.Lock()
	poisoned := reloaded.poisoned
	active := reloaded.ledger.ActiveRequestID
	retained := reloaded.poisonLock != nil ||
		(reloaded.active != nil && reloaded.active.lock != nil)
	reloaded.mu.Unlock()
	if poisoned == nil || !retained {
		t.Fatalf("unproven host was released: poisoned=%v retained=%v", poisoned, retained)
	}
	if active != testMutationRequestID {
		t.Fatalf("unproven host lost its frozen job: active=%q", active)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status == serviceMutationStatusFailed {
		t.Fatalf("unproven host reported a terminal failure: %+v", job)
	}
	releasePoisonedVPNPeerSyncTestManager(reloaded)
}

// TestVPNPeerSyncRecoveryDoesNotAbandonACompletablePlan: a plan the recovery
// can still finish is still finished, with its published receipt. Failing
// cleanly is for plans that cannot succeed, not for plans that are hard.
func TestVPNPeerSyncRecoveryDoesNotAbandonACompletablePlan(t *testing.T) {
	host := newVPNRPCTestHost(t)
	_, commitment := vpnPeerSyncRecoveryPayload(t, 55)
	published := markedVPNPeerSyncTestConfig(t, testMutationRequestID, commitment.Qualifier)
	root, _ := stageCommittedVPNPeerSyncRecovery(t, host, published)

	reloaded := reloadVPNPeerSyncTestManager(t, root)
	publishedPhase, err := formatVPNPeerSyncCommitPhase(
		vpnPeerSyncCommitPublished, testMutationRequestID, commitment.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded || job.Phase != publishedPhase {
		t.Fatalf("completable VPN plan was abandoned: %+v", job)
	}
	requireReleasedVPNHost(t, reloaded)
}

// TestVPNPeerSyncLiveApplyFailsCleanlyAndReleasesTheLedger closes the same
// wedge on the path the operator drives: the peer apply itself, not a later
// boot. The live interface refused the peer set and was put back from the
// exact bytes on disk, and that durable configuration reads back unchanged -
// so the plan is finished as failed, with its reason recorded by the agent
// rather than left to a generic "the privileged host operation did not
// complete", and the host is mutable again.
func TestVPNPeerSyncLiveApplyFailsCleanlyAndReleasesTheLedger(t *testing.T) {
	host := newVPNRPCTestHost(t)
	original := managedVPNTestConfig("original")
	if err := os.WriteFile(host.configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.liveConfigPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host.interfacePath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(
		"VPN_TEST_SYNCCONF_FAIL_ONCE_MARKER",
		filepath.Join(t.TempDir(), "sync.failed"),
	)
	peers, commitment := vpnPeerSyncRecoveryPayload(t, 56)
	binding, manager := beginVPNRPCTestMutationWithManager(
		t, "vpn_peer_sync", commitment.Qualifier,
	)
	var response SyncVPNPeersResponse
	if err := (&Agent{}).SyncVPNPeersV2(&SyncVPNPeersRequest{
		ServiceMutationBinding: binding,
		DesiredGeneration:      56,
		Peers:                  peers,
	}, &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.AppliedGeneration != 0 {
		t.Fatalf("a refused peer apply reported success: %+v", response)
	}
	if !strings.Contains(response.Error, "The committed VPN peer change could not be applied") {
		t.Fatalf("the caller was not told what happened: %q", response.Error)
	}
	requireVPNPeerSyncCleanFailure(
		t,
		manager.status(testMutationRequestID),
		vpnPeerSyncFailedRestoredCode,
		"wg syncconf failed",
		false,
	)
	durable, err := os.ReadFile(host.configPath)
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(host.liveConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(durable) != string(original) || string(live) != string(original) {
		t.Fatal("the restored classification did not put the exact live and durable state back")
	}
	requireReleasedVPNHost(t, manager)
}
