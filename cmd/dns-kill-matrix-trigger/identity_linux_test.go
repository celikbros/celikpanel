//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestTriggerIdentityPersistsExactRetryAuthority(t *testing.T) {
	const cellID = "bind__intent__before-write__standalone__peer-reachable"
	request := mustScenarioRequest(t, scenario{
		Schema: scenarioSchema, Driver: "bind", SourceFixture: "uninitialized",
		Mode:         transport.DNSEngineSwitchModeSwitch,
		TargetEngine: transport.DNSEngineBIND, TargetEpoch: 1,
		Topology: transport.DNSTopologyStandalone,
		Zones:    []transport.DNSEngineSwitchZoneSnapshot{},
	})
	path := filepath.Join(t.TempDir(), "trigger-identity.json")
	initial, err := triggerIdentity(
		path, false, cellID, "bind", "uninitialized", testRequestID,
		request.ManifestQualifier,
	)
	if err != nil {
		t.Fatalf("publish trigger identity: %v", err)
	}
	if initial.OwnerID != "59a107acb2a58009b94ac24bcf943db1" {
		t.Fatalf("owner ID=%q", initial.OwnerID)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 ||
		stat.Nlink != 1 {
		t.Fatalf("identity receipt metadata mode=%v stat=%+v", info.Mode(), stat)
	}
	retry, err := triggerIdentity(
		path, true, cellID, "bind", "uninitialized", testRequestID,
		request.ManifestQualifier,
	)
	if err != nil {
		t.Fatalf("read retry trigger identity: %v", err)
	}
	if retry != initial {
		t.Fatalf("retry receipt=%+v, initial=%+v", retry, initial)
	}
	if _, err := triggerIdentity(
		path, false, cellID, "bind", "uninitialized", testRequestID,
		request.ManifestQualifier,
	); err == nil {
		t.Fatal("initial trigger replaced an existing identity receipt")
	}
	if _, err := triggerIdentity(
		path, true, cellID, "bind", "managed-pdns", testRequestID,
		request.ManifestQualifier,
	); err == nil {
		t.Fatal("retry accepted changed source provenance")
	}
}
