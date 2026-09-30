//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/transport"
)

// These are component tests of the fresh paired PowerDNS primary (V3)
// recovery policy and product gate. They inject every native observation and
// effect; they are not native evidence.

func freshPairedPDNSPrimaryManifest(t *testing.T) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS, 0, 1, 0,
		transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func switchRequestForManifest(manifest mutationpayload.DNSEngineSwitchManifestCommitment) SwitchDNSEngineV1Request {
	return SwitchDNSEngineV1Request{
		ServiceMutationBinding: mutationTestBinding(),
		Mode:                   manifest.Mode, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology, PairRole: manifest.PairRole,
		LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		Zones: manifest.Zones, SnapshotBytes: manifest.SnapshotBytes,
		ManifestQualifier: manifest.Qualifier,
	}
}

func openFreshPairedPDNSPrimaryGate(t *testing.T) {
	t.Helper()
	previous := pdnsFreshPairedPrimaryGateOpen
	pdnsFreshPairedPrimaryGateOpen = true
	t.Cleanup(func() { pdnsFreshPairedPrimaryGateOpen = previous })
}

// closeFreshPairedPDNSPrimaryGate pins the closed gate for a test that states
// the closed behaviour, so the test holds whichever way the product constant
// stands.
func closeFreshPairedPDNSPrimaryGate(t *testing.T) {
	t.Helper()
	previous := pdnsFreshPairedPrimaryGateOpen
	pdnsFreshPairedPrimaryGateOpen = false
	t.Cleanup(func() { pdnsFreshPairedPrimaryGateOpen = previous })
}

func TestFreshPairedPDNSPrimaryGateStaysClosedInThisRelease(t *testing.T) {
	if freshPairedPDNSPrimaryAdmitted || pdnsFreshPairedPrimaryGateOpen {
		t.Fatal("the fresh paired PowerDNS primary gate must stay closed until row 6 has native acceptance")
	}
	if !pdnsPairedPrimarySwitchPaused(freshPairedPDNSPrimaryManifest(t)) {
		t.Fatal("closed gate admitted the fresh paired PowerDNS primary")
	}
}

func TestFreshPairedPDNSPrimaryOpenGatePolicy(t *testing.T) {
	fresh := freshPairedPDNSPrimaryManifest(t)
	bindSource := testPairedPDNSSwitchManifest(t, transport.DNSPairRolePrimary, nil)
	pdnsSource := bindSource
	pdnsSource.SourceEngine = transport.DNSEnginePowerDNS
	epochOnly := fresh
	epochOnly.SourceEpoch = 1
	for _, tc := range []struct {
		name               string
		manifest           mutationpayload.DNSEngineSwitchManifestCommitment
		open               bool
		paused, bindRefuse bool
	}{
		{"closed fresh", fresh, false, true, false},
		{"closed BIND source", bindSource, false, true, true},
		{"open fresh", fresh, true, false, false},
		{"open BIND source keeps D-026", bindSource, true, false, true},
		{"open PowerDNS source", pdnsSource, true, true, false},
		{"open source epoch without engine", epochOnly, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pdnsPairedPrimarySwitchPausedWithGate(tc.manifest, tc.open); got != tc.paused {
				t.Fatalf("paused=%v, want %v", got, tc.paused)
			}
			if got := bindSourcePDNSSwitchUnsupported(tc.manifest); got != tc.bindRefuse {
				t.Fatalf("BIND-source refusal=%v, want %v", got, tc.bindRefuse)
			}
			if tc.open && !tc.paused && !tc.bindRefuse &&
				(tc.manifest.SourceEngine != "" || tc.manifest.SourceEpoch != 0) {
				t.Fatal("open gate admitted a paired primary with a source")
			}
		})
	}
	for _, other := range []func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.PairRole = transport.DNSPairRoleSecondary
		},
		func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Topology, m.PairRole = transport.DNSTopologyStandalone, ""
		},
		func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.TargetEngine = transport.DNSEngineBIND },
	} {
		manifest := fresh
		other(&manifest)
		if pdnsPairedPrimarySwitchPausedWithGate(manifest, false) || pdnsPairedPrimarySwitchPausedWithGate(manifest, true) {
			t.Fatalf("unrelated transition was paused: %+v", manifest)
		}
	}
}

func TestPDNSPairedPrimaryHostAdmissionOnlyFreshV3(t *testing.T) {
	fresh := freshPairedPDNSPrimaryManifest(t)
	if err := pdnsPairedPrimaryHostAdmission(fresh, false); err != nil {
		t.Fatalf("fresh paired primary refused: %v", err)
	}
	if err := pdnsPairedPrimaryHostAdmission(fresh, true); err == nil || err.Error() != pdnsPairedPrimarySwitchPausedReason {
		t.Fatalf("paired primary beside a state receipt admitted: %v", err)
	}
	withSource := testPairedPDNSSwitchManifest(t, transport.DNSPairRolePrimary, nil)
	withSource.SourceEngine = transport.DNSEnginePowerDNS
	if err := pdnsPairedPrimaryHostAdmission(withSource, false); err == nil {
		t.Fatal("paired primary with an active engine admitted by the host backend")
	}
	secondary := testPairedPDNSSwitchManifest(t, transport.DNSPairRoleSecondary, nil)
	if err := pdnsPairedPrimaryHostAdmission(secondary, true); err != nil {
		t.Fatalf("paired secondary was affected: %v", err)
	}
}

