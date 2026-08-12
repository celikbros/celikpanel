package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const serviceMutationWorkerFaultAfterStartBeforeRegister = "after_start_before_register"

var serviceMutationWorkerFaultHook func(string, *exec.Cmd) error

type serviceMutationExecutionTrackerKey struct{}

type serviceMutationExecutionTracker struct {
	manager                 *serviceMutationManager
	runtime                 *serviceMutationRuntime
	allowCancellingRecovery bool
}

// serviceMutationCancellingRecoveryContext keeps rollback commands bounded,
// tracked in the durable worker ledger, and eligible to register while the
// owning mutation is cancelling. It never detaches or disables the tracker.
func serviceMutationCancellingRecoveryContext(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil || timeout <= 0 {
		return nil, nil, errors.New("invalid service mutation recovery context")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return nil, nil, errors.New("service mutation recovery requires a durable execution tracker")
	}
	recoveryBase := context.WithoutCancel(ctx)
	recoveryTracker := *tracker
	recoveryTracker.allowCancellingRecovery = true
	recoveryBase = context.WithValue(
		recoveryBase,
		serviceMutationExecutionTrackerKey{},
		&recoveryTracker,
	)
	recoveryCtx, cancel := context.WithTimeout(recoveryBase, timeout)
	return recoveryCtx, cancel, nil
}

func runServiceMutationCombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return serviceMutationCommand(ctx, name, args...).CombinedOutput()
}

func runServiceMutationCombinedOutputEnv(
	ctx context.Context,
	env []string,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := serviceMutationCommand(ctx, name, args...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

func runTrackedServiceMutationCommand(ctx context.Context, cmd *exec.Cmd, name string) ([]byte, error) {
	return runTrackedServiceMutationCommandOutput(ctx, cmd, name, true)
}

func runTrackedServiceMutationOutput(ctx context.Context, cmd *exec.Cmd, name string) ([]byte, error) {
	return runTrackedServiceMutationCommandOutput(ctx, cmd, name, false)
}

func runTrackedServiceMutationCommandOutput(
	ctx context.Context,
	cmd *exec.Cmd,
	name string,
	combined bool,
) ([]byte, error) {
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker != nil {
		var err error
		cmd, err = superviseServiceMutationCommand(ctx, cmd, tracker)
		if err != nil {
			return nil, err
		}
	}
	var output bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &output
	if combined {
		cmd.Stderr = &output
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}
	if tracker != nil && serviceMutationWorkerFaultHook != nil {
		if err := serviceMutationWorkerFaultHook(
			serviceMutationWorkerFaultAfterStartBeforeRegister,
			cmd,
		); err != nil {
			_ = cmd.Cancel()
			_ = cmd.Wait()
			return output.Bytes(), fmt.Errorf("injected privileged worker start failure: %w", err)
		}
	}

	registered := false
	if tracker != nil {
		started, err := serviceMutationProcessStartIdentity(cmd.Process.Pid)
		if err != nil {
			_ = cmd.Cancel()
			_ = cmd.Wait()
			return output.Bytes(), fmt.Errorf("identify privileged worker: %w", err)
		}
		command := filepath.Base(strings.TrimSpace(name))
		if len(command) > 64 {
			command = command[:64]
		}
		if err := tracker.register(cmd.Process.Pid, started, command); err != nil {
			_ = cmd.Cancel()
			_ = cmd.Wait()
			return output.Bytes(), fmt.Errorf("persist privileged worker identity: %w", err)
		}
		registered = true
	}

	waitErr := cmd.Wait()
	if !combined {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitErr.Stderr = append([]byte(nil), stderr.Bytes()...)
		}
	}
	if registered {
		if clearErr := tracker.clear(cmd.Process.Pid); clearErr != nil {
			if waitErr != nil {
				return output.Bytes(), errors.Join(waitErr, clearErr)
			}
			return output.Bytes(), clearErr
		}
	}
	return output.Bytes(), waitErr
}

func (t *serviceMutationExecutionTracker) register(pid int, started, command string) error {
	if t == nil || t.manager == nil || t.runtime == nil || pid <= 0 || started == "" {
		return errors.New("invalid privileged worker identity")
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	statusAllowsWorker := t.runtime.job.Status == serviceMutationStatusRunning ||
		(t.allowCancellingRecovery && t.runtime.job.Status == serviceMutationStatusCancelling)
	if m.active != t.runtime || !statusAllowsWorker ||
		t.runtime.steps != 1 || t.runtime.job.WorkerPID != 0 {
		return errors.New("service mutation cannot register this privileged worker")
	}
	before := cloneServiceMutationLedger(m.ledger)
	t.runtime.job.WorkerPID = pid
	t.runtime.job.WorkerStarted = started
	t.runtime.job.WorkerCommand = command
	t.runtime.job.UpdatedAt = m.now()
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return err
	}
	return nil
}

func (t *serviceMutationExecutionTracker) clear(pid int) error {
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	if m.active != t.runtime || t.runtime.job.WorkerPID != pid {
		return errors.New("privileged worker identity changed before completion")
	}
	before := cloneServiceMutationLedger(m.ledger)
	t.runtime.job.WorkerPID = 0
	t.runtime.job.WorkerStarted = ""
	t.runtime.job.WorkerCommand = ""
	t.runtime.job.UpdatedAt = m.now()
	return m.persistLedgerMutationLocked(before)
}
