//go:build !linux

package main

import (
	"context"
	"errors"
	"os/exec"
)

func superviseServiceMutationCommand(
	_ context.Context,
	cmd *exec.Cmd,
	tracker *serviceMutationExecutionTracker,
) (*exec.Cmd, error) {
	if tracker != nil {
		return nil, errors.New("durable service mutation supervision is supported only on Linux")
	}
	return cmd, nil
}
