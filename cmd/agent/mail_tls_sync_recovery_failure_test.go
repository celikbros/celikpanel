//go:build linux

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

// stageCommittedMailTLSSyncRecovery leaves the exact durable state R-046 was
// found in: an intent receipt on the ledger, its journal beside it, and no
// live worker - so the next manager start runs the committed recovery.
// stageCommittedMailTLSSyncRecovery, R-046'nin bulundugu kalici durumu birakir.
func stageCommittedMailTLSSyncRecovery(
	t *testing.T,
	commitment mutationpayload.MailTLSSyncCommitment,
) (*serviceMutationManager, string) {
	t.Helper()
	manager, root := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(
		t, manager, "mail_tls_sync", "mail-tls", commitment.Qualifier,
	)
	journal := mailTLSSyncPreparedJournal(commitment)
	journal.RequestID = testMutationRequestID
	if err := writeMailTLSSyncJournal(mailTLSSyncJournalPath(manager), journal); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.Phase = journalPhase(t, mailTLSSyncCommitIntent, commitment.Qualifier)
	manager.active.job.WorkerPID = 999999999
	manager.active.job.WorkerStarted = "dead-worker"
	manager.active.job.WorkerCommand = "postconf"
	manager.active.job.UpdatedAt = manager.now()
	err := manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	abandonFirewallApplyTestRuntime(t, manager)
	return manager, root
}

func reloadMailTLSSyncTestManager(
	t *testing.T,
	root string,
) *serviceMutationManager {
	t.Helper()
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return reloaded
}

func replaceMailTLSSyncRecovery(
	t *testing.T,
	outcome mailTLSHostOutcome,
	cause error,
	calls *int,
) {
	t.Helper()
	previous := recoverMailTLSSyncHost
	recoverMailTLSSyncHost = func(
		_ context.Context,
		_ *mailTLSSyncJournal,
	) (mailTLSHostOutcome, error) {
		if calls != nil {
			*calls++
		}
		return outcome, cause
	}
	t.Cleanup(func() { recoverMailTLSSyncHost = previous })
}

