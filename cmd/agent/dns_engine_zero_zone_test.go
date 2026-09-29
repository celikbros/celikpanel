package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Zero-zone first installs (pair2 finding P-A). A server set up through the
// wizard has no zones yet, so every fresh install path must accept an empty
// zone and member set whether a list is nil or empty. Component tests only;
// they are not native evidence.

func freshZeroZoneManifest(
	t *testing.T,
	target transport.DNSEngine,
	topology, role string,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	localIP, localNS, peerIP, peerNS := "", "", "", ""
	if topology == transport.DNSTopologyPaired {
		localIP, localNS, peerIP, peerNS = "192.0.2.10", "ns1.example.test", "192.0.2.20", "ns2.example.test"
		if role == transport.DNSPairRoleSecondary {
			localNS, peerNS = "ns2.example.test", "ns1.example.test"
		}
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", target, 0, 1, 0,
		topology, role, localIP, localNS, peerIP, peerNS, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Zones) != 0 {
		t.Fatalf("zero-zone manifest has zones: %+v", manifest.Zones)
	}
	return manifest
}

func stubZeroMemberPeerCatalog(t *testing.T) {
	t.Helper()
	previous := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(_ context.Context, _, _ string) (dnsCatalogAXFRResult, error) {
		// The AXFR reader returns nil Members for an empty catalog.
		return dnsCatalogAXFRResult{Serial: 7}, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })
}

// The fresh PowerDNS candidate is built and then verified against the same
// manifest. The paired primary is the pair2 t3 defect: its producer has no
// members and the manifest has no zones.
func TestZeroZonePDNSFreshCandidateVerifiesForEveryTopology(t *testing.T) {
	stubZeroMemberPeerCatalog(t)
	for _, tc := range []struct {
		name, topology, role string
		serial               uint32
	}{
		{"standalone", transport.DNSTopologyStandalone, "", 0},
		{"paired primary", transport.DNSTopologyPaired, transport.DNSPairRolePrimary, 1},
		{"paired secondary", transport.DNSTopologyPaired, transport.DNSPairRoleSecondary, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := freshZeroZoneManifest(t, transport.DNSEnginePowerDNS, tc.topology, tc.role)
			path := filepath.Join(t.TempDir(), "candidate.sqlite3")
			binding := testPDNSEngineBinding()
			if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(
				context.Background(), path, manifest, binding, tc.serial,
			); err != nil {
				t.Fatal(err)
			}
			if err := verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial(
				context.Background(), path, manifest, binding, tc.serial,
			); err != nil {
				t.Fatalf("zero-zone %s candidate failed its own verification: %v", tc.name, err)
			}
		})
	}
}