// With the gate open, the Agent RPC admits the exact fresh manifest to the
// backend, still refuses a serving BIND source with the D-026 reason and a
// paired primary that already has a PowerDNS engine with the paused reason.
func TestFreshPairedPDNSPrimaryOpenGateRPC(t *testing.T) {
	openFreshPairedPDNSPrimaryGate(t)
	t.Run("fresh reaches backend", func(t *testing.T) {
		request := switchRequestForManifest(freshPairedPDNSPrimaryManifest(t))
		manager, _ := newMutationTestManager(t)
		installGlobalMutationTestManager(t, manager)
		beginMutationTestJobWithIdentity(t, manager, "dns_engine_switch", "pdns", request.ManifestQualifier)
		backend := &fakeDNSEngineBackend{
			switchErr: errors.New("injected pre-start failure"),
			recovery:  dnsEngineSwitchRecoveryRolledBack,
		}
		useFakeDNSEngineBackend(t, backend)
		var response SwitchDNSEngineV1Response
		if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
			t.Fatal(err)
		}
		if backend.switchCalls != 1 || response.Error == pdnsPairedPrimarySwitchPausedReason ||
			response.Error == bindSourcePDNSSwitchUnsupportedReason {
			t.Fatalf("open gate did not admit the fresh manifest: calls=%d response=%+v", backend.switchCalls, response)
		}
		if _, err := manager.finish(&ServiceMutationFinishRequest{
			RequestID: request.MutationRequestID, OwnerID: request.MutationOwnerID,
			Success: false, FailureCode: "expected_test_cleanup", Message: "expected test cleanup",
		}); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name     string
		manifest func(*testing.T) mutationpayload.DNSEngineSwitchManifestCommitment
		reason   string
	}{
		{"BIND source", func(t *testing.T) mutationpayload.DNSEngineSwitchManifestCommitment {
			return testPairedPDNSSwitchManifest(t, transport.DNSPairRolePrimary, nil)
		}, bindSourcePDNSSwitchUnsupportedReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := tc.manifest(t)
			request := switchRequestForManifest(manifest)
			backend := &fakeDNSEngineBackend{}
			useFakeDNSEngineBackend(t, backend)
			var response SwitchDNSEngineV1Response
			if err := (&Agent{}).SwitchDNSEngineV1(&request, &response); err != nil {
				t.Fatal(err)
			}
			if response.Error != tc.reason || backend.switchCalls != 0 {
				t.Fatalf("response=%+v calls=%d, want %q", response, backend.switchCalls, tc.reason)
			}
			// The host backend refuses the same manifest before any host work.
			if _, err := (hostDNSEngineBackend{}).Switch(
				context.Background(), manifest, request.ServiceMutationBinding,
			); err == nil || err.Error() != tc.reason {
				t.Fatalf("host backend: %v, want %q", err, tc.reason)
			}
		})
	}
}

// A paired primary whose active engine is already PowerDNS cannot even be
// expressed as a canonical switch manifest, so the RPC refuses it before any
// gate; the policy itself also keeps it paused when open.
func TestFreshPairedPDNSPrimaryOpenGateActivePowerDNSRefused(t *testing.T) {
	openFreshPairedPDNSPrimaryGate(t)
	if _, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, transport.DNSEnginePowerDNS, transport.DNSEnginePowerDNS,
		3, 4, 9, transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test", "192.0.2.20", "ns2.example.test", nil,
	); err == nil {
		t.Fatal("a PowerDNS-to-PowerDNS paired-primary switch became canonical")
	}
	active := freshPairedPDNSPrimaryManifest(t)
	active.SourceEngine, active.SourceEpoch, active.TargetEpoch = transport.DNSEnginePowerDNS, 3, 4
	if !pdnsPairedPrimarySwitchPaused(active) {
		t.Fatal("open gate admitted a paired primary with an active PowerDNS")
	}
	if err := pdnsPairedPrimaryHostAdmission(active, true); err == nil {
		t.Fatal("host backend admitted a paired primary with an active PowerDNS")
	}
}

func TestFreshPDNSTargetOwnGuardMaskV3(t *testing.T) {
	profile := hostplatform.Profile{PackageManager: hostplatform.PackageManagerAPT}
	packages := []string{"pdns-backend-sqlite3", "pdns-server"}
	manifest := freshPairedPDNSPrimaryManifest(t)
	installed := dnsEngineInstallOwnershipReceipt{
		Schema: dnsEngineInstallOwnershipSchema, Engine: transport.DNSEnginePowerDNS,
		PackageManager: string(hostplatform.PackageManagerAPT), Packages: packages,
		MissingBefore: packages, ManifestQualifier: "earlier", MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID: strings.Repeat("b", 32),
	}
	reader := func(receipt dnsEngineInstallOwnershipReceipt, exists bool, err error) func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return func(engine transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
			if engine != transport.DNSEnginePowerDNS {
				t.Fatalf("receipt engine=%s", engine)
			}
			return receipt, exists, err
		}
	}
	adopted := installed
	adopted.MissingBefore, adopted.AdoptedPresent = []string{}, true
	otherPackages := installed
	otherPackages.Packages = []string{"pdns-server"}
	runtimeMask := freshPDNSGuardMaskV3
	runtimeMask.UnitFileState = "masked-runtime"
	disabled := dnsUnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	for _, tc := range []struct {
		name    string
		before  dnsUnitSnapshot
		present bool
		read    func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error)
		want    bool
		wantErr bool
	}{
		{"own guarded install", freshPDNSGuardMaskV3, true, reader(installed, true, nil), true, false},
		{"no receipt keeps owner mask", freshPDNSGuardMaskV3, true, reader(dnsEngineInstallOwnershipReceipt{}, false, nil), false, false},
		{"adopted packages keep owner mask", freshPDNSGuardMaskV3, true, reader(adopted, true, nil), false, false},
		{"other package set", freshPDNSGuardMaskV3, true, reader(otherPackages, true, nil), false, false},
		{"packages missing now", freshPDNSGuardMaskV3, false, reader(installed, true, nil), false, false},
		{"runtime mask", runtimeMask, true, reader(installed, true, nil), false, false},
		{"not masked", disabled, true, reader(installed, true, nil), false, false},
		{"receipt unreadable", freshPDNSGuardMaskV3, true, reader(dnsEngineInstallOwnershipReceipt{}, false, errors.New("unreadable")), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := freshPDNSTargetOwnGuardMaskWithReceiptV3(tc.before, tc.present, packages, profile, manifest, tc.read)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("own=%v err=%v", got, err)
			}
		})
	}
	if err := validateFreshPDNSTargetAfterOwnGuardMaskV3(freshPDNSGuardMaskV3, freshPDNSGuardMaskV3, false); err != nil {
		t.Fatalf("unchanged own seal refused: %v", err)
	}
	if err := validateFreshPDNSTargetAfterOwnGuardMaskV3(freshPDNSGuardMaskV3, freshPDNSGuardMaskV3, true); err == nil {
		t.Fatal("own seal admitted a new package install")
	}
	if err := validateFreshPDNSTargetAfterOwnGuardMaskV3(freshPDNSGuardMaskV3, disabled, false); err == nil {
		t.Fatal("own seal admitted a changed unit")
	}
	// A preexisting mask without the receipt keeps the old refusal.
	if err := validateFreshPDNSTargetBeforePackagesV3(freshPDNSGuardMaskV3); err == nil {
		t.Fatal("pre-package mask without provenance was admitted")
	}
}

