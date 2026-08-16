package binddns

import (
	"strings"
	"testing"
)

const pairingTestRoot = "/var/cache/bind/celikpanel"

func testPairing(role string) *Pairing {
	return &Pairing{
		Role: role, LocalIP: "192.0.2.10", LocalNS: "ns1.example.test",
		PeerIP: "192.0.2.20", PeerNS: "ns2.example.test",
	}
}

func verifyPairingGeneration(t *testing.T, generation Generation) VerifiedTree {
	t.Helper()
	files := make(map[string][]byte, len(generation.Zones)+1)
	for _, zone := range generation.Zones {
		files["zones/"+zone.FileName] = zone.Data
	}
	if generation.Catalog != nil && generation.ReceiptValue.Pairing != nil {
		files[generation.ReceiptValue.Pairing.CatalogFile] = generation.Catalog.Data
	}
	tree, err := VerifyTree(generation.Receipt, generation.Config, files)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestPrimaryPairingRendersCatalogAndTransferPolicy(t *testing.T) {
	generation, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 4, Pairing: testPairing(PairRolePrimary),
		Zones: []ZoneSnapshot{
			boundSnapshot("example.test", 9, testZoneRecords("example.test", "192.0.2.30")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if generation.ReceiptValue.Schema != receiptSchemaV2 ||
		generation.ReceiptValue.Pairing == nil || generation.Catalog == nil {
		t.Fatalf("paired receipt/catalog missing: %#v", generation.ReceiptValue)
	}
	config := string(generation.Config)
	for _, want := range []string{
		"allow-transfer { 192.0.2.20; };",
		"also-notify { 192.0.2.20; };",
		"zone \"catalog-c000020a.celikpanel.invalid\"",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("primary config missing %q:\n%s", want, config)
		}
	}
	catalog := string(generation.Catalog.Data)
	if !strings.Contains(catalog, "version IN TXT \"2\"") ||
		!strings.Contains(catalog, " IN PTR example.test.") {
		t.Fatalf("catalog does not contain version/member records:\n%s", catalog)
	}
	files := map[string][]byte{
		generation.ReceiptValue.Zones[0].File:       generation.Zones[0].Data,
		generation.ReceiptValue.Pairing.CatalogFile: generation.Catalog.Data,
	}
	if _, err := VerifyTree(generation.Receipt, generation.Config, files); err != nil {
		t.Fatalf("verify primary tree: %v", err)
	}
}

func TestSecondaryPairingRendersCatalogSubscriptionAndRejectsOwnedZones(t *testing.T) {
	generation, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 5, Pairing: testPairing(PairRoleSecondary),
	})
	if err != nil {
		t.Fatal(err)
	}
	if generation.Catalog != nil || len(generation.Zones) != 0 {
		t.Fatalf("secondary rendered local data: %#v", generation)
	}
	config := string(generation.Config)
	for _, want := range []string{
		"catalog-zones {",
		"zone \"catalog-c0000214.celikpanel.invalid\"",
		"default-primaries { 192.0.2.20; };",
		"in-memory yes;",
	} {
		if !strings.Contains(config, want) {
			t.Errorf("secondary config missing %q:\n%s", want, config)
		}
	}
	if _, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 5, Pairing: testPairing(PairRoleSecondary),
		Zones: []ZoneSnapshot{
			boundSnapshot("example.test", 1, testZoneRecords("example.test", "192.0.2.30")),
		},
	}); err == nil {
		t.Fatal("secondary accepted a panel-owned primary zone")
	}
}

func TestCatalogZoneRecordsAreEngineNeutralAndCanonical(t *testing.T) {
	domain, records, err := CatalogZoneRecords(
		"192.0.2.10", 17, []string{"z.example.test", "a.example.test"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if domain != "catalog-c000020a.celikpanel.invalid" || len(records) != 5 ||
		records[0].Type != "SOA" || records[2].Content != "\"2\"" ||
		records[3].Content != "a.example.test" ||
		records[4].Content != "z.example.test" {
		t.Fatalf("domain=%q records=%#v", domain, records)
	}
	if _, _, err := CatalogZoneRecords(
		"192.0.2.10", 17, []string{"a.example.test", "a.example.test"},
	); err == nil {
		t.Fatal("duplicate catalog member was accepted")
	}
}

func TestPrimaryDeltaAdvancesCatalogSerialAndKeepsLegacyStandaloneV1(t *testing.T) {
	primary, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 2, Pairing: testPairing(PairRolePrimary),
		Zones: []ZoneSnapshot{
			boundSnapshot("example.test", 1, testZoneRecords("example.test", "192.0.2.30")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree := verifyPairingGeneration(t, primary)
	delta := boundSnapshot("example.test", 2, testZoneRecords("example.test", "192.0.2.31"))
	plan, err := ApplyDelta(tree, delta)
	if err != nil {
		t.Fatal(err)
	}
	next, err := RenderTree(pairingTestRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	if next.ReceiptValue.Pairing.CatalogSerial != 2 || next.ID == primary.ID {
		t.Fatalf("catalog serial/generation did not advance: %#v", next.ReceiptValue.Pairing)
	}
	standalone, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if standalone.ReceiptValue.Schema != receiptSchemaV1 || standalone.ReceiptValue.Pairing != nil {
		t.Fatalf("standalone compatibility changed: %#v", standalone.ReceiptValue)
	}
}
