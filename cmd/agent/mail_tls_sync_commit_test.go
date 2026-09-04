//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func mailTLSSyncTestCommitment(t *testing.T) mutationpayload.MailTLSSyncCommitment {
	t.Helper()
	commitment, err := mutationpayload.CanonicalMailTLSSync(
		"/etc/ssl/celikpanel",
		"mail.panel.test",
		[]transport.MailSNIEntry{{
			Names:    []string{"example.test", "mail.example.test"},
			CertPath: "/etc/ssl/celikpanel/example.test/sha256-" + strings.Repeat("a", 64) + "/fullchain.pem",
			KeyPath:  "/etc/ssl/celikpanel/example.test/sha256-" + strings.Repeat("a", 64) + "/privkey.pem",
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment
}

func mailTLSSyncPreparedJournal(commitment mutationpayload.MailTLSSyncCommitment) *mailTLSSyncJournal {
	journal := &mailTLSSyncJournal{
		Version:     mailTLSSyncJournalVersion,
		Qualifier:   commitment.Qualifier,
		ManagedRoot: commitment.ManagedRoot,
		Myhostname:  commitment.Myhostname,
		SNI:         append([]transport.MailSNIEntry(nil), commitment.SNI...),
	}
	for index := range journal.SNI {
		journal.SNI[index].Names = append([]string(nil), journal.SNI[index].Names...)
	}
	return journal
}

func acquireMailTLSSyncTestStep(
	t *testing.T,
	manager *serviceMutationManager,
	commitment mutationpayload.MailTLSSyncCommitment,
) (context.Context, func()) {
	t.Helper()
	beginMutationTestJobWithIdentity(
		t, manager, "mail_tls_sync", "mail-tls", commitment.Qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		mutationTestBinding(),
		newServiceMutationStepClaim(
			serviceMutationStepSyncMailTLS,
			"mail-tls",
			commitment.Qualifier,
			"sync",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, finishStep
}

func TestMailTLSSyncIntentCancelExpiryAndFinishCannotReportFailure(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	defer finishStep()
	journal, err := commitStandaloneMailTLSSyncIntent(
		ctx, mailTLSSyncPreparedJournal(commitment),
	)
	if err != nil {
		t.Fatal(err)
	}

	cancelled, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID: testMutationRequestID, ExpectedOwner: testMutationOwnerID,
	})
	if err != nil || cancelled == nil || cancelled.Status != serviceMutationStatusRunning ||
		!strings.HasPrefix(cancelled.Phase, mailTLSSyncCommitPhasePrefix) {
		t.Fatalf("cancel changed committed mail TLS job: job=%+v err=%v", cancelled, err)
	}
	manager.mu.Lock()
	runtime := manager.active
	manager.mu.Unlock()
	manager.expire(runtime)
	if expired := manager.status(testMutationRequestID); expired == nil ||
		expired.Status != serviceMutationStatusRunning {
		t.Fatalf("expiry changed committed mail TLS job: %+v", expired)
	}
	finished, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, Success: false,
	})
	if err == nil || finished == nil || finished.Status != serviceMutationStatusRunning {
		t.Fatalf("Finish(false) job=%+v err=%v", finished, err)
	}
	if err := publishStandaloneMailTLSSync(ctx, journal); err != nil {
		t.Fatal(err)
	}
	terminal := manager.status(testMutationRequestID)
	if terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("terminal job=%+v", terminal)
	}
}

