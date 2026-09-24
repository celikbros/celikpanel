//go:build linux

package main

import (
	"bytes"
	"errors"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDNSSwitchStatusRequiresOwnerAndExactCommand(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if got := runDNSSwitchStatus([]string{"dns-switch-status", "extra"}, 0, &out, &diagnostic); got != exitUsage {
		t.Fatalf("unexpected argument was accepted: %d", got)
	}
	if got := runDNSSwitchStatus([]string{"dns-switch-status"}, 1000, &out, &diagnostic); got != exitNotOwner {
		t.Fatalf("unprivileged observation was accepted: %d", got)
	}
	if got := runDNSSwitchStatus([]string{"dns-switch-status", "--quiesced"}, 1000, &out, &diagnostic); got != exitNotOwner {
		t.Fatalf("unprivileged quiesced observation was accepted: %d", got)
	}
	if !strings.Contains(diagnostic.String(), "Owner authentication") || out.Len() != 0 {
		t.Fatalf("unexpected guidance or data disclosure: %q / %q", diagnostic.String(), out.String())
	}
}
func TestLocalCelikPanelGroupIsBoundedAndUnambiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "group")
	write := func(data string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("root:x:0:\ncelikpanel:x:975:\n", 0o644)
	if got, err := localCelikPanelGroupID(path); err != nil || got != 975 {
		t.Fatalf("local identity: %d, %v", got, err)
	}
	for _, data := range []string{
		"root:x:0:\n",
		"celikpanel:x:0:\n",
		"celikpanel:x:not-a-number:\n",
		"celikpanel:x:975:\ncelikpanel:x:976:\n",
	} {
		write(data, 0o644)
		if _, err := localCelikPanelGroupID(path); err == nil {
			t.Fatalf("ambiguous or missing group accepted: %q", data)
		}
	}
	write("celikpanel:x:975:\n", 0o666)
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("writable group file accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("celikpanel:x:975:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := localCelikPanelGroupID(path); err == nil {
		t.Fatal("symlink group file accepted")
	}
}
func TestDNSObservationLocksKeepReleaseThenHostAndReleaseOnFailure(t *testing.T) {
	root := t.TempDir()
	makeLock := func(name string) string {
		t.Helper()
		parent := filepath.Join(root, name)
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parent, "transaction.lock")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	releasePath, hostPath := makeLock("release"), makeLock("host")
	locks, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostmutationlock.AcquireExisting(hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("host lock was not retained: %v", err)
	}
	locks.Close()

	heldHost, err := hostmutationlock.AcquireExisting(hostPath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("busy host was not refused: %v", err)
	}
	// The failed second acquisition must release the first lease.
	releaseAfterFailure, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatalf("release lease leaked: %v", err)
	}
	_ = releaseAfterFailure.Close()
	_ = heldHost.Close()

	heldRelease, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireDNSObservationLocks(releasePath, hostPath, hostmutationlock.Owner{}); !errors.Is(err, hostmutationlock.ErrBusy) {
		t.Fatalf("busy release was not refused: %v", err)
	}
	_ = heldRelease.Close()
}

func TestLocalBINDGroupRejectsNoncanonicalAndDuplicateRecords(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned group fixture requires root")
	}
	path := filepath.Join(t.TempDir(), "group")
	check := func(data string, wantOK bool) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		gid, err := localServiceGroupID(path, "bind")
		if wantOK && (err != nil || gid != 1234) {
			t.Fatalf("canonical BIND group rejected: %d %v", gid, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("unsafe BIND group accepted: %q", data)
		}
	}
	check("bind:x:1234:\n", true)
	check("bind:x:1234:\nbind:x:1235:\n", false)
	check("bind:x:01234:\n", false)
	check("bind:x:1234:someone\n", false)
	check("bind:x:1234:\nbind:malformed\n", false)
}
