//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	serviceMutationSupervisorMode   = "--internal-service-mutation-supervisor"
	serviceMutationSupervisorLockFD = 3
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == serviceMutationSupervisorMode {
		os.Exit(runServiceMutationSupervisor(os.Args[2:]))
	}
}

// The supervisor alone inherits the host-lock descriptor. It marks that
// descriptor close-on-exec before starting the real command, so the command
// cannot accidentally unlock or close the shared open-file description.
// Host-kilit descriptor'ını yalnız supervisor miras alır. Gerçek komutu
// başlatmadan önce descriptor'a close-on-exec koyar; böylece komut ortak açık
// dosya tanımının kilidini yanlışlıkla açamaz veya kapatamaz.
func superviseServiceMutationCommand(
	ctx context.Context,
	cmd *exec.Cmd,
	tracker *serviceMutationExecutionTracker,
) (*exec.Cmd, error) {
	if cmd == nil || tracker == nil || tracker.manager == nil ||
		tracker.runtime == nil || tracker.runtime.lock == nil ||
		tracker.runtime.lock.file == nil {
		return nil, errors.New("service mutation supervisor requires the retained host lock")
	}
	if len(cmd.ExtraFiles) != 0 {
		return nil, errors.New("service mutation commands may not supply inherited files")
	}
	lockFile := tracker.runtime.lock.file
	if err := verifyInheritedServiceMutationFileLockFD(
		tracker.manager.lockPath,
		int(lockFile.Fd()),
	); err != nil {
		return nil, fmt.Errorf("prove parent service mutation host lock: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve service mutation supervisor executable: %w", err)
	}
	args := []string{
		serviceMutationSupervisorMode,
		tracker.manager.lockPath,
		cmd.Path,
	}
	if len(cmd.Args) > 1 {
		args = append(args, cmd.Args[1:]...)
	}
	supervisor := exec.CommandContext(ctx, executable, args...)
	supervisor.Env = cmd.Env
	supervisor.Dir = cmd.Dir
	supervisor.Stdin = cmd.Stdin
	supervisor.ExtraFiles = []*os.File{lockFile}
	configureServiceMutationProcessGroup(supervisor)
	return supervisor, nil
}

func runServiceMutationSupervisor(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "service mutation supervisor arguments are incomplete")
		return 125
	}
	lockPath, commandPath := args[0], args[1]
	if err := verifyInheritedServiceMutationFileLockFD(
		lockPath,
		serviceMutationSupervisorLockFD,
	); err != nil {
		fmt.Fprintf(os.Stderr, "service mutation supervisor lock proof failed: %v\n", err)
		return 125
	}
	flags, err := unix.FcntlInt(
		uintptr(serviceMutationSupervisorLockFD),
		unix.F_GETFD,
		0,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "inspect supervisor lock descriptor flags: %v\n", err)
		return 125
	}
	if _, err := unix.FcntlInt(
		uintptr(serviceMutationSupervisorLockFD),
		unix.F_SETFD,
		flags|unix.FD_CLOEXEC,
	); err != nil {
		fmt.Fprintf(os.Stderr, "isolate supervisor lock descriptor from worker: %v\n", err)
		return 125
	}
	child := exec.Command(commandPath, args[2:]...)
	child.Env = os.Environ()
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start supervised service mutation worker: %v\n", err)
		return 126
	}
	err = child.Wait()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		fmt.Fprintf(os.Stderr, "wait for supervised service mutation worker: %v\n", err)
		return 125
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 125
	}
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return status.ExitStatus()
}
