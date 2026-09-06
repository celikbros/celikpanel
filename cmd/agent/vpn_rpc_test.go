package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRunVPNCommitRollbackAttemptsLiveAndDurableRecovery(t *testing.T) {
	liveCalled := false
	diskCalled := false
	err := runVPNCommitRollback(
		true,
		func() error {
			liveCalled = true
			return errors.New("live rollback failed")
		},
		func() error {
			diskCalled = true
			return errors.New("durable rollback failed")
		},
	)
	if !liveCalled || !diskCalled {
		t.Fatalf("rollback calls live=%v durable=%v, want both", liveCalled, diskCalled)
	}
	if err == nil ||
		!strings.Contains(err.Error(), "live rollback failed") ||
		!strings.Contains(err.Error(), "durable rollback failed") {
		t.Fatalf("rollback error=%v, want both failures", err)
	}
}

func TestRunVPNCommitRollbackSkipsLiveRecoveryForDownInterface(t *testing.T) {
	liveCalled := false
	diskCalled := false
	err := runVPNCommitRollback(
		false,
		func() error {
			liveCalled = true
			return nil
		},
		func() error {
			diskCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("rollback error: %v", err)
	}
	if liveCalled {
		t.Fatal("live rollback ran for an interface that was originally down")
	}
	if !diskCalled {
		t.Fatal("durable rollback did not run")
	}
}

// R-058. The panel answers "there is no VPN server here, set it up first" only
// on the structural fact that the configuration is absent. A configuration that
// is present and cannot be read is a different fault with a different remedy,
// and must never borrow this one's words - which is what an errors.Is on the
// filesystem's own answer buys, and what a message match would not.
//
// R-058. Panel, "burada VPN sunucusu yok, once kurun" yanitini yalnizca
// yapilandirmanin yoklugu yapisal olgusu uzerine verir. Var olup okunamayan bir
// yapilandirma baska bir arizadir ve bunun sozlerini odunc almamalidir.
func TestVPNConfigurationAbsentReadsTheFilesystemNotAMessage(t *testing.T) {
	if !vpnConfigurationAbsent(fs.ErrNotExist) {
		t.Fatal("an absent configuration was not recognized")
	}
	if !vpnConfigurationAbsent(
		fmt.Errorf("lstat %s: %w", "/etc/wireguard", fs.ErrNotExist),
	) {
		t.Fatal("an absent configuration was not recognized through wrapping")
	}
	if !vpnConfigurationAbsent(syscall.ENOENT) {
		t.Fatal("the kernel's own answer was not recognized")
	}
	for name, err := range map[string]error{
		"permission":          fs.ErrPermission,
		"security validation": errors.New("VPN configuration file failed security validation"),
		"a message that lies": errors.New("VPN server is not set up"),
		"a raced writer":      errors.New("VPN configuration changed while it was read"),
	} {
		if vpnConfigurationAbsent(err) {
			t.Fatalf("%s was reported as an absent configuration", name)
		}
	}
}

// The absence has to survive the directory check to reach the caller, and it
// used to be swallowed there: a directory that is not on disk is not a
// directory that failed a security check, and calling it one leaves the panel
// with no way to tell "set the VPN up" from "do not trust this host".
//
// Yokluk, cagirana ulasmak icin dizin denetiminden gecmelidir; orada
// yutuluyordu. Diskte olmayan bir dizin, guvenlik denetiminden kalmis bir dizin
// degildir.
func TestVPNDirectoryValidationKeepsTheAbsenceItFinds(t *testing.T) {
	previous := wgConfDir
	t.Cleanup(func() { wgConfDir = previous })
	wgConfDir = filepath.Join(t.TempDir(), "no-such-wireguard")

	err := validateVPNDirectory(wgConfDir)
	if err == nil {
		t.Fatal("a missing VPN directory validated")
	}
	if !vpnConfigurationAbsent(err) {
		t.Fatalf("the absence was lost: %v", err)
	}
}
