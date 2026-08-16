//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func prepareDNSEngineOwnershipTest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
	return root
}

func TestDNSEngineOwnershipReceiptCanonicalRoundTrip(t *testing.T) {
	root := prepareDNSEngineOwnershipTest(t)
	state := legacyDurableDNSState(transport.DNSEngineBIND)
	if err := writeDNSEngineOwnership(state); err != nil {
		t.Fatal(err)
	}
	got, exists, err := readDNSEngineOwnership(transport.DNSEngineBIND)
	if err != nil || !exists || got != state {
		t.Fatalf("exists=%v state=%+v err=%v", exists, got, err)
	}
	path, err := dnsEngineOwnershipPath(transport.DNSEngineBIND)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != root {
		t.Fatalf("ownership escaped state root: %s", path)
	}
	encoded, err := encodeDNSEngineState(
		legacyDurableDNSState(transport.DNSEnginePowerDNS),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readDNSEngineOwnership(transport.DNSEngineBIND); err == nil {
		t.Fatal("receipt whose engine differs from its path was accepted")
	}
}

func TestDNSEngineSourceOwnershipBindsJournalStateExactly(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	encoded, err := encodeDNSEngineState(state)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{
		SourceEngine: state.Engine, SourceEpoch: state.EngineEpoch,
		StateBefore: dnsFileSnapshot{
			Path: dnsEngineStatePath(), Exists: true, Mode: 0o600,
			SHA256: digestDNSBytes(encoded), Data: encoded,
		},
	}
	if err := verifyDNSSwitchSourceOwnership(journal); err == nil {
		t.Fatal("missing source ownership receipt was accepted")
	}
	manifest := mutationpayloadSwitchForOwnershipTest(t, state)
	if err := publishDNSEngineSourceOwnership(manifest, state, true); err != nil {
		t.Fatal(err)
	}
	if err := verifyDNSSwitchSourceOwnership(journal); err != nil {
		t.Fatalf("exact source ownership rejected: %v", err)
	}
	other := state
	other.ManifestQualifier = "dns-engine-switch/v1:sha256:" + strings.Repeat("e", 64)
	if err := writeDNSEngineOwnership(other); err != nil {
		t.Fatal(err)
	}
	if err := verifyDNSSwitchSourceOwnership(journal); err == nil {
		t.Fatal("different source ownership receipt was accepted")
	}
}

func mutationpayloadSwitchForOwnershipTest(
	t *testing.T,
	state dnsEngineStateReceipt,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	target := transport.DNSEngineBIND
	if state.Engine == transport.DNSEngineBIND {
		target = transport.DNSEnginePowerDNS
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		state.Engine, target, state.EngineEpoch, state.EngineEpoch+1, 1,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestDNSEngineInstallOwnershipPrecedesInstallerAndSurvivesFailure(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	manifest := mutationpayloadSwitchForOwnershipTest(t, state)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("1", 32),
		MutationOwnerID:   strings.Repeat("2", 32),
	}
	receipt, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9-utils", "bind9"}, []string{"bind9"},
		manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	installerCalls := 0
	injected := errors.New("injected crash after package manager entry")
	err = installOwnedDNSEnginePackages(receipt, func() error {
		installerCalls++
		actual, exists, readErr := readDNSEngineInstallOwnership(
			transport.DNSEngineBIND,
		)
		if readErr != nil || !exists || !exactDNSEngineInstallOwnership(
			actual, exists, transport.DNSEngineBIND,
			hostplatform.PackageManagerAPT,
			[]string{"bind9", "bind9-utils"},
		) {
			t.Fatalf("install ownership was not durable before callback: %+v %v", actual, readErr)
		}
		return injected
	})
	if !errors.Is(err, injected) || installerCalls != 1 {
		t.Fatalf("err=%v installerCalls=%d", err, installerCalls)
	}
	actual, exists, readErr := readDNSEngineInstallOwnership(
		transport.DNSEngineBIND,
	)
	if readErr != nil || !exists || !reflect.DeepEqual(actual, receipt) {
		t.Fatalf("install ownership did not survive failure: %+v %v", actual, readErr)
	}
	actual.Packages = []string{"bind9-utils", "bind9"}
	if _, err := encodeDNSEngineInstallOwnership(actual); err == nil {
		t.Fatal("unsorted install ownership package set was accepted")
	}
	if _, err := newDNSEngineInstallOwnership(
		transport.DNSEnginePowerDNS, hostplatform.PackageManagerAPT,
		[]string{"pdns-server"}, []string{"pdns-server"}, manifest, binding,
	); err == nil {
		t.Fatal("install ownership accepted a target that differs from the manifest")
	}
}

func TestDNSEngineInstallOwnershipRetiresOnlyAfterCommittedFinalize(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	manifest := mutationpayloadSwitchForOwnershipTest(t, state)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("3", 32),
		MutationOwnerID:   strings.Repeat("4", 32),
	}
	receipt, err := newDNSEngineInstallOwnership(
		manifest.TargetEngine, hostplatform.PackageManagerAPT,
		[]string{"bind9"}, []string{"bind9"}, manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineInstallOwnership(receipt); err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{
		Phase: dnsSwitchPhaseRollingBack, TargetEngine: manifest.TargetEngine,
	}
	if err := retireDNSEngineInstallOwnership(journal); err == nil {
		t.Fatal("rollback retired the target install ownership")
	}
	if _, exists, err := readDNSEngineInstallOwnership(manifest.TargetEngine); err != nil || !exists {
		t.Fatalf("rollback/crash did not retain install ownership: exists=%v err=%v", exists, err)
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := retireDNSEngineInstallOwnership(journal); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := readDNSEngineInstallOwnership(manifest.TargetEngine); err != nil || exists {
		t.Fatalf("committed finalize did not retire install ownership: exists=%v err=%v", exists, err)
	}
	if err := retireDNSEngineInstallOwnership(journal); err != nil {
		t.Fatalf("committed retirement is not idempotent: %v", err)
	}
}
