//go:build linux

package services

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAtomicManagedConfigPreservesLinuxOwnershipAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.conf")
	if err := os.WriteFile(path, []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before, ok := beforeInfo.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("Linux ownership metadata is unavailable")
	}
	if err := atomicWriteManagedConfig(path, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	after, ok := afterInfo.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("Linux ownership metadata is unavailable after replacement")
	}
	if before.Uid != after.Uid || before.Gid != after.Gid {
		t.Fatalf("ownership changed from %d:%d to %d:%d", before.Uid, before.Gid, after.Uid, after.Gid)
	}
	if afterInfo.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed from 0640 to %04o", afterInfo.Mode().Perm())
	}
}