func TestFreshPDNSPrimaryHostProfileV3(t *testing.T) {
	debian := hostplatform.Profile{
		ID: "debian", Version: "13", Arch: "amd64", DistroFamily: hostplatform.DistroFamilyDebian,
		PackageManager: hostplatform.PackageManagerAPT, ServiceManager: hostplatform.ServiceManagerSystemd,
	}
	if err := validateFreshPDNSPrimaryHostProfileV3(debian); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*hostplatform.Profile){
		func(p *hostplatform.Profile) { p.Version = "12" },
		func(p *hostplatform.Profile) { p.Arch = "arm64" },
		func(p *hostplatform.Profile) { p.ID = "arch" },
	} {
		profile := debian
		change(&profile)
		err := validateFreshPDNSPrimaryHostProfileV3(profile)
		if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
			t.Fatalf("%+v: %v", profile, err)
		}
	}
}

// freshPrimaryV3AgentFixture builds exact V3 journals from the historical
// codec fixture and the retained measured staged SQL, as the recovery package
// tests do.
func freshPrimaryV3AgentFixture(t *testing.T, frozen dnsUnitSnapshot) (dnsengineartifact.JournalPolicy, dnsEngineSwitchJournal, dnsEngineSwitchJournal) {
	t.Helper()
	policy := dnsengineartifact.JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json", RequireOwner: true,
		PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.DecodeSwitchJournal(raw)
	if err != nil {
		t.Fatal(err)
	}
	manifest := freshPairedPDNSPrimaryManifest(t)
	base.SourceEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision = "", 0, 1, 0
	base.Topology, base.PairRole = manifest.Topology, manifest.PairRole
	base.LocalIP, base.LocalNS, base.PeerIP, base.PeerNS = manifest.LocalIP, manifest.LocalNS, manifest.PeerIP, manifest.PeerNS
	base.ManifestQualifier, base.SnapshotBytes, base.Zones = manifest.Qualifier, manifest.SnapshotBytes, manifest.Zones
	base.PrimaryCatalogSerial = 1
	base.StateBefore = dnsengineartifact.FileSnapshot{Path: policy.StatePath}
	base.SourceUnitsBefore = nil
	base.TargetUnitsBefore = []dnsengineartifact.UnitSnapshot{frozen}
	after := append([]dnsengineartifact.FileSnapshot(nil), base.ConfigBefore...)
	for _, i := range []int{1, 2} {
		after[i] = dnsengineartifact.FileSnapshot{Path: base.ConfigBefore[i].Path, Exists: true, Mode: 0o644, OwnerKnown: true, Data: []byte("managed=1\n")}
		after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
	}
	intent, err := policy.BuildPDNSFreshPrimaryJournalV3(base, after)
	if err != nil {
		t.Fatal(err)
	}
	measuredRaw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "e2e", "dns-kill-matrix", "evidence", "pdns-master-bind-20260928", "debian-primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var measured struct {
		Staged struct {
			SQLite pdnsnative.Snapshot `json:"sqlite"`
		} `json:"staged"`
	}
	if err := json.Unmarshal(measuredRaw, &measured); err != nil {
		t.Fatal(err)
	}
	candidate := dnsengineartifact.PDNSTargetCandidateProofV4{Path: intent.PDNSCandidatePath, Device: 9, Inode: 11, Mode: 0o640, UID: 0, GID: 42, Size: 4096, SHA256: strings.Repeat("f", 64), NoSidecars: true}
	staged, err := policy.StagePDNSFreshPrimaryCandidateV3(intent, candidate, measured.Staged.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	return policy, intent, staged
}

func withFreshPrimaryV3Phase(t *testing.T, policy dnsengineartifact.JournalPolicy, j dnsEngineSwitchJournal, phase string) dnsEngineSwitchJournal {
	t.Helper()
	j.Phase = phase
	if err := policy.ValidateSwitchJournal(j); err != nil {
		t.Fatalf("fixture phase %s: %v", phase, err)
	}
	return j
}

