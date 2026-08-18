package binddns

import (
	"reflect"
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
	if strings.Contains(catalog, " IN APL ") {
		t.Fatalf("managed producer catalog contains a custom transfer ACL:\n%s", catalog)
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
	for _, record := range records {
		if record.Type == "APL" {
			t.Fatalf("engine-neutral catalog emitted a custom transfer ACL: %#v", records)
		}
	}
	if _, _, err := CatalogZoneRecords(
		"192.0.2.10", 17, []string{"a.example.test", "a.example.test"},
	); err == nil {
		t.Fatal("duplicate catalog member was accepted")
	}
}

func TestPrimaryDeltaAdvancesCatalogSerialOnlyForMembershipChanges(t *testing.T) {
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
	if next.ReceiptValue.Pairing.CatalogSerial != 1 || next.ID == primary.ID {
		t.Fatalf("record-only update changed catalog membership serial or not generation: %#v", next.ReceiptValue.Pairing)
	}

	deleteDelta := boundSnapshot("example.test", 3, nil)
	deleteDelta.Delete = true
	deletedPlan, err := ApplyDelta(verifyPairingGeneration(t, next), deleteDelta)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := RenderTree(pairingTestRoot, deletedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ReceiptValue.Pairing.CatalogSerial != 2 {
		t.Fatalf("member deletion did not advance catalog serial: %#v", deleted.ReceiptValue.Pairing)
	}

	readdedPlan, err := ApplyDelta(
		verifyPairingGeneration(t, deleted),
		boundSnapshot("example.test", 4, testZoneRecords("example.test", "192.0.2.32")),
	)
	if err != nil {
		t.Fatal(err)
	}
	readded, err := RenderTree(pairingTestRoot, readdedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if readded.ReceiptValue.Pairing.CatalogSerial != 3 {
		t.Fatalf("member re-add did not advance catalog serial: %#v", readded.ReceiptValue.Pairing)
	}
	standalone, err := RenderManifest(pairingTestRoot, Manifest{EngineEpoch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if standalone.ReceiptValue.Schema != receiptSchemaV1 || standalone.ReceiptValue.Pairing != nil {
		t.Fatalf("standalone compatibility changed: %#v", standalone.ReceiptValue)
	}
}

func TestSecondaryDeltaRejectsCreateUpdateAndDelete(t *testing.T) {
	generation, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 9, Pairing: testPairing(PairRoleSecondary),
	})
	if err != nil {
		t.Fatal(err)
	}
	tree := verifyPairingGeneration(t, generation)
	if err := validateReceipt(tree.receipt); err != nil {
		t.Fatalf("secondary fixture receipt invalid: %v", err)
	}
	before := tree.CurrentReceipt()
	for _, test := range []struct {
		name  string
		delta ZoneSnapshot
	}{
		{name: "create", delta: boundSnapshot("new.example.test", 1, testZoneRecords("new.example.test", "192.0.2.40"))},
		{name: "update", delta: boundSnapshot("existing.example.test", 2, testZoneRecords("existing.example.test", "192.0.2.41"))},
		{name: "delete", delta: func() ZoneSnapshot {
			delta := boundSnapshot("existing.example.test", 3, nil)
			delta.Delete = true
			return delta
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ApplyDelta(tree, test.delta); err == nil ||
				err.Error() != "BIND secondary cannot mutate a panel-owned zone" {
				t.Fatalf("secondary delta error=%v", err)
			}
			if after := tree.CurrentReceipt(); !reflect.DeepEqual(before, after) {
				t.Fatalf("rejected delta mutated source receipt: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPrimaryCatalogSerialHandoffAndExhaustion(t *testing.T) {
	seeded, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 11, Pairing: testPairing(PairRolePrimary),
		PrimaryCatalogSerial: 41,
		Zones: []ZoneSnapshot{
			boundSnapshot("example.test", 1, testZoneRecords("example.test", "192.0.2.30")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if seeded.ReceiptValue.Pairing == nil ||
		seeded.ReceiptValue.Pairing.CatalogSerial != 41 {
		t.Fatalf("seeded pairing receipt=%+v", seeded.ReceiptValue.Pairing)
	}

	maximum, err := RenderManifest(pairingTestRoot, Manifest{
		EngineEpoch: 12, Pairing: testPairing(PairRolePrimary),
		PrimaryCatalogSerial: ^uint32(0),
		Zones: []ZoneSnapshot{
			boundSnapshot("example.test", 1, testZoneRecords("example.test", "192.0.2.30")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tree := verifyPairingGeneration(t, maximum)
	recordPlan, err := ApplyDelta(
		tree,
		boundSnapshot("example.test", 2, testZoneRecords("example.test", "192.0.2.31")),
	)
	if err != nil {
		t.Fatalf("record-only update at maximum catalog serial failed: %v", err)
	}
	recordUpdated, err := RenderTree(pairingTestRoot, recordPlan)
	if err != nil {
		t.Fatal(err)
	}
	if recordUpdated.ReceiptValue.Pairing.CatalogSerial != ^uint32(0) {
		t.Fatal("record-only update changed maximum catalog serial")
	}
	tree = verifyPairingGeneration(t, recordUpdated)
	if _, err := ApplyDelta(
		tree,
		boundSnapshot("new.test", 1, testZoneRecords("new.test", "192.0.2.31")),
	); err == nil {
		t.Fatal("catalog membership change wrapped an exhausted serial")
	}
	if tree.CurrentReceipt().Pairing.CatalogSerial != ^uint32(0) {
		t.Fatal("failed membership delta mutated the verified source tree")
	}
	changedPairing := testPairing(PairRolePrimary)
	changedPairing.PeerNS = "ns3.example.test"
	if _, err := ReconfigurePairing(tree, changedPairing); err == nil {
		t.Fatal("pair reconfiguration wrapped an exhausted catalog serial")
	}

	if _, err := NewTreePlan(Manifest{
		EngineEpoch: 1, PrimaryCatalogSerial: 7,
	}); err == nil {
		t.Fatal("standalone BIND accepted a primary catalog serial")
	}
	if _, err := NewTreePlan(Manifest{
		EngineEpoch: 1, Pairing: testPairing(PairRoleSecondary),
		PrimaryCatalogSerial: 7,
	}); err == nil {
		t.Fatal("secondary BIND accepted a primary catalog serial")
	}
}
