//go:build linux

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// stageCommittedFirewallApplyRecovery leaves the exact durable state R-054 was
// found in: an intent receipt on the ledger, its journal beside it, and no
// live worker - so the next manager start runs the committed recovery.
// stageCommittedFirewallApplyRecovery, R-054'un bulundugu kalici durumu birakir.
func stageCommittedFirewallApplyRecovery(t *testing.T) (string, *firewallApplyJournal) {
	t.Helper()
	commitment := firewallApplyTestCommitment(t)
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "firewall_apply", "nftables", commitment.Qualifier,
	)
	journal := firewallApplyTestJournal(t)
	journal.PriorRestoreUnit = firewallRestoreUnitDisabled
	persistFirewallApplyTestIntent(t, manager, journal)
	abandonFirewallApplyTestRuntime(t, manager)
	return root, journal
}

func reloadFirewallApplyTestManager(t *testing.T, root string) *serviceMutationManager {
	t.Helper()
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return reloaded
}

func replaceFirewallApplyRecovery(
	t *testing.T,
	outcome firewallHostOutcome,
	cause error,
	calls *int,
) {
	t.Helper()
	previous := recoverFirewallApplyHost
	recoverFirewallApplyHost = func(
		context.Context,
		*firewallApplyJournal,
	) (firewallHostOutcome, error) {
		if calls != nil {
			*calls++
		}
		return outcome, cause
	}
	t.Cleanup(func() { recoverFirewallApplyHost = previous })
}

// requireReleasedFirewallHost proves the whole host is mutable again: nothing
// is poisoned, the ledger points at no active job, and an unrelated mutation
// can take the lease. That last one is the property R-054 lost - a firewall
// enable on a host that could not load nftables took DNS, mail, sites and
// updates with it, and a restart did not give them back.
// requireReleasedFirewallHost, makinenin yeniden degistirilebilir oldugunu
// kanitlar: ilgisiz bir mutasyon kirayi alabilir.
func requireReleasedFirewallHost(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	poisoned := manager.poisoned
	active := manager.ledger.ActiveRequestID
	manager.mu.Unlock()
	if poisoned != nil {
		t.Fatalf("firewall recovery left the manager poisoned: %v", poisoned)
	}
	if active != "" {
		t.Fatalf("firewall recovery kept the ledger active: %q", active)
	}
	unrelated, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: strings.Repeat("b", 32),
		OwnerID:   strings.Repeat("c", 32),
		Kind:      "package_install",
		Target:    "nginx",
	})
	if err != nil || unrelated == nil {
		t.Fatalf(
			"host refused an unrelated mutation after recovery: job=%+v err=%v",
			unrelated, err,
		)
	}
	abandonFirewallApplyTestRuntime(t, manager)
}