type prestartObserverFixture struct {
	policy     dnsengineartifact.JournalPolicy
	unit       dnsUnitSnapshot
	unitAfter  *dnsUnitSnapshot
	unitReads  int
	sourceErr  error
	stoppedErr error
	configs    []dnsenginerecovery.PDNSTargetConfigStateV4
	configErr  error
	present    map[string]bool
	candidate  dnsengineartifact.PDNSTargetCandidateProofV4
	liveErr    error
}

func (f *prestartObserverFixture) observers() freshPrimaryPrestartObserversV3 {
	return freshPrimaryPrestartObserversV3{
		policy:              f.policy,
		sourceUnitsInactive: func(context.Context) error { return f.sourceErr },
		targetUnit: func(context.Context) (dnsUnitSnapshot, error) {
			f.unitReads++
			if f.unitReads > 1 && f.unitAfter != nil {
				return *f.unitAfter, nil
			}
			return f.unit, nil
		},
		stoppedTarget: func(context.Context) error { return f.stoppedErr },
		configs: func(context.Context, dnsEngineSwitchJournal) ([]dnsenginerecovery.PDNSTargetConfigStateV4, error) {
			return f.configs, f.configErr
		},
		absent: func(paths ...string) bool {
			for _, path := range paths {
				if f.present[path] {
					return false
				}
			}
			return true
		},
		candidate: func(string) (dnsengineartifact.PDNSTargetCandidateProofV4, error) { return f.candidate, nil },
		live:      func(dnsengineartifact.PDNSTargetCandidateProofV4, string) error { return f.liveErr },
		partial: func(j dnsEngineSwitchJournal) ([]string, error) {
			build, sidecars, err := dnsengineartifact.PDNSFreshCandidateBuildPathsV3(j.PDNSCandidatePath)
			if err != nil {
				return nil, err
			}
			var present []string
			for _, path := range append(sidecars, build) {
				if f.present[path] {
					present = append(present, path)
				}
			}
			return present, nil
		},
	}
}

func allConfigs(state dnsenginerecovery.PDNSTargetConfigStateV4) []dnsenginerecovery.PDNSTargetConfigStateV4 {
	return []dnsenginerecovery.PDNSTargetConfigStateV4{state, state, state}
}

func TestAssessFreshPrimaryPrestartV3(t *testing.T) {
	policy, intent, staged := freshPrimaryV3AgentFixture(t, freshPDNSGuardMaskV3)
	db := policy.PDNSDatabasePath
	enabled := dnsUnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"}
	active := enabled
	active.ActiveState = "active"
	changed := func(err error) bool {
		var target *freshPrimaryV3ChangedError
		return errors.As(err, &target)
	}
	type check func(t *testing.T, shape freshPrimaryPrestartShapeV3, err error)
	wantShape := func(want freshPrimaryPrestartShapeV3) check {
		return func(t *testing.T, shape freshPrimaryPrestartShapeV3, err error) {
			if err != nil || shape != want {
				t.Fatalf("shape=%d err=%v, want %d", shape, err, want)
			}
		}
	}
	wantUnknown := func(t *testing.T, shape freshPrimaryPrestartShapeV3, err error) {
		if err == nil || shape != freshPrimaryPrestartUnknownV3 || changed(err) {
			t.Fatalf("shape=%d err=%v, want unknown without an owner-change mark", shape, err)
		}
	}
	wantChanged := func(what string) check {
		return func(t *testing.T, shape freshPrimaryPrestartShapeV3, err error) {
			var target *freshPrimaryV3ChangedError
			if shape != freshPrimaryPrestartUnknownV3 || !errors.As(err, &target) || target.what != what {
				t.Fatalf("shape=%d err=%v, want change of %s", shape, err, what)
			}
		}
	}
	before := dnsenginerecovery.PDNSTargetConfigBeforeV4
	afterState := dnsenginerecovery.PDNSTargetConfigAfterV4
	for _, tc := range []struct {
		name    string
		journal dnsEngineSwitchJournal
		fixture prestartObserverFixture
		check   check
	}{
		{"intent clean", intent, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before)}, wantShape(freshPrimaryPrestartIntentCleanV3)},
		// A file under the candidate name at intent is unsealed and can no
		// longer be partial (the build is renamed only when complete): it
		// stays unknown. The operation's own temporary build is its only
		// admitted effect at intent (item 4b).
		{"intent with an unsealed file under the candidate name", intent, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), present: map[string]bool{intent.PDNSCandidatePath: true}}, wantUnknown},
		{"intent with its own interrupted build", intent, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), present: map[string]bool{freshPrimaryV3BuildPathForTest(t, intent): true, freshPrimaryV3BuildPathForTest(t, intent) + "-journal": true}}, wantShape(freshPrimaryPrestartIntentPartialV3)},
		{"intent with configuration already changed", intent, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(afterState)}, wantChanged(freshPrimaryV3ChangedConfig)},
		{"staged candidate", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(afterState), candidate: *staged.PDNSFreshPlan.Candidate}, wantShape(freshPrimaryPrestartStagedV3)},
		{"staged candidate changed", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before)}, wantChanged(freshPrimaryV3ChangedDatabase)},
		{"sidecar beside never-started target", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), present: map[string]bool{db: true, db + "-wal": true}}, wantChanged(freshPrimaryV3ChangedDatabase)},
		{"owner configuration edit", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configErr: errors.New("unknown config bytes")}, wantChanged(freshPrimaryV3ChangedConfig)},
		{"state record appeared", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), present: map[string]bool{policy.StatePath: true}}, wantChanged(freshPrimaryV3ChangedState)},
		{"renamed at enable-intent", withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseTargetEnableIntent), prestartObserverFixture{unit: enabled, configs: allConfigs(afterState), present: map[string]bool{db: true}}, wantShape(freshPrimaryPrestartRenamedV3)},
		{"renamed but live file differs", withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseTargetEnableIntent), prestartObserverFixture{unit: enabled, configs: allConfigs(afterState), present: map[string]bool{db: true}, liveErr: errors.New("inode differs")}, wantChanged(freshPrimaryV3ChangedDatabase)},
		{"unit enabled before enable-intent", staged, prestartObserverFixture{unit: enabled, configs: allConfigs(before), candidate: *staged.PDNSFreshPlan.Candidate}, wantUnknown},
		{"target active", withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseTargetEnableIntent), prestartObserverFixture{unit: active, configs: allConfigs(afterState), present: map[string]bool{db: true}}, wantUnknown},
		{"stopped proof or listener refused", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), candidate: *staged.PDNSFreshPlan.Candidate, stoppedErr: errors.New("public port-53 listener")}, wantUnknown},
		{"BIND unit active", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before), candidate: *staged.PDNSFreshPlan.Candidate, sourceErr: errors.New("named active")}, wantUnknown},
		{"unit changed during assessment", staged, prestartObserverFixture{unit: freshPDNSGuardMaskV3, unitAfter: &enabled, configs: allConfigs(before), candidate: *staged.PDNSFreshPlan.Candidate}, wantUnknown},
		{"restored after decision", withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseRollingBack), prestartObserverFixture{unit: freshPDNSGuardMaskV3, configs: allConfigs(before)}, wantShape(freshPrimaryPrestartRestoredV3)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := tc.fixture
			fixture.policy = policy
			shape, err := assessFreshPrimaryPrestartV3(context.Background(), tc.journal, fixture.observers())
			tc.check(t, shape, err)
		})
	}
	native := staged
	plan := *native.PDNSFreshPlan
	plan.Native = &pdnsnative.RecordedTransition{}
	native.PDNSFreshPlan = &plan
	if freshPrimaryPrestartJournalShapeV3(native) {
		t.Fatal("a journal with a native receipt was classified pre-start")
	}
	fixture := prestartObserverFixture{policy: policy, unit: freshPDNSGuardMaskV3, configs: allConfigs(before)}
	if _, err := assessFreshPrimaryPrestartV3(context.Background(), native, fixture.observers()); err == nil {
		t.Fatal("started target assessed as pre-start")
	}
}

