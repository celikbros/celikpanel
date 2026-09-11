//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
)

func testMailHostCertificateQualifier(t *testing.T) string {
	t.Helper()
	commitment, err := mutationpayload.CanonicalMailHostCertificate(
		"panel.example.test",
		"admin@example.test",
		"unknown",
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment.Qualifier
}

func TestMailHostCertificateCommitPhaseRoundTripPreservesQualifierSlash(
	t *testing.T,
) {
	qualifier := testMailHostCertificateQualifier(t)
	for _, state := range []string{
		mailHostCertificateCommitIntent,
		mailHostCertificateCommitPublished,
	} {
		phase, err := formatMailHostCertificateCommitPhase(
			state,
			testMutationRequestID,
			"panel.example.test",
			qualifier,
		)
		if err != nil {
			t.Fatal(err)
		}
		gotState, gotRequestID, gotDomain, gotQualifier, err :=
			parseMailHostCertificateCommitPhase(phase)
		if err != nil {
			t.Fatal(err)
		}
		if gotState != state ||
			gotRequestID != testMutationRequestID ||
			gotDomain != "panel.example.test" ||
			gotQualifier != qualifier {
			t.Fatalf(
				"parsed phase=%q request=%q domain=%q qualifier=%q",
				gotState,
				gotRequestID,
				gotDomain,
				gotQualifier,
			)
		}
	}
}

func TestMailHostCertificateReceiptStrictCanonicalRoundTrip(t *testing.T) {
	receipt, err := newMailHostCertificateReceipt(
		testMutationRequestID,
		testMailHostCertificateQualifier(t),
		"panel.example.test",
		[]byte("leaf DER"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalMailHostCertificateReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeMailHostCertificateReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != receipt {
		t.Fatalf("receipt round trip=%#v want %#v", got, receipt)
	}
	if _, err := decodeMailHostCertificateReceipt(
		append([]byte(" "), raw...),
	); err == nil {
		t.Fatal("non-canonical receipt was accepted")
	}
}

func TestMailHostCertificateIntentAloneNeverWinsCommit(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	originalVerify := mailHostCertificateVerifyPublished
	mailHostCertificateVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		mailHostCertificateVerifyPublished = originalVerify
	})
	qualifier := testMailHostCertificateQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"mail_host_certificate",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssueMailHostCertificate,
			"panel.example.test",
			qualifier,
			"issue",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()

	hostPublished, err := commitStandaloneMailHostCertificateStep(
		ctx,
		func() error { return errors.New("rename refused") },
	)
	if err == nil || hostPublished {
		t.Fatalf("commit hostPublished=%v err=%v", hostPublished, err)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	job := manager.ledger.Jobs[testMutationRequestID]
	if job == nil ||
		job.Status != serviceMutationStatusRunning ||
		!strings.HasPrefix(job.Phase, mailHostCertificateCommitPhasePrefix) ||
		manager.active == nil ||
		manager.active.mailHostCertificatePublishedPhase != "" {
		t.Fatalf("intent-only mutation changed terminal outcome: %+v", job)
	}
}

func TestMailHostCertificateTerminalWriteFailurePoisonsAndRetainsLock(
	t *testing.T,
) {
	manager, _ := newMutationTestManager(t)
	originalVerify := mailHostCertificateVerifyPublished
	mailHostCertificateVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		mailHostCertificateVerifyPublished = originalVerify
	})
	qualifier := testMailHostCertificateQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"mail_host_certificate",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssueMailHostCertificate,
			"panel.example.test",
			qualifier,
			"issue",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()

	writes := 0
	manager.writeFault = func(point string) error {
		commitPhase := manager.active != nil &&
			manager.active.job != nil &&
			manager.active.job.WorkerPID == 0 &&
			strings.HasPrefix(
				manager.active.job.Phase,
				mailHostCertificateCommitPhasePrefix,
			)
		if point == serviceMutationWriteFaultBeforeRename && commitPhase {
			writes++
			if writes == 2 {
				return errors.New("injected terminal receipt failure")
			}
		}
		return nil
	}
	hostPublished, err := commitStandaloneMailHostCertificateStep(
		ctx,
		func() error { return nil },
	)
	if err == nil || !hostPublished {
		t.Fatalf("commit hostPublished=%v err=%v", hostPublished, err)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.poisoned == nil ||
		manager.active == nil ||
		manager.active.lock == nil ||
		manager.active.mailHostCertificatePublishedPhase == "" {
		t.Fatalf(
			"terminal uncertainty did not poison and retain ownership: poisoned=%v active=%+v",
			manager.poisoned,
			manager.active,
		)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
}

func TestMailHostCertificateSuccessfulCallbackRequiresExactReceipt(
	t *testing.T,
) {
	manager, _ := newMutationTestManager(t)
	qualifier := testMailHostCertificateQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"mail_host_certificate",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssueMailHostCertificate,
			"panel.example.test",
			qualifier,
			"issue",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()
	originalVerify := mailHostCertificateVerifyPublished
	mailHostCertificateVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		mailHostCertificateVerifyPublished = originalVerify
	})

	hostPublished, err := commitStandaloneMailHostCertificateStep(
		ctx,
		func() error { return nil },
	)
	if err == nil || hostPublished {
		t.Fatalf("callback-only commit published=%v err=%v", hostPublished, err)
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.poisoned == nil ||
		manager.active == nil ||
		manager.active.mailHostCertificatePublishedPhase != "" {
		t.Fatalf(
			"missing exact receipt did not retain fail-closed ownership: poisoned=%v active=%+v",
			manager.poisoned,
			manager.active,
		)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
}

func TestMailHostCertificateStageCloseNeverPromotes(t *testing.T) {
	published := false
	cleaned := false
	stage := &mailHostCertificateStage{
		publishAction: func() (bool, error) {
			published = true
			return true, nil
		},
		cleanupAction: func(wasPublished bool) error {
			if wasPublished {
				t.Fatal("unpublished stage was reported published")
			}
			cleaned = true
			return nil
		},
	}
	if err := stage.close(); err != nil {
		t.Fatal(err)
	}
	if published || !cleaned {
		t.Fatalf("stage close published=%v cleaned=%v", published, cleaned)
	}
}

func TestMailHostCertificateStageCloseAfterPublishDoesNotRemoveCurrent(
	t *testing.T,
) {
	cleanupPublished := false
	stage := &mailHostCertificateStage{
		publishAction: func() (bool, error) { return true, nil },
		cleanupAction: func(wasPublished bool) error {
			cleanupPublished = wasPublished
			return nil
		},
	}
	if err := stage.publish(); err != nil {
		t.Fatal(err)
	}
	if err := stage.close(); err != nil {
		t.Fatal(err)
	}
	if !cleanupPublished {
		t.Fatal("published stage cleanup did not preserve current")
	}
}

func TestMailHostCertificateCommitRejectsUntrackedContext(t *testing.T) {
	published, err := commitStandaloneMailHostCertificateStep(
		context.Background(),
		func() error { t.Fatal("untracked commit callback ran"); return nil },
	)
	if err == nil || published {
		t.Fatalf("untracked commit published=%v err=%v", published, err)
	}
}
