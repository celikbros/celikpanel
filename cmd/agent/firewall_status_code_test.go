package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func stageRebootRequiredMarker(t *testing.T, present bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reboot-required")
	if present {
		if err := os.WriteFile(path, []byte("*** System restart required ***\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previous := firewallRebootRequiredMarker
	firewallRebootRequiredMarker = path
	t.Cleanup(func() { firewallRebootRequiredMarker = previous })
}

// Decision C (D-024, 2026-09-30): the firewall status names what it can
// prove. Arch removes the running kernel's module tree on upgrade (the
// structural proof); Debian and Ubuntu keep it and write /run/reboot-required.
func TestFirewallStatusErrorCodeFromHostFacts(t *testing.T) {
	// The fake runner returns only an error; nft's words ride in it.
	kernelOut := errors.New("exit status 1: Error: cache initialization failed: Protocol not supported")
	otherOut := errors.New("exit status 1: Error: something nobody classified")
	for _, tc := range []struct {
		name     string
		replaced bool
		marker   bool
		out      error
		want     string
	}{
		{"arch: running kernel's modules gone", true, false, kernelOut, transport.FirewallStatusHostRestartRequired},
		{"arch: modules gone, unknown nft words", true, false, otherOut, transport.FirewallStatusHostRestartRequired},
		{"debian/ubuntu: reboot-required marker", false, true, kernelOut, transport.FirewallStatusHostRestartRequired},
		{"kernel unreachable without restart proof", false, false, kernelOut, transport.FirewallStatusKernelUnavailable},
		{"anything else", false, false, otherOut, transport.FirewallStatusUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.replaced {
				stageReplacedKernelFixture(t)
			} else {
				stageIntactKernelFixture(t)
			}
			stageRebootRequiredMarker(t, tc.marker)
			firewallLastRestoreError = ""
			runner := &fakeFirewallCommandRunner{listErr: tc.out}
			var resp FirewallStatusResponse
			if err := firewallStatusWithRunnerAndStore(runner, &fakeFirewallStateStore{}, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.ErrorCode != tc.want || resp.Error == "" {
				t.Fatalf("status code=%q error=%q, want %q", resp.ErrorCode, resp.Error, tc.want)
			}
		})
	}
	t.Run("healthy host has no code", func(t *testing.T) {
		stageIntactKernelFixture(t)
		stageRebootRequiredMarker(t, true)
		firewallLastRestoreError = ""
		var resp FirewallStatusResponse
		if err := firewallStatusWithRunnerAndStore(&fakeFirewallCommandRunner{}, &fakeFirewallStateStore{}, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Error != "" || resp.ErrorCode != "" {
			t.Fatalf("a pending restart alone blocked a working firewall: %+v", resp)
		}
	})
	t.Run("marker must be a regular file", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "reboot-required")
		if err := os.Symlink(filepath.Join(dir, "elsewhere"), link); err != nil {
			t.Skip("symlinks unavailable")
		}
		previous := firewallRebootRequiredMarker
		firewallRebootRequiredMarker = link
		t.Cleanup(func() { firewallRebootRequiredMarker = previous })
		if hostRebootRequiredMarkerPresent() {
			t.Fatal("a symlinked marker was trusted")
		}
	})
	if !strings.Contains(transport.FirewallStatusHostRestartRequired, "restart") {
		t.Fatal("stable code changed")
	}
}
