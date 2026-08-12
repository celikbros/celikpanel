//go:build linux

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func panelCertificateIntegrationQualifier(character string) string {
	return "panel-certificate-issue/v1:sha256:" + strings.Repeat(character, 64)
}

func TestPanelCertificateBeginRejectsInvalidIdentityBeforeHostLock(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	lock, err := acquireServiceMutationFileLock(manager.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	valid := panelCertificateIntegrationQualifier("0")
	tests := []struct {
		name        string
		target      string
		packageName string
	}{
		{name: "legacy certbot package", target: "panel.example.test", packageName: "certbot"},
		{name: "omitted qualifier", target: "panel.example.test"},
		{name: "malformed qualifier", target: "panel.example.test", packageName: valid[:len(valid)-1]},
		{name: "uppercase digest", target: "panel.example.test", packageName: panelCertificateIntegrationQualifier("A")},
		{name: "noncanonical target", target: "Panel.Example.Test", packageName: valid},
		{name: "trailing dot target", target: "panel.example.test.", packageName: valid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, beginErr := manager.begin(&ServiceMutationBeginRequest{
				RequestID:   testMutationRequestID,
				OwnerID:     testMutationOwnerID,
				Kind:        "panel_certificate_issue",
				Target:      test.target,
				PackageName: test.packageName,
			})
			if beginErr == nil || job != nil ||
				!strings.Contains(beginErr.Error(), "invalid panel certificate") {
				t.Fatalf("unsafe begin job=%+v err=%v", job, beginErr)
			}
			if manager.active != nil || manager.ledger.ActiveRequestID != "" ||
				len(manager.ledger.Jobs) != 0 {
				t.Fatal("invalid certificate identity occupied the durable lease")
			}
		})
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   testMutationRequestID,
		OwnerID:     testMutationOwnerID,
		Kind:        "panel_certificate_issue",
		Target:      "panel.example.test",
		PackageName: valid,
	})
	if err != nil || job == nil {
		t.Fatalf("canonical certificate begin job=%+v err=%v", job, err)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func panelCertificateIntegrationLedgerJob(
	t *testing.T,
	status, phase, qualifier string,
) serviceMutationLedger {
	t.Helper()
	started := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	job := &ServiceMutationJob{
		RequestID:   testMutationRequestID,
		OwnerID:     testMutationOwnerID,
		Kind:        "panel_certificate_issue",
		Target:      "panel.example.test",
		PackageName: qualifier,
		Status:      status,
		Phase:       phase,
		Attempt:     1,
		StartedAt:   started,
		UpdatedAt:   started.Add(time.Minute),
		DeadlineAt:  started.Add(time.Hour),
	}
	ledger := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{job.RequestID: job},
	}
	if serviceMutationStatusActive(status) {
		job.LeaseExpiresAt = started.Add(10 * time.Minute)
		ledger.ActiveRequestID = job.RequestID
	} else {
		job.FinishedAt = started.Add(2 * time.Minute)
	}
	return ledger
}

func TestPanelCertificateCommitPhaseLedgerBindingIsExact(t *testing.T) {
	qualifier := panelCertificateIntegrationQualifier("0")
	intent, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitIntent,
		testMutationRequestID,
		"panel.example.test",
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	published, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitPublished,
		testMutationRequestID,
		"panel.example.test",
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		ledger serviceMutationLedger
		valid  bool
	}{
		{name: "running intent", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusRunning, intent, qualifier), valid: true},
		{name: "cancelling intent", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusCancelling, intent, qualifier), valid: true},
		{name: "published success", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusSucceeded, published, qualifier), valid: true},
		{name: "orphaned intent", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusOrphaned, intent, qualifier)},
		{name: "failed intent", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusFailed, intent, qualifier)},
		{name: "running published", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusRunning, published, qualifier)},
		{name: "failed published", ledger: panelCertificateIntegrationLedgerJob(t, serviceMutationStatusFailed, published, qualifier)},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateServiceMutationLedger(&test.ledger)
			if test.valid && err != nil {
				t.Fatalf("valid certificate receipt rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid certificate receipt/status pair was accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*ServiceMutationJob){
		"kind":   func(job *ServiceMutationJob) { job.Kind = "service_install" },
		"target": func(job *ServiceMutationJob) { job.Target = "other.example.test" },
		"package": func(job *ServiceMutationJob) {
			job.PackageName = panelCertificateIntegrationQualifier("1")
		},
	} {
		t.Run("identity mismatch "+name, func(t *testing.T) {
			ledger := panelCertificateIntegrationLedgerJob(
				t, serviceMutationStatusRunning, intent, qualifier,
			)
			mutate(ledger.Jobs[testMutationRequestID])
			if err := validateServiceMutationLedger(&ledger); err == nil {
				t.Fatal("certificate receipt identity mismatch was accepted")
			}
		})
	}
}

func beginPanelCertificateIntegrationRuntime(
	t *testing.T,
) (*serviceMutationManager, *serviceMutationRuntime, string, string) {
	t.Helper()
	manager, _ := newMutationTestManager(t)
	qualifier := panelCertificateIntegrationQualifier("0")
	beginMutationTestJobWithIdentity(
		t,
		manager,
		"panel_certificate_issue",
		"panel.example.test",
		qualifier,
	)
	intent, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitIntent,
		testMutationRequestID,
		"panel.example.test",
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	published, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitPublished,
		testMutationRequestID,
		"panel.example.test",
		qualifier,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.active.job.Phase = intent
	runtime := manager.active
	manager.mu.Unlock()
	return manager, runtime, intent, published
}

