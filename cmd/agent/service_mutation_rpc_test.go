//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	testMutationRequestID = "00112233445566778899aabbccddeeff"
	testMutationOwnerID   = "ffeeddccbbaa99887766554433221100"
)

func mutationTestRoot(t *testing.T) string {
	t.Helper()
	previousUID := serviceMutationRequiredOwnerUID
	serviceMutationRequiredOwnerUID = uint32(os.Getuid())
	t.Cleanup(func() { serviceMutationRequiredOwnerUID = previousUID })
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func newMutationTestManager(t *testing.T) (*serviceMutationManager, string) {
	t.Helper()
	root := mutationTestRoot(t)
	manager, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return manager, root
}

func beginMutationTestJob(t *testing.T, manager *serviceMutationManager) *ServiceMutationJob {
	t.Helper()
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Kind:      "service_install",
		Target:    "nginx",
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func installGlobalMutationTestManager(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	globalServiceMutationMu.Lock()
	previousManager := globalServiceMutationManager
	previousErr := globalServiceMutationErr
	globalServiceMutationManager = manager
	globalServiceMutationErr = nil
	globalServiceMutationMu.Unlock()
	t.Cleanup(func() {
		globalServiceMutationMu.Lock()
		globalServiceMutationManager = previousManager
		globalServiceMutationErr = previousErr
		globalServiceMutationMu.Unlock()
	})
}

func TestRequiredServiceMutationStepRejectsMissingAndWrongBinding(t *testing.T) {
	agent := &Agent{}
	var missing ConfigureDBToolsResponse
	if err := agent.ConfigureDBTools(&ServiceMutationRequest{}, &missing); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missing.Error, "durable service mutation lease") {
		t.Fatalf("missing binding error = %q", missing.Error)
	}

	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)
	var wrong NodeRemoveResponse
	if err := agent.RemoveNodeVersion(&NodeRemoveRequest{
		ServiceMutationBinding: ServiceMutationBinding{
			MutationRequestID: testMutationRequestID,
			MutationOwnerID:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Version: "24.18.0",
	}, &wrong); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wrong.Error, "does not own the active lease") {
		t.Fatalf("wrong binding error = %q", wrong.Error)
	}
	if job := manager.status(testMutationRequestID); job == nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("wrong owner changed durable job: %+v", job)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyFirewallRequiresDurableMutationBinding(t *testing.T) {
	var response FirewallStatusResponse
	if err := (&Agent{}).ApplyFirewall(
		&ApplyFirewallRequest{Enabled: false},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "valid durable service mutation lease") {
		t.Fatalf("missing binding error = %q", response.Error)
	}
}

func TestResetFailedUnitMutationRejectsUnknownManagedUnit(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)

	var reply bool
	err := (&Agent{}).ResetFailedUnitMutation(
		&ServiceMutationServiceRequest{
			ServiceMutationBinding: ServiceMutationBinding{
				MutationRequestID: testMutationRequestID,
				MutationOwnerID:   testMutationOwnerID,
			},
			ServiceName: "not-in-managed-catalog.service",
		},
		&reply,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown managed service") {
		t.Fatalf("unknown reset-failed unit error = %v", err)
	}
	if reply {
		t.Fatal("unknown reset-failed unit reported success")
	}
	if job := manager.status(testMutationRequestID); job == nil || job.Status != serviceMutationStatusRunning {
		t.Fatalf("unknown unit changed durable job: %+v", job)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredServiceMutationStepHoldsJobUntilRelease(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	installGlobalMutationTestManager(t, manager)
	beginMutationTestJob(t, manager)

	ctx, release, err := (&Agent{}).requiredServiceMutationStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("held step context = %v", ctx.Err())
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err == nil || !strings.Contains(err.Error(), "active privileged step") {
		t.Fatalf("finish with required step held error = %v", err)
	}
	release()
	terminal, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != serviceMutationStatusSucceeded {
		t.Fatalf("terminal job = %+v", terminal)
	}
}

func TestServiceMutationIdleCheckIsReadOnlyAndFailClosed(t *testing.T) {
	t.Run("idle and terminal ledger", func(t *testing.T) {
		manager, root := newMutationTestManager(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := checkServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("fresh idle state rejected: %v", err)
		}
		beginMutationTestJob(t, manager)
		if err := checkServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("active state err=%v want not idle", err)
		}
		if _, err := manager.finish(&ServiceMutationFinishRequest{
			RequestID: testMutationRequestID,
			OwnerID:   testMutationOwnerID,
			Success:   true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := checkServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("terminal idle state rejected: %v", err)
		}
	})

	t.Run("lock held without active pointer", func(t *testing.T) {
		_, root := newMutationTestManager(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		lock, err := acquireServiceMutationFileLock(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		if err := checkServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("held lock err=%v want not idle", err)
		}
	})

	t.Run("missing state directory", func(t *testing.T) {
		root := mutationTestRoot(t)
		err := checkServiceMutationIdle(
			filepath.Join(root, "missing-state"),
			filepath.Join(root, "service-mutation.lock"),
		)
		if !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("missing state err=%v want not idle", err)
		}
	})

	t.Run("group-readable state directory", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		if err := os.Mkdir(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		err := checkServiceMutationIdle(
			stateDir,
			filepath.Join(root, "service-mutation.lock"),
		)
		if !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("group-readable state err=%v want not idle", err)
		}
	})
}

func TestServiceMutationLeaseIsDurableAndHeldUntilTerminalState(t *testing.T) {
	manager, root := newMutationTestManager(t)
	job := beginMutationTestJob(t, manager)
	if job.Status != serviceMutationStatusRunning || job.Phase != "leased" {
		t.Fatalf("begin job=%+v", job)
	}

	ledgerInfo, err := os.Stat(filepath.Join(root, "state", "service-mutations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ledgerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("ledger mode=%#o want 0600", ledgerInfo.Mode().Perm())
	}
	lockInfo, err := os.Stat(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if lockInfo.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode=%#o want 0600", lockInfo.Mode().Perm())
	}
	if second, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second host lock err=%v want host busy", err)
	}

	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("step context unexpectedly cancelled: %v", ctx.Err())
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err == nil || !strings.Contains(err.Error(), "active privileged step") {
		t.Fatalf("finish with live step err=%v", err)
	}
	done()

	terminal, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != serviceMutationStatusSucceeded || terminal.Phase != "completed" {
		t.Fatalf("terminal job=%+v", terminal)
	}
	reacquired, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("host lock was not released after durable terminal write: %v", err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "state", "service-mutations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ledger serviceMutationLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, canonical) || ledger.ActiveRequestID != "" {
		t.Fatalf("terminal ledger is not canonical or still active: %s", raw)
	}
}

func TestServiceMutationManagerReconcilesPersistedInterruption(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	ledger := serviceMutationLedger{
		Version:         serviceMutationLedgerVersion,
		ActiveRequestID: testMutationRequestID,
		Jobs: map[string]*ServiceMutationJob{
			testMutationRequestID: {
				RequestID: testMutationRequestID, OwnerID: testMutationOwnerID,
				Kind: "service_install", Target: "nginx",
				Status: serviceMutationStatusRunning, Phase: "installing", Attempt: 1,
				StartedAt: now, UpdatedAt: now,
				LeaseExpiresAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour),
			},
		},
	}
	raw, err := json.Marshal(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(stateDir, "service-mutations.json")
	if err := os.WriteFile(ledgerPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ledgerPath, 0o600); err != nil {
		t.Fatal(err)
	}

	manager, err := newServiceMutationManager(
		stateDir,
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatal(err)
	}
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != "agent_restarted_before_completion" {
		t.Fatalf("reconciled job=%+v", job)
	}
	manager.mu.Lock()
	active := manager.ledger.ActiveRequestID
	manager.mu.Unlock()
	if active != "" {
		t.Fatalf("reconciled ledger still active: %q", active)
	}
}

func TestServiceMutationLedgerRejectsUnsafeFilesAndUnknownSchema(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, stateDir string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, stateDir string) {
				target := filepath.Join(filepath.Dir(stateDir), "target.json")
				if err := os.WriteFile(target, []byte(`{"version":1,"jobs":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(stateDir, "service-mutations.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, stateDir string) {
				path := filepath.Join(stateDir, "service-mutations.json")
				if err := os.WriteFile(path, []byte(`{"version":1,"jobs":{}}`), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unknown field",
			setup: func(t *testing.T, stateDir string) {
				path := filepath.Join(stateDir, "service-mutations.json")
				if err := os.WriteFile(path, []byte(`{"version":1,"jobs":{},"unexpected":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "non canonical key order",
			setup: func(t *testing.T, stateDir string) {
				path := filepath.Join(stateDir, "service-mutations.json")
				if err := os.WriteFile(path, []byte(`{"jobs":{},"version":1}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			if err := os.Mkdir(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			test.setup(t, stateDir)
			if _, err := newServiceMutationManager(
				stateDir,
				filepath.Join(root, "service-mutation.lock"),
			); err == nil {
				t.Fatal("unsafe ledger was accepted")
			}
		})
	}
}

func TestServiceMutationCancellationKillsTrackedProcessGroup(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}

	type commandResult struct {
		output []byte
		err    error
	}
	resultCh := make(chan commandResult, 1)
	go func() {
		output, runErr := runServiceMutationCombinedOutput(
			ctx,
			"/bin/sh",
			"-c",
			"sleep 30 & echo $!; wait",
		)
		resultCh <- commandResult{output: output, err: runErr}
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		job := manager.status(testMutationRequestID)
		if job != nil && job.WorkerPID > 0 && job.WorkerStarted != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tracked worker was not persisted: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err == nil {
		t.Fatal("finish released lease while tracked worker was alive")
	}

	if _, err := manager.cancelJob(&ServiceMutationCancelRequest{
		RequestID:      testMutationRequestID,
		ExpectedOwner:  testMutationOwnerID,
		FailureCode:    "test_cancel",
		FailureMessage: "test cancellation",
	}); err != nil {
		t.Fatal(err)
	}
	var result commandResult
	select {
	case result = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("tracked process group did not stop after lease cancellation")
	}
	if result.err == nil {
		t.Fatal("cancelled command unexpectedly succeeded")
	}
	done()

	deadline = time.Now().Add(3 * time.Second)
	for {
		job := manager.status(testMutationRequestID)
		if job != nil && job.Status == serviceMutationStatusFailed {
			if job.WorkerPID != 0 || job.WorkerStarted != "" {
				t.Fatalf("terminal job retained worker identity: %+v", job)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelled job did not become terminal: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}

	childText := strings.TrimSpace(string(result.output))
	if newline := strings.IndexByte(childText, '\n'); newline >= 0 {
		childText = childText[:newline]
	}
	childPID, err := strconv.Atoi(childText)
	if err != nil || childPID <= 0 {
		t.Fatalf("child pid output=%q err=%v", result.output, err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		err = syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived process-group cancellation: %v", childPID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