type prestartInverseTrace struct {
	stored  dnsEngineSwitchJournal
	shapes  []freshPrimaryPrestartShapeV3
	effects []string
	guard   error
	assessN int
}

func (tr *prestartInverseTrace) effectsOps(t *testing.T) freshPrimaryPrestartEffectsV3 {
	return freshPrimaryPrestartEffectsV3{
		read: func() (dnsEngineSwitchJournal, bool, error) { return tr.stored, true, nil },
		assess: func(context.Context, dnsEngineSwitchJournal) (freshPrimaryPrestartShapeV3, error) {
			if tr.assessN >= len(tr.shapes) {
				return tr.shapes[len(tr.shapes)-1], nil
			}
			shape := tr.shapes[tr.assessN]
			tr.assessN++
			if shape == freshPrimaryPrestartUnknownV3 {
				return shape, errors.New("PowerDNS may have started")
			}
			return shape, nil
		},
		checkpoint: func(before, after dnsEngineSwitchJournal) error {
			if !reflect.DeepEqual(before, tr.stored) {
				t.Fatal("checkpoint ignored the exact journal preimage")
			}
			tr.stored = after
			tr.effects = append(tr.effects, "checkpoint:"+after.Phase)
			return nil
		},
		restoreUnit: func(ctx context.Context, snapshot dnsengineartifact.UnitSnapshot, guard func(context.Context) error) error {
			if err := guard(ctx); err != nil {
				return err
			}
			tr.effects = append(tr.effects, "unit:"+snapshot.UnitFileState)
			return nil
		},
		restoreRenamed: func(_ dnsEngineSwitchJournal, guard func() error) error {
			if err := guard(); err != nil {
				return err
			}
			tr.effects = append(tr.effects, "unrename")
			return nil
		},
		restoreConfigs: func(ctx context.Context, _ dnsEngineSwitchJournal, guard func(context.Context) error) error {
			if tr.guard != nil {
				return tr.guard
			}
			if err := guard(ctx); err != nil {
				return err
			}
			tr.effects = append(tr.effects, "configs")
			return nil
		},
		removeStaged: func(_ dnsEngineSwitchJournal, guard func() error) error {
			if err := guard(); err != nil {
				return err
			}
			tr.effects = append(tr.effects, "remove-candidate")
			return nil
		},
		removePartial: func(j dnsEngineSwitchJournal, guard func() error) error {
			if j.Phase != dnsengineartifact.SwitchPhaseRollingBack {
				t.Fatalf("partial build removed before a durable rollback decision (phase %s)", j.Phase)
			}
			if err := guard(); err != nil {
				return err
			}
			tr.effects = append(tr.effects, "remove-partial")
			return nil
		},
	}
}

func freshPrimaryV3BuildPathForTest(t *testing.T, j dnsEngineSwitchJournal) string {
	t.Helper()
	build, _, err := dnsengineartifact.PDNSFreshCandidateBuildPathsV3(j.PDNSCandidatePath)
	if err != nil {
		t.Fatal(err)
	}
	return build
}

