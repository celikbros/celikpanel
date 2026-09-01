//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSEngineStateSnapshotRoundTripUsesExactServiceOwner(t *testing.T) {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("exact root:service-group state ownership test requires root:root")
	}
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID = 0
	serviceMutationRequiredOwnerGID = 1
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
	})

	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	path := filepath.Join(root, "dns-engine-state.json")
	beforeState := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	before, err := encodeDNSEngineState(beforeState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, path, 0, 1, 0o600)

	snapshot, err := captureDNSEngineStateSnapshot(false)
	if err != nil {
		t.Fatalf("capture root:service-group DNS state: %v", err)
	}
	if !snapshot.OwnerKnown || snapshot.UID != 0 || snapshot.GID != 1 ||
		snapshot.Mode != 0o600 || !bytes.Equal(snapshot.Data, before) {
		t.Fatalf("captured DNS state metadata differs: %+v", snapshot)
	}

	changedState := beforeState
	changedState.EngineEpoch++
	changed, err := encodeDNSEngineState(changedState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreDNSEngineStateSnapshot(snapshot); err != nil {
		t.Fatalf("restore root:service-group DNS state: %v", err)
	}
	actual, err := captureDNSEngineStateSnapshot(false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, snapshot) {
		t.Fatalf("restored DNS state differs:\n got: %+v\nwant: %+v", actual, snapshot)
	}
}

func TestDNSEngineStateSnapshotRestoreRejectsOwnerDriftBeforeWrite(t *testing.T) {
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		t.Skip("exact root:service-group state ownership test requires root:root")
	}
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID = 0
	serviceMutationRequiredOwnerGID = 1
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
	})

	root := t.TempDir()
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	path := filepath.Join(root, "dns-engine-state.json")
	beforeState := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	before, err := encodeDNSEngineState(beforeState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, path, 0, 1, 0o600)
	snapshot, err := captureDNSEngineStateSnapshot(false)
	if err != nil {
		t.Fatal(err)
	}

	driftedState := beforeState
	driftedState.EngineEpoch++
	drifted, err := encodeDNSEngineState(driftedState)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, drifted, 0o600); err != nil {
		t.Fatal(err)
	}
	mustChownMode(t, path, 0, 2, 0o600)
	if err := restoreDNSEngineStateSnapshot(snapshot); err == nil {
		t.Fatal("DNS state restore accepted foreign live ownership")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, drifted) {
		t.Fatal("DNS state restore changed bytes before rejecting owner drift")
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Gid != 2 {
		t.Fatalf("DNS state restore changed foreign ownership: %+v", info.Sys())
	}

	mustChownMode(t, path, 0, 1, 0o600)
	absent := dnsFileSnapshot{Path: filepath.Clean(path)}
	if err := restoreDNSEngineStateSnapshot(absent); err != nil {
		t.Fatalf("restore absent DNS state: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restored absent DNS state still exists: %v", err)
	}
}
