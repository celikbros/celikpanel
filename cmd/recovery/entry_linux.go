//go:build linux

package main

import (
	"path/filepath"
	"syscall"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

func runSelectedRuntime(args []string) error {
	runtime, err := recoveryruntime.Resolve()
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := runtime.Revalidate(); err != nil {
		return err
	}
	entry := filepath.Join(runtime.Root, "deploy/recovery/runtime-entry.sh")
	// The clean environment cannot smuggle a target path, test root, alternate
	// binary or new operation into the executor. It classifies and locks the
	// existing native transaction itself. Final-proof arguments only verify.
	environment := []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "USER=root", "LOGNAME=root", "SHELL=/bin/bash", "LANG=C", "LC_ALL=C", "CELIKPANEL_RECOVERY_RUNTIME_ROOT=" + runtime.Root}
	argv := append([]string{"/bin/bash", entry}, args...)
	if err := syscall.Exec("/bin/bash", argv, environment); err != nil {
		return recoveryruntime.ErrUnavailable
	}
	return nil
}
func enrollRuntime(source string) error { return recoveryruntime.Enroll(source, 9) }
