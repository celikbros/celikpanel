package services

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const defaultNginxCommandTimeout = 30 * time.Second

var nginxCommandTimeout = defaultNginxCommandTimeout

var executeNginxCommand = func(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}

// runNginxCommand gives validation and reload an agent-owned deadline.
// net/rpc cancellation only closes the transport; it cannot stop a handler
// that has already entered the global nginx mutation transaction.
func runNginxCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nginxCommandTimeout)
	defer cancel()

	output, err := executeNginxCommand(ctx, name, args...)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output, fmt.Errorf(
			"%s timed out after %s: %w",
			name,
			nginxCommandTimeout,
			ctxErr,
		)
	}
	return output, err
}