func TestPanelCertificateIntentSurvivesHeartbeatCancelAndExpiry(t *testing.T) {
	t.Run("heartbeat", func(t *testing.T) {
		manager, _, intent, _ := beginPanelCertificateIntegrationRuntime(t)
		job, err := manager.heartbeat(&ServiceMutationHeartbeatRequest{
			RequestID: testMutationRequestID,
			OwnerID:   testMutationOwnerID,
			Phase:     "caller-progress",
		})
		if err != nil || job == nil || job.Phase != intent {
			t.Fatalf("heartbeat job=%+v err=%v", job, err)
		}
		if _, err := manager.finish(&ServiceMutationFinishRequest{
			RequestID: testMutationRequestID,
			OwnerID:   testMutationOwnerID,
			Success:   false,
		}); err != nil {
			t.Fatal(err)
		}
	})
	for _, test := range []struct {
		name string
		run  func(*serviceMutationManager, *serviceMutationRuntime) (*ServiceMutationJob, error)
	}{
		{
			name: "cancel",
			run: func(manager *serviceMutationManager, _ *serviceMutationRuntime) (*ServiceMutationJob, error) {
				return manager.cancelJob(&ServiceMutationCancelRequest{
					RequestID:     testMutationRequestID,
					ExpectedOwner: testMutationOwnerID,
				})
			},
		},
		{
			name: "expiry",
			run: func(manager *serviceMutationManager, runtime *serviceMutationRuntime) (*ServiceMutationJob, error) {
				manager.expire(runtime)
				return manager.status(testMutationRequestID), nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, runtime, intent, _ := beginPanelCertificateIntegrationRuntime(t)
			manager.mu.Lock()
			runtime.steps = 1
			manager.mu.Unlock()
			job, err := test.run(manager, runtime)
			if err != nil || job == nil ||
				job.Status != serviceMutationStatusCancelling ||
				job.Phase != intent {
				t.Fatalf("%s job=%+v err=%v", test.name, job, err)
			}
			manager.mu.Lock()
			runtime.steps = 0
			manager.mu.Unlock()
			manager.finishExpired(runtime)
		})
	}
}

func TestPanelCertificatePublishedReceiptWinsEveryTerminalRace(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*serviceMutationManager, *serviceMutationRuntime) (*ServiceMutationJob, error)
	}{
		{
			name: "heartbeat",
			run: func(manager *serviceMutationManager, _ *serviceMutationRuntime) (*ServiceMutationJob, error) {
				return manager.heartbeat(&ServiceMutationHeartbeatRequest{
					RequestID: testMutationRequestID,
					OwnerID:   testMutationOwnerID,
				})
			},
		},
		{
			name: "cancel",
			run: func(manager *serviceMutationManager, _ *serviceMutationRuntime) (*ServiceMutationJob, error) {
				return manager.cancelJob(&ServiceMutationCancelRequest{
					RequestID:     testMutationRequestID,
					ExpectedOwner: testMutationOwnerID,
				})
			},
		},
		{
			name: "finish false",
			run: func(manager *serviceMutationManager, _ *serviceMutationRuntime) (*ServiceMutationJob, error) {
				return manager.finish(&ServiceMutationFinishRequest{
					RequestID: testMutationRequestID,
					OwnerID:   testMutationOwnerID,
					Success:   false,
				})
			},
		},
		{
			name: "expiry",
			run: func(manager *serviceMutationManager, runtime *serviceMutationRuntime) (*ServiceMutationJob, error) {
				manager.expire(runtime)
				return manager.status(testMutationRequestID), nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, runtime, _, published := beginPanelCertificateIntegrationRuntime(t)
			manager.mu.Lock()
			runtime.panelCertificateIssuePublishedPhase = published
			manager.mu.Unlock()
			job, err := test.run(manager, runtime)
			if err != nil || job == nil ||
				job.Status != serviceMutationStatusSucceeded ||
				job.Phase != published {
				t.Fatalf("%s job=%+v err=%v", test.name, job, err)
			}
		})
	}
}

func TestPanelCertificateRecoveryIsWiredBeforeGenericOrphanFailure(t *testing.T) {
	raw, err := os.ReadFile("service_mutation_rpc.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	if strings.Count(source, "recoverPersistedPanelCertificateIssueLocked(job, lock)") != 2 {
		t.Fatal("panel certificate recovery must be wired exactly once in startup and orphan resolution")
	}
	startupStart := strings.Index(source, "func (m *serviceMutationManager) reconcilePersistedActive() error")
	orphanStart := strings.Index(source, "func (m *serviceMutationManager) tryResolvePersistedOrphan() error")
	orphanEnd := strings.Index(source, "func (m *serviceMutationManager) finishPersistedOrphanLocked(")
	if startupStart < 0 || orphanStart < 0 || orphanEnd < 0 {
		t.Fatal("service mutation recovery functions were not found")
	}
	startup := source[startupStart:orphanStart]
	orphan := source[orphanStart:orphanEnd]
	for name, body := range map[string]string{"startup": startup, "orphan": orphan} {
		recovery := strings.Index(body, "recoverPersistedPanelCertificateIssueLocked(job, lock)")
		generic := strings.Index(body, "packageManagerMutationBusy()")
		if recovery < 0 || generic < 0 || recovery > generic {
			t.Fatalf("%s certificate recovery is not before generic host recovery", name)
		}
	}
}

func TestLegacyCertificateCommitPhaseCannotBeFormatted(t *testing.T) {
	if _, err := formatPanelCertificateIssueCommitPhase(
		panelCertificateIssueCommitIntent,
		testMutationRequestID,
		"panel.example.test",
		"certbot",
	); err == nil {
		t.Fatal("legacy certbot package produced a V2 commit phase")
	}
}
