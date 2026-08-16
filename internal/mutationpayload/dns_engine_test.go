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
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		2, 3, 7, transport.DNSTopologyStandalone, zones,
	)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
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
	var wantBytes int64
	for _, zone := range commitment.Zones {
		encoded, err := MarshalDNSZoneSnapshotRecords(zone.Records)
		if err != nil {
			t.Fatal(err)
		}
		wantBytes += int64(len(encoded))
	}
	if commitment.SnapshotBytes != wantBytes {
		t.Fatalf("snapshot bytes=%d want=%d", commitment.SnapshotBytes, wantBytes)
	}
	changed := append([]transport.DNSEngineSwitchZoneSnapshot(nil), zones...)
	changed[0].Records = []transport.ZoneRecord{{Name: "z.example.test", Type: "A", Content: "192.0.2.5"}}
	mutated, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
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

func TestDNSEngineSwitchSnapshotByteLimitIsFailClosed(t *testing.T) {
	empty, err := MarshalDNSZoneSnapshotRecords(nil)
	if err != nil || string(empty) != "[]" {
		t.Fatalf("empty snapshot encoding=%q err=%v", empty, err)
	}
	if got, err := checkedDNSEngineSnapshotBytes(
		DNSEngineSwitchMaxSnapshotBytes-2, 2,
	); err != nil || got != DNSEngineSwitchMaxSnapshotBytes {
		t.Fatalf("exact cap result=%d err=%v", got, err)
	}
	if _, err := checkedDNSEngineSnapshotBytes(
		DNSEngineSwitchMaxSnapshotBytes-1, 2,
	); err == nil {
		t.Fatal("snapshot larger than 64 MiB was accepted")
	}
}

func TestCanonicalDNSEngineSwitchManifestBindsExplicitMode(t *testing.T) {
	switchManifest, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	adoptManifest, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if switchManifest.Qualifier == adoptManifest.Qualifier {
		t.Fatal("DNS engine qualifier did not bind switch versus adopt mode")
	}
	if _, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeAdopt,
		transport.DNSEngineBIND, transport.DNSEnginePowerDNS, 1, 2, 4,
		transport.DNSTopologyStandalone, nil,
	); err == nil {
		t.Fatal("adopt accepted a resolved source engine")
	}
	if _, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEngineBIND, 0, 1, 4,
		transport.DNSTopologyStandalone, nil,
	); err == nil {
		t.Fatal("adopt accepted a BIND target")
	}
	if _, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1, 2, 4,
		transport.DNSTopologyPaired, nil,
	); err == nil {
		t.Fatal("normal switch accepted paired topology")
	}
	if _, err := CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyPaired, nil,
	); err == nil {
		t.Fatal("paired managed PowerDNS adoption accepted a missing peer tuple")
	}
	paired, err := CanonicalDNSEngineSwitchManifestWithPeer(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyPaired, "192.0.2.53", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatalf("paired managed PowerDNS adoption was rejected: %v", err)
	}
	changed, err := CanonicalDNSEngineSwitchManifestWithPeer(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyPaired, "192.0.2.54", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if paired.Qualifier == changed.Qualifier {
		t.Fatal("paired manifest qualifier did not bind the peer tuple")
	}
	if _, err := CanonicalDNSEngineSwitchManifestWithPeer(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 4,
		transport.DNSTopologyStandalone, "192.0.2.53", "ns2.example.test", nil,
	); err == nil {
		t.Fatal("standalone manifest accepted a peer tuple")
	}
}
