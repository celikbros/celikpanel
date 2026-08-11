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
	testMutationRequestID       = "00112233445566778899aabbccddeeff"
	testMutationSecondRequestID = "11223344556677889900aabbccddeeff"
	testMutationOwnerID         = "ffeeddccbbaa99887766554433221100"
)

func mutationTestRoot(t *testing.T) string {
	t.Helper()
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID = uint32(os.Getuid())
	serviceMutationRequiredOwnerGID = uint32(os.Getgid())
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
	})
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func newMutationTestManager(t *testing.T) (*serviceMutationManager, string) {
	t.Helper()
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatalf("initialize service mutation ledger: %v", err)
	}
	manager, err := newServiceMutationManager(
		stateDir,
		lockPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manager, root
}

func TestInitializeServiceMutationLedgerPublishesExactlyOnce(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatalf("initialize service mutation ledger: %v", err)
	}
	if err := initializeServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errServiceMutationLedgerAlreadyInitialized) {
		t.Fatalf("second initialization err=%v want already initialized", err)
	}

	ledgerPath := filepath.Join(stateDir, "service-mutations.json")
	raw, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read initial durable ledger: %v", err)
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatalf("decode initial durable ledger: %v", err)
	}
	if ledger.Version != serviceMutationLedgerVersion ||
		ledger.ActiveRequestID != "" ||
		len(ledger.Jobs) != 0 {
		t.Fatalf("initial durable ledger = %+v", ledger)
	}
	expected, err := json.Marshal(&serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, expected) {
		t.Fatalf("initial durable ledger is not canonical: %q", raw)
	}
	manager, err := newServiceMutationManager(stateDir, lockPath)
	if err != nil {
		t.Fatalf("load initialized service mutation ledger: %v", err)
	}
	if manager.ledger.Version != ledger.Version ||
		manager.ledger.ActiveRequestID != ledger.ActiveRequestID ||
		len(manager.ledger.Jobs) != len(ledger.Jobs) {
		t.Fatalf("memory and durable ledgers disagree: memory=%+v durable=%+v", manager.ledger, ledger)
	}
	info, err := os.Stat(ledgerPath)
	if err != nil {
		t.Fatalf("stat initial durable ledger: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("initial durable ledger mode=%#o want 0600", info.Mode().Perm())
	}
}

func TestInitializeServiceMutationLedgerRequiresSharedHostLock(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("initializer with held host lock err=%v want host busy", err)
	}
	if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("blocked initializer created state directory: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatalf("initializer after host lock release: %v", err)
	}
}

func TestInitializeServiceMutationLedgerIsCrashAtomicAndNoReplace(t *testing.T) {
	t.Run("existing final survives", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		sentinel := []byte("existing ledger must survive")
		if err := os.WriteFile(ledgerPath, sentinel, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := initializeServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errServiceMutationLedgerAlreadyInitialized) {
			t.Fatalf("initializer with existing final err=%v want already initialized", err)
		}
		if got, err := os.ReadFile(ledgerPath); err != nil || !bytes.Equal(got, sentinel) {
			t.Fatalf("existing final changed: got=%q err=%v", got, err)
		}
		staged, err := filepath.Glob(filepath.Join(stateDir, ".service-mutations-initial-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(staged) != 0 {
			t.Fatalf("initializer leaked staged files: %v", staged)
		}
	})

	t.Run("arbitrary abandoned stage fails closed", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		stagePath := filepath.Join(stateDir, ".service-mutations-initial-12345.json")
		partial := []byte("{\"version\":2")
		if err := os.WriteFile(stagePath, partial, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger check beside arbitrary stage err=%v want not idle", err)
		}
		if err := initializeServiceMutationLedger(stateDir, lockPath); err == nil {
			t.Fatal("initializer accepted arbitrary abandoned stage")
		}
		if _, err := os.Lstat(filepath.Join(stateDir, serviceMutationLedgerFileName)); !os.IsNotExist(err) {
			t.Fatalf("initializer published beside arbitrary stage: %v", err)
		}
		if got, err := os.ReadFile(stagePath); err != nil || !bytes.Equal(got, partial) {
			t.Fatalf("abandoned stage changed: got=%q err=%v", got, err)
		}
	})
}

