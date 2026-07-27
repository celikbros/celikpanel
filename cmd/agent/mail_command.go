package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const defaultMailTLSCommandTimeout = 30 * time.Second

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
