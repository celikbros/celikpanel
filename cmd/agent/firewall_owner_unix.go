//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"fmt"
	"os"
	"syscall"
)

// requireRootOwner accepts a trusted filesystem object only when the platform
// exposes Unix ownership metadata and its effective owner is root.
func requireRootOwner(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s owner UID is unavailable", label)
	}
	if stat.Uid != 0 {
		return fmt.Errorf("%s owner UID is %d, want 0", label, stat.Uid)
	}
	return nil
}
