package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	panelCertIssueTimeout   = 15 * time.Minute
	panelCertCleanupTimeout = 2 * time.Minute
	panelCertSystemdTimeout = 30 * time.Second
	panelCertWaitDelay      = 2 * time.Second
)

var executePanelCertCommand = func(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = panelCertWaitDelay
	return cmd.CombinedOutput()
}

// runPanelCertCommand gives certificate and systemd subprocesses an
// agent-owned deadline. A disconnected net/rpc client cannot cancel work
// already dispatched inside the agent.
func runPanelCertCommand(
	timeout time.Duration,
	name string,
	args ...string,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	output, err := executePanelCertCommand(ctx, name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, fmt.Errorf(
			"%s timed out after %s: %w",
			name,
			timeout,
			ctxErr,
		)
	}
	return output, err
}

// runPanelCertMutationCommand is the certificate command path for an active
// durable service mutation. Unlike the detached compatibility helper above,
// it inherits the mutation deadline and uses the supervised runner so an
// update or rollback can neither miss nor orphan the privileged subprocess.
func runPanelCertMutationCommand(
	parent context.Context,
	timeout time.Duration,
	name string,
	args ...string,
) ([]byte, error) {
	if parent == nil {
		return nil, fmt.Errorf("%s requires a durable mutation context", name)
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := runServiceMutationCombinedOutput(ctx, name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, fmt.Errorf(
			"%s stopped after %s: %w",
			name,
			timeout,
			ctxErr,
		)
	}
	return output, err
}

func panelCertCommandError(label string, output []byte, err error) error {
	detail := certbotFirstError(string(output))
	if detail == "" {
		return fmt.Errorf("%s: %w", label, err)
	}
	return fmt.Errorf("%s: %s: %w", label, detail, err)
}
