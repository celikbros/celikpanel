//go:build linux

package firewalllock

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("native root ownership fixture requires root")
	}
	root, err := os.MkdirTemp("/root", "celikpanel-firewall-lock-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return filepath.Join(root, "lock")
}

func TestLockDifferentProcessAndCrashRelease(t *testing.T) {
	if path := os.Getenv("CELIKPANEL_TEST_FIREWALL_LOCK_CHILD"); path != "" {
		lock, err := acquireAt(path)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Println("held")
		var b [1]byte
		os.Stdin.Read(b[:])
		return
	}
	path := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockDifferentProcessAndCrashRelease$")
	cmd.Env = append(os.Environ(), "CELIKPANEL_TEST_FIREWALL_LOCK_CHILD="+path)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "held\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	if second, err := acquireAt(path); !errors.Is(err, ErrBusy) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("concurrent operation admitted: %v", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("child was not interrupted")
	}
	next, err := acquireAt(path)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestLockCompatibleOwnershipAndUntrustedMetadata(t *testing.T) {
	for _, kind := range []string{"readonly_group", "writable_parent", "foreign_owner", "symlink_file", "hardlink_file", "unsafe_mode"} {
		t.Run(kind, func(t *testing.T) {
			dir := fixture(t)
			lock, err := acquireAt(dir)
			if err != nil {
				t.Fatal(err)
			}
			lock.Close()
			path := filepath.Join(dir, "restore.lock")
			switch kind {
			case "readonly_group":
				err = os.Chown(dir, 0, 989)
				if err == nil {
					err = os.Chown(path, 0, 989)
				}
			case "writable_parent":
				err = os.Chmod(dir, 0770)
			case "foreign_owner":
				err = os.Chown(path, 989, 989)
			case "symlink_file":
				err = os.Rename(path, path+".real")
				if err == nil {
					err = os.Symlink(path+".real", path)
				}
			case "hardlink_file":
				err = os.Link(path, path+".link")
			case "unsafe_mode":
				err = os.Chmod(path, 0640)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := acquireAt(dir)
			if got != nil {
				got.Close()
			}
			if (err == nil) != (kind == "readonly_group") {
				t.Fatalf("wrong metadata decision: %v", err)
			}
		})
	}
}
