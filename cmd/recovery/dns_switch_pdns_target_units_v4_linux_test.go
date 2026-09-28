//go:build linux

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsunitrestore"
)

func TestPDNSTargetUnitRestoreStopsBeforeSecondEffectAfterOwnerDrift(t *testing.T) {
	mutations := 0
	checks := 0
	ownerChanged := false
	run := guardedPDNSTargetUnitRunnerV4(func(context.Context) error {
		checks++
		if ownerChanged {
			return errors.New("frozen BIND source changed")
		}
		return nil
	}, func(_ context.Context, path string, args ...string) ([]byte, error) {
		if path != "/usr/bin/systemctl" {
			t.Fatal("unexpected executable")
		}
		if len(args) == 0 {
			t.Fatal("missing systemctl action")
		}
		if args[0] == "show" {
			return []byte("LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"), nil
		}
		mutations++
		ownerChanged = true
		return nil, nil
	})
	snapshots := []dnsengineartifact.UnitSnapshot{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	err := dnsunitrestore.Restore(context.Background(), snapshots, map[string]bool{"named.service": true, "bind9.service": true}, dnsunitrestore.Ops{
		Systemctl: "/usr/bin/systemctl", VerifyMaskParent: func() error { return nil }, RunSystemd: run,
	})
	if err == nil || !strings.Contains(err.Error(), "frozen BIND source changed") || checks < 2 || mutations != 1 {
		t.Fatalf("owner edit was not stopped before second systemctl effect: err=%v checks=%d effects=%d", err, checks, mutations)
	}
}

func TestPDNSTargetUnitGuardFailureCannotBeSwallowedByReadback(t *testing.T) {
	called := 0
	guardErr := errors.New("owner changed native DNS")
	run := guardedPDNSTargetUnitRunnerV4(func(context.Context) error { return guardErr }, func(context.Context, string, ...string) ([]byte, error) {
		called++
		return []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\n"), nil
	})
	if _, err := run(context.Background(), "/usr/bin/systemctl", "start", "named.service"); !errors.Is(err, guardErr) {
		t.Fatalf("guard failure not returned: %v", err)
	}
	if _, err := run(context.Background(), "/usr/bin/systemctl", "show", "named.service", "--property=LoadState,ActiveState,UnitFileState", "--no-pager"); !errors.Is(err, guardErr) || called != 0 {
		t.Fatalf("readback bypassed failed guard: err=%v native calls=%d", err, called)
	}
}
