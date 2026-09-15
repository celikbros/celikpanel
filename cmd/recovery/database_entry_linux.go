//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"golang.org/x/sys/unix"
)

type databaseResultBuffer struct{ buffer bytes.Buffer }

func (b *databaseResultBuffer) Len() int       { return b.buffer.Len() }
func (b *databaseResultBuffer) String() string { return b.buffer.String() }
func (b *databaseResultBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, fmt.Errorf("database helper result exceeded its bound")
	}
	return b.buffer.Write(p)
}

// The checker dies with this recovery command and has a bounded deadline.
// It independently checks the inherited native flock and writer state.
// Doğrulayıcı bu kurtarma komutuyla sonlanır ve süre sınırı taşır. Devralınan
// yerel kilidi ve yazıcı durumunu ayrıca kendisi doğrular.
func runDatabaseAction(command, snapshot string) (result string, returnErr error) {
	selected, err := recoveryruntime.Resolve()
	if err != nil {
		return "", err
	}
	defer func() { returnErr = errors.Join(returnErr, selected.Close()) }()
	if err := selected.VerifyExecutingBinary(); err != nil {
		return "", err
	}
	if !databaseActionCommand(command) {
		return "", recoveryruntime.ErrUnavailable
	}
	lockFD, err := unix.FcntlInt(9, unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		return "", fmt.Errorf("inherited release lock is unavailable: %w", err)
	}
	lock := os.NewFile(uintptr(lockFD), "database-release-lock")
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	child := exec.CommandContext(ctx, filepath.Join(selected.Root, "bin", "panel-checker"), "--"+command+"="+snapshot)
	child.Dir = "/"
	child.Env = recoveryCompatibilityEnvironment()
	child.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, nil, lock}
	child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	var output databaseResultBuffer
	child.Stdout = &output
	child.Stderr = os.Stderr
	if err := selected.Revalidate(); err != nil {
		return "", err
	}
	err = child.Run()
	proofErr := selected.Revalidate()
	if err != nil || proofErr != nil {
		return "", errors.Join(err, proofErr)
	}
	return strings.TrimSuffix(output.String(), "\n"), nil
}

func runDatabaseProbe() (returnErr error) {
	selected, err := recoveryruntime.Resolve()
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, selected.Close()) }()
	if err := selected.VerifyExecutingBinary(); err != nil {
		return err
	}
	if err := recoveryruntime.VerifyPreflightBoundary(9); err != nil {
		return err
	}
	err = recoverypublication.ProbeDatabaseMigration()
	return errors.Join(err, selected.Revalidate(), recoveryruntime.VerifyPreflightBoundary(9))
}
