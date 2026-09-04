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
	useTestServiceMutationOwner(t)
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

func TestCommittedDNSEngineTargetOwnershipPublishesExactState(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	journal := testBINDSwitchJournal(t)
	journal.Phase = dnsSwitchPhaseCommitted
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: journal.Mode,
		Engine: journal.TargetEngine, EngineEpoch: journal.TargetEpoch,
		Generation:        journal.TargetGeneration,
		SourceRevision:    journal.SourceRevision,
		ManifestQualifier: journal.ManifestQualifier,
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
	}
	if err := writeDNSEngineState(state); err != nil {
		t.Fatal(err)
	}
	stale := state
	stale.Generation = strings.Repeat("9", 64)
	if err := writeDNSEngineOwnership(stale); err != nil {
		t.Fatal(err)
	}
	if err := publishCommittedDNSEngineTargetOwnership(journal); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readDNSEngineOwnership(transport.DNSEngineBIND)
	if err != nil || !exists || actual != state {
		t.Fatalf("published ownership exists=%v state=%+v err=%v", exists, actual, err)
	}
	journal.Phase = dnsSwitchPhaseRolledBack
	if err := publishCommittedDNSEngineTargetOwnership(journal); err == nil {
		t.Fatal("non-committed journal published target ownership")
	}
}

func TestDNSEngineInstallOwnershipHandoffRebindsExactRetry(t *testing.T) {
	prepareDNSEngineOwnershipTest(t)
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	oldManifest := mutationpayloadSwitchForOwnershipTest(t, state)
	oldBinding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("1", 32),
		MutationOwnerID:   strings.Repeat("2", 32),
	}
	receipt, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9-utils", "bind9"}, []string{"bind9"},
		oldManifest, oldBinding,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDNSEngineInstallOwnership(receipt); err != nil {
		t.Fatal(err)
	}
	newManifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		state.Engine, transport.DNSEngineBIND,
		state.EngineEpoch, state.EngineEpoch+1, 2,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	newBinding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("3", 32),
		MutationOwnerID:   strings.Repeat("4", 32),
	}
	if err := assumeExistingDNSEnginePackageOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9", "bind9-utils"}, newManifest, newBinding,
	); err != nil {
		t.Fatal(err)
	}
	actual, exists, err := readDNSEngineInstallOwnership(transport.DNSEngineBIND)
	if err != nil || !exists {
		t.Fatalf("read rebound install ownership: exists=%v err=%v", exists, err)
	}
	want := receipt
	want.ManifestQualifier = newManifest.Qualifier
	want.MutationRequestID = newBinding.MutationRequestID
	want.MutationOwnerID = newBinding.MutationOwnerID
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("unexpected rebound receipt:\n got: %+v\nwant: %+v", actual, want)
	}
	if !reflect.DeepEqual(actual.MissingBefore, receipt.MissingBefore) {
		t.Fatal("handoff changed the original missing-before provenance")
	}
}

func TestDNSEngineInstallOwnershipHandoffRejectsMismatchedOrCorruptReceipt(t *testing.T) {
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	manifest := mutationpayloadSwitchForOwnershipTest(t, state)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("3", 32),
		MutationOwnerID:   strings.Repeat("4", 32),
	}
	base, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9", "bind9-utils"}, []string{"bind9"},
		manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*dnsEngineInstallOwnershipReceipt)
	}{
		{name: "engine", mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
			receipt.Engine = transport.DNSEnginePowerDNS
		}},
		{name: "manager", mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
			receipt.PackageManager = string(hostplatform.PackageManagerPacman)
		}},
		{name: "package-set", mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
			receipt.Packages = []string{"bind9"}
		}},
		{name: "corrupt-provenance", mutate: func(receipt *dnsEngineInstallOwnershipReceipt) {
			receipt.MissingBefore = []string{"not-in-package-set"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			existing := base
			existing.Packages = append([]string(nil), base.Packages...)
			existing.MissingBefore = append([]string(nil), base.MissingBefore...)
			test.mutate(&existing)
			writeCalls := 0
			err := assumeExistingDNSEnginePackageOwnershipWithOps(
				transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
				[]string{"bind9", "bind9-utils"}, manifest, binding,
				dnsEngineInstallOwnershipAssumeOps{
					read: func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
						return existing, true, nil
					},
					write: func(dnsEngineInstallOwnershipReceipt) error {
						writeCalls++
						return nil
					},
				},
			)
			if err == nil || writeCalls != 0 {
				t.Fatalf("err=%v writeCalls=%d", err, writeCalls)
			}
		})
	}
}

