//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestServiceMutationLockRetriesAfterFirstCreateCrash(t *testing.T) {
	root := mutationTestRoot(t)
	lockPath := filepath.Join(root, "service-mutation.lock")
	previousHook := serviceMutationLockFaultHook
	serviceMutationLockFaultHook = func(point string) error {
		if point == serviceMutationLockFaultAfterCreate {
			return errors.New("simulated crash after O_EXCL")
		}
		return nil
	}
	t.Cleanup(func() { serviceMutationLockFaultHook = previousHook })

	if lock, err := acquireServiceMutationFileLock(lockPath); err == nil {
		_ = lock.Close()
		t.Fatal("injected first-create crash unexpectedly acquired the lock")
	}
	info, err := os.Lstat(lockPath)
	if err != nil {
		t.Fatalf("first-create residue missing: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		t.Fatalf("first-create residue metadata = mode %#o size %d", info.Mode(), info.Size())
	}

	serviceMutationLockFaultHook = nil
	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		t.Fatalf("retry did not recover the exact first-create residue: %v", err)
	}
	if second, err := acquireServiceMutationFileLock(lockPath); !errors.Is(err, errServiceMutationHostBusy) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("recovered lock was not retained: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMutationLockRepairsOnlyUnlockedRootResidue(t *testing.T) {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("exact root:root residue repair requires a root test process")
	}
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	previousHook := serviceMutationLockFaultHook
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
		serviceMutationLockFaultHook = previousHook
	})

	targetGID := uint32(1)
	serviceMutationRequiredOwnerUID = 0
	serviceMutationRequiredOwnerGID = targetGID
	root := t.TempDir()
	if err := os.Chown(root, 0, int(targetGID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "service-mutation.lock")
	residue, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := residue.Close(); err != nil {
		t.Fatal(err)
	}

	residueFD, err := unix.Open(lockPath, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(residueFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(residueFD)
		t.Fatal(err)
	}
	if lock, err := acquireServiceMutationFileLock(lockPath); !errors.Is(err, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		_ = unix.Close(residueFD)
		t.Fatalf("held root residue err=%v want host busy", err)
	}
	info, err := os.Lstat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Gid != 0 {
		t.Fatalf("held residue was changed before lock ownership proof: gid=%d", stat.Gid)
	}
	if err := unix.Flock(residueFD, unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := unix.Close(residueFD); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		t.Fatalf("unlocked root residue was not repaired: %v", err)
	}
	info, err = lock.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	stat = info.Sys().(*syscall.Stat_t)
	if stat.Uid != 0 || stat.Gid != targetGID || stat.Nlink != 1 ||
		info.Mode().Perm() != 0o600 || info.Size() != 0 {
		t.Fatalf("repaired lock metadata uid=%d gid=%d links=%d mode=%#o size=%d",
			stat.Uid, stat.Gid, stat.Nlink, info.Mode().Perm(), info.Size())
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMutationLockRejectsBroaderResidues(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root, lockPath string)
	}{
		{
			name: "nonempty",
			setup: func(t *testing.T, _, lockPath string) {
				if err := os.WriteFile(lockPath, []byte("unexpected"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, _, lockPath string) {
				if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(lockPath, 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hard link",
			setup: func(t *testing.T, root, lockPath string) {
				if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(lockPath, filepath.Join(root, "second-link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, root, lockPath string) {
				target := filepath.Join(root, "target")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, lockPath); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := mutationTestRoot(t)
			lockPath := filepath.Join(root, "service-mutation.lock")
			test.setup(t, root, lockPath)
			if lock, err := acquireServiceMutationFileLock(lockPath); err == nil {
				_ = lock.Close()
				t.Fatal("unsafe lock residue was accepted")
			}
		})
	}
}

type serviceMutationCrashOperation struct {
	name    string
	prepare bool
	invoke  func(*serviceMutationManager) error
}

func serviceMutationCrashOperations() []serviceMutationCrashOperation {
	return []serviceMutationCrashOperation{
		{
			name: "begin",
			invoke: func(manager *serviceMutationManager) error {
				_, err := manager.begin(&ServiceMutationBeginRequest{
					RequestID: testMutationRequestID,
					OwnerID:   testMutationOwnerID,
					Kind:      "service_install",
					Target:    "nginx",
				})
				return err
			},
		},
		{
			name:    "heartbeat",
			prepare: true,
			invoke: func(manager *serviceMutationManager) error {
				_, err := manager.heartbeat(&ServiceMutationHeartbeatRequest{
					RequestID: testMutationRequestID,
					OwnerID:   testMutationOwnerID,
					Phase:     "fault-injected-heartbeat",
				})
				return err
			},
		},
		{
			name:    "cancel",
			prepare: true,
			invoke: func(manager *serviceMutationManager) error {
				_, err := manager.cancelJob(&ServiceMutationCancelRequest{
					RequestID:     testMutationRequestID,
					ExpectedOwner: testMutationOwnerID,
					Reason:        "fault-injected-cancel",
				})
				return err
			},
		},
		{
			name:    "finish",
			prepare: true,
			invoke: func(manager *serviceMutationManager) error {
				_, err := manager.finish(&ServiceMutationFinishRequest{
					RequestID: testMutationRequestID,
					OwnerID:   testMutationOwnerID,
					Success:   true,
				})
				return err
			},
		},
	}
}

func injectOneServiceMutationWriteFault(
	manager *serviceMutationManager,
	point string,
) *bool {
	fired := false
	setServiceMutationWriteFault(manager, func(actual string) error {
		if actual == point && !fired {
			fired = true
			return fmt.Errorf("injected %s", point)
		}
		return nil
	})
	return &fired
}

func setServiceMutationWriteFault(
	manager *serviceMutationManager,
	fault func(string) error,
) {
	manager.mu.Lock()
	manager.writeFault = fault
	manager.mu.Unlock()
}

func readServiceMutationCrashLedger(t *testing.T, manager *serviceMutationManager) ([]byte, serviceMutationLedger) {
	t.Helper()
	raw, err := os.ReadFile(manager.ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		t.Fatalf("decode durable crash-test ledger: %v", err)
	}
	return raw, ledger
}

func assertServiceMutationMemoryMatchesDisk(t *testing.T, manager *serviceMutationManager, raw []byte) {
	t.Helper()
	manager.mu.Lock()
	memory, err := json.Marshal(&manager.ledger)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(memory, raw) {
		t.Fatalf("memory and durable ledgers differ:\nmemory=%s\ndurable=%s", memory, raw)
	}
}

func finishServiceMutationCrashTestManager(t *testing.T, manager *serviceMutationManager) {
	t.Helper()
	setServiceMutationWriteFault(manager, nil)
	manager.mu.Lock()
	poisoned := manager.poisoned != nil
	runtime := manager.active
	manager.mu.Unlock()
	if poisoned {
		manager.mu.Lock()
		if runtime != nil {
			runtime.cancel()
			if runtime.lock != nil {
				_ = runtime.lock.Close()
				runtime.lock = nil
			}
		}
		if manager.poisonLock != nil {
			_ = manager.poisonLock.Close()
			manager.poisonLock = nil
		}
		manager.mu.Unlock()
		return
	}
	if runtime != nil && runtime.job != nil && serviceMutationStatusActive(runtime.job.Status) {
		if _, err := manager.finish(&ServiceMutationFinishRequest{
			RequestID: runtime.job.RequestID,
			OwnerID:   runtime.job.OwnerID,
			Success:   false,
		}); err != nil {
			t.Fatalf("clean crash-test manager: %v", err)
		}
	}
}

func TestServiceMutationLedgerPrePublishFailuresRollBack(t *testing.T) {
	for _, operation := range serviceMutationCrashOperations() {
		t.Run(operation.name, func(t *testing.T) {
			manager, _ := newMutationTestManager(t)
			if operation.prepare {
				beginMutationTestJob(t, manager)
			}
			beforeRaw, _ := readServiceMutationCrashLedger(t, manager)
			fired := injectOneServiceMutationWriteFault(manager, serviceMutationWriteFaultBeforeRename)
			if err := operation.invoke(manager); err == nil {
				t.Fatal("pre-publish fault unexpectedly succeeded")
			} else if errors.Is(err, errServiceMutationManagerPoisoned) {
				t.Fatalf("provably pre-publish fault poisoned manager: %v", err)
			}
			if !*fired {
				t.Fatal("pre-publish fault hook did not fire")
			}
			afterRaw, _ := readServiceMutationCrashLedger(t, manager)
			if !bytes.Equal(beforeRaw, afterRaw) {
				t.Fatalf("pre-publish fault changed durable ledger:\nbefore=%s\nafter=%s", beforeRaw, afterRaw)
			}
			assertServiceMutationMemoryMatchesDisk(t, manager, afterRaw)

			setServiceMutationWriteFault(manager, nil)
			if err := operation.invoke(manager); err != nil {
				t.Fatalf("retry after pre-publish fault: %v", err)
			}
			finishServiceMutationCrashTestManager(t, manager)
		})
	}
}

func TestServiceMutationLedgerPostPublishFailuresPoisonAndRetainLock(t *testing.T) {
	for _, operation := range serviceMutationCrashOperations() {
		t.Run(operation.name, func(t *testing.T) {
			manager, root := newMutationTestManager(t)
			if operation.prepare {
				beginMutationTestJob(t, manager)
			}
			fired := injectOneServiceMutationWriteFault(manager, serviceMutationWriteFaultAfterRename)
			err := operation.invoke(manager)
			if !errors.Is(err, errServiceMutationManagerPoisoned) {
				t.Fatalf("post-publish fault err=%v want poisoned manager", err)
			}
			if !*fired {
				t.Fatal("post-publish fault hook did not fire")
			}

			manager.mu.Lock()
			runtime := manager.active
			memoryLedger := cloneServiceMutationLedger(manager.ledger)
			manager.mu.Unlock()
			if runtime == nil || runtime.lock == nil {
				t.Fatal("poisoned manager released its runtime or host lock")
			}
			select {
			case <-runtime.ctx.Done():
			default:
				t.Fatal("poisoned manager did not cancel the active runtime")
			}
			if err := validateServiceMutationLedger(&memoryLedger); err != nil {
				t.Fatalf("poisoned in-memory ledger invariant: %v", err)
			}
			if lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
				if lock != nil {
					_ = lock.Close()
				}
				t.Fatalf("poisoned manager did not retain host lock: %v", err)
			}
			durableRaw, durable := readServiceMutationCrashLedger(t, manager)
			if err := validateServiceMutationLedger(&durable); err != nil {
				t.Fatalf("post-publish durable ledger invariant: %v", err)
			}

			refused := []error{}
			_, beginErr := manager.begin(&ServiceMutationBeginRequest{
				RequestID: testMutationSecondRequestID,
				OwnerID:   testMutationOwnerID,
				Kind:      "service_install",
				Target:    "apache",
			})
			refused = append(refused, beginErr)
			_, heartbeatErr := manager.heartbeat(&ServiceMutationHeartbeatRequest{
				RequestID: testMutationRequestID,
				OwnerID:   testMutationOwnerID,
			})
			refused = append(refused, heartbeatErr)
			_, cancelErr := manager.cancelJob(&ServiceMutationCancelRequest{
				RequestID:     testMutationRequestID,
				ExpectedOwner: testMutationOwnerID,
			})
			refused = append(refused, cancelErr)
			_, finishErr := manager.finish(&ServiceMutationFinishRequest{
				RequestID: testMutationRequestID,
				OwnerID:   testMutationOwnerID,
				Success:   true,
			})
			refused = append(refused, finishErr)
			_, _, stepErr := manager.acquireStep(ServiceMutationBinding{
				MutationRequestID: testMutationRequestID,
				MutationOwnerID:   testMutationOwnerID,
			})
			refused = append(refused, stepErr)
			for index, refusal := range refused {
				if !errors.Is(refusal, errServiceMutationManagerPoisoned) {
					t.Fatalf("refused operation %d err=%v want poisoned manager", index, refusal)
				}
			}
			afterRefusal, _ := readServiceMutationCrashLedger(t, manager)
			if !bytes.Equal(durableRaw, afterRefusal) {
				t.Fatalf("refused calls changed durable ledger:\nbefore=%s\nafter=%s", durableRaw, afterRefusal)
			}
			finishServiceMutationCrashTestManager(t, manager)
		})
	}
}

func TestServiceMutationPoisonedLedgerReloadsWithoutSecondActiveJob(t *testing.T) {
	manager, root := newMutationTestManager(t)
	fired := injectOneServiceMutationWriteFault(manager, serviceMutationWriteFaultAfterRename)
	_, err := manager.begin(&ServiceMutationBeginRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Kind:      "service_install",
		Target:    "nginx",
	})
	if !errors.Is(err, errServiceMutationManagerPoisoned) || !*fired {
		t.Fatalf("begin post-publish fault err=%v fired=%v", err, *fired)
	}
	beforeRaw, _ := readServiceMutationCrashLedger(t, manager)

	blocked, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if blocked != nil || !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("reload while poisoned owner retains lock manager=%v err=%v", blocked, err)
	}
	afterBlockedRaw, _ := readServiceMutationCrashLedger(t, manager)
	if !bytes.Equal(beforeRaw, afterBlockedRaw) {
		t.Fatalf("blocked startup changed durable ledger:\nbefore=%s\nafter=%s", beforeRaw, afterBlockedRaw)
	}

	manager.mu.Lock()
	if manager.active == nil || manager.active.lock == nil {
		manager.mu.Unlock()
		t.Fatal("poisoned owner lost the retained lock before simulated restart")
	}
	oldLock := manager.active.lock
	manager.active.lock = nil
	manager.mu.Unlock()
	if err := oldLock.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if err != nil {
		t.Fatalf("reload after simulated owner restart: %v", err)
	}
	job := reloaded.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != "agent_restarted_before_completion" {
		t.Fatalf("reloaded manager did not resolve the sole orphan after lock release: %+v", job)
	}
	_, durable := readServiceMutationCrashLedger(t, reloaded)
	if durable.ActiveRequestID != "" {
		t.Fatalf("reloaded ledger still has an active pointer: %+v", durable)
	}
	finishServiceMutationCrashTestManager(t, manager)
}

func TestServiceMutationCancelWinsDeterministicFinishRace(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	_, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	blocked := false
	setServiceMutationWriteFault(manager, func(point string) error {
		if point == serviceMutationWriteFaultBeforeRename && !blocked {
			blocked = true
			close(entered)
			<-release
		}
		return nil
	})
	type result struct {
		job *ServiceMutationJob
		err error
	}
	cancelResult := make(chan result, 1)
	go func() {
		job, cancelErr := manager.cancelJob(&ServiceMutationCancelRequest{
			RequestID:      testMutationRequestID,
			ExpectedOwner:  testMutationOwnerID,
			FailureCode:    "service_mutation_cancelled",
			FailureMessage: "The test explicitly cancelled this mutation.",
		})
		cancelResult <- result{job: job, err: cancelErr}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not reach the deterministic pre-rename gate")
	}
	finishResult := make(chan result, 1)
	go func() {
		job, finishErr := manager.finish(&ServiceMutationFinishRequest{
			RequestID: testMutationRequestID,
			OwnerID:   testMutationOwnerID,
			Success:   true,
		})
		finishResult <- result{job: job, err: finishErr}
	}()
	close(release)

	cancelled := <-cancelResult
	if cancelled.err != nil || cancelled.job == nil ||
		cancelled.job.Status != serviceMutationStatusCancelling ||
		cancelled.job.ErrorCode != "service_mutation_cancelled" {
		t.Fatalf("cancel result job=%+v err=%v", cancelled.job, cancelled.err)
	}
	finished := <-finishResult
	if finished.err == nil || finished.job == nil ||
		finished.job.Status != serviceMutationStatusCancelling {
		t.Fatalf("finish raced past cancellation job=%+v err=%v", finished.job, finished.err)
	}

	setServiceMutationWriteFault(manager, nil)
	done()
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.ErrorCode != "service_mutation_cancelled" {
		t.Fatalf("cancelled mutation did not become terminal after its step exited: %+v", job)
	}
}

func TestServiceMutationSupervisorRetainsLockAcrossStartRegisterCrashGap(t *testing.T) {
	manager, root := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousHook := serviceMutationWorkerFaultHook
	serviceMutationWorkerFaultHook = func(point string, _ *exec.Cmd) error {
		if point != serviceMutationWorkerFaultAfterStartBeforeRegister {
			return nil
		}
		manager.mu.Lock()
		runtime := manager.active
		if runtime == nil || runtime.lock == nil || runtime.lock.file == nil {
			manager.mu.Unlock()
			return errors.New("test runtime lost its parent lock descriptor")
		}
		parentFile := runtime.lock.file
		runtime.lock.file = nil
		manager.mu.Unlock()
		if err := parentFile.Close(); err != nil {
			return fmt.Errorf("simulate parent descriptor loss: %w", err)
		}
		if lock, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errServiceMutationHostBusy) {
			if lock != nil {
				_ = lock.Close()
			}
			return fmt.Errorf("supervisor did not retain host flock: %v", err)
		}
		return errors.New("simulated agent crash after start before register")
	}
	t.Cleanup(func() { serviceMutationWorkerFaultHook = previousHook })

	if _, err := runServiceMutationCombinedOutput(ctx, "/bin/sh", "-c", "sleep 30"); err == nil {
		t.Fatal("injected start/register crash gap unexpectedly succeeded")
	}
	serviceMutationWorkerFaultHook = previousHook

	replacement, err := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock"))
	if err != nil {
		t.Fatalf("supervisor did not release lock after cancellation and wait: %v", err)
	}
	manager.mu.Lock()
	manager.active.lock = replacement
	manager.mu.Unlock()
	done()
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatalf("clean start/register crash-gap test manager: %v", err)
	}
}

func TestServiceMutationCommandTerminalMethodsUseTrackedSupervisorAndPreserveEnv(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousHook := serviceMutationWorkerFaultHook
	hookCalls := 0
	serviceMutationWorkerFaultHook = func(point string, _ *exec.Cmd) error {
		if point != serviceMutationWorkerFaultAfterStartBeforeRegister {
			return nil
		}
		hookCalls++
		return errors.New("terminal method reached tracked supervisor")
	}
	t.Cleanup(func() { serviceMutationWorkerFaultHook = previousHook })

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "Run",
			run: func() error {
				return serviceMutationCommand(ctx, "/bin/sh", "-c", "exit 0").Run()
			},
		},
		{
			name: "Output",
			run: func() error {
				_, err := serviceMutationCommand(ctx, "/bin/sh", "-c", "printf output").Output()
				return err
			},
		},
		{
			name: "CombinedOutput",
			run: func() error {
				_, err := serviceMutationCommand(ctx, "/bin/sh", "-c", "printf combined").CombinedOutput()
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil || !strings.Contains(err.Error(), "terminal method reached tracked supervisor") {
				t.Fatalf("terminal method bypassed tracked supervisor: %v", err)
			}
		})
	}
	if hookCalls != len(tests) {
		t.Fatalf("tracked supervisor hook calls=%d want=%d", hookCalls, len(tests))
	}

	serviceMutationWorkerFaultHook = nil
	cmd := serviceMutationCommand(ctx, "/bin/sh", "-c", `printf %s "$CELIKPANEL_TRACKED_ENV"`)
	cmd.Env = append(os.Environ(), "CELIKPANEL_TRACKED_ENV=preserved")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tracked env command: output=%q err=%v", out, err)
	}
	if string(out) != "preserved" {
		t.Fatalf("tracked command env=%q want preserved", out)
	}

	done()
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEnableServiceMutationReconcilesWithUntrackedProbeAfterCancellation(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	systemctl := filepath.Join(binDir, "systemctl")
	script := `#!/bin/sh
case "$1" in
  enable) sleep 30 ;;
  show) printf 'ActiveState=active\nSubState=running\nResult=success\nConditionResult=yes\n' ;;
  is-enabled) printf 'enabled\n' ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(systemctl, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	previousHook := serviceMutationWorkerFaultHook
	hookCalls := 0
	serviceMutationWorkerFaultHook = func(point string, _ *exec.Cmd) error {
		if point != serviceMutationWorkerFaultAfterStartBeforeRegister {
			return nil
		}
		hookCalls++
		if hookCalls > 1 {
			return errors.New("read-only reconciliation probe inherited durable tracker")
		}
		job, cancelErr := manager.cancelJob(&ServiceMutationCancelRequest{
			RequestID:     testMutationRequestID,
			ExpectedOwner: testMutationOwnerID,
			Reason:        "cancel_during_systemd_client",
		})
		if cancelErr != nil {
			return cancelErr
		}
		if job == nil || job.Status != serviceMutationStatusCancelling {
			return fmt.Errorf("cancelled job status=%v", job)
		}
		return nil
	}
	t.Cleanup(func() { serviceMutationWorkerFaultHook = previousHook })

	if err := enableServiceForMutationWithExecutable(ctx, systemctl, "celikpanel-test.service", true); err != nil {
		t.Fatalf("verified systemd state did not reconcile the cancelled client: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("tracked mutating systemctl hook calls=%d want=1", hookCalls)
	}
	serviceMutationWorkerFaultHook = previousHook
	done()
	job := manager.status(testMutationRequestID)
	if job == nil || job.Status != serviceMutationStatusFailed ||
		job.Phase != "failed" {
		t.Fatalf("cancelled mutation did not finish after reconciliation: %+v", job)
	}
}

func TestServiceMutationSupervisorDoesNotLeakLockFDToWorker(t *testing.T) {
	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	ctx, done, err := manager.acquireStep(ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runServiceMutationCombinedOutput(
		ctx,
		"/bin/sh",
		"-c",
		"test ! -e /proc/self/fd/3",
	); err != nil {
		t.Fatalf("real worker inherited supervisor lock fd: output=%q err=%v", output, err)
	}
	done()
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMutationStartupLeavesLiveWorkerLedgerUnchangedWithoutHostLock(t *testing.T) {
	manager, root := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	started, err := serviceMutationProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	before := cloneServiceMutationLedger(manager.ledger)
	manager.active.job.WorkerPID = os.Getpid()
	manager.active.job.WorkerStarted = started
	manager.active.job.WorkerCommand = "test-agent"
	manager.active.job.UpdatedAt = manager.now()
	err = manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	durableBefore, _ := readServiceMutationCrashLedger(t, manager)

	blocked, err := newServiceMutationManager(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
	)
	if blocked != nil || !errors.Is(err, errServiceMutationHostBusy) {
		t.Fatalf("startup with live worker and held lock manager=%v err=%v", blocked, err)
	}
	durableAfter, _ := readServiceMutationCrashLedger(t, manager)
	if !bytes.Equal(durableBefore, durableAfter) {
		t.Fatalf("blocked live-worker startup changed ledger:\nbefore=%s\nafter=%s", durableBefore, durableAfter)
	}

	manager.mu.Lock()
	before = cloneServiceMutationLedger(manager.ledger)
	manager.active.job.WorkerPID = 0
	manager.active.job.WorkerStarted = ""
	manager.active.job.WorkerCommand = ""
	manager.active.job.UpdatedAt = manager.now()
	err = manager.persistLedgerMutationLocked(before)
	manager.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   false,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceMutationStartupPostPublishFailurePoisonsAndRetainsLock(t *testing.T) {
	manager, root := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	manager.mu.Lock()
	runtime := manager.active
	runtime.cancel()
	oldLock := runtime.lock
	runtime.lock = nil
	manager.poisoned = errors.New("simulated stopped original agent")
	manager.mu.Unlock()
	if err := oldLock.Close(); err != nil {
		t.Fatal(err)
	}

	fired := false
	reloaded, err := newServiceMutationManagerWithWriteFault(
		filepath.Join(root, "state"),
		filepath.Join(root, "service-mutation.lock"),
		func(point string) error {
			if point == serviceMutationWriteFaultAfterRename && !fired {
				fired = true
				return errors.New("simulated startup directory-sync crash")
			}
			return nil
		},
	)
	if reloaded == nil || !fired || !errors.Is(err, errServiceMutationManagerPoisoned) {
		t.Fatalf("ambiguous startup reconcile manager=%v fired=%v err=%v", reloaded, fired, err)
	}
	reloaded.mu.Lock()
	retained := reloaded.poisonLock != nil && reloaded.poisonLock.file != nil
	reloaded.mu.Unlock()
	if !retained {
		t.Fatal("ambiguous startup reconcile did not retain its reconciliation lock")
	}
	if lock, lockErr := acquireServiceMutationFileLock(filepath.Join(root, "service-mutation.lock")); !errors.Is(lockErr, errServiceMutationHostBusy) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("ambiguous startup reconcile released host lock: %v", lockErr)
	}
	_, durable := readServiceMutationCrashLedger(t, reloaded)
	if err := validateServiceMutationLedger(&durable); err != nil {
		t.Fatalf("ambiguous startup reconcile published invalid ledger: %v", err)
	}
	reloaded.mu.Lock()
	if reloaded.poisonLock != nil {
		_ = reloaded.poisonLock.Close()
		reloaded.poisonLock = nil
	}
	reloaded.mu.Unlock()
}

func TestServiceMutationExternalLockModesRequireExactInheritedFlock(t *testing.T) {
	newState := func(t *testing.T) (string, string) {
		t.Helper()
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatal(err)
		}
		return stateDir, lockPath
	}
	checks := func(stateDir, lockPath string) []struct {
		name string
		run  func() error
	} {
		return []struct {
			name string
			run  func() error
		}{
			{name: "idle", run: func() error {
				return checkServiceMutationIdleUnderExternalLock(stateDir, lockPath)
			}},
			{name: "pre-ledger", run: func() error {
				return checkPreLedgerServiceMutationIdleUnderExternalLock(stateDir, lockPath)
			}},
			{name: "initial", run: func() error {
				return checkInitialServiceMutationLedgerUnderExternalLock(stateDir, lockPath)
			}},
		}
	}
	assertRejected := func(t *testing.T, stateDir, lockPath string) {
		t.Helper()
		for _, check := range checks(stateDir, lockPath) {
			if err := check.run(); err == nil {
				t.Fatalf("%s external-lock mode accepted unproved authority", check.name)
			}
		}
	}

	t.Run("missing descriptor", func(t *testing.T) {
		stateDir, lockPath := newState(t)
		t.Setenv(serviceMutationExternalLockFDEnvironment, "")
		assertRejected(t, stateDir, lockPath)
	})

	t.Run("unlocked common descriptor", func(t *testing.T) {
		stateDir, lockPath := newState(t)
		file, err := os.OpenFile(lockPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(file.Fd())))
		assertRejected(t, stateDir, lockPath)
	})

	t.Run("wrong descriptor", func(t *testing.T) {
		stateDir, lockPath := newState(t)
		file, err := os.Open(filepath.Join(stateDir, serviceMutationLedgerFileName))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(file.Fd())))
		assertRejected(t, stateDir, lockPath)
	})

	t.Run("wrong metadata despite held flock", func(t *testing.T) {
		stateDir, lockPath := newState(t)
		fd, err := unix.Open(lockPath, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		if err := unix.Fchmod(fd, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer unix.Flock(fd, unix.LOCK_UN)
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(fd))
		assertRejected(t, stateDir, lockPath)
	})

	t.Run("exact held descriptor", func(t *testing.T) {
		stateDir, lockPath := newState(t)
		lock, err := acquireServiceMutationFileLock(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(lock.file.Fd())))
		for _, check := range checks(stateDir, lockPath) {
			if err := check.run(); err != nil {
				t.Fatalf("%s external-lock mode rejected exact held descriptor: %v", check.name, err)
			}
		}
	})
}

func TestServiceMutationStartupCleansOnlyCanonicalAbandonedWriteStages(t *testing.T) {
	t.Run("canonical stage is removed", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(stateDir, serviceMutationLedgerFileName))
		if err != nil {
			t.Fatal(err)
		}
		stagePath := filepath.Join(stateDir, ".service-mutations-123.json")
		if err := os.WriteFile(stagePath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := newServiceMutationManager(stateDir, lockPath); err != nil {
			t.Fatalf("canonical abandoned stage was not recovered: %v", err)
		}
		if _, err := os.Lstat(stagePath); !os.IsNotExist(err) {
			t.Fatalf("canonical abandoned stage remains after recovery: %v", err)
		}
	})

	unsafe := []struct {
		name  string
		setup func(t *testing.T, stateDir, stagePath string, canonical []byte)
	}{
		{
			name: "partial content",
			setup: func(t *testing.T, _, stagePath string, _ []byte) {
				if err := os.WriteFile(stagePath, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, _, stagePath string, canonical []byte) {
				if err := os.WriteFile(stagePath, canonical, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(stagePath, 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, stateDir, stagePath string, canonical []byte) {
				target := filepath.Join(stateDir, "stage-target")
				if err := os.WriteFile(target, canonical, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, stagePath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hard link",
			setup: func(t *testing.T, stateDir, stagePath string, canonical []byte) {
				if err := os.WriteFile(stagePath, canonical, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(stagePath, filepath.Join(stateDir, "stage-second-link")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range unsafe {
		t.Run(test.name+" fails closed", func(t *testing.T) {
			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			lockPath := filepath.Join(root, "service-mutation.lock")
			if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
				t.Fatal(err)
			}
			canonical, err := os.ReadFile(filepath.Join(stateDir, serviceMutationLedgerFileName))
			if err != nil {
				t.Fatal(err)
			}
			stagePath := filepath.Join(stateDir, ".service-mutations-456.json")
			test.setup(t, stateDir, stagePath, canonical)
			if manager, err := newServiceMutationManager(stateDir, lockPath); manager != nil || err == nil {
				t.Fatalf("unsafe abandoned stage manager=%v err=%v", manager, err)
			}
			if lock, err := acquireServiceMutationFileLock(lockPath); err != nil {
				t.Fatalf("failed startup retained lock for a prepublish cleanup error: %v", err)
			} else {
				_ = lock.Close()
			}
		})
	}

	t.Run("too many stages fail closed before deletion", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatal(err)
		}
		canonical, err := os.ReadFile(filepath.Join(stateDir, serviceMutationLedgerFileName))
		if err != nil {
			t.Fatal(err)
		}
		for index := 0; index <= serviceMutationStageLimit; index++ {
			path := filepath.Join(stateDir, fmt.Sprintf(".service-mutations-%03d.json", index))
			if err := os.WriteFile(path, canonical, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if manager, err := newServiceMutationManager(stateDir, lockPath); manager != nil || err == nil {
			t.Fatalf("excess abandoned stages manager=%v err=%v", manager, err)
		}
		entries, err := os.ReadDir(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		stageCount := 0
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".service-mutations-") &&
				!strings.HasPrefix(entry.Name(), initialServiceMutationStagePrefix) {
				stageCount++
			}
		}
		if stageCount != serviceMutationStageLimit+1 {
			t.Fatalf("bounded cleanup partially deleted stages: got %d", stageCount)
		}
	})
}

func TestServiceMutationLedgerRejectsCrossFieldInvariantViolations(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	active := ServiceMutationJob{
		RequestID:      testMutationRequestID,
		OwnerID:        testMutationOwnerID,
		Kind:           "service_install",
		Target:         "nginx",
		Status:         serviceMutationStatusRunning,
		Phase:          "installing",
		Attempt:        1,
		StartedAt:      now,
		UpdatedAt:      now,
		LeaseExpiresAt: now.Add(time.Minute),
		DeadlineAt:     now.Add(10 * time.Minute),
	}
	validActive := serviceMutationLedger{
		Version:         serviceMutationLedgerVersion,
		ActiveRequestID: active.RequestID,
		Jobs:            map[string]*ServiceMutationJob{active.RequestID: &active},
	}
	if err := validateServiceMutationLedger(&validActive); err != nil {
		t.Fatalf("valid active ledger rejected: %v", err)
	}

	terminal := active
	terminal.Status = serviceMutationStatusFailed
	terminal.Phase = "failed"
	terminal.UpdatedAt = now.Add(2 * time.Minute)
	terminal.FinishedAt = terminal.UpdatedAt
	terminal.LeaseExpiresAt = time.Time{}
	validTerminal := serviceMutationLedger{
		Version: serviceMutationLedgerVersion,
		Jobs:    map[string]*ServiceMutationJob{terminal.RequestID: &terminal},
	}
	if err := validateServiceMutationLedger(&validTerminal); err != nil {
		t.Fatalf("valid terminal ledger rejected: %v", err)
	}

	tests := []struct {
		name   string
		active bool
		mutate func(*ServiceMutationJob)
	}{
		{name: "invalid owner", active: true, mutate: func(job *ServiceMutationJob) {
			job.OwnerID = "not-an-owner"
		}},
		{name: "partial worker identity", active: true, mutate: func(job *ServiceMutationJob) {
			job.WorkerPID = 123
		}},
		{name: "active finish timestamp", active: true, mutate: func(job *ServiceMutationJob) {
			job.FinishedAt = now.Add(time.Minute)
		}},
		{name: "updated before start", active: true, mutate: func(job *ServiceMutationJob) {
			job.UpdatedAt = now.Add(-time.Second)
		}},
		{name: "lease after deadline", active: true, mutate: func(job *ServiceMutationJob) {
			job.LeaseExpiresAt = job.DeadlineAt.Add(time.Second)
		}},
		{name: "terminal worker", mutate: func(job *ServiceMutationJob) {
			job.WorkerPID = 123
			job.WorkerStarted = "456"
			job.WorkerCommand = "apt-get"
		}},
		{name: "terminal missing finish", mutate: func(job *ServiceMutationJob) {
			job.FinishedAt = time.Time{}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := terminal
			ledger := serviceMutationLedger{
				Version: serviceMutationLedgerVersion,
				Jobs:    map[string]*ServiceMutationJob{job.RequestID: &job},
			}
			if test.active {
				job = active
				ledger.ActiveRequestID = job.RequestID
				ledger.Jobs[job.RequestID] = &job
			}
			test.mutate(&job)
			if err := validateServiceMutationLedger(&ledger); err == nil {
				t.Fatal("cross-field invariant violation was accepted")
			}
		})
	}
}
