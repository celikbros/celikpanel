//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestConfigurePowerDNSSQLiteDiscoveryFailureTouchesNoDBOrCommand(t *testing.T) {
	preservePublicListenAddressSeams(t)
	dbPath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", dbPath)
	manager, _ := newMutationTestManager(t)
	beginMutationTestJobWithIdentity(t, manager, "pdns_configure", "pdns", "")
	installGlobalMutationTestManager(t, manager)
	t.Cleanup(func() { releasePoisonedDNSZoneSyncTestManager(manager) })
	oldAuthority := legacyPowerDNSMutationAuthorityCheck
	oldRuntime := legacyPowerDNSRuntimeSafetyCheck
	legacyPowerDNSMutationAuthorityCheck = func(bool) error { return nil }
	legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error { return nil }
	t.Cleanup(func() {
		legacyPowerDNSMutationAuthorityCheck = oldAuthority
		legacyPowerDNSRuntimeSafetyCheck = oldRuntime
	})
	publicListenAddressExecutableResolver = func(string) (string, error) {
		return "", errors.New("untrusted ip executable")
	}
	commandRuns := 0
	publicListenAddressCommandRunner = func(context.Context, string, ...string) ([]byte, error) {
		commandRuns++
		return nil, nil
	}
	request := &ServiceMutationRequest{ServiceMutationBinding: transport.ServiceMutationBinding{
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
	}}
	response := SyncDNSZoneResponse{Synced: true, Error: "stale"}
	if err := (&Agent{}).ConfigurePowerDNSSQLite(request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Synced || !strings.Contains(response.Error, "untrusted ip executable") || commandRuns != 0 {
		t.Fatalf("response=%+v commandRuns=%d", response, commandRuns)
	}
	if _, err := os.Stat(dbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovery failure touched PowerDNS DB: %v", err)
	}
}