func TestMailTLSSyncPublishRequiresExactPersistedJournal(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	defer finishStep()
	journal, err := commitStandaloneMailTLSSyncIntent(
		ctx, mailTLSSyncPreparedJournal(commitment),
	)
	if err != nil {
		t.Fatal(err)
	}
	other, err := mutationpayload.CanonicalMailTLSSync(
		commitment.ManagedRoot, "other.panel.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	mismatched := mailTLSSyncPreparedJournal(other)
	mismatched.RequestID = testMutationRequestID
	if err := writeMailTLSSyncJournal(mailTLSSyncJournalPath(manager), mismatched); err != nil {
		t.Fatal(err)
	}
	if err := publishStandaloneMailTLSSync(ctx, journal); err == nil ||
		!errors.Is(err, errServiceMutationManagerPoisoned) {
		t.Fatalf("mismatched journal publish err=%v", err)
	}
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	retained := manager.active != nil && manager.active.lock != nil
	manager.mu.Unlock()
	if !poisoned || !retained {
		t.Fatalf("journal mismatch did not poison and retain flock: poisoned=%v retained=%v", poisoned, retained)
	}
	releasePoisonedFirewallApplyTestManager(manager)
}

func TestMailTLSSyncPublishFailureClearsEverySuccessField(t *testing.T) {
	response := SecureMailTLSResponse{
		Configured:  true,
		DefaultCert: "stale-cert",
		SNICount:    99,
		Detail:      "stale detail",
		Error:       "stale error",
	}
	if publishMailTLSSyncResponse(context.Background(), nil, &response) {
		t.Fatal("publication without the durable tracker reported success")
	}
	want := SecureMailTLSResponse{Error: mailTLSSyncReceiptUnavailableError}
	if response != want {
		t.Fatalf("publish failure response = %+v, want %+v", response, want)
	}
}

func TestMailTLSSyncLiveWorkerPreservesIntentThenRecoversForward(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	manager, root := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	journal, err := commitStandaloneMailTLSSyncIntent(
		ctx, mailTLSSyncPreparedJournal(commitment),
	)
	if err != nil {
		t.Fatal(err)
	}
	finishStep()
	started, err := serviceMutationProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.WorkerPID = os.Getpid()
	manager.active.job.WorkerStarted = started
	manager.active.job.WorkerCommand = "mail-tls-test"
	manager.active.job.UpdatedAt = manager.now()
	err = manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	abandonFirewallApplyTestRuntime(t, manager)

	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.mu.Lock()
	orphaned := cloneServiceMutationJob(reloaded.ledger.Jobs[testMutationRequestID])
	reloaded.mu.Unlock()
	if orphaned == nil || orphaned.Status != serviceMutationStatusOrphaned ||
		orphaned.Phase != journalPhase(t, mailTLSSyncCommitIntent, commitment.Qualifier) {
		t.Fatalf("live worker lost durable intent: %+v", orphaned)
	}

	previousRecovery := recoverMailTLSSyncHost
	recoveryCalls := 0
	recoverMailTLSSyncHost = func(
		ctx context.Context,
		got *mailTLSSyncJournal,
	) (mailTLSHostOutcome, error) {
		recoveryCalls++
		tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
		if tracker == nil || !tracker.allowCancellingRecovery ||
			!equalMailTLSSyncJournals(got, journal) {
			return mailTLSHostAmbiguous, errors.New("recovery lost exact tracked journal")
		}
		return mailTLSHostConverged, nil
	}
	t.Cleanup(func() { recoverMailTLSSyncHost = previousRecovery })
	reloaded.mu.Lock()
	before = cloneServiceMutationLedger(reloaded.ledger)
	reloaded.ledger.Jobs[testMutationRequestID].WorkerPID = 999999999
	reloaded.ledger.Jobs[testMutationRequestID].WorkerStarted = "dead-worker"
	reloaded.ledger.Jobs[testMutationRequestID].WorkerCommand = "mail-tls-test"
	reloaded.ledger.Jobs[testMutationRequestID].UpdatedAt = reloaded.now()
	err = reloaded.persistLedgerMutationLocked(before)
	reloaded.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	terminal := reloaded.status(testMutationRequestID)
	if recoveryCalls != 1 || terminal == nil || terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("forward recovery calls=%d terminal=%+v", recoveryCalls, terminal)
	}
}

