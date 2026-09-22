//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

// The durable enrollment reservation, not an RPC lease, owns these commands.
// This closed adapter also works in the independent renewal build. The caller
// validates the accepted operation and inherited locks at every native boundary.
type mailEnrollmentNativeHost struct{}

func (mailEnrollmentNativeHost) ObserveUnit(ctx context.Context, unit string) ([]byte, error) {
	return runMailEnrollmentSystemctl(ctx, "show", unit)
}
func (mailEnrollmentNativeHost) Reload(ctx context.Context) error {
	_, err := runMailEnrollmentSystemctl(ctx, "daemon-reload", "")
	return err
}
func (mailEnrollmentNativeHost) StartTimer(ctx context.Context) error {
	_, err := runMailEnrollmentSystemctl(ctx, "start", mailrenewalkit.TimerName)
	return err
}
func (mailEnrollmentNativeHost) StopTimer(ctx context.Context) error {
	_, err := runMailEnrollmentSystemctl(ctx, "stop", mailrenewalkit.TimerName)
	return err
}
func mailEnrollmentSystemctlArgs(action, unit string) ([]string, error) {
	switch action {
	case "show":
		if unit != mailrenewalkit.ServiceName && unit != mailrenewalkit.TimerName {
			break
		}
		args := []string{"show", "--no-pager"}
		for _, p := range mailrenewalkit.ScheduleProperties() {
			args = append(args, "--property="+p)
		}
		return append(args, unit), nil
	case "daemon-reload":
		if unit == "" {
			return []string{action}, nil
		}
	case "start", "stop":
		if unit == mailrenewalkit.TimerName {
			return []string{action, unit}, nil
		}
	}
	return nil, errors.New("unsupported native mail enrollment command")
}
func runMailEnrollmentSystemctl(ctx context.Context, action, unit string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("native mail enrollment context is missing")
	}
	args, err := mailEnrollmentSystemctlArgs(action, unit)
	if err != nil {
		return nil, err
	}
	const path = "/usr/bin/systemctl"
	if _, err = validateTrustedExecutablePath(path, "mail enrollment systemctl"); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, path, args...)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C", "SYSTEMD_PAGER=cat"}
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = 2 * time.Second
	stdout, stderr := newServiceMutationOutputBuffer(8192), newServiceMutationOutputBuffer(8192)
	command.Stdout, command.Stderr = stdout, stderr
	err = command.Run()
	if bounded.Err() != nil {
		return nil, fmt.Errorf("mail enrollment %s timed out or was interrupted; preserve the operation and inspect native systemd state before resuming: %w", action, bounded.Err())
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errors.New("mail enrollment systemd output exceeded its limit; the native result remains unknown")
	}
	return readMailEnrollmentSystemctlResult(action, unit, stdout.Bytes(), stderr.Bytes(), err)
}
func readMailEnrollmentSystemctlResult(action, unit string, stdout, stderr []byte, err error) ([]byte, error) {
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, hostcmd.Fail("native mail enrollment could not start; inspect systemd and resume the same operation", stderr, err, classifyMailEnrollmentCommand)
		}
		code = exit.ExitCode()
	}
	if action == "show" {
		if _, parseErr := mailrenewalkit.ParseUnitObservationResult(unit, stdout, code); parseErr != nil {
			return nil, errors.Join(parseErr, hostcmd.Fail("native mail enrollment state could not be verified; inspect systemd and resume the same operation", stderr, err, classifyMailEnrollmentCommand))
		}
		return stdout, nil
	}
	if err != nil {
		return nil, hostcmd.Fail("native mail enrollment "+action+" failed; the owner must inspect systemd, resolve the cause and resume the same operation", stderr, err, classifyMailEnrollmentCommand)
	}
	return nil, nil
}
func classifyMailEnrollmentCommand(message string) string {
	s := strings.ToLower(message)
	switch {
	case strings.Contains(s, "permission denied"), strings.Contains(s, "access denied"):
		return "systemd denied the operation"
	case strings.Contains(s, "masked"):
		return "the native mail renewal unit is masked"
	case strings.Contains(s, "failed to connect to bus"):
		return "the native systemd manager is unavailable"
	case strings.Contains(s, "timed out"), strings.Contains(s, "timeout"):
		return "the native systemd request timed out"
	}
	return ""
}
