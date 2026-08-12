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

func testPanelCertificateIssueQualifier(t *testing.T) string {
	t.Helper()
	commitment, err := mutationpayload.CanonicalPanelCertificateIssue(
		"panel.example.test",
		"admin@example.test",
		managedPanelTLSDir,
		"unknown",
	)
	if err != nil {
		t.Fatal(err)
	}
	return commitment.Qualifier
}

func TestPanelCertificateIssueCommitPhaseRoundTripPreservesQualifierSlash(
	t *testing.T,
) {
	qualifier := testPanelCertificateIssueQualifier(t)
	for _, state := range []string{
		panelCertificateIssueCommitIntent,
		panelCertificateIssueCommitPublished,
	} {
		phase, err := formatPanelCertificateIssueCommitPhase(
			state,
			testMutationRequestID,
			"panel.example.test",
			qualifier,
		)
		if err != nil {
			t.Fatal(err)
		}
		gotState, gotRequestID, gotDomain, gotQualifier, err :=
			parsePanelCertificateIssueCommitPhase(phase)
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

func TestPanelCertificateIssueReceiptStrictCanonicalRoundTrip(t *testing.T) {
	receipt, err := newPanelCertificateIssueReceipt(
		testMutationRequestID,
		testPanelCertificateIssueQualifier(t),
		"panel.example.test",
		[]byte("leaf DER"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalPanelCertificateIssueReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePanelCertificateIssueReceipt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != receipt {
		t.Fatalf("receipt round trip=%#v want %#v", got, receipt)
	}
	if _, err := decodePanelCertificateIssueReceipt(
		append([]byte(" "), raw...),
	); err == nil {
		t.Fatal("non-canonical receipt was accepted")
	}
}

func TestPanelCertificateIssueIntentAloneNeverWinsCommit(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	originalVerify := panelCertificateIssueVerifyPublished
	panelCertificateIssueVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		panelCertificateIssueVerifyPublished = originalVerify
	})
	qualifier := testPanelCertificateIssueQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"panel_certificate_issue",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssuePanelCertificate,
			"panel.example.test",
			qualifier,
			"issue",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()

	hostPublished, err := commitStandalonePanelCertificateIssueStep(
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
		!strings.HasPrefix(job.Phase, panelCertificateIssueCommitPhasePrefix) ||
		manager.active == nil ||
		manager.active.panelCertificateIssuePublishedPhase != "" {
		t.Fatalf("intent-only mutation changed terminal outcome: %+v", job)
	}
}

func TestPanelCertificateIssueTerminalWriteFailurePoisonsAndRetainsLock(
	t *testing.T,
) {
	manager, _ := newMutationTestManager(t)
	originalVerify := panelCertificateIssueVerifyPublished
	panelCertificateIssueVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return true, nil
	}
	t.Cleanup(func() {
		panelCertificateIssueVerifyPublished = originalVerify
	})
	qualifier := testPanelCertificateIssueQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"panel_certificate_issue",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssuePanelCertificate,
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
				panelCertificateIssueCommitPhasePrefix,
			)
		if point == serviceMutationWriteFaultBeforeRename && commitPhase {
			writes++
			if writes == 2 {
				return errors.New("injected terminal receipt failure")
			}
		}
		return nil
	}
	hostPublished, err := commitStandalonePanelCertificateIssueStep(
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
		manager.active.panelCertificateIssuePublishedPhase == "" {
		t.Fatalf(
			"terminal uncertainty did not poison and retain ownership: poisoned=%v active=%+v",
			manager.poisoned,
			manager.active,
		)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
}

func TestPanelCertificateIssueSuccessfulCallbackRequiresExactReceipt(
	t *testing.T,
) {
	manager, _ := newMutationTestManager(t)
	qualifier := testPanelCertificateIssueQualifier(t)
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"panel_certificate_issue",
		"panel.example.test",
		qualifier,
	)
	ctx, finishStep, err := manager.acquireStep(
		ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   testMutationOwnerID,
		},
		newServiceMutationStepClaim(
			serviceMutationStepIssuePanelCertificate,
			"panel.example.test",
			qualifier,
			"issue",
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer finishStep()
	originalVerify := panelCertificateIssueVerifyPublished
	panelCertificateIssueVerifyPublished = func(
		string, string, string,
	) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() {
		panelCertificateIssueVerifyPublished = originalVerify
	})

	hostPublished, err := commitStandalonePanelCertificateIssueStep(
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
		manager.active.panelCertificateIssuePublishedPhase != "" {
		t.Fatalf(
			"missing exact receipt did not retain fail-closed ownership: poisoned=%v active=%+v",
			manager.poisoned,
			manager.active,
		)
	}
	t.Cleanup(func() { releasePoisonedVPNPeerSyncTestManager(manager) })
}

func TestPanelCertificateIssueStageCloseNeverPromotes(t *testing.T) {
	published := false
	cleaned := false
	stage := &panelCertificateIssueStage{
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

func TestPanelCertificateIssueStageCloseAfterPublishDoesNotRemoveCurrent(
	t *testing.T,
) {
	cleanupPublished := false
	stage := &panelCertificateIssueStage{
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

func TestPanelCertificateIssueCommitRejectsUntrackedContext(t *testing.T) {
	published, err := commitStandalonePanelCertificateIssueStep(
		context.Background(),
		func() error { t.Fatal("untracked commit callback ran"); return nil },
	)
	if err == nil || published {
		t.Fatalf("untracked commit published=%v err=%v", published, err)
	}
}

func TestPanelCertificateIssueStartupRejectsLegacyAndMalformedQualifiers(
	t *testing.T,
) {
	for _, packageName := range []string{
		"certbot",
		"panel-certificate-issue/v1:sha256:malformed",
	} {
		t.Run(packageName, func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			beginMutationTestJobWithIdentity(
				t,
				manager,
				"panel_certificate_issue",
				"panel.example.test",
				testPanelCertificateIssueQualifier(t),
			)
			manager.mu.Lock()
			before := cloneServiceMutationLedger(manager.ledger)
			manager.active.job.PackageName = packageName
			manager.active.job.UpdatedAt = manager.now()
			if err := manager.persistLedgerMutationLocked(before); err != nil {
				manager.mu.Unlock()
				t.Fatal(err)
			}
			manager.mu.Unlock()
			abandonVPNPeerSyncTestRuntime(t, manager)

			reloaded, err := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if err == nil ||
				reloaded == nil ||
				reloaded.poisoned == nil ||
				reloaded.poisonLock == nil {
				t.Fatalf(
					"unsafe recovery manager=%v err=%v",
					reloaded,
					err,
				)
			}
			t.Cleanup(func() {
				releasePoisonedVPNPeerSyncTestManager(reloaded)
			})
			second, secondErr := newServiceMutationManager(
				filepath.Join(root, "state"),
				filepath.Join(root, "service-mutation.lock"),
			)
			if second != nil ||
				!errors.Is(secondErr, errServiceMutationHostBusy) {
				t.Fatalf(
					"retained lock manager=%v err=%v",
					second,
					secondErr,
				)
			}
		})
	}
}