func journalPhase(t *testing.T, state, qualifier string) string {
	t.Helper()
	phase, err := formatMailTLSSyncCommitPhase(state, testMutationRequestID, qualifier)
	if err != nil {
		t.Fatal(err)
	}
	return phase
}

func TestPrepareMailTLSSyncRejectsTamperBeforeIntentOrCommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CELIKPANEL_CUSTOM_CERT_ROOT", root)
	snapshot := createManagedMailTLSSnapshot(t, root, "example.test", "")
	commitment, err := mutationpayload.CanonicalMailTLSSync(
		root, "mail.panel.test", []transport.MailSNIEntry{validMailSNIEntry(snapshot)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot.keyPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousLookup := lookupMailTLSCommand
	lookupCalls := 0
	lookupMailTLSCommand = func(name string) (string, error) {
		lookupCalls++
		return "/bin/" + name, nil
	}
	t.Cleanup(func() { lookupMailTLSCommand = previousLookup })
	manager, _ := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	defer finishStep()
	if _, err := prepareMailTLSSyncJournal(ctx, commitment); err == nil ||
		!strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("tampered prepare err=%v", err)
	}
	manager.mu.Lock()
	job := cloneServiceMutationJob(manager.active.job)
	manager.mu.Unlock()
	if lookupCalls != 0 || job.Phase != "leased" {
		t.Fatalf("tamper reached command/intent: lookups=%d job=%+v", lookupCalls, job)
	}
}

func TestMailTLSSyncJournalContainsNoPriorPrivateKey(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	journal := mailTLSSyncPreparedJournal(commitment)
	journal.RequestID = testMutationRequestID
	raw, err := encodeMailTLSSyncJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"prior", "PRIVATE KEY", "default-key.pem"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("journal retained forbidden secret material %q: %s", forbidden, raw)
		}
	}
}

func TestMailTLSSyncRecoveryClearsStaleWorkerBeforeTrackedCommands(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
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
	manager.active.job.WorkerStarted = "stale-worker"
	manager.active.job.WorkerCommand = "postconf"
	manager.active.job.UpdatedAt = manager.now()
	err := manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	abandonFirewallApplyTestRuntime(t, manager)

	previousRecovery := recoverMailTLSSyncHost
	recoverMailTLSSyncHost = func(
		ctx context.Context,
		_ *mailTLSSyncJournal,
	) (mailTLSHostOutcome, error) {
		tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
		if tracker == nil {
			return mailTLSHostAmbiguous, errors.New("missing recovery tracker")
		}
		tracker.manager.mu.Lock()
		defer tracker.manager.mu.Unlock()
		job := tracker.runtime.job
		if job.Status != serviceMutationStatusCancelling || job.WorkerPID != 0 ||
			job.WorkerStarted != "" || job.WorkerCommand != "" {
			return mailTLSHostAmbiguous, errors.New(
				"stale worker was not durably cleared before recovery",
			)
		}
		return mailTLSHostConverged, nil
	}
	t.Cleanup(func() { recoverMailTLSSyncHost = previousRecovery })
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"), filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusSucceeded {
		t.Fatalf("recovered job=%+v", job)
	}
}

func TestMailTLSSyncExpiryGuardRemainsStableAcrossClockAdvance(t *testing.T) {
	commitment := mailTLSSyncTestCommitment(t)
	manager, _ := newMutationTestManager(t)
	ctx, finishStep := acquireMailTLSSyncTestStep(t, manager, commitment)
	defer finishStep()
	if _, err := commitStandaloneMailTLSSyncIntent(ctx, mailTLSSyncPreparedJournal(commitment)); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	runtime := manager.active
	manager.now = func() time.Time { return runtime.job.DeadlineAt.Add(time.Hour) }
	manager.mu.Unlock()
	manager.expire(runtime)
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusRunning ||
		!strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) {
		t.Fatalf("clock expiry changed committed job=%+v", job)
	}
}
