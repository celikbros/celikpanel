package binddns

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func boundSnapshot(domain string, generation int64, records []transport.ZoneRecord) ZoneSnapshot {
	return ZoneSnapshot{
		DesiredGeneration: generation,
		Domain:            domain,
		Qualifier:         "dns-zone-sync/v3:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("1", 32),
		MutationOwnerID:   strings.Repeat("2", 32),
		Records:           records,
	}
}

func filesForGeneration(generation Generation) map[string][]byte {
	files := make(map[string][]byte, len(generation.Zones))
	for _, zone := range generation.Zones {
		files["zones/"+zone.FileName] = append([]byte(nil), zone.Data...)
	}
	return files
}

func TestManifestDeterminismAndSecurityBindings(t *testing.T) {
	firstZone := boundSnapshot("alpha.example", 4, testZoneRecords("alpha.example", "192.0.2.1"))
	secondZone := boundSnapshot("beta.example", 9, testZoneRecords("beta.example", "192.0.2.2"))
	first, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 7, Zones: []ZoneSnapshot{secondZone, firstZone},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 7, Zones: []ZoneSnapshot{firstZone, secondZone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !bytes.Equal(first.Config, second.Config) || !bytes.Equal(first.Receipt, second.Receipt) {
		t.Fatal("manifest order changed canonical generation")
	}

	epochChanged, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 8, Zones: []ZoneSnapshot{firstZone, secondZone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if epochChanged.ID == first.ID {
		t.Fatal("engine epoch was not bound into generation ID")
	}
	firstZone.MutationRequestID = strings.Repeat("3", 32)
	bindingChanged, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 7, Zones: []ZoneSnapshot{firstZone, secondZone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bindingChanged.ID == first.ID {
		t.Fatal("request binding was not bound into generation ID")
	}
}

func TestDeleteTombstoneHasNoZoneFile(t *testing.T) {
	deleted := boundSnapshot("gone.example", 11, nil)
	deleted.Delete = true
	generation, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 2, Zones: []ZoneSnapshot{deleted},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(generation.Zones) != 0 || strings.Contains(string(generation.Config), "gone.example") {
		t.Fatalf("delete tombstone leaked into live config: %s", generation.Config)
	}
	zone := generation.ReceiptValue.Zones[0]
	if !zone.Delete || zone.File != "" || zone.RenderedSHA256 != "" || zone.RecordsSHA256 != emptyRecordsSHA256() {
		t.Fatalf("invalid delete receipt: %#v", zone)
	}
}

func TestVerifyTreeAndApplyDeltaPreserveUnchangedZoneExactly(t *testing.T) {
	alpha := boundSnapshot("alpha.example", 1, testZoneRecords("alpha.example", "192.0.2.1"))
	beta := boundSnapshot("beta.example", 1, testZoneRecords("beta.example", "192.0.2.2"))
	initial, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 3, Zones: []ZoneSnapshot{alpha, beta},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := VerifyTree(initial.Receipt, initial.Config, filesForGeneration(initial))
	if err != nil {
		t.Fatal(err)
	}

	alpha.DesiredGeneration = 2
	alpha.MutationRequestID = strings.Repeat("3", 32)
	alpha.Records = testZoneRecords("alpha.example", "192.0.2.10")
	plan, err := ApplyDelta(tree, alpha)
	if err != nil {
		t.Fatal(err)
	}
	next, err := RenderTree("/var/lib/celikpanel/bind", plan)
	if err != nil {
		t.Fatal(err)
	}
	initialFiles := filesForGeneration(initial)
	nextFiles := filesForGeneration(next)
	betaFile := "zones/" + zoneFileName("beta.example")
	alphaFile := "zones/" + zoneFileName("alpha.example")
	if !bytes.Equal(initialFiles[betaFile], nextFiles[betaFile]) {
		t.Fatal("unmodified beta zone was not preserved byte-for-byte")
	}
	if bytes.Equal(initialFiles[alphaFile], nextFiles[alphaFile]) {
		t.Fatal("alpha delta did not change alpha zone")
	}
	if next.ReceiptValue.EngineEpoch != 3 {
		t.Fatal("delta changed engine epoch")
	}

	if _, err := ApplyDelta(tree, ZoneSnapshot{
		DesiredGeneration: 1, Domain: alpha.Domain, Qualifier: alpha.Qualifier,
		MutationRequestID: strings.Repeat("4", 32), MutationOwnerID: alpha.MutationOwnerID,
		Records: testZoneRecords("alpha.example", "192.0.2.99"),
	}); err == nil {
		t.Fatal("reused generation with different binding was accepted")
	}
	stale := alpha
	stale.DesiredGeneration = 0
	if _, err := ApplyDelta(tree, stale); err == nil {
		t.Fatal("stale delta was accepted")
	}
}

func TestVerifiedTreeZoneAccessorIsCanonicalAndDefensive(t *testing.T) {
	live := boundSnapshot("live.example", 3, testZoneRecords("live.example", "192.0.2.3"))
	deleted := boundSnapshot("gone.example", 4, nil)
	deleted.Delete = true
	generation, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 9, Zones: []ZoneSnapshot{deleted, live},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := VerifyTree(generation.Receipt, generation.Config, filesForGeneration(generation))
	if err != nil {
		t.Fatal(err)
	}
	receipt, data, ok := tree.Zone("live.example")
	if !ok || receipt.Domain != "live.example" || len(data) == 0 {
		t.Fatalf("live lookup receipt=%+v bytes=%d ok=%v", receipt, len(data), ok)
	}
	receipt.Domain = "mutated.example"
	data[0] ^= 0xff
	again, againData, ok := tree.Zone("live.example")
	if !ok || again.Domain != "live.example" || bytes.Equal(data, againData) {
		t.Fatal("verified tree accessor leaked mutable receipt or byte aliases")
	}
	tombstone, tombstoneData, ok := tree.Zone("gone.example")
	if !ok || !tombstone.Delete || tombstoneData != nil {
		t.Fatalf("delete lookup receipt=%+v data=%v ok=%v", tombstone, tombstoneData, ok)
	}
	for _, invalid := range []string{"LIVE.example", "live.example.", "../live.example"} {
		if _, _, ok := tree.Zone(invalid); ok {
			t.Fatalf("noncanonical domain %q was accepted", invalid)
		}
	}
	if _, _, ok := tree.Zone("missing.example"); ok {
		t.Fatal("missing zone was reported present")
	}
}

func TestVerifyTreeFailsClosedOnAnyMismatch(t *testing.T) {
	snapshot := boundSnapshot("example.com", 1, testZoneRecords("example.com", "192.0.2.1"))
	generation, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		receipt []byte
		config  []byte
		files   map[string][]byte
	}{
		{"config", generation.Receipt, []byte("changed"), filesForGeneration(generation)},
		{"missing file", generation.Receipt, generation.Config, map[string][]byte{}},
		{"extra file", generation.Receipt, generation.Config, map[string][]byte{
			"zones/" + zoneFileName("example.com"):       generation.Zones[0].Data,
			"zones/" + strings.Repeat("f", 64) + ".zone": []byte("extra"),
		}},
		{"changed file", generation.Receipt, generation.Config, map[string][]byte{
			"zones/" + zoneFileName("example.com"): []byte("changed"),
		}},
		{"noncanonical receipt", append(bytes.TrimSuffix(generation.Receipt, []byte("\n")), ' '), generation.Config, filesForGeneration(generation)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := VerifyTree(test.receipt, test.config, test.files); err == nil {
				t.Fatal("mismatched tree was accepted")
			}
		})
	}
}

func TestZoneFilenameIsFixedLengthDigestBoundToReceiptDomain(t *testing.T) {
	generation, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{
		EngineEpoch: 1,
		Zones: []ZoneSnapshot{
			boundSnapshot("a.example", 1, testZoneRecords("a.example", "192.0.2.1")),
			boundSnapshot(strings.Repeat("x", 60)+".example", 1, testZoneRecords(strings.Repeat("x", 60)+".example", "192.0.2.2")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, zone := range generation.ReceiptValue.Zones {
		name := strings.TrimPrefix(zone.File, "zones/")
		if len(name) != 69 || !strings.HasSuffix(name, ".zone") || !validDigest(strings.TrimSuffix(name, ".zone")) {
			t.Fatalf("zone %s filename %q is not a fixed-length digest", zone.Domain, name)
		}
		if zone.File != "zones/"+zoneFileName(zone.Domain) {
			t.Fatalf("zone %s receipt file %q is not domain-bound", zone.Domain, zone.File)
		}
	}
	tampered := generation.ReceiptValue.Zones[0]
	tampered.File = "zones/" + strings.Repeat("0", 64) + ".zone"
	if err := validateZoneReceipt(tampered); err == nil {
		t.Fatal("receipt accepted a digest filename not derived from its domain")
	}
}

func TestManifestRejectsUnsafePathsAndBindings(t *testing.T) {
	snapshot := boundSnapshot("example.com", 1, testZoneRecords("example.com", "192.0.2.1"))
	for _, root := range []string{"", "/", "relative", "/var/lib/../tmp", `/var/lib/bind"evil`} {
		if _, err := RenderManifest(root, Manifest{EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot}}); err == nil {
			t.Fatalf("unsafe root %q was accepted", root)
		}
	}
	snapshot.Qualifier = "bad\nqualifier"
	if _, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot}}); err == nil {
		t.Fatal("unsafe qualifier was accepted")
	}
	snapshot = boundSnapshot("example.com", 1, testZoneRecords("example.com", "192.0.2.1"))
	snapshot.Qualifier = "dns-zone-sync/v1:sha256:" + strings.Repeat("a", 64)
	if _, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot}}); err == nil {
		t.Fatal("v1 qualifier was accepted as v3 authority")
	}
	snapshot = boundSnapshot("example.com", 1, testZoneRecords("example.com", "192.0.2.1"))
	snapshot.MutationRequestID = strings.Repeat("A", 32)
	if _, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{EngineEpoch: 1, Zones: []ZoneSnapshot{snapshot}}); err == nil {
		t.Fatal("noncanonical mutation request ID was accepted")
	}
	if _, err := RenderManifest("/var/lib/celikpanel/bind", Manifest{EngineEpoch: 0}); err == nil {
		t.Fatal("non-positive engine epoch was accepted")
	}
}