func TestFreshPrimaryPrestartInverseV3(t *testing.T) {
	policy, intent, staged := freshPrimaryV3AgentFixture(t, freshPDNSGuardMaskV3)
	enableIntent := withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseTargetEnableIntent)
	rollingBack := withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseRollingBack)
	rolledBack := withFreshPrimaryV3Phase(t, policy, staged, dnsengineartifact.SwitchPhaseRolledBack)
	S, R, N, C, U := freshPrimaryPrestartStagedV3, freshPrimaryPrestartRenamedV3, freshPrimaryPrestartRestoredV3, freshPrimaryPrestartIntentCleanV3, freshPrimaryPrestartUnknownV3
	for _, tc := range []struct {
		name        string
		journal     dnsEngineSwitchJournal
		shapes      []freshPrimaryPrestartShapeV3
		guardErr    error
		wantOutcome dnsEngineSwitchRecoveryOutcome
		wantEffects []string
		wantPhase   string
	}{
		{"target-staged removes only the sealed candidate", staged, []freshPrimaryPrestartShapeV3{S, S, S, S, S, N, N}, nil,
			dnsenginerecovery.OutcomeRolledBack,
			[]string{"checkpoint:rolling-back", "configs", "remove-candidate", "checkpoint:rolled-back"}, dnsengineartifact.SwitchPhaseRolledBack},
		{"enable-intent restores the guard seal first", enableIntent, []freshPrimaryPrestartShapeV3{R, R, R, R, S, S, S, N, N}, nil,
			dnsenginerecovery.OutcomeRolledBack,
			[]string{"checkpoint:rolling-back-target-enable", "unit:masked", "unrename", "configs", "remove-candidate", "checkpoint:rolled-back"}, dnsengineartifact.SwitchPhaseRolledBack},
		{"intent has no native effect", intent, []freshPrimaryPrestartShapeV3{C, C, C}, nil,
			dnsenginerecovery.OutcomeRolledBack,
			[]string{"checkpoint:rolling-back", "checkpoint:rolled-back"}, dnsengineartifact.SwitchPhaseRolledBack},
		{"intent removes only its own interrupted build", intent, []freshPrimaryPrestartShapeV3{freshPrimaryPrestartIntentPartialV3, freshPrimaryPrestartIntentPartialV3, freshPrimaryPrestartIntentPartialV3, C, C}, nil,
			dnsenginerecovery.OutcomeRolledBack,
			[]string{"checkpoint:rolling-back", "remove-partial", "checkpoint:rolled-back"}, dnsengineartifact.SwitchPhaseRolledBack},
		{"interrupted build that stays is not rolled back", intent, []freshPrimaryPrestartShapeV3{freshPrimaryPrestartIntentPartialV3}, nil,
			dnsenginerecovery.OutcomeAbsent,
			[]string{"checkpoint:rolling-back", "remove-partial"}, dnsengineartifact.SwitchPhaseRollingBack},
		{"resumes a durable decision without a second one", rollingBack, []freshPrimaryPrestartShapeV3{S, S, S, S, S, N, N}, nil,
			dnsenginerecovery.OutcomeRolledBack,
			[]string{"configs", "remove-candidate", "checkpoint:rolled-back"}, dnsengineartifact.SwitchPhaseRolledBack},
		{"rolled-back is re-proved without effects", rolledBack, []freshPrimaryPrestartShapeV3{N, N}, nil,
			dnsenginerecovery.OutcomeRolledBack, nil, dnsengineartifact.SwitchPhaseRolledBack},
		{"possibly started target is never touched", enableIntent, []freshPrimaryPrestartShapeV3{U}, nil,
			dnsenginerecovery.OutcomeAbsent, nil, dnsengineartifact.SwitchPhaseTargetEnableIntent},
		{"restored shape before a decision is refused", staged, []freshPrimaryPrestartShapeV3{N}, nil,
			dnsenginerecovery.OutcomeAbsent, nil, dnsengineartifact.SwitchPhaseTargetStaged},
		{"guard refusal keeps the decision without a verdict", staged, []freshPrimaryPrestartShapeV3{S, S}, errors.New("owner edit found by guard"),
			dnsenginerecovery.OutcomeAbsent, []string{"checkpoint:rolling-back"}, dnsengineartifact.SwitchPhaseRollingBack},
		{"inverse that does not restore is not rolled back", staged, []freshPrimaryPrestartShapeV3{S, S, S, S, S, S, S}, nil,
			dnsenginerecovery.OutcomeAbsent, []string{"checkpoint:rolling-back", "configs", "remove-candidate"}, dnsengineartifact.SwitchPhaseRollingBack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &prestartInverseTrace{stored: tc.journal, shapes: tc.shapes, guard: tc.guardErr}
			outcome, err := runFreshPrimaryPrestartInverseV3(context.Background(), tc.journal, tr.effectsOps(t))
			if outcome != tc.wantOutcome || (err == nil) != (tc.wantOutcome == dnsenginerecovery.OutcomeRolledBack) {
				t.Fatalf("outcome=%s err=%v, want %s", outcome, err, tc.wantOutcome)
			}
			if !reflect.DeepEqual(tr.effects, tc.wantEffects) {
				t.Fatalf("effects=%v, want %v", tr.effects, tc.wantEffects)
			}
			if tr.stored.Phase != tc.wantPhase {
				t.Fatalf("journal phase=%s, want %s", tr.stored.Phase, tc.wantPhase)
			}
		})
	}
	t.Run("changed journal refuses before any effect", func(t *testing.T) {
		tr := &prestartInverseTrace{stored: rollingBack, shapes: []freshPrimaryPrestartShapeV3{S}}
		if _, err := runFreshPrimaryPrestartInverseV3(context.Background(), staged, tr.effectsOps(t)); err == nil || len(tr.effects) != 0 {
			t.Fatalf("err=%v effects=%v", err, tr.effects)
		}
	})
}