func requireFirewallApplyCleanFailure(
	t *testing.T,
	job *ServiceMutationJob,
	wantCode, wantReason string,
	afterRestart bool,
) {
	t.Helper()
	if job == nil || job.Status != serviceMutationStatusFailed {
		t.Fatalf("unfinishable firewall plan did not reach a terminal failure: %+v", job)
	}
	if job.Phase != firewallApplyFailedPhase {
		t.Fatalf("terminal phase = %q, want %q", job.Phase, firewallApplyFailedPhase)
	}
	if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
		t.Fatalf("terminal failure kept a commit receipt: %q", job.Phase)
	}
	if job.ErrorCode != wantCode {
		t.Fatalf("terminal code = %q, want %q", job.ErrorCode, wantCode)
	}
	if !strings.Contains(job.ErrorMessage, wantReason) {
		t.Fatalf("terminal message %q does not carry the reason %q", job.ErrorMessage, wantReason)
	}
	if !strings.Contains(job.ErrorMessage, "The firewall was not changed") {
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

// TestFirewallApplyRecoveryFailsARestoredPlanAndReleasesTheLedger is R-054's
// second half: startup recovery meets a plan this machine cannot serve on a
// host it proved it left where it found it, and the plan ends failed instead
// of being re-attempted at every boot until somebody reboots the machine.
func TestFirewallApplyRecoveryFailsARestoredPlanAndReleasesTheLedger(t *testing.T) {
	root, _ := stageCommittedFirewallApplyRecovery(t)
	calls := 0
	replaceFirewallApplyRecovery(
		t,
		firewallHostRestored,
		&firewallEngineError{
			fault: firewallEngineFaultModulesMissing,
			message: "nft apply failed: cache initialization failed: Invalid argument - " +
				firewallEngineRebootSentence,
		},
		&calls,
	)
	reloaded := reloadFirewallApplyTestManager(t, root)
	if calls != 1 {
		t.Fatalf("recovery attempts = %d, want 1", calls)
	}
	requireFirewallApplyCleanFailure(
		t,
		reloaded.status(testMutationRequestID),
		firewallApplyFailedRestoredCode,
		"Restart this server",
		true,
	)
	requireReleasedFirewallHost(t, reloaded)
}

// TestFirewallApplyRecoveryFailureSurvivesAnotherRestart: the reason is
// durable, and the second boot neither re-attempts the plan nor holds the host.
func TestFirewallApplyRecoveryFailureSurvivesAnotherRestart(t *testing.T) {
	root, _ := stageCommittedFirewallApplyRecovery(t)
	calls := 0
	replaceFirewallApplyRecovery(
		t,
		firewallHostUntouched,
		errors.New("nft table discovery failed: cache initialization failed: Invalid argument"),
		&calls,
	)
	first := reloadFirewallApplyTestManager(t, root)
	requireFirewallApplyCleanFailure(
		t,
		first.status(testMutationRequestID),
		firewallApplyFailedUntouchedCode,
		"cache initialization failed",
		true,
	)
	releasePoisonedFirewallApplyTestManager(first)

	second := reloadFirewallApplyTestManager(t, root)
	if calls != 1 {
		t.Fatalf("second boot re-attempted the failed plan: attempts = %d", calls)
	}
	requireFirewallApplyCleanFailure(
		t,
		second.status(testMutationRequestID),
		firewallApplyFailedUntouchedCode,
		"cache initialization failed",
		true,
	)
	requireReleasedFirewallHost(t, second)
}

// TestFirewallApplyRecoveryStillHoldsAnUnprovenHost keeps the fail-closed
// rule: only a host the recovery proved it left alone may be released. A
// ruleset that may be half applied still poisons and still retains the lock.
func TestFirewallApplyRecoveryStillHoldsAnUnprovenHost(t *testing.T) {
	root, _ := stageCommittedFirewallApplyRecovery(t)
	replaceFirewallApplyRecovery(
		t,
		firewallHostAmbiguous,
		errors.New("nft apply failed: transaction refused halfway"),
		nil,
	)
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
	releasePoisonedFirewallApplyTestManager(reloaded)
}

// TestFirewallApplyRecoveryDoesNotAbandonACompletablePlan: a plan the recovery
// can still finish is still finished, with its published receipt. Failing
// cleanly is for plans that cannot succeed, not for plans that are hard.
func TestFirewallApplyRecoveryDoesNotAbandonACompletablePlan(t *testing.T) {
	root, journal := stageCommittedFirewallApplyRecovery(t)
	calls := 0
	replaceFirewallApplyRecovery(t, firewallHostConverged, nil, &calls)
	reloaded := reloadFirewallApplyTestManager(t, root)
	job := reloaded.status(testMutationRequestID)
	if calls != 1 {
		t.Fatalf("completable plan was not attempted: attempts = %d", calls)
	}
	publishedPhase, err := formatFirewallApplyCommitPhase(
		firewallApplyCommitPublished, journal.RequestID, journal.Qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != publishedPhase {
		t.Fatalf("completable plan was abandoned: %+v", job)
	}
	requireReleasedFirewallHost(t, reloaded)
}

// TestFirewallApplyLiveCommitFailsCleanlyWithoutRestart closes the same wedge
// on the path that opened it: the operator's own click, not a later boot.
func TestFirewallApplyLiveCommitFailsCleanlyWithoutRestart(t *testing.T) {
	commitment := firewallApplyTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJobWithIdentity(
		t, manager, "firewall_apply", "nftables", commitment.Qualifier,
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
	journal, err := commitStandaloneFirewallApplyIntent(ctx, firewallApplyTestJournal(t))
	if err != nil {
		t.Fatal(err)
	}
	code, message, clean := firewallApplyCleanFailureText(
		firewallHostUntouched,
		errors.New("nft table discovery failed: cache initialization failed: Invalid argument"),
		false,
	)
	if !clean {
		t.Fatal("an untouched host was not classified as a clean failure")
	}
	if err := failStandaloneFirewallApply(ctx, journal, code, message); err != nil {
		t.Fatal(err)
	}
	finishStep()
	requireFirewallApplyCleanFailure(
		t,
		manager.status(testMutationRequestID),
		firewallApplyFailedUntouchedCode,
		"cache initialization failed",
		false,
	)
	requireReleasedFirewallHost(t, manager)
}

// TestFirewallApplyCleanFailureRefusesAnUnprovenOutcome: the classifier is the
// only door to releasing the ledger, and a host that may be half changed may
// not pass through it.
func TestFirewallApplyCleanFailureRefusesAnUnprovenOutcome(t *testing.T) {
	if _, _, clean := firewallApplyCleanFailureText(
		firewallHostAmbiguous, errors.New("nft apply failed"), true,
	); clean {
		t.Fatal("an ambiguous host was offered a clean failure")
	}
	if _, _, clean := firewallApplyCleanFailureText(
		firewallHostConverged, nil, false,
	); clean {
		t.Fatal("a converged plan was offered a clean failure")
	}
	code, message, clean := firewallApplyCleanFailureText(
		firewallHostUntouched, errors.New("cache initialization failed"), true,
	)
	if !clean || code != firewallApplyFailedUntouchedCode ||
		!strings.Contains(message, "without changing anything on this server") ||
		!strings.Contains(message, "An earlier attempt was interrupted") {
		t.Fatalf(
			"untouched host classification: code=%q clean=%v message=%q",
			code, clean, message,
		)
	}
}
