package main

import (
	"context"
	"os/exec"
)

func serviceMutationCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	configureServiceMutationProcessGroup(cmd)
	return cmd
}