func TestFreshPrimaryV3RecoveryGuidance(t *testing.T) {
	id := strings.Repeat("c", 32)
	prestart := classifyFreshPrimaryV3RecoveryError(id, true, "not running", errors.New("listener"))
	poststart := classifyFreshPrimaryV3RecoveryError(id, false, "running", errors.New("peer not converged"))
	owner := classifyFreshPrimaryV3RecoveryError(id, false, "running",
		freshPrimaryV3Changed(freshPrimaryV3ChangedDatabase, errors.New("native snapshot changed records")))
	ownerBefore := classifyFreshPrimaryV3RecoveryError(id, true, "not running",
		freshPrimaryV3Changed(freshPrimaryV3ChangedConfig, errors.New("unknown bytes")))
	status := "dns-switch-status --quiesced --request-id " + id
	for _, tc := range []struct {
		name     string
		err      error
		contains []string
		excludes []string
	}{
		{"before start", prestart, []string{"before PowerDNS started", "undoes such an install by itself",
			"recover-dns-pdns-fresh-prestart --request-id " + id, status, "is not running",
			"had no DNS engine before the install", "other server changes can continue"}, []string{"never removes"}},
		{"after start", poststart, []string{"after PowerDNS had started", "only completes the same install",
			"never removes a PowerDNS that has started", "is running on this server now", status,
			"continues the same install"}, []string{"recover-dns-pdns-fresh-prestart"}},
		{"owner change after start", owner, []string{"the PowerDNS database is not as this install wrote it",
			"kept that change and neither continued nor undid", status, "contact support with request id " + id}, []string{"recover-dns-pdns-fresh-prestart"}},
		{"owner change before start", ownerBefore, []string{"the PowerDNS configuration is not as this install wrote it"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The panel receipts use the same text at startup and in-process.
			for _, message := range []string{
				releasedDNSSwitchUnknownMessage(tc.err),
				releasedDNSSwitchInProcessUnknownMessage(&dnsSwitchNativeRecoveryUnknownError{err: tc.err}),
			} {
				for _, want := range tc.contains {
					if !strings.Contains(message, want) {
						t.Fatalf("message lacks %q:\n%s", want, message)
					}
				}
				for _, unwanted := range tc.excludes {
					if strings.Contains(message, unwanted) {
						t.Fatalf("message contains %q:\n%s", unwanted, message)
					}
				}
			}
		})
	}
	var classified *freshPrimaryV3RecoveryError
	if !errors.As(owner, &classified) || classified.kind != freshPrimaryV3OwnerChange {
		t.Fatal("owner change was not classified")
	}
	if again := classifyFreshPrimaryV3RecoveryError(id, true, "", owner); again != owner {
		t.Fatal("classification was applied twice")
	}
	if classifyFreshPrimaryV3RecoveryError(id, true, "", nil) != nil {
		t.Fatal("success was classified as a refusal")
	}
	unknown := classifyFreshPrimaryV3RecoveryError(id, false, "", errors.New("x"))
	if !strings.Contains(releasedDNSSwitchUnknownMessage(unknown), "could not be read") {
		t.Fatal("unknown service state was reported as known")
	}
}

// Three legacy call sites read this host's own catalog as the peer re-serves
// it. The parse policy follows the local engine that produced it.
func TestLegacyPrimaryPeerCatalogFollowsLocalProducer(t *testing.T) {
	previousSOA, previousBIND, previousPDNS := probeDNSZoneSOA, probeDNSBoundCatalogAXFR, probeDNSBoundPDNSCatalogAXFR
	t.Cleanup(func() {
		probeDNSZoneSOA, probeDNSBoundCatalogAXFR, probeDNSBoundPDNSCatalogAXFR = previousSOA, previousBIND, previousPDNS
	})
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.10", PeerIP: "192.0.2.11", Domain: "catalog-c000020a.celikpanel.invalid", Serial: 7,
		Members: []string{}, MemberSerials: []uint32{},
	}
	probeDNSZoneSOA = func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
		return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{7}}, nil
	}
	var used []string
	probeDNSBoundCatalogAXFR = func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
		used = append(used, "BIND")
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
	}
	probeDNSBoundPDNSCatalogAXFR = func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
		used = append(used, "PowerDNS")
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
	}
	for _, tc := range []struct {
		engine transport.DNSEngine
		want   string
	}{
		{transport.DNSEnginePowerDNS, "PowerDNS"},
		{transport.DNSEngineBIND, "BIND"},
	} {
		used = nil
		if _, err := verifyLegacyPrimaryPeerCatalogAuthorityForLocalEngine(context.Background(), evidence, tc.engine); err != nil {
			t.Fatalf("%s: %v", tc.engine, err)
		}
		if !reflect.DeepEqual(used, []string{tc.want}) {
			t.Fatalf("%s local producer read the peer catalog with %v", tc.engine, used)
		}
	}
	if _, err := peerReservedCatalogAXFRForLocalEngine(""); err == nil {
		t.Fatal("unknown local producer was given a parse policy")
	}
	// The three call sites must select by the local engine, never hard-code
	// the BIND policy.
	for _, site := range []struct{ file, function string }{
		{"dns_engine_host.go", "func syncPDNSV3Zone("},
		{"dns_engine_catalog_handoff.go", "func primaryCatalogSerialFromSource("},
		{"dns_engine_catalog_handoff.go", "func verifyLegacyCompletedPrimaryCatalogTarget("},
	} {
		raw, err := os.ReadFile(site.file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		start := strings.Index(source, site.function)
		if start < 0 {
			t.Fatalf("%s is missing", site.function)
		}
		body := source[start+len(site.function):]
		if end := strings.Index(body, "\nfunc "); end > 0 {
			body = body[:end]
		}
		if strings.Contains(body, "probeDNSBoundCatalogAXFR") ||
			(!strings.Contains(body, "peerReservedCatalogAXFRForLocalEngine(") &&
				!strings.Contains(body, "verifyLegacyPrimaryPeerCatalogAuthorityForLocalEngine(")) {
			t.Fatalf("%s does not select the peer catalog policy by its local producer", site.function)
		}
	}
}