// requireReleasedMailTLSHost proves the whole host is mutable again: nothing
// is poisoned, the ledger points at no active job, and an unrelated mutation
// can take the lease. That last one is the property R-046 lost - a failed mail
// step took DNS, the firewall, sites and updates with it.
// requireReleasedMailTLSHost, makinenin yeniden degistirilebilir oldugunu
// kanitlar: ilgisiz bir mutasyon kirayi alabilir.
func requireReleasedMailTLSHost(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	manager.mu.Lock()
	poisoned := manager.poisoned
	active := manager.ledger.ActiveRequestID
	manager.mu.Unlock()
	if poisoned != nil {
		t.Fatalf("mail TLS recovery left the manager poisoned: %v", poisoned)
	}
	if active != "" {
		t.Fatalf("mail TLS recovery kept the ledger active: %q", active)
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

func requireMailTLSSyncCleanFailure(
	t *testing.T,
	job *ServiceMutationJob,
	wantCode, wantReason string,
	afterRestart bool,
) {
	t.Helper()
	if job == nil || job.Status != serviceMutationStatusFailed {
		t.Fatalf("unfinishable mail TLS plan did not reach a terminal failure: %+v", job)
	}
	if job.Phase != mailTLSSyncFailedPhase {
		t.Fatalf("terminal phase = %q, want %q", job.Phase, mailTLSSyncFailedPhase)
	}
	if strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) {
		t.Fatalf("terminal failure kept a commit receipt: %q", job.Phase)
	}
	if job.ErrorCode != wantCode {
		t.Fatalf("terminal code = %q, want %q", job.ErrorCode, wantCode)
	}
	if !strings.Contains(job.ErrorMessage, wantReason) {
		t.Fatalf("terminal message %q does not carry the reason %q", job.ErrorMessage, wantReason)
	}
	if !strings.Contains(job.ErrorMessage, "packages it installed") {
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

// TestMailTLSSyncRecoveryFailsARestoredPlanAndReleasesTheLedger is R-046's
// second half: startup recovery meets a step that can never succeed on a host
// it proved it left where it found it, and the plan ends failed instead of
// being re-attempted for ever.
func TestMailTLSSyncRecoveryFailsARestoredPlanAndReleasesTheLedger(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	_, root := stageCommittedMailTLSSyncRecovery(t, commitment)
	calls := 0
	replaceMailTLSSyncRecovery(
		t,
		mailTLSHostRestored,
		errors.New("default certificate: verify default mail certificate metadata: "+
			"/etc/ssl/celikpanel/_mail/default-cert.pem group does not match "+
			"managed directory group"),
		&calls,
	)
	reloaded := reloadMailTLSSyncTestManager(t, root)
	if calls != 1 {
		t.Fatalf("recovery attempts = %d, want 1", calls)
	}
	requireMailTLSSyncCleanFailure(
		t,
		reloaded.status(testMutationRequestID),
		mailTLSSyncFailedRestoredCode,
		"group does not match managed directory group",
		true,
	)
	requireReleasedMailTLSHost(t, reloaded)
}

// TestMailTLSSyncRecoveryFailureSurvivesAnotherRestart: the reason is durable,
// and the second boot neither re-attempts the plan nor holds the host.
func TestMailTLSSyncRecoveryFailureSurvivesAnotherRestart(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	_, root := stageCommittedMailTLSSyncRecovery(t, commitment)
	calls := 0
	replaceMailTLSSyncRecovery(
		t, mailTLSHostRestored, errors.New("mail TLS step cannot converge"), &calls,
	)
	first := reloadMailTLSSyncTestManager(t, root)
	requireMailTLSSyncCleanFailure(
		t,
		first.status(testMutationRequestID),
		mailTLSSyncFailedRestoredCode,
		"mail TLS step cannot converge",
		true,
	)
	releasePoisonedFirewallApplyTestManager(first)

	second := reloadMailTLSSyncTestManager(t, root)
	if calls != 1 {
		t.Fatalf("second boot re-attempted the failed plan: attempts = %d", calls)
	}
	requireMailTLSSyncCleanFailure(
		t,
		second.status(testMutationRequestID),
		mailTLSSyncFailedRestoredCode,
		"mail TLS step cannot converge",
		true,
	)
	requireReleasedMailTLSHost(t, second)
}

// TestMailTLSSyncRecoveryStillHoldsAnUnprovenHost keeps the fail-closed rule:
// only a host the recovery proved it restored may be released. A rollback that
// could not be proved still poisons and still retains the host lock.
func TestMailTLSSyncRecoveryStillHoldsAnUnprovenHost(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	_, root := stageCommittedMailTLSSyncRecovery(t, commitment)
	replaceMailTLSSyncRecovery(
		t,
		mailTLSHostAmbiguous,
		errors.New("postfix configuration; rollback incomplete: restore SNI map"),
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

// TestMailTLSSyncRecoveryDoesNotAbandonACompletablePlan: a plan the recovery
// can still finish is still finished, with its published receipt.
func TestMailTLSSyncRecoveryDoesNotAbandonACompletablePlan(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	_, root := stageCommittedMailTLSSyncRecovery(t, commitment)
	calls := 0
	replaceMailTLSSyncRecovery(t, mailTLSHostConverged, nil, &calls)
	reloaded := reloadMailTLSSyncTestManager(t, root)
	job := reloaded.status(testMutationRequestID)
	if calls != 1 {
		t.Fatalf("completable plan was not attempted: attempts = %d", calls)
	}
	if job == nil || job.Status != serviceMutationStatusSucceeded ||
		job.Phase != journalPhase(t, mailTLSSyncCommitPublished, commitment.Qualifier) {
		t.Fatalf("completable plan was abandoned: %+v", job)
	}
	requireReleasedMailTLSHost(t, reloaded)
}

// TestMailTLSSyncLiveCommitFailsCleanlyWithoutRestart closes the same wedge on
// the path that opened it: the install's own step, not a later boot.
func TestMailTLSSyncLiveCommitFailsCleanlyWithoutRestart(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	journal, err := commitStandaloneMailTLSSyncIntent(
		ctx, mailTLSSyncPreparedJournal(commitment),
	)
	if err != nil {
		t.Fatal(err)
	}
	code, message, clean := mailTLSSyncCleanFailureText(
		mailTLSHostRestored, errors.New("mail TLS step cannot converge"), false,
	)
	if !clean {
		t.Fatal("a restored host was not classified as a clean failure")
	}
	if err := failStandaloneMailTLSSync(ctx, journal, code, message); err != nil {
		t.Fatal(err)
	}
	finishStep()
	requireMailTLSSyncCleanFailure(
		t,
		manager.status(testMutationRequestID),
		mailTLSSyncFailedRestoredCode,
		"mail TLS step cannot converge",
		false,
	)
	requireReleasedMailTLSHost(t, manager)
}

// TestMailTLSSyncCleanFailureRefusesAnUnprovenOutcome: the classifier is the
// only door to releasing the ledger, and an ambiguous host may not pass it.
func TestMailTLSSyncCleanFailureRefusesAnUnprovenOutcome(t *testing.T) {
	if _, _, clean := mailTLSSyncCleanFailureText(
		mailTLSHostAmbiguous, errors.New("rollback incomplete"), true,
	); clean {
		t.Fatal("an ambiguous host was offered a clean failure")
	}
	if _, _, clean := mailTLSSyncCleanFailureText(mailTLSHostConverged, nil, false); clean {
		t.Fatal("a converged plan was offered a clean failure")
	}
	code, message, clean := mailTLSSyncCleanFailureText(
		mailTLSHostUntouched,
		errors.New("trusted managed certificate root does not match"),
		true,
	)
	if !clean || code != mailTLSSyncFailedUntouchedCode ||
		!strings.Contains(message, "without changing anything on this server") ||
		!strings.Contains(message, "An earlier attempt was interrupted") {
		t.Fatalf(
			"untouched host classification: code=%q clean=%v message=%q",
			code, clean, message,
		)
	}
}