func TestDNSEngineInstallOwnershipHandoffAbsentAndIOFailures(t *testing.T) {
	state := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	manifest := mutationpayloadSwitchForOwnershipTest(t, state)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: strings.Repeat("3", 32),
		MutationOwnerID:   strings.Repeat("4", 32),
	}
	receipt, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9"}, []string{"bind9"}, manifest, binding,
	)
	if err != nil {
		t.Fatal(err)
	}
	// No receipt to rebind is not "nothing happened": the packages are present,
	// this mutation is taking them under management, and finalization will ask
	// for the proof. Leaving the host without one is R-026.
	//
	// Yeniden bağlanacak makbuzun olmaması "hiçbir şey olmadı" demek değildir:
	// paketler oradadır, bu mutasyon onları yönetimine almaktadır ve sonlandırma
	// bunun kanıtını isteyecektir. Sunucuyu kanıtsız bırakmak R-026'dır.
	t.Run("absent-is-adopted", func(t *testing.T) {
		written := []dnsEngineInstallOwnershipReceipt{}
		err := assumeExistingDNSEnginePackageOwnershipWithOps(
			transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
			[]string{"bind9"}, manifest, binding,
			dnsEngineInstallOwnershipAssumeOps{
				read: func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
					if len(written) == 0 {
						return dnsEngineInstallOwnershipReceipt{}, false, nil
					}
					return written[len(written)-1], true, nil
				},
				write: func(receipt dnsEngineInstallOwnershipReceipt) error {
					written = append(written, receipt)
					return nil
				},
			},
		)
		if err != nil || len(written) != 1 {
			t.Fatalf("err=%v writes=%d", err, len(written))
		}
		adopted := written[0]
		if !adopted.AdoptedPresent || len(adopted.MissingBefore) != 0 ||
			adopted.ManifestQualifier != manifest.Qualifier ||
			adopted.MutationRequestID != binding.MutationRequestID ||
			adopted.MutationOwnerID != binding.MutationOwnerID {
			t.Fatalf("adoption receipt is not this mutation's provenance: %+v", adopted)
		}
	})
	t.Run("write-failure", func(t *testing.T) {
		injected := errors.New("injected write failure")
		err := assumeExistingDNSEnginePackageOwnershipWithOps(
			transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
			[]string{"bind9"}, manifest, binding,
			dnsEngineInstallOwnershipAssumeOps{
				read: func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
					return receipt, true, nil
				},
				write: func(dnsEngineInstallOwnershipReceipt) error { return injected },
			},
		)
		if !errors.Is(err, injected) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("readback-mismatch", func(t *testing.T) {
		readCalls := 0
		err := assumeExistingDNSEnginePackageOwnershipWithOps(
			transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
			[]string{"bind9"}, manifest, binding,
			dnsEngineInstallOwnershipAssumeOps{
				read: func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
					readCalls++
					if readCalls == 1 {
						return receipt, true, nil
					}
					return dnsEngineInstallOwnershipReceipt{}, false, nil
				},
				write: func(dnsEngineInstallOwnershipReceipt) error { return nil },
			},
		)
		if err == nil || !strings.Contains(err.Error(), "readback mismatch") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
