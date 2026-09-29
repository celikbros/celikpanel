package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// freshPDNSSecondaryScenario is the manifest the Panel builds for a fresh
// paired-secondary PowerDNS install (setup or card action "install"), with the
// harness's uninitialized 0/0 source identity.
func freshPDNSSecondaryScenario() scenario {
	return scenario{
		Schema: scenarioSchema, Driver: "pdns-switch",
		SourceFixture: "uninitialized",
		Mode:          transport.DNSEngineSwitchModeSwitch,
		TargetEngine:  transport.DNSEnginePowerDNS, TargetEpoch: 1,
		Topology: transport.DNSTopologyPaired,
		PairRole: transport.DNSPairRoleSecondary,
		LocalIP:  "192.0.2.10", LocalNS: "ns2.s1-kill.test",
		PeerIP: "192.0.2.11", PeerNS: "ns1.s1-kill.test",
		Zones: []transport.DNSEngineSwitchZoneSnapshot{},
	}
}

func TestFreshPDNSPairSecondaryInstallIsAcceptedOnlyWithUninitializedProvenance(t *testing.T) {
	fresh := freshPDNSSecondaryScenario()
	request, err := requestForScenario(fresh, "pdns-switch")
	if err != nil {
		t.Fatalf("fresh paired-secondary PowerDNS install rejected: %v", err)
	}
	if request.Mode != transport.DNSEngineSwitchModeSwitch ||
		request.TargetEngine != transport.DNSEnginePowerDNS ||
		request.SourceEngine != "" || request.SourceEpoch != 0 ||
		request.TargetEpoch != 1 || request.SourceRevision != 0 ||
		request.Topology != transport.DNSTopologyPaired ||
		request.PairRole != transport.DNSPairRoleSecondary ||
		len(request.Zones) != 0 || request.SnapshotBytes != 0 {
		t.Fatalf("fresh install request is not the exact empty paired-secondary switch: %+v", request)
	}

	// The legacy reconfiguration has the same manifest; only provenance and
	// driver differ. The Agent separates them from live pdns.service state.
	legacy := fresh
	legacy.Driver = "pdns-secondary-reconfigure"
	legacy.SourceFixture = "legacy-pdns-secondary"
	legacyRequest, err := requestForScenario(legacy, legacy.Driver)
	if err != nil {
		t.Fatalf("legacy reconfiguration rejected: %v", err)
	}
	if legacyRequest.ManifestQualifier != request.ManifestQualifier {
		t.Fatal("fresh install and legacy reconfiguration no longer share one manifest; revisit the provenance split")
	}
}

func TestFreshPDNSPairSecondaryKeepsExistingRejections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		driver string
		edit   func(*scenario)
	}{
		{"legacy provenance under pdns-switch", "pdns-switch", func(v *scenario) {
			v.SourceFixture = "legacy-pdns-secondary"
		}},
		{"uninitialized provenance under reconfigure driver", "pdns-secondary-reconfigure", func(v *scenario) {
			v.Driver = "pdns-secondary-reconfigure"
		}},
		{"managed-bind provenance for an empty source", "pdns-switch", func(v *scenario) {
			v.SourceFixture = "managed-bind"
		}},
		{"nonzero revision", "pdns-switch", func(v *scenario) { v.SourceRevision = 1 }},
		{"target epoch", "pdns-switch", func(v *scenario) { v.TargetEpoch = 2 }},
		{"source epoch", "pdns-switch", func(v *scenario) { v.SourceEpoch = 1 }},
		{"source engine", "pdns-switch", func(v *scenario) { v.SourceEngine = transport.DNSEnginePowerDNS }},
		{"locally owned zone on a secondary", "pdns-switch", func(v *scenario) {
			v.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
				Domain: "s1-kill.test", DesiredGeneration: 1, ZoneType: "MASTER",
				Records: []transport.ZoneRecord{{
					Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300,
				}},
			}}
		}},
		{"BIND target under pdns-switch", "pdns-switch", func(v *scenario) {
			v.TargetEngine = transport.DNSEngineBIND
		}},
		{"adopt mode", "pdns-switch", func(v *scenario) { v.Mode = transport.DNSEngineSwitchModeAdopt }},
		{"bind driver", "bind", func(v *scenario) { v.Driver = "bind" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := freshPDNSSecondaryScenario()
			tc.edit(&value)
			if _, err := requestForScenario(value, tc.driver); err == nil {
				t.Fatal("trigger accepted a manifest outside the fresh paired-secondary PowerDNS install")
			}
		})
	}
}

// The canonicalizer may already refuse some of these; the driver check must
// refuse them on its own so provenance can never widen the fresh shape.
func TestValidateDriverManifestNarrowsUninitializedPairSecondary(t *testing.T) {
	exact := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode: transport.DNSEngineSwitchModeSwitch, TargetEngine: transport.DNSEnginePowerDNS,
		TargetEpoch: 1, Topology: transport.DNSTopologyPaired,
		PairRole: transport.DNSPairRoleSecondary, LocalIP: "192.0.2.10",
		LocalNS: "ns2.s1-kill.test", PeerIP: "192.0.2.11", PeerNS: "ns1.s1-kill.test",
	}
	if err := validateDriverManifest("pdns-switch", "uninitialized", exact); err != nil {
		t.Fatalf("exact fresh paired-secondary manifest rejected: %v", err)
	}
	for name, edit := range map[string]func(*mutationpayload.DNSEngineSwitchManifestCommitment){
		"zones": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) {
			m.Zones = []transport.DNSEngineSwitchZoneSnapshot{{Domain: "s1-kill.test"}}
		},
		"snapshot bytes": func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.SnapshotBytes = 1 },
		"target epoch":   func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.TargetEpoch = 2 },
		"revision":       func(m *mutationpayload.DNSEngineSwitchManifestCommitment) { m.SourceRevision = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := exact
			edit(&manifest)
			if err := validateDriverManifest("pdns-switch", "uninitialized", manifest); err == nil {
				t.Fatal("uninitialized provenance widened the fresh paired-secondary shape")
			}
		})
	}
	if err := validateDriverManifest("pdns-switch", "legacy-pdns-secondary", exact); err == nil {
		t.Fatal("legacy reconfiguration provenance accepted under pdns-switch")
	}
	if err := validateDriverManifest("pdns-secondary-reconfigure", "uninitialized", exact); err == nil {
		t.Fatal("fresh provenance accepted under the reconfiguration driver")
	}
	if err := validateDriverManifest("pdns-secondary-reconfigure", "legacy-pdns-secondary", exact); err != nil {
		t.Fatalf("legacy reconfiguration rejected: %v", err)
	}
}

func TestFreshPDNSPairedPrimaryIntentShapeIsUnchanged(t *testing.T) {
	primary := freshPDNSSecondaryScenario()
	primary.PairRole = transport.DNSPairRolePrimary
	primary.LocalNS, primary.PeerNS = "ns1.s1-kill.test", "ns2.s1-kill.test"
	primary.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "s1-kill.test", DesiredGeneration: 1, ZoneType: "MASTER",
		Records: []transport.ZoneRecord{{
			Name: "www.s1-kill.test", Type: "A", Content: "192.0.2.10", TTL: 300,
		}},
	}}
	if _, err := requestForScenario(primary, "pdns-switch"); err != nil {
		t.Fatalf("existing fresh paired-primary scenario rejected: %v", err)
	}
}
