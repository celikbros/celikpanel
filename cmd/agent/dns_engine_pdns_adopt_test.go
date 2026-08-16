package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	testPDNSAdoptionPeerIP = "192.0.2.53"
	testPDNSAdoptionPeerNS = "ns2.example.test"
)

func testPDNSAdoptionManifest(
	t *testing.T,
	topology string,
	domain string,
	records []transport.ZoneRecord,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, 9, domain, false, "NATIVE", records,
	)
	if err != nil {
		t.Fatal(err)
	}
	peerIP, peerNS := "", ""
	if topology == transport.DNSTopologyPaired {
		peerIP, peerNS = testPDNSAdoptionPeerIP, testPDNSAdoptionPeerNS
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPeer(
		transport.DNSEngineSwitchModeAdopt,
		"", transport.DNSEnginePowerDNS, 0, 1, 17, topology,
		peerIP, peerNS,
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: domain, DesiredGeneration: 9, ZoneType: "NATIVE",
			Records: zone.Records, ZoneQualifier: zone.Qualifier,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func createPDNSAdoptionDatabase(
	t *testing.T,
	path, domain string,
	records []transport.ZoneRecord,
	paired, signed bool,
) {
	t.Helper()
	db, err := initializePDNSEngineDB(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO domains (name, type) VALUES (?, 'NATIVE')`, domain)
	if err != nil {
		t.Fatal(err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		disabled := 0
		if record.Disabled {
			disabled = 1
		}
		if _, err := db.Exec(`
			INSERT INTO records
			(domain_id, name, type, content, ttl, prio, disabled, auth)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1)
		`, domainID, record.Name, record.Type, record.Content,
			record.TTL, record.Prio, disabled); err != nil {
			t.Fatal(err)
		}
	}
	if signed {
		for _, statement := range []string{
			`INSERT INTO domainmetadata (domain_id, kind, content) VALUES (?, 'PRESIGNED', '1')`,
			`INSERT INTO cryptokeys (domain_id, flags, active, published, content) VALUES (?, 257, 1, 1, 'private-key-material')`,
		} {
			if _, err := db.Exec(statement, domainID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if paired {
		if _, err := db.Exec(`
			INSERT INTO domains (name, type, master, account)
			VALUES ('peer-owned.test', 'SLAVE', ?, 'celikpanel')
		`, testPDNSAdoptionPeerIP); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO supermasters (ip, nameserver, account)
			VALUES (?, ?, 'celikpanel')
		`, testPDNSAdoptionPeerIP, testPDNSAdoptionPeerNS); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPDNSAdoptionPreservesSignedPairedDatabaseBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	domain := "secure.test"
	records := testPDNSEngineRecords(domain)
	createPDNSAdoptionDatabase(t, path, domain, records, true, true)
	manifest := testPDNSAdoptionManifest(
		t, transport.DNSTopologyPaired, domain, records,
	)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSAdoptionDatabase(context.Background(), path, manifest); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only adoption changed the signed paired PowerDNS database")
	}
	db, err := openPDNSEngineDB(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"cryptokeys", "domainmetadata"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want preserved", table, count)
		}
	}
}

func TestVerifyPDNSAdoptionRejectsExtraStandaloneAndTamperedZone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	domain := "example.test"
	records := testPDNSEngineRecords(domain)
	createPDNSAdoptionDatabase(t, path, domain, records, true, false)
	standalone := testPDNSAdoptionManifest(
		t, transport.DNSTopologyStandalone, domain, records,
	)
	if err := verifyPDNSAdoptionDatabase(context.Background(), path, standalone); err == nil {
		t.Fatal("standalone adoption accepted an unowned extra zone")
	}
	paired := testPDNSAdoptionManifest(t, transport.DNSTopologyPaired, domain, records)
	db, err := openPDNSEngineDB(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE records SET content = '192.0.2.99' WHERE type = 'A'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSAdoptionDatabase(context.Background(), path, paired); err == nil {
		t.Fatal("adoption accepted runtime records that differ from the ledger")
	}
}