// A publication's completion wave follows the daemon's producer serial
// re-stamp (identical members and member serials, a new CATALOG-HASH) and
// nothing else.
func TestCompleteDNSV3PrimaryPropagationFollowsOnlyDaemonSerial(t *testing.T) {
	planned := testPDNSPrimaryPropagationEvidence(1790605418, []string{"example.test"}, []uint32{41})
	planned.CatalogHash = testCatalogHashBefore
	liveSerial := uint32(1790607757)
	soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		serial := uint32(41)
		if domain == planned.Domain {
			serial = liveSerial
		}
		return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial}}, nil
	}
	local := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: liveSerial, Members: []string{"example.test"}}, nil
	}
	peer := func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: liveSerial, Members: []string{"example.test"}}, nil
	}
	zone := absentTestPeerZoneAXFR(planned)
	for _, tc := range []struct {
		name    string
		fresh   func() dnsPrimaryCatalogEvidence
		wantErr bool
	}{
		{"daemon re-stamp followed", func() dnsPrimaryCatalogEvidence {
			fresh := planned
			fresh.Serial = liveSerial
			fresh.CatalogHash = testCatalogHashAfter
			return fresh
		}, false},
		{"member change not followed", func() dnsPrimaryCatalogEvidence {
			fresh := planned
			fresh.Serial = liveSerial
			fresh.CatalogHash = testCatalogHashAfter
			fresh.MemberSerials = []uint32{40}
			return fresh
		}, true},
		{"higher serial without a new hash not followed", func() dnsPrimaryCatalogEvidence {
			fresh := planned
			fresh.Serial = liveSerial
			return fresh
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refreshes := 0
			plan := dnsV3PrimaryPropagationPlan{
				SourceState: dnsEngineStateReceipt{Engine: transport.DNSEnginePowerDNS},
				Evidence:    planned,
				Changed:     expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
				RefreshEvidence: func(context.Context) (dnsPrimaryCatalogEvidence, error) {
					refreshes++
					return tc.fresh(), nil
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
			defer cancel()
			err := completeDNSV3PrimaryPropagationAt(ctx, plan, soa, local, peer, zone)
			if (err != nil) != tc.wantErr || refreshes == 0 {
				t.Fatalf("err=%v refreshes=%d", err, refreshes)
			}
		})
	}
	// Without a refresher (BIND, legacy PowerDNS) the plan stays fixed.
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if err := completeDNSV3PrimaryPropagationAt(ctx, dnsV3PrimaryPropagationPlan{
		Evidence: planned, Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
	}, soa, local, peer, zone); err == nil {
		t.Fatal("a fixed plan accepted a different live serial")
	}
}

func TestNativePDNSProducerAdmitsOnlyDaemonWrittenFields(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   []string
		wantErr bool
	}{
		{"staged producer", nil, false},
		{"daemon notified serial", []string{`UPDATE domains SET notified_serial = 1 WHERE id = 1`}, false},
		{"daemon catalog hash", []string{`INSERT INTO domainmetadata(domain_id, kind, content) VALUES (1, 'CATALOG-HASH', 'RJjZhwvgTMG61d9tU4WMEoSTWkeoIylKJ2UGpSD7h3k=')`}, false},
		{"second hash row", []string{
			`INSERT INTO domainmetadata(domain_id, kind, content) VALUES (1, 'CATALOG-HASH', 'RJjZhwvgTMG61d9tU4WMEoSTWkeoIylKJ2UGpSD7h3k=')`,
			`INSERT INTO domainmetadata(domain_id, kind, content) VALUES (1, 'CATALOG-HASH', 'RJjZhwvgTMG61d9tU4WMEoSTWkeoIylKJ2UGpSD7h3k=')`,
		}, true},
		{"owner metadata", []string{`INSERT INTO domainmetadata(domain_id, kind, content) VALUES (1, 'ALSO-NOTIFY', '198.51.100.1')`}, true},
		{"malformed hash", []string{`INSERT INTO domainmetadata(domain_id, kind, content) VALUES (1, 'CATALOG-HASH', 'short')`}, true},
		{"last_check written", []string{`UPDATE domains SET last_check = 1790607757 WHERE id = 1`}, true},
		{"producer options", []string{`UPDATE domains SET options = '{"coo":"other.test."}' WHERE id = 1`}, true},
		{"producer master", []string{`UPDATE domains SET master = '198.51.100.1' WHERE id = 1`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "producer.sqlite3")
			db, err := initializePDNSEngineDB(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			statements := append([]string{
				`INSERT INTO domains(id, name, type, account) VALUES (1, 'catalog-c000020a.celikpanel.invalid', 'PRODUCER', 'celikpanel-bind-catalog-v1')`,
			}, tc.setup...)
			for _, statement := range statements {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			err = verifyNativePDNSProducerDaemonStateTx(context.Background(), tx, 1)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
