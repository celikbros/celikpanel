package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func fixtureZone() []transport.DNSEngineSwitchZoneSnapshot {
	return []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "s1-kill.test", DesiredGeneration: 1, ZoneType: "NATIVE",
		Records: []transport.ZoneRecord{{
			Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
	}}
}

// stoppedBINDTakeoverScenario is row 12: byte-identical to a fresh standalone
// BIND install on the wire, with the truthful stopped-owner-BIND provenance.
func stoppedBINDTakeoverScenario() scenario {
	value := standaloneScenario("bind", "", transport.DNSEngineBIND, 0)
	value.SourceFixture = fixtureUnmanagedBINDStopped
	value.Zones = fixtureZone()
	return value
}

// bindReinstallScenario is row 14: the Panel's reinstall_active manifest,
// source = target = bind with the unchanged managed epoch.
func bindReinstallScenario() scenario {
	return scenario{
		Schema: scenarioSchema, Driver: "bind",
		SourceFixture: fixtureManagedBINDAbsent,
		Mode:          transport.DNSEngineSwitchModeReinstall,
		SourceEngine:  transport.DNSEngineBIND, TargetEngine: transport.DNSEngineBIND,
		SourceEpoch: 1, TargetEpoch: 1,
		Topology: transport.DNSTopologyStandalone,
		Zones:    fixtureZone(),
	}
}

func TestStoppedBINDTakeoverHasFreshInstallRPCWithOwnProvenance(t *testing.T) {
	takeover := stoppedBINDTakeoverScenario()
	request, err := requestForScenario(takeover, "bind")
	if err != nil {
		t.Fatalf("stopped BIND takeover rejected: %v", err)
	}
	fresh := takeover
	fresh.SourceFixture = "uninitialized"
	freshRequest, err := requestForScenario(fresh, "bind")
	if err != nil {
		t.Fatalf("fresh BIND install rejected: %v", err)
	}
	// The Agent chooses the takeover from host state; the RPC is identical.
	if request.ManifestQualifier != freshRequest.ManifestQualifier ||
		request.Mode != transport.DNSEngineSwitchModeSwitch ||
		request.SourceEngine != "" || request.TargetEpoch != 1 {
		t.Fatalf("takeover request differs from the fresh install: %+v vs %+v", request, freshRequest)
	}
}

func TestStoppedBINDTakeoverRefusesEveryOtherShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*scenario)
	}{
		{"source engine", func(v *scenario) {
			v.SourceEngine = transport.DNSEngineBIND
			v.SourceEpoch = 1
			v.TargetEpoch = 2
		}},
		{"source epoch", func(v *scenario) { v.SourceEpoch = 1; v.TargetEpoch = 2 }},
		{"target epoch", func(v *scenario) { v.TargetEpoch = 2 }},
		{"revision", func(v *scenario) { v.SourceRevision = 1 }},
		{"reinstall mode", func(v *scenario) { v.Mode = transport.DNSEngineSwitchModeReinstall }},
		{"adopt mode", func(v *scenario) { v.Mode = transport.DNSEngineSwitchModeAdopt }},
		{"paired", func(v *scenario) {
			v.Topology = transport.DNSTopologyPaired
			v.PairRole = transport.DNSPairRolePrimary
			v.LocalIP, v.LocalNS = "192.0.2.10", "ns1.s1-kill.test"
			v.PeerIP, v.PeerNS = "192.0.2.11", "ns2.s1-kill.test"
		}},
		{"PowerDNS target", func(v *scenario) { v.TargetEngine = transport.DNSEnginePowerDNS }},
		{"pdns-switch driver", func(v *scenario) {
			v.Driver = "pdns-switch"
			v.TargetEngine = transport.DNSEnginePowerDNS
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := stoppedBINDTakeoverScenario()
			tc.edit(&value)
			if _, err := requestForScenario(value, value.Driver); err == nil {
				t.Fatal("unmanaged-bind-stopped provenance accepted a manifest outside the fresh standalone BIND install")
			}
		})
	}
}