func TestNewServiceMutationManagerRejectsMissingLedgerWithoutCreatingIt(t *testing.T) {
	for _, createStateDir := range []bool{false, true} {
		name := "missing state directory"
		if createStateDir {
			name = "secure state directory without ledger"
		}
		t.Run(name, func(t *testing.T) {
			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			if createStateDir {
				if err := os.Mkdir(stateDir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := newServiceMutationManager(
				stateDir,
				filepath.Join(root, "service-mutation.lock"),
			)
			if err == nil {
				t.Fatalf("missing ledger startup err=%v", err)
			}
			if createStateDir && !strings.Contains(err.Error(), "not initialized") {
				t.Fatalf("secure state without ledger startup err=%v", err)
			}
			if _, err := os.Lstat(filepath.Join(stateDir, "service-mutations.json")); !os.IsNotExist(err) {
				t.Fatalf("manager created missing ledger: %v", err)
			}
			if !createStateDir {
				if _, err := os.Lstat(stateDir); !os.IsNotExist(err) {
					t.Fatalf("manager created missing state directory: %v", err)
				}
			}
		})
	}
}

func beginMutationTestJob(t *testing.T, manager *serviceMutationManager) *ServiceMutationJob {
	return beginMutationTestJobWithIdentity(t, manager, "service_install", "nginx", "")
}

func beginMutationTestJobWithIdentity(
	t *testing.T,
	manager *serviceMutationManager,
	kind, target, packageName string,
) *ServiceMutationJob {
	t.Helper()
	job, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID:   testMutationRequestID,
		OwnerID:     testMutationOwnerID,
		Kind:        kind,
		Target:      target,
		PackageName: packageName,
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func nginxInstallTestStepClaim() serviceMutationStepClaim {
	return newServiceMutationStepClaim(
		serviceMutationStepEnsureNginxReady,
		"nginx",
		"",
		"ready",
	)
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

func TestAgentServiceMutationManagerRetriesTransientHostBusy(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", stateDir)
	t.Setenv("CELIKPANEL_MUTATION_LOCK", lockPath)

	globalServiceMutationMu.Lock()
	previousManager := globalServiceMutationManager
	previousErr := globalServiceMutationErr
	globalServiceMutationManager = nil
	globalServiceMutationErr = nil
	globalServiceMutationMu.Unlock()
	t.Cleanup(func() {
		globalServiceMutationMu.Lock()
		globalServiceMutationManager = previousManager
		globalServiceMutationErr = previousErr
		globalServiceMutationMu.Unlock()
	})

	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if manager, err := agentServiceMutationManager(); manager != nil || !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("first initialization manager=%v err=%v want transient host busy", manager, err)
	}
	globalServiceMutationMu.Lock()
	cachedManager, cachedErr := globalServiceMutationManager, globalServiceMutationErr
	globalServiceMutationMu.Unlock()
	if cachedManager != nil || cachedErr != nil {
		t.Fatalf("transient host busy was cached: manager=%v err=%v", cachedManager, cachedErr)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	manager, err := agentServiceMutationManager()
	if err != nil || manager == nil {
		t.Fatalf("retry after lock release manager=%v err=%v", manager, err)
	}
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
	}, nginxInstallTestStepClaim())
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
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(lock.file.Fd())))
		if err := checkServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("held lock err=%v want not idle", err)
		}
		if err := checkServiceMutationIdleUnderExternalLock(stateDir, lockPath); err != nil {
			t.Fatalf("external-lock idle proof rejected safe state: %v", err)
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

	t.Run("pre-ledger mode accepts missing state and runtime without creating them", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "missing-state")
		lockPath := filepath.Join(root, "missing-runtime", "service-mutation.lock")
		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("pre-ledger missing state and runtime rejected: %v", err)
		}
		for label, path := range map[string]string{
			"state":   stateDir,
			"runtime": filepath.Dir(lockPath),
		} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("pre-ledger check created or accepted unexpected %s path %q: %v", label, path, err)
			}
		}
	})

	t.Run("pre-ledger mode accepts a secure state directory without a ledger while strict mode rejects it", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("pre-ledger secure state without ledger rejected: %v", err)
		}
		if err := checkServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("strict check missing ledger err=%v want not idle", err)
		}
		if _, err := os.Lstat(filepath.Join(stateDir, "service-mutations.json")); !os.IsNotExist(err) {
			t.Fatalf("idle checks created missing ledger: %v", err)
		}
	})

	t.Run("pre-ledger mode rejects a held lock without state", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "missing-state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		lock, err := acquireServiceMutationFileLock(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(lock.file.Fd())))
		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger held lock err=%v want not idle", err)
		}
		if err := checkPreLedgerServiceMutationIdleUnderExternalLock(stateDir, lockPath); err != nil {
			t.Fatalf("pre-ledger external-lock proof rejected safe missing state: %v", err)
		}
	})

	t.Run("pre-ledger mode rejects unsafe existing state", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		if err := os.Mkdir(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		err := checkPreLedgerServiceMutationIdle(stateDir, filepath.Join(root, "service-mutation.lock"))
		if !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger unsafe state err=%v want not idle", err)
		}
	})

	t.Run("pre-ledger mode rejects malformed existing ledger", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		if err := os.WriteFile(ledgerPath, []byte(`{"version":1,"jobs":{},"unexpected":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ledgerPath, 0o600); err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(root, "service-mutation.lock")
		err := checkPreLedgerServiceMutationIdle(stateDir, lockPath)
		if !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger malformed ledger err=%v want not idle", err)
		}
		if err := checkPreLedgerServiceMutationIdleUnderExternalLock(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger external-lock checker accepted malformed ledger: %v", err)
		}
	})

	t.Run("pre-ledger mode rejects active existing state", func(t *testing.T) {
		manager, managedRoot := newMutationTestManager(t)
		beginMutationTestJob(t, manager)
		err := checkPreLedgerServiceMutationIdle(
			filepath.Join(managedRoot, "state"),
			filepath.Join(managedRoot, "service-mutation.lock"),
		)
		if !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("pre-ledger active existing state err=%v want not idle", err)
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
	}, nginxInstallTestStepClaim())
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

func TestServiceMutationBeginReloadsLedgerAfterHostLock(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	lockPath := filepath.Join(root, "service-mutation.lock")
	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatal(err)
	}
	managerA, err := newServiceMutationManager(stateDir, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	managerB, err := newServiceMutationManager(stateDir, lockPath)
	if err != nil {
		t.Fatal(err)
	}

	beginMutationTestJob(t, managerA)
	if _, err := managerA.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := managerB.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationSecondRequestID,
		OwnerID:   testMutationOwnerID,
		Kind:      "service_install",
		Target:    "postfix",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != serviceMutationStatusRunning {
		t.Fatalf("second job=%+v", second)
	}
	if first := managerB.status(testMutationRequestID); first == nil || first.Status != serviceMutationStatusSucceeded {
		t.Fatalf("stale manager lost first manager history: %+v", first)
	}
	if _, err := managerB.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationSecondRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, serviceMutationLedgerFileName))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Jobs) != 2 || ledger.Jobs[testMutationRequestID] == nil || ledger.Jobs[testMutationSecondRequestID] == nil {
		t.Fatalf("durable history after sequential managers=%+v", ledger.Jobs)
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

func TestDecodeServiceMutationLedgerEnforcesCanonicalIdentityAndActiveState(t *testing.T) {
	job := func(requestID, status string) *ServiceMutationJob {
		started := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
		result := &ServiceMutationJob{
			RequestID:  requestID,
			OwnerID:    testMutationOwnerID,
			Kind:       "service_install",
			Target:     "nginx",
			Status:     status,
			Phase:      "test",
			Attempt:    1,
			StartedAt:  started,
			UpdatedAt:  started.Add(time.Minute),
			DeadlineAt: started.Add(time.Hour),
		}
		if serviceMutationStatusActive(status) {
			result.LeaseExpiresAt = started.Add(10 * time.Minute)
		} else if status == serviceMutationStatusSucceeded || status == serviceMutationStatusFailed {
			result.FinishedAt = started.Add(2 * time.Minute)
		}
		return result
	}
	valid := map[string]serviceMutationLedger{
		"empty": {
			Version: serviceMutationLedgerVersion,
			Jobs:    map[string]*ServiceMutationJob{},
		},
		"terminal history": {
			Version: serviceMutationLedgerVersion,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID:       job(testMutationRequestID, serviceMutationStatusSucceeded),
				testMutationSecondRequestID: job(testMutationSecondRequestID, serviceMutationStatusFailed),
			},
		},
	}
	for _, status := range []string{
		serviceMutationStatusRunning,
		serviceMutationStatusCancelling,
		serviceMutationStatusOrphaned,
	} {
		valid["active "+status] = serviceMutationLedger{
			Version:         serviceMutationLedgerVersion,
			ActiveRequestID: testMutationRequestID,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID:       job(testMutationRequestID, status),
				testMutationSecondRequestID: job(testMutationSecondRequestID, serviceMutationStatusSucceeded),
			},
		}
	}
	for name, ledger := range valid {
		t.Run("valid "+name, func(t *testing.T) {
			raw, err := json.Marshal(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeServiceMutationLedger(raw); err != nil {
				t.Fatalf("valid ledger rejected: %v", err)
			}
		})
	}

	invalid := map[string]serviceMutationLedger{
		"nil job": {
			Version: serviceMutationLedgerVersion,
			Jobs:    map[string]*ServiceMutationJob{testMutationRequestID: nil},
		},
		"map key differs from request id": {
			Version: serviceMutationLedgerVersion,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID: job(testMutationSecondRequestID, serviceMutationStatusSucceeded),
			},
		},
		"active job without pointer": {
			Version: serviceMutationLedgerVersion,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID: job(testMutationRequestID, serviceMutationStatusRunning),
			},
		},
		"pointer selects terminal job": {
			Version:         serviceMutationLedgerVersion,
			ActiveRequestID: testMutationRequestID,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID: job(testMutationRequestID, serviceMutationStatusSucceeded),
			},
		},
		"pointer selects missing job": {
			Version:         serviceMutationLedgerVersion,
			ActiveRequestID: testMutationRequestID,
			Jobs:            map[string]*ServiceMutationJob{},
		},
		"multiple active jobs": {
			Version:         serviceMutationLedgerVersion,
			ActiveRequestID: testMutationRequestID,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID:       job(testMutationRequestID, serviceMutationStatusRunning),
				testMutationSecondRequestID: job(testMutationSecondRequestID, serviceMutationStatusOrphaned),
			},
		},
		"unsupported job status": {
			Version: serviceMutationLedgerVersion,
			Jobs: map[string]*ServiceMutationJob{
				testMutationRequestID: job(testMutationRequestID, "paused"),
			},
		},
	}
	for name, ledger := range invalid {
		t.Run("invalid "+name, func(t *testing.T) {
			raw, err := json.Marshal(&ledger)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeServiceMutationLedger(raw); err == nil {
				t.Fatal("invalid canonical ledger was accepted by decoder")
			}

			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			if err := os.Mkdir(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			ledgerPath := filepath.Join(stateDir, "service-mutations.json")
			if err := os.WriteFile(ledgerPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := newServiceMutationManager(
				stateDir,
				filepath.Join(root, "service-mutation.lock"),
			); err == nil {
				t.Fatal("invalid canonical ledger was accepted during startup")
			}
		})
	}
}

func TestServiceMutationManagerRejectsZeroLifecycleBeforeReconcile(t *testing.T) {
	root := mutationTestRoot(t)
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ledger := serviceMutationLedger{
		Version:         serviceMutationLedgerVersion,
		ActiveRequestID: testMutationRequestID,
		Jobs: map[string]*ServiceMutationJob{
			testMutationRequestID: {
				RequestID: testMutationRequestID,
				OwnerID:   testMutationOwnerID,
				Kind:      "service_install",
				Target:    "nginx",
				Status:    serviceMutationStatusRunning,
				Phase:     "leased",
				Attempt:   1,
			},
		},
	}
	raw, err := json.Marshal(&ledger)
	if err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
	if err := os.WriteFile(ledgerPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newServiceMutationManager(stateDir, filepath.Join(root, "service-mutation.lock")); err == nil ||
		!strings.Contains(err.Error(), "lifecycle timestamps") {
		t.Fatalf("zero lifecycle startup err=%v", err)
	}
	got, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("invalid zero-lifecycle ledger was mutated during rejected startup")
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
	manager, root := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}, nginxInstallTestStepClaim())
	if err != nil {
		t.Fatal(err)
	}

	type commandResult struct {
		output []byte
		err    error
	}
	resultCh := make(chan commandResult, 1)
	childPIDPath := filepath.Join(root, "child.pid")
	go func() {
		output, runErr := runServiceMutationCombinedOutput(
			ctx,
			"/bin/sh",
			"-c",
			"sleep 30 & echo $! > $1; wait",
			"service-mutation-test",
			childPIDPath,
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
	var childPID int
	for {
		raw, readErr := os.ReadFile(childPIDPath)
		if readErr == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(raw)))
			if err == nil && childPID > 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("supervised child pid was not published: raw=%q err=%v", raw, readErr)
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
