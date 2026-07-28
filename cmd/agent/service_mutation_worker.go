package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type serviceMutationExecutionTrackerKey struct{}

type serviceMutationExecutionTracker struct {
	manager *serviceMutationManager
	runtime *serviceMutationRuntime
}

func runServiceMutationCombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := serviceMutationCommand(ctx, name, args...)
	return runTrackedServiceMutationCommand(ctx, cmd, name)
}

func runServiceMutationCombinedOutputEnv(
	ctx context.Context,
	env []string,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := serviceMutationCommand(ctx, name, args...)
	cmd.Env = env
	return runTrackedServiceMutationCommand(ctx, cmd, name)
}

func runTrackedServiceMutationCommand(ctx context.Context, cmd *exec.Cmd, name string) ([]byte, error) {
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return output.Bytes(), err
	}

	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
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
	if m.active != t.runtime || t.runtime.job.Status != serviceMutationStatusRunning ||
		t.runtime.steps != 1 || t.runtime.job.WorkerPID != 0 {
		return errors.New("service mutation cannot register this privileged worker")
	}
	t.runtime.job.WorkerPID = pid
	t.runtime.job.WorkerStarted = started
	t.runtime.job.WorkerCommand = command
	t.runtime.job.UpdatedAt = m.now()
	if err := m.writeLocked(); err != nil {
		t.runtime.job.WorkerPID = 0
		t.runtime.job.WorkerStarted = ""
		t.runtime.job.WorkerCommand = ""
		return err
	}
	return nil
}

func (t *serviceMutationExecutionTracker) clear(pid int) error {
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != t.runtime || t.runtime.job.WorkerPID != pid {
		return errors.New("privileged worker identity changed before completion")
	}
	t.runtime.job.WorkerPID = 0
	t.runtime.job.WorkerStarted = ""
	t.runtime.job.WorkerCommand = ""
	t.runtime.job.UpdatedAt = m.now()
	return m.writeLocked()
}
