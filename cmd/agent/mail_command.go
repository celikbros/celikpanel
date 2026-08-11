package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	defaultMailTLSCommandTimeout = 30 * time.Second
	mailMutationLockRetry        = 10 * time.Millisecond
)

var mailTLSCommandTimeout = defaultMailTLSCommandTimeout

var executeMailTLSCommand = func(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}

// runMailTLSCommand gives every Postfix/Dovecot/systemd subprocess an
// agent-owned deadline. Closing a net/rpc client cannot cancel a dispatched
// server method, so the handler itself must bound every external operation.
func runMailTLSCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mailTLSCommandTimeout)
	defer cancel()

	output, err := executeMailTLSCommand(ctx, name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, fmt.Errorf(
			"%s timed out after %s: %w",
			name,
			mailTLSCommandTimeout,
			ctxErr,
		)
	}
	return output, err
}

// runMailTLSMutationCommand is the durable-service-mutation counterpart of
// runMailTLSCommand. It keeps the lease context attached to the child so host
// commands are tracked and killed when ownership is lost, while retaining the
// per-command deadline used by the legacy lifecycle RPC.
func runMailTLSMutationCommand(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%s: service mutation context is required", name)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%s canceled before execution: %w", name, err)
	}

	commandCtx, cancel := context.WithTimeout(ctx, mailTLSCommandTimeout)
	defer cancel()
	output, err := serviceMutationCommand(commandCtx, name, args...).CombinedOutput()
	if ctxErr := commandCtx.Err(); ctxErr != nil {
		if leaseErr := ctx.Err(); leaseErr != nil {
			return output, fmt.Errorf("%s canceled by service mutation lease: %w", name, leaseErr)
		}
		return output, fmt.Errorf(
			"%s timed out after %s: %w",
			name,
			mailTLSCommandTimeout,
			ctxErr,
		)
	}
	return output, err
}

// lockMailMutation acquires the process-wide mail transaction lock without
// outliving the durable mutation lease. The post-acquire context check closes
// the race where cancellation and TryLock success happen together.
func lockMailMutation(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("service mutation lease context is required")
	}
	ticker := time.NewTicker(mailMutationLockRetry)
	defer ticker.Stop()
	for {
		if mailMutex.TryLock() {
			if err := ctx.Err(); err != nil {
				mailMutex.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