func TestVerifyPDNSAdoptionRejectsMismatchedPairedOwnership(t *testing.T) {
	domain := "example.test"
	records := testPDNSEngineRecords(domain)
	manifest := testPDNSAdoptionManifest(
		t, transport.DNSTopologyPaired, domain, records,
	)
	tests := []struct {
		name   string
		mutate string
	}{
		{name: "secondary master", mutate: `
			UPDATE domains SET master = '192.0.2.54'
			WHERE name = 'peer-owned.test'`},
		{name: "secondary owner", mutate: `
			UPDATE domains SET account = 'manual'
			WHERE name = 'peer-owned.test'`},
		{name: "autoprimary ip", mutate: `
			UPDATE supermasters SET ip = '192.0.2.54'`},
		{name: "autoprimary nameserver", mutate: `
			UPDATE supermasters SET nameserver = 'other.example.test'`},
		{name: "autoprimary owner", mutate: `
			UPDATE supermasters SET account = 'manual'`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pdns.sqlite3")
			createPDNSAdoptionDatabase(t, path, domain, records, true, false)
			db, err := openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(test.mutate); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := verifyPDNSAdoptionDatabase(
				context.Background(), path, manifest,
			); err == nil {
				t.Fatal("paired adoption accepted mismatched peer ownership")
			}
		})
	}
}

func TestPDNSAdoptionUnitEvidenceRejectsAnotherRunningEngine(t *testing.T) {
	before := []dnsUnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	if err := validatePDNSAdoptionUnitEvidence(before); err != nil {
		t.Fatal(err)
	}
	after := append([]dnsUnitSnapshot(nil), before...)
	if !exactDNSUnitSnapshotSet(before, after) {
		t.Fatal("identical unit snapshots were not exact")
	}
	after[1].ActiveState = "active"
	if err := validatePDNSAdoptionUnitEvidence(after); err == nil {
		t.Fatal("adoption accepted BIND running beside PowerDNS")
	}
	if exactDNSUnitSnapshotSet(before, after) {
		t.Fatal("unit state mutation was not detected")
	}
}