func TestBINDReinstallIsAdmittedOnlyWithRemovedEngineProvenance(t *testing.T) {
	request, err := requestForScenario(bindReinstallScenario(), "bind")
	if err != nil {
		t.Fatalf("BIND reinstall rejected: %v", err)
	}
	if request.Mode != transport.DNSEngineSwitchModeReinstall ||
		request.SourceEngine != transport.DNSEngineBIND ||
		request.TargetEngine != transport.DNSEngineBIND ||
		request.SourceEpoch != 1 || request.TargetEpoch != 1 {
		t.Fatalf("reinstall request is not the exact same-epoch BIND reinstall: %+v", request)
	}
	higher := bindReinstallScenario()
	higher.SourceEpoch, higher.TargetEpoch = 3, 3
	if _, err := requestForScenario(higher, "bind"); err != nil {
		t.Fatalf("reinstall at a later managed epoch rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		driver string
		edit   func(*scenario)
	}{
		{"managed-bind provenance", "bind", func(v *scenario) { v.SourceFixture = "managed-bind" }},
		{"uninitialized provenance", "bind", func(v *scenario) { v.SourceFixture = "uninitialized" }},
		{"takeover provenance", "bind", func(v *scenario) { v.SourceFixture = fixtureUnmanagedBINDStopped }},
		{"switch mode", "bind", func(v *scenario) { v.Mode = transport.DNSEngineSwitchModeSwitch }},
		{"epoch change", "bind", func(v *scenario) { v.TargetEpoch = 2 }},
		{"zero epoch", "bind", func(v *scenario) { v.SourceEpoch, v.TargetEpoch = 0, 0 }},
		{"PowerDNS source", "bind", func(v *scenario) { v.SourceEngine = transport.DNSEnginePowerDNS }},
		{"PowerDNS reinstall", "pdns-switch", func(v *scenario) {
			v.Driver = "pdns-switch"
			v.SourceEngine, v.TargetEngine = transport.DNSEnginePowerDNS, transport.DNSEnginePowerDNS
		}},
		{"adoption driver", "pdns-adopt", func(v *scenario) { v.Driver = "pdns-adopt" }},
		{"reconfigure driver", "pdns-secondary-reconfigure", func(v *scenario) {
			v.Driver = "pdns-secondary-reconfigure"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := bindReinstallScenario()
			tc.edit(&value)
			if _, err := requestForScenario(value, tc.driver); err == nil {
				t.Fatal("managed-bind-absent or reinstall accepted outside the exact BIND reinstall")
			}
		})
	}
}

// The canonicalizer refuses some shapes itself; the fixture checks must refuse
// them independently so provenance never widens.
func TestProvenanceFixturesNarrowTheManifestOnTheirOwn(t *testing.T) {
	reinstall := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode:         transport.DNSEngineSwitchModeReinstall,
		SourceEngine: transport.DNSEngineBIND, TargetEngine: transport.DNSEngineBIND,
		SourceEpoch: 2, TargetEpoch: 2, Topology: transport.DNSTopologyStandalone,
	}
	if err := validateDriverManifest("bind", fixtureManagedBINDAbsent, reinstall); err != nil {
		t.Fatalf("exact reinstall commitment rejected: %v", err)
	}
	for name, edit := range map[string]func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		"paired":        func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.Topology = transport.DNSTopologyPaired },
		"peer ip":       func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.PeerIP = "192.0.2.11" },
		"unequal epoch": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.TargetEpoch = 3 },
		"switch mode": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Mode = transport.DNSEngineSwitchModeSwitch
		},
	} {
		t.Run("reinstall "+name, func(t *testing.T) {
			manifest := reinstall
			edit(&manifest)
			if err := validateDriverManifest("bind", fixtureManagedBINDAbsent, manifest); err == nil {
				t.Fatal("managed-bind-absent widened the reinstall shape")
			}
		})
	}
	for _, fixture := range []string{"uninitialized", "managed-pdns", "owner-bind", "managed-bind", fixtureUnmanagedBINDStopped} {
		if err := validateDriverManifest("bind", fixture, reinstall); err == nil {
			t.Fatalf("reinstall accepted under %s provenance", fixture)
		}
	}
	takeover := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode: transport.DNSEngineSwitchModeSwitch, TargetEngine: transport.DNSEngineBIND,
		TargetEpoch: 1, Topology: transport.DNSTopologyStandalone,
	}
	if err := validateDriverManifest("bind", fixtureUnmanagedBINDStopped, takeover); err != nil {
		t.Fatalf("exact takeover commitment rejected: %v", err)
	}
	for name, edit := range map[string]func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		"paired":   func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.Topology = transport.DNSTopologyPaired },
		"local ns": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.LocalNS = "ns1.s1-kill.test" },
		"epoch":    func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.TargetEpoch = 2 },
		"revision": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.SourceRevision = 1 },
	} {
		t.Run("takeover "+name, func(t *testing.T) {
			manifest := takeover
			edit(&manifest)
			if err := validateDriverManifest("bind", fixtureUnmanagedBINDStopped, manifest); err == nil {
				t.Fatal("unmanaged-bind-stopped widened the fresh install shape")
			}
		})
	}
	for _, driver := range []string{"pdns-switch", "pdns-adopt", "pdns-secondary-reconfigure", "signed-update-finalize"} {
		if err := validateDriverManifest(driver, fixtureUnmanagedBINDStopped, takeover); err == nil {
			t.Fatalf("takeover provenance accepted under driver %s", driver)
		}
		if err := validateDriverManifest(driver, fixtureManagedBINDAbsent, reinstall); err == nil {
			t.Fatalf("reinstall provenance accepted under driver %s", driver)
		}
	}
}

// The identity receipt binds the fixture name, so a retry of a takeover cannot
// be replayed as a fresh install (or the reverse).
func TestIdentityReceiptBindsProvenanceFixture(t *testing.T) {
	takeover := mustScenarioRequest(t, stoppedBINDTakeoverScenario())
	receipt := identityReceipt{
		Schema: identityReceiptSchema, CellID: "bind__target-staged__after-write__standalone__peer-reachable",
		Driver: "bind", SourceFixture: fixtureUnmanagedBINDStopped,
		RequestID: "0123456789abcdef0123456789abcdef", OwnerID: "fedcba9876543210fedcba9876543210",
		ManifestQualifier: takeover.ManifestQualifier,
	}
	fresh := receipt
	fresh.SourceFixture = "uninitialized"
	first, err := canonicalIdentityReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalIdentityReceipt(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("identity receipt does not distinguish takeover from fresh-install provenance")
	}
}
