package main

import (
	"context"
	"io"
	"os/exec"
)

// serviceMutationCmd deliberately does not expose exec.Cmd.Start/Wait. Every
// terminal execution method is routed through runTrackedServiceMutationCommand,
// so a context carrying a durable mutation tracker cannot accidentally launch
// an unregistered child.
//
// serviceMutationCmd bilerek exec.Cmd.Start/Wait'i açığa çıkarmaz. Her terminal
// çalıştırma yöntemi runTrackedServiceMutationCommand üzerinden geçer; böylece
// kalıcı mutation tracker taşıyan bir context yanlışlıkla kayıtsız child
// başlatamaz.
type serviceMutationCmd struct {
	ctx   context.Context
	name  string
	args  []string
	Env   []string
	Dir   string
	Stdin io.Reader
}

func serviceMutationCommand(ctx context.Context, name string, args ...string) *serviceMutationCmd {
	if ctx == nil {
		ctx = context.Background()
	}
	return &serviceMutationCmd{
		ctx:  ctx,
		name: name,
		args: append([]string(nil), args...),
	}
}

func (c *serviceMutationCmd) execute(combined bool, outputLimit int) ([]byte, error) {
	cmd := exec.CommandContext(c.ctx, c.name, c.args...)
	cmd.Env = c.Env
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	configureServiceMutationProcessGroup(cmd)
	if combined {
		return runTrackedServiceMutationCommandLimited(c.ctx, cmd, c.name, outputLimit)
	}
	return runTrackedServiceMutationOutputLimited(c.ctx, cmd, c.name, outputLimit)
}

func (c *serviceMutationCmd) CombinedOutput() ([]byte, error) {
	return c.execute(true, 0)
}

func (c *serviceMutationCmd) CombinedOutputLimited(maximumBytes int) ([]byte, error) {
	return c.execute(true, maximumBytes)
}

func (c *serviceMutationCmd) Output() ([]byte, error) {
	return c.execute(false, 0)
}

func (c *serviceMutationCmd) Run() error {
	_, err := c.execute(true, 0)
	return err
}