func TestPDNSAdoptionConfigEvidenceIsByteAndModeExact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.conf")
	if err := os.WriteFile(path, []byte("launch=gsqlite3\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	_, owner, err := readDNSFileForSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureDNSFileSnapshotPreserveForOwner(
		path, false, owner.UID, owner.GID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyDNSFileSnapshotsExactForOwner(
		[]dnsFileSnapshot{snapshot}, owner.UID, owner.GID,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("launch=bind\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := verifyDNSFileSnapshotsExactForOwner(
		[]dnsFileSnapshot{snapshot}, owner.UID, owner.GID,
	); err == nil {
		t.Fatal("adoption config evidence missed a byte change")
	}
}

func TestVerifyPDNSAdoptionTopologyIsExact(t *testing.T) {
	previous := dnsClusterConf
	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	t.Cleanup(func() { dnsClusterConf = previous })
	standalone := testPDNSAdoptionManifest(
		t, transport.DNSTopologyStandalone, "example.test",
		testPDNSEngineRecords("example.test"),
	)
	paired := testPDNSAdoptionManifest(
		t, transport.DNSTopologyPaired, "example.test",
		testPDNSEngineRecords("example.test"),
	)
	if err := verifyPDNSAdoptionTopology(standalone); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSAdoptionTopology(paired); err == nil {
		t.Fatal("paired adoption accepted an absent cluster config")
	}
	expected := dnsClusterConfig(&DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: testPDNSAdoptionPeerIP,
		PeerNS: testPDNSAdoptionPeerNS,
	})
	if err := os.WriteFile(dnsClusterConf, []byte(expected), 0o640); err != nil {
		t.Fatal(err)
	}
	_, owner, err := readDNSFileForSnapshot(dnsClusterConf)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSAdoptionTopologyForOwner(
		paired, owner.UID, owner.GID,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSAdoptionTopologyForOwner(
		standalone, owner.UID, owner.GID,
	); err == nil {
		t.Fatal("standalone adoption accepted an active paired config")
	}
	tampered := paired
	tampered.PeerIP = "192.0.2.54"
	if err := verifyPDNSAdoptionTopologyForOwner(
		tampered, owner.UID, owner.GID,
	); err == nil {
		t.Fatal("paired adoption accepted a different managed peer")
	}
}

func TestPDNSAdoptionTransactionBindingAcceptsOnlyExactSelfJournal(t *testing.T) {
	manifest := testPDNSAdoptionManifest(
		t, transport.DNSTopologyStandalone, "example.test",
		testPDNSEngineRecords("example.test"),
	)
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode: manifest.Mode, MutationRequestID: strings.Repeat("a", 32),
		MutationOwnerID:   strings.Repeat("b", 32),
		ManifestQualifier: manifest.Qualifier,
		TargetEngine:      manifest.TargetEngine, TargetEpoch: manifest.TargetEpoch,
		SourceRevision: manifest.SourceRevision, Topology: manifest.Topology,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
	}
	exactState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine: manifest.TargetEngine, EngineEpoch: manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, dnsEngineSwitchJournal{}, false,
		dnsEngineStateReceipt{}, false, pdnsAdoptionEvidencePreflight,
	); err != nil {
		t.Fatalf("clean adoption preflight rejected: %v", err)
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, exactState, true, pdnsAdoptionEvidenceTarget,
	); err != nil {
		t.Fatalf("exact attached adoption journal rejected: %v", err)
	}
	different := journal
	different.Phase = dnsSwitchPhaseTargetVerified
	if err := validatePDNSAdoptionTransactionBinding(
		journal, different, true, exactState, true, pdnsAdoptionEvidenceTarget,
	); err == nil {
		t.Fatal("different adoption journal was accepted as the current transaction")
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, dnsEngineStateReceipt{}, false,
		pdnsAdoptionEvidenceTarget,
	); err == nil {
		t.Fatal("adoption target proof accepted an absent state receipt")
	}
	journal.Phase = dnsSwitchPhaseRollingBack
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, exactState, true, pdnsAdoptionEvidenceTarget,
	); err == nil {
		t.Fatal("recovery rolled forward after the rollback decision was durable")
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, dnsEngineStateReceipt{}, false,
		pdnsAdoptionEvidenceRollback,
	); err != nil {
		t.Fatalf("exact rollback after state restoration rejected: %v", err)
	}
	if err := validatePDNSAdoptionTransactionBinding(
		journal, journal, true, exactState, true, pdnsAdoptionEvidenceRollback,
	); err == nil {
		t.Fatal("rollback accepted the target receipt instead of restored empty source")
	}
}

func TestPDNSAdoptionRollbackNeverOverwritesDifferentJournal(t *testing.T) {
	expected := dnsEngineSwitchJournal{
		Mode:              transport.DNSEngineSwitchModeAdopt,
		Phase:             dnsSwitchPhaseIntent,
		MutationRequestID: strings.Repeat("c", 32),
		MutationOwnerID:   strings.Repeat("d", 32),
		TargetEngine:      transport.DNSEnginePowerDNS,
	}
	foreign := expected
	foreign.MutationOwnerID = strings.Repeat("e", 32)
	writes := 0
	write := func(dnsEngineSwitchJournal) error {
		writes++
		return nil
	}
	if _, err := transitionPDNSAdoptionJournalToRollback(
		expected,
		func() (dnsEngineSwitchJournal, bool, error) { return foreign, true, nil },
		write,
	); err == nil {
		t.Fatal("rollback accepted a different current adoption journal")
	}
	if writes != 0 || foreign.MutationOwnerID != strings.Repeat("e", 32) {
		t.Fatal("rollback overwrote or mutated the different current journal")
	}
	next, err := transitionPDNSAdoptionJournalToRollback(
		expected,
		func() (dnsEngineSwitchJournal, bool, error) { return expected, true, nil },
		func(actual dnsEngineSwitchJournal) error {
			writes++
			if actual.Phase != dnsSwitchPhaseRollingBack ||
				actual.MutationRequestID != expected.MutationRequestID {
				t.Fatal("rollback transition wrote the wrong journal identity")
			}
			return nil
		},
	)
	if err != nil || next.Phase != dnsSwitchPhaseRollingBack || writes != 1 {
		t.Fatalf("exact rollback transition failed: next=%+v writes=%d err=%v", next, writes, err)
	}
}
