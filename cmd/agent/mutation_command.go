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

func (c *serviceMutationCmd) execute(combined bool) ([]byte, error) {
	cmd := exec.CommandContext(c.ctx, c.name, c.args...)
	cmd.Env = c.Env
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	configureServiceMutationProcessGroup(cmd)
	if combined {
		return runTrackedServiceMutationCommand(c.ctx, cmd, c.name)
	}
	return runTrackedServiceMutationOutput(c.ctx, cmd, c.name)
}

func (c *serviceMutationCmd) CombinedOutput() ([]byte, error) {
	return c.execute(true)
}

func (c *serviceMutationCmd) Output() ([]byte, error) {
	return c.execute(false)
}

func (c *serviceMutationCmd) Run() error {
	_, err := c.execute(true)
	return err
}
