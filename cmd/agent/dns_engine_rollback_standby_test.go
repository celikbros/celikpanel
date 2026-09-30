package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Decision B (2026-09-30): the one Agent-side definition of a rolled-back first
// install's standby. Only packages CelikPanel itself installed (non-empty
// missing-before, not an adoption), stopped under the guard mask or disabled,
// with no engine state or ownership receipt, qualify. An owner-installed
// engine never does.
func TestRollbackStandbyForBackendReadiness(t *testing.T) {
	packages := []string{"pdns-backend-sqlite3", "pdns-server"}
	runtime := transport.DNSBackendRuntimeState{
		Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service", Installed: true,
	}
	masked := dnsUnitState{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}
	disabled := dnsUnitState{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	installed := dnsEngineInstallOwnershipReceipt{
		Schema: dnsEngineInstallOwnershipSchema, Engine: transport.DNSEnginePowerDNS,
		PackageManager: "apt", Packages: packages, MissingBefore: packages,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("b", 32), MutationOwnerID: strings.Repeat("c", 32),
	}
	adopted := installed
	adopted.MissingBefore, adopted.AdoptedPresent = nil, true
	check := func(name string, want bool, runtime transport.DNSBackendRuntimeState, unit dnsUnitState,
		stateExists, ownershipExists bool, receipt dnsEngineInstallOwnershipReceipt, receiptExists bool, receiptErr error) {
		t.Helper()
		got := rollbackStandbyForBackendReadiness(runtime, unit, stateExists, ownershipExists, nil,
			receipt, receiptExists, receiptErr, hostplatform.PackageManagerAPT, packages)
		if got != want {
			t.Fatalf("%s: standby=%v, want %v", name, got, want)
		}
	}
	check("guard-masked standby", true, runtime, masked, false, false, installed, true, nil)
	check("disabled standby", true, runtime, disabled, false, false, installed, true, nil)
	check("owner-installed (adopted receipt)", false, runtime, masked, false, false, adopted, true, nil)
	check("owner-installed (no receipt)", false, runtime, disabled, false, false, dnsEngineInstallOwnershipReceipt{}, false, nil)
	check("engine state exists", false, runtime, masked, true, false, installed, true, nil)
	check("engine ownership exists", false, runtime, masked, false, true, installed, true, nil)
	check("unreadable receipt", false, runtime, masked, false, false, installed, true, errors.New("io"))
	enabled := disabled
	enabled.UnitFileState = "enabled"
	check("enabled unit", false, runtime, enabled, false, false, installed, true, nil)
	active := masked
	active.ActiveState = "active"
	check("active unit", false, runtime, active, false, false, installed, true, nil)
	running := runtime
	running.Running = true
	check("running", false, running, disabled, false, false, installed, true, nil)
	other := installed
	other.Packages, other.MissingBefore = []string{"pdns-server"}, []string{"pdns-server"}
	check("other package set", false, runtime, masked, false, false, other, true, nil)
}

func TestInitialDNSEngineInstallRollbackEvidenceScope(t *testing.T) {
	// The table states the closed gate; pin it so the test holds whichever
	// way the product constant stands. The open gate has its own test.
	previousGate := pdnsFreshPairedPrimaryGateOpen
	pdnsFreshPairedPrimaryGateOpen = false
	t.Cleanup(func() { pdnsFreshPairedPrimaryGateOpen = previousGate })
	manifest := func(target transport.DNSEngine, topology, role string) mutationpayload.DNSEngineSwitchManifestCommitment {
		m := mutationpayload.DNSEngineSwitchManifestCommitment{
			Mode: transport.DNSEngineSwitchModeSwitch, TargetEngine: target, TargetEpoch: 1, Topology: topology, PairRole: role,
		}
		if topology == transport.DNSTopologyPaired {
			m.LocalIP, m.LocalNS, m.PeerIP, m.PeerNS = "192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test"
		}
		return m
	}
	for name, tc := range map[string]struct {
		m    mutationpayload.DNSEngineSwitchManifestCommitment
		want bool
	}{
		"bind standalone":        {manifest(transport.DNSEngineBIND, transport.DNSTopologyStandalone, ""), true},
		"bind paired primary":    {manifest(transport.DNSEngineBIND, transport.DNSTopologyPaired, transport.DNSPairRolePrimary), true},
		"bind paired secondary":  {manifest(transport.DNSEngineBIND, transport.DNSTopologyPaired, transport.DNSPairRoleSecondary), true},
		"pdns standalone":        {manifest(transport.DNSEnginePowerDNS, transport.DNSTopologyStandalone, ""), true},
		"pdns paired secondary":  {manifest(transport.DNSEnginePowerDNS, transport.DNSTopologyPaired, transport.DNSPairRoleSecondary), true},
		"pdns paired primary V3": {manifest(transport.DNSEnginePowerDNS, transport.DNSTopologyPaired, transport.DNSPairRolePrimary), false},
	} {
		if got := initialDNSEngineInstallRollbackEvidenceScope(tc.m); got != tc.want {
			t.Fatalf("%s: scope=%v, want %v", name, got, tc.want)
		}
	}
	for name, edit := range map[string]func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		"source": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.SourceEngine, m.SourceEpoch = transport.DNSEngineBIND, 1
		},
		"adopt": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Mode = transport.DNSEngineSwitchModeAdopt
		},
		"epoch 2":      func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.TargetEpoch = 2 },
		"partial pair": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.PeerNS = "" },
	} {
		m := manifest(transport.DNSEnginePowerDNS, transport.DNSTopologyPaired, transport.DNSPairRoleSecondary)
		edit(&m)
		if initialDNSEngineInstallRollbackEvidenceScope(m) {
			t.Fatalf("%s: widened scope accepted %+v", name, m)
		}
	}
}

// A paired-secondary rollback never accepts the takeover's "running,
// unmanaged, restored" shape.
func TestRollbackEvidenceRefusesRestoredTakeoverForSecondary(t *testing.T) {
	withRollbackEvidenceReadersForTest(t)
	request, _ := rollbackEvidenceRequestForTest(t, "", 0)
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyPaired, transport.DNSPairRoleSecondary,
		"192.0.2.10", "ns1.example.test", "192.0.2.11", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.ManifestQualifier = manifest.Qualifier
	readRollbackEvidenceJournal = func() (dnsEngineSwitchJournal, bool, error) { return dnsEngineSwitchJournal{}, false, nil }
	readRollbackEvidenceState = func() (dnsEngineStateReceipt, bool, error) { return dnsEngineStateReceipt{}, false, nil }
	readRollbackEvidenceOwnership = func(transport.DNSEngine) (dnsEngineStateReceipt, bool, error) {
		return dnsEngineStateReceipt{}, false, nil
	}
	readRollbackEvidenceInstallOwnership = func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error) {
		return installOwnershipForEvidenceTest(request, manifest), true, nil
	}
	var seen dnsEngineRollbackTargetHost
	verifyRollbackEvidenceTargetSeal = func(_ context.Context, _ transport.DNSEngine, host dnsEngineRollbackTargetHost) error {
		seen = host
		return nil
	}
	if outcome, err := classifyDNSEngineRollbackHostEvidence(context.Background(), request, manifest); err != nil ||
		outcome != transport.DNSEngineRollbackSafe || !seen.RefuseRestoredTakeover {
		t.Fatalf("outcome=%q err=%v host=%+v", outcome, err, seen)
	}
}
