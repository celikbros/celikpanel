//go:build linux

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLinuxPackageProcessBusyRecognizesRHELTransactions(t *testing.T) {
	for _, process := range []string{
		"dnf",
		"dnf5",
		"yum",
		"microdnf",
		"rpm",
		"rpmdb",
		"packagekitd",
		"packagekit",
		"pkcon",
		"dnfdaemon-serve",
	} {
		t.Run(process, func(t *testing.T) {
			root := t.TempDir()
			pidDir := filepath.Join(root, "101")
			if err := os.Mkdir(pidDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(process+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			busy, err := linuxPackageProcessBusyAt(root)
			if err != nil || !busy {
				t.Fatalf("process %q: busy=%v err=%v, want busy", process, busy, err)
			}
		})
	}
}

func TestLinuxPackageProcessBusyIgnoresBenignAndVanishedProcesses(t *testing.T) {
	root := t.TempDir()
	for pid, process := range map[string]string{
		"101": "systemd",
		"102": "nginx",
		// Linux truncates the long-lived unattended-upgrade-shutdown helper's
		// comm value. The idle shutdown listener is not an APT transaction;
		// only an exact package process name or an actual fcntl lock is busy.
		"104": "unattended-upgr",
	} {
		pidDir := filepath.Join(root, pid)
		if err := os.Mkdir(pidDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(process+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "103"), 0o755); err != nil {
		t.Fatal(err)
	}
	busy, err := linuxPackageProcessBusyAt(root)
	if err != nil || busy {
		t.Fatalf("benign process table: busy=%v err=%v, want idle", busy, err)
	}
}

func TestLinuxPackageProcessBusyFailsClosedWhenProcessTableCannotBeRead(t *testing.T) {
	busy, err := linuxPackageProcessBusyAt(filepath.Join(t.TempDir(), "missing"))
	if err == nil || busy {
		t.Fatalf("missing process table: busy=%v err=%v, want a fail-closed inspection error", busy, err)
	}
}

func TestLinuxPackageManagerLockPathsIncludeBothRPMDatabaseLocations(t *testing.T) {
	want := []string{
		"/var/lib/dpkg/lock-frontend",
		"/var/lib/dpkg/lock",
		"/var/cache/apt/archives/lock",
		"/var/lib/rpm/.rpm.lock",
		"/usr/lib/sysimage/rpm/.rpm.lock",
	}
	if got := linuxPackageManagerFcntlLockPaths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("package-manager lock paths = %q, want %q", got, want)
	}
}