// The producer membership comparison: an empty member set equals an empty
// member set whether the expected list is nil or empty; order, multiplicity
// and exact names stay strict.
func TestPDNSProducerMembershipEmptyAndExact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	catalog, err := reconcilePDNSBINDCatalogTx(context.Background(), tx, true, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]string{"nil": nil, "empty": {}} {
		if err := verifyPDNSProducerMembershipTx(context.Background(), tx, "192.0.2.10", expected); err != nil {
			t.Fatalf("%s expected members refused an empty producer: %v", name, err)
		}
	}
	if err := verifyPDNSProducerMembershipTx(context.Background(), tx, "192.0.2.10", []string{"one.test"}); err == nil {
		t.Fatal("an absent expected member was accepted")
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO domains(name, type, catalog) VALUES('one.test', 'MASTER', ?)`, catalog.Domain); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSProducerMembershipTx(context.Background(), tx, "192.0.2.10", []string{"one.test"}); err != nil {
		t.Fatalf("exact member refused: %v", err)
	}
	for name, expected := range map[string][]string{
		"none expected, one present": nil,
		"duplicate expected":         {"one.test", "one.test"},
		"different spelling":         {"One.test"},
		"extra expected":             {"one.test", "two.test"},
	} {
		if err := verifyPDNSProducerMembershipTx(context.Background(), tx, "192.0.2.10", expected); err == nil {
			t.Fatalf("%s: membership accepted", name)
		}
	}
}

// Zone sync: the first zone added after a zero-zone paired PowerDNS primary
// install is applied and becomes the only catalog member.
func TestZeroZonePDNSPrimaryFirstZoneAdd(t *testing.T) {
	prepareManagedPDNSCatalogConfig(t)
	config, err := dnsDirectionalClusterConfig(transport.DNSPairRolePrimary, "192.0.2.10", "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsClusterConf, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := freshZeroZoneManifest(t, transport.DNSEnginePowerDNS, transport.DNSTopologyPaired, transport.DNSPairRolePrimary)
	path := filepath.Join(t.TempDir(), "zero-zone-primary.sqlite3")
	binding := testPDNSEngineBinding()
	const catalogSerial = uint32(1)
	if err := buildPDNSSwitchCandidateWithPrimaryCatalogSerial(context.Background(), path, manifest, binding, catalogSerial); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_PDNS_DB", path)
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: manifest.TargetEpoch,
		PairRole: manifest.PairRole, PairLocalIP: manifest.LocalIP, PairPeerIP: manifest.PeerIP,
		PrimaryCatalogSerial: catalogSerial, SourceRevision: manifest.SourceRevision,
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, manifest.TargetEpoch, 1, "first.test", false, "MASTER",
		testPDNSEngineRecords("first.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneDatabaseForState(context.Background(), path, commitment, binding, state); err != nil {
		t.Fatalf("first zone after a zero-zone install: %v", err)
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := verifyPDNSProducerMembershipTx(context.Background(), tx, manifest.LocalIP, []string{"first.test"}); err != nil {
		t.Fatalf("first zone is not the only catalog member: %v", err)
	}
}

// BIND: the zero-zone tree renders for every topology, and the paired
// primary's catalog handoff proof accepts an empty durable and live member
// set whether a list is nil or empty.
func TestZeroZoneBINDFreshTreeAndCatalogHandoff(t *testing.T) {
	for _, tc := range []struct {
		name, topology, role string
		serial               uint32
	}{
		{"standalone", transport.DNSTopologyStandalone, "", 0},
		{"paired primary", transport.DNSTopologyPaired, transport.DNSPairRolePrimary, 1},
		{"paired secondary", transport.DNSTopologyPaired, transport.DNSPairRoleSecondary, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := freshZeroZoneManifest(t, transport.DNSEngineBIND, tc.topology, tc.role)
			plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(manifest, testPDNSEngineBinding(), tc.serial)
			if err != nil {
				t.Fatal(err)
			}
			generation, err := binddns.RenderTree("/var/cache/bind/celikpanel", plan)
			if err != nil {
				t.Fatalf("zero-zone %s BIND tree: %v", tc.name, err)
			}
			if len(generation.ReceiptValue.Zones) != 0 {
				t.Fatalf("zero-zone receipt zones=%+v", generation.ReceiptValue.Zones)
			}
			if members := primaryCatalogManifestMembers(manifest); members == nil || len(members) != 0 {
				t.Fatalf("manifest members=%#v", members)
			}
		})
	}
	manifest := freshZeroZoneManifest(t, transport.DNSEngineBIND, transport.DNSTopologyPaired, transport.DNSPairRolePrimary)
	domain, err := binddns.CatalogDomain(manifest.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	soa := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
		return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{1}}, nil
	}
	axfr := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: 1}, nil
	}
	for name, members := range map[string][]string{"nil": nil, "empty": {}} {
		evidence := dnsPrimaryCatalogEvidence{LocalIP: manifest.LocalIP, PeerIP: manifest.PeerIP, Domain: domain, Serial: 1,
			Members: members, MemberSerials: []uint32{}}
		if err := verifyPrimaryCatalogHandoffEvidenceAt(context.Background(), evidence, manifest, 1, soa, axfr); err != nil {
			t.Fatalf("%s: zero-member catalog handoff refused: %v", name, err)
		}
	}
}

// Recovery input: a zero-zone switch journal survives its durable encoding
// and reproduces exactly the canonical manifest the recovery paths compare
// (dns_engine_rpc.go, switch_journal.go).
func TestZeroZoneSwitchJournalReproducesCanonicalManifest(t *testing.T) {
	policy := dnsengineartifact.JournalPolicy{
		StatePath: "/var/lib/celikpanel-agent-private/dns-engine-state.json", RequireOwner: true,
		PDNSMainPath: "/etc/powerdns/pdns.conf", PDNSManagedPath: "/etc/powerdns/pdns.d/celikpanel.conf",
		PDNSClusterPath: "/etc/powerdns/pdns.d/celikpanel-cluster.conf", PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "switch-journal", "alpha81-pdns-switch.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, topology, role string }{
		{"standalone", transport.DNSTopologyStandalone, ""},
		{"paired secondary", transport.DNSTopologyPaired, transport.DNSPairRoleSecondary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, err := policy.DecodeSwitchJournal(raw)
			if err != nil {
				t.Fatal(err)
			}
			manifest := freshZeroZoneManifest(t, transport.DNSEnginePowerDNS, tc.topology, tc.role)
			base.SourceEngine, base.SourceEpoch, base.TargetEpoch, base.SourceRevision = "", 0, 1, 0
			base.Topology, base.PairRole = manifest.Topology, manifest.PairRole
			base.LocalIP, base.LocalNS, base.PeerIP, base.PeerNS = manifest.LocalIP, manifest.LocalNS, manifest.PeerIP, manifest.PeerNS
			base.ManifestQualifier, base.SnapshotBytes = manifest.Qualifier, manifest.SnapshotBytes
			base.PrimaryCatalogSerial = 0
			base.StateBefore = dnsengineartifact.FileSnapshot{Path: policy.StatePath}
			base.SourceUnitsBefore = nil
			for _, zones := range [][]transport.DNSEngineSwitchZoneSnapshot{nil, {}} {
				journal := base
				journal.Zones = zones
				encoded, err := policy.EncodeSwitchJournal(journal)
				if err != nil {
					t.Fatalf("zones=%#v: encode: %v", zones, err)
				}
				decoded, err := policy.DecodeSwitchJournal(encoded)
				if err != nil {
					t.Fatalf("zones=%#v: decode: %v", zones, err)
				}
				rebuilt, err := dnsengineartifact.SwitchJournalManifest(decoded)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(rebuilt, manifest) {
					t.Fatalf("zones=%#v: rebuilt manifest differs:\n%+v\n%+v", zones, rebuilt, manifest)
				}
			}
		})
	}
}
