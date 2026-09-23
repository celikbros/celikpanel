//go:build linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// This is explicit owner dispatch, never polling or an update entrypoint. A
// successful handoff means systemd accepted the worker, not enrollment completion.
func launchIndependentMailEnrollment(ctx context.Context, accepted []string) error {
	if !mailRenewalOnlyBuild || os.Geteuid() != 0 || !validMailEnrollmentWorkerArgs(accepted) {
		return servicemutationledger.ErrMailEnrollment
	}
	helper, err := verifiedMailEnrollmentHelper()
	if err != nil {
		return err
	}
	return dispatchMailEnrollmentHelper(ctx, helper, accepted)
}

func dispatchMailEnrollmentHelper(ctx context.Context, helper string, accepted []string) error {
	if ctx == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runner, err := resolveFixedRootExecutable("/usr/bin/systemd-run")
	if err != nil {
		return err
	}
	args, err := mailEnrollmentWorkerUnitArgs(helper, accepted)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = runFixedSystemUpdateCommand(bounded, runner, args)
	if err != nil {
		return errors.New("mail enrollment handoff is unconfirmed; inspect the same request's native worker unit before retrying")
	}
	return nil
}

func validMailEnrollmentWorkerArgs(args []string) bool {
	return (len(args) == 1 && validMutationIdentity(args[0])) ||
		(len(args) == 3 && validMutationIdentity(args[0]) && validMutationIdentity(args[1]) && recoveryruntime.ValidDigest(args[2]))
}
func mailEnrollmentWorkerUnitArgs(helper string, accepted []string) ([]string, error) {
	if !validMailEnrollmentWorkerArgs(accepted) || !filepath.IsAbs(helper) || filepath.Clean(helper) != helper || filepath.Base(helper) != mailrenewalkit.BinaryName ||
		filepath.Dir(filepath.Dir(helper)) != mailrenewalkit.InstalledRoot || !recoveryruntime.ValidDigest(filepath.Base(filepath.Dir(helper))) {
		return nil, servicemutationledger.ErrMailEnrollment
	}
	if len(accepted) == 3 && accepted[2] != filepath.Base(filepath.Dir(helper)) {
		return nil, servicemutationledger.ErrMailEnrollment
	}
	args := []string{"--unit=celikpanel-mail-enrollment-" + accepted[0] + ".service", "--property=Type=exec", "--property=User=root", "--property=Group=root", "--property=UMask=0077", "--property=KillMode=control-group", "--property=TimeoutStartSec=30s", "--property=RuntimeMaxSec=3min", "--property=TimeoutStopSec=15s", "--collect", "--no-block", "--", helper, "--enrollment-worker"}
	return append(args, accepted...), nil
}

// The detached worker owns exclusion before executing the consumer. Re-exec via
// a pinned descriptor assigns fd8/fd9 without overwriting Go runtime descriptors.
// Recorded owner continuation can restore absent volatile locks from verified
// durable evidence under the release lock; existing owner paths are preserved.
func runIndependentMailEnrollmentWorker(ctx context.Context, accepted []string) error {
	return runIndependentMailEnrollmentWorkerMode(ctx, accepted, false)
}
func runIndependentMailEnrollmentWorkerMode(ctx context.Context, accepted []string, automatic bool) error {
	if !mailRenewalOnlyBuild || os.Geteuid() != 0 || !validMailEnrollmentWorkerArgs(accepted) || automatic && len(accepted) != 1 {
		return servicemutationledger.ErrMailEnrollment
	}
	gid, ok := lookupGroupID("celikpanel")
	if !ok || gid < 0 || uint32(gid) != serviceMutationRequiredOwnerGID {
		return servicemutationledger.ErrMailEnrollment
	}
	args := []string{"--resume-enrollment", accepted[0]}
	if automatic {
		args[0] = "--boot-enrollment-under-lock"
	}
	if len(accepted) == 3 {
		args = append([]string{"--enroll-under-lock"}, accepted...)
	}
	return runMailEnrollmentWithPreparedLocks(ctx, "/var/lib/celikpanel-release-transaction/transaction.lock", "/run/celikpanel/service-mutation.lock", serviceMutationLockOwner(), args, func(release *os.File) error {
		return prepareRecordedMailEnrollmentRuntime(ctx, accepted, int(release.Fd()))
	})
}
func runMailEnrollmentWithLocks(ctx context.Context, releasePath, hostPath string, owner hostmutationlock.Owner, args []string) error {
	return runMailEnrollmentWithPreparedLocks(ctx, releasePath, hostPath, owner, args, nil)
}
func runMailEnrollmentWithPreparedLocks(ctx context.Context, releasePath, hostPath string, owner hostmutationlock.Owner, args []string, prepare func(*os.File) error) error {
	if ctx == nil {
		return servicemutationledger.ErrMailEnrollment
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	release, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		if errors.Is(err, hostmutationlock.ErrBusy) {
			return errors.Join(errMailEnrollmentWorkerBusy, err)
		}
		return err
	}
	defer release.Close()
	if prepare != nil {
		if err = prepare(release); err != nil {
			return err
		}
	}
	host, err := hostmutationlock.AcquireExisting(hostPath, owner)
	if err != nil {
		if errors.Is(err, hostmutationlock.ErrBusy) {
			return errors.Join(errMailEnrollmentWorkerBusy, err)
		}
		return err
	}
	defer host.Close()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return err
	}
	defer self.Close()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", args...)
	command.ExtraFiles = []*os.File{self, nil, nil, nil, nil, host, release}
	command.Env = []string{"PATH=" + independentMailPath, "LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	command.WaitDelay = 2 * time.Second
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

// Dispatch only the installed content-addressed helper corresponding to this
// executing image and the current verified Agent declaration. Never execute an
// Agent with a helper flag or accept a caller-supplied executable path.
func verifiedMailEnrollmentHelper() (string, error) {
	agent, err := recoveryruntime.InspectCompatibleMailAgent("/opt/celikpanel/bin")
	if err != nil {
		return "", err
	}
	defer agent.Close()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return "", err
	}
	defer self.Close()
	raw, err := io.ReadAll(io.LimitReader(self, mailrenewalkit.MaxBinarySize+1))
	if err != nil {
		return "", err
	}
	kit, _, err := mailrenewalkit.Payload(raw)
	if err != nil || kit.Generation != agent.Contract.MailRenewalGeneration {
		return "", servicemutationledger.ErrMailEnrollment
	}
	path := filepath.Join(mailrenewalkit.InstalledRoot, kit.Generation, mailrenewalkit.BinaryName)
	path, err = resolveFixedRootExecutable(path)
	if err != nil {
		return "", err
	}
	running, err := self.Stat()
	if err != nil {
		return "", err
	}
	named, err := os.Lstat(path)
	if err != nil || !os.SameFile(running, named) {
		return "", servicemutationledger.ErrMailEnrollment
	}
	if err = agent.Revalidate(); err != nil {
		return "", err
	}
	return path, nil
}
