package mutationpayload

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestCanonicalDNSZoneSyncV3BindsEngineEpochAndFullSnapshot(t *testing.T) {
	records := []transport.ZoneRecord{
		{Name: "www.example.test", Type: "A", Content: "192.0.2.2", TTL: 300},
		{Name: "example.test", Type: "MX", Content: "mail.example.test", TTL: 3600, Prio: 10},
	}
	bind, err := CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 3, 9, "EXAMPLE.TEST.", false, " native ", records,
	)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 3, 9, "example.test", false, "NATIVE",
		[]transport.ZoneRecord{records[1], records[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bind, reordered) {
		t.Fatalf("equivalent V3 snapshots differ:\n%#v\n%#v", bind, reordered)
	}
	if !ValidDNSZoneSyncV3Qualifier(bind.Qualifier) {
		t.Fatalf("invalid V3 qualifier: %q", bind.Qualifier)
	}
	pdns, _ := CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 3, 9, "example.test", false, "NATIVE", records,
	)
	nextEpoch, _ := CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 4, 9, "example.test", false, "NATIVE", records,
	)
	if bind.Qualifier == pdns.Qualifier || bind.Qualifier == nextEpoch.Qualifier {
		t.Fatal("V3 qualifier did not bind engine and epoch")
	}
	legacy, err := CanonicalDNSZoneSync(9, "example.test", false, "NATIVE", records)
	if err != nil || !strings.HasPrefix(legacy.Qualifier, "dns-zone-sync/v1:sha256:") {
		t.Fatalf("legacy qualifier changed: %#v, %v", legacy, err)
	}
}

func TestCanonicalDNSZoneSyncV3RejectsInvalidEngineIdentity(t *testing.T) {
	records := []transport.ZoneRecord{{Name: "example.test", Type: "A", Content: "192.0.2.2"}}
	if _, err := CanonicalDNSZoneSyncV3("BIND", 1, 1, "example.test", false, "NATIVE", records); err == nil {
		t.Fatal("noncanonical engine accepted")
	}
	if _, err := CanonicalDNSZoneSyncV3(transport.DNSEngineBIND, 0, 1, "example.test", false, "NATIVE", records); err == nil {
		t.Fatal("zero engine epoch accepted")
	}
}

func TestCanonicalDNSEngineSwitchManifestIsStableAndTransitive(t *testing.T) {
	zones := []transport.DNSEngineSwitchZoneSnapshot{
		{
			Domain: "z.example.test", DesiredGeneration: 4, ZoneType: "NATIVE",
			Records: []transport.ZoneRecord{{Name: "z.example.test", Type: "A", Content: "192.0.2.4"}},
		},
		{
			Domain: "a.example.test", DesiredGeneration: 8, Delete: true,
			ZoneType: "MASTER", Records: []transport.ZoneRecord{},
		},
	}
	commitment, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		2, 3, 7, transport.DNSTopologyStandalone, zones,
	)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		2, 3, 7, transport.DNSTopologyStandalone,
		[]transport.DNSEngineSwitchZoneSnapshot{zones[1], zones[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(commitment, reordered) {
		t.Fatalf("equivalent manifests differ:\n%#v\n%#v", commitment, reordered)
	}
	if len(commitment.Zones) != 2 || commitment.Zones[0].Ordinal != 0 ||
		commitment.Zones[0].Domain != "a.example.test" || commitment.Zones[1].Ordinal != 1 {
		t.Fatalf("manifest was not canonically ordered: %#v", commitment.Zones)
	}
	if !ValidDNSEngineSwitchQualifier(commitment.Qualifier) {
		t.Fatalf("invalid switch qualifier: %q", commitment.Qualifier)
	}
	changed := append([]transport.DNSEngineSwitchZoneSnapshot(nil), zones...)
	changed[0].Records = []transport.ZoneRecord{{Name: "z.example.test", Type: "A", Content: "192.0.2.5"}}
	mutated, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		2, 3, 7, transport.DNSTopologyStandalone, changed,
	)
	if err != nil {
		t.Fatal(err)
	}
	if commitment.Qualifier == mutated.Qualifier {
		t.Fatal("switch manifest did not transitively bind zone records")
	}
}
