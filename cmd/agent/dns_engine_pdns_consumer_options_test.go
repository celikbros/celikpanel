//go:build linux

package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Native evidence, batch 5 cells c3 and c4 (PowerDNS 4.9.17-0+deb13u1): the
// consumed member s1-kill.test of catalog catalog-c000020b.celikpanel.invalid,
// PTR owner b076e924....zones.catalog-c000020b.celikpanel.invalid.
const (
	evidenceConsumedMember  = "s1-kill.test"
	evidenceConsumedLabel   = "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a"
	evidenceConsumedOptions = `{"consumer": {"unique": "b076e9241974292fffe8ecc0209b7ace316d1d36063423d9ccb0849a."}}`
)

func TestEvidenceConsumedLabelIsTheBINDCatalogMemberLabel(t *testing.T) {
	label, err := binddns.CatalogMemberLabel(evidenceConsumedMember)
	if err != nil || label != evidenceConsumedLabel {
		t.Fatalf("CatalogMemberLabel(%s)=%q err=%v", evidenceConsumedMember, label, err)
	}
}

func TestPDNSConsumedMemberOptionsAcceptsOnlyTheConsumerObject(t *testing.T) {
	other := strings.Repeat("a", 56)
	for _, test := range []struct {
		name    string
		options string
		label   string
		ok      bool
	}{
		{"native evidence bytes", evidenceConsumedOptions, evidenceConsumedLabel, true},
		{"compact spelling", `{"consumer":{"unique":"` + evidenceConsumedLabel + `."}}`, evidenceConsumedLabel, true},
		{"empty", "", evidenceConsumedLabel, true},
		{"empty without a label", "", "", true},
		{"PowerDNS producer label", `{"consumer": {"unique": "0123456789abcdefghijklmnopqrstuv."}}`, "0123456789abcdefghijklmnopqrstuv", true},
		{"label without its trailing dot", `{"consumer": {"unique": "` + evidenceConsumedLabel + `"}}`, evidenceConsumedLabel, false},
		{"another label", `{"consumer": {"unique": "` + other + `."}}`, evidenceConsumedLabel, false},
		{"upper-case label", `{"consumer": {"unique": "` + strings.ToUpper(evidenceConsumedLabel) + `."}}`, evidenceConsumedLabel, false},
		{"no label in the peer catalog", evidenceConsumedOptions, "", false},
		{"extra top-level key", `{"consumer": {"unique": "` + evidenceConsumedLabel + `."}, "producer": {}}`, evidenceConsumedLabel, false},
		{"extra consumer key", `{"consumer": {"unique": "` + evidenceConsumedLabel + `.", "group": ["g"]}}`, evidenceConsumedLabel, false},
		{"duplicate consumer key", `{"consumer": {"unique": "` + evidenceConsumedLabel + `."}, "consumer": {"unique": "` + evidenceConsumedLabel + `."}}`, evidenceConsumedLabel, false},
		{"duplicate unique key", `{"consumer": {"unique": "` + other + `.", "unique": "` + evidenceConsumedLabel + `."}}`, evidenceConsumedLabel, false},
		{"coo property", `{"consumer": {"coo": "x."}}`, evidenceConsumedLabel, false},
		{"non-string label", `{"consumer": {"unique": 1}}`, evidenceConsumedLabel, false},
		{"array", `["consumer"]`, evidenceConsumedLabel, false},
		{"trailing content", evidenceConsumedOptions + ` {}`, evidenceConsumedLabel, false},
		{"not JSON", `consumer`, evidenceConsumedLabel, false},
		{"over the bound", `{"consumer": {"unique": "` + evidenceConsumedLabel + `."}}` + strings.Repeat(" ", pdnsConsumedMemberOptionsLimit), evidenceConsumedLabel, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := verifyPDNSConsumedMemberOptions(test.options, test.label)
			if (err == nil) != test.ok {
				t.Fatalf("options %q label %q: err=%v", test.options, test.label, err)
			}
		})
	}
}

// The catalog transfer records each member's PTR label for both producers.
func TestCatalogAXFRResultRecordsMemberLabels(t *testing.T) {
	catalog := "catalog-c000020b.celikpanel.invalid"
	state, err := newDNSCatalogAXFRState(1, catalog)
	if err != nil {
		t.Fatal(err)
	}
	owner := evidenceConsumedLabel + ".zones." + catalog
	if !state.exactMemberOwner(owner, evidenceConsumedMember) {
		t.Fatal("evidence PTR owner is not the BIND member owner")
	}
	state.members[evidenceConsumedMember] = true
	state.memberLabels[evidenceConsumedMember] = strings.TrimSuffix(owner, ".zones."+catalog)
	state.questionSeen, state.opened, state.closed, state.soaCount, state.serial = true, true, true, 2, 1
	state.nsSeen, state.versionSeen = true, true
	result, err := state.result()
	if err != nil || result.MemberLabels[evidenceConsumedMember] != evidenceConsumedLabel {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func useFakePeerCatalog(t *testing.T, catalog dnsCatalogAXFRResult) {
	t.Helper()
	previous := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return catalog, nil
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })
}

// The live-projection verifier used by the fresh secondary's readiness loop,
// its target verification and its rollback accepts the consumer's own options.
func TestPDNSPairedSecondaryVerifierAcceptsConsumerOptions(t *testing.T) {
	manifest := testPairedPDNSSwitchManifest(t, transport.DNSPairRoleSecondary, nil)
	catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	withLabels := dnsCatalogAXFRResult{
		Serial: 17, Members: []string{evidenceConsumedMember},
		MemberLabels: map[string]string{evidenceConsumedMember: evidenceConsumedLabel},
	}
	withoutLabels := dnsCatalogAXFRResult{Serial: 17, Members: []string{evidenceConsumedMember}}
	for _, test := range []struct {
		name    string
		catalog dnsCatalogAXFRResult
		options any
		ok      bool
	}{
		{"native consumer options", withLabels, evidenceConsumedOptions, true},
		{"no options", withLabels, nil, true},
		{"options naming another label", withLabels, `{"consumer": {"unique": "` + strings.Repeat("c", 56) + `."}}`, false},
		{"options with a group", withLabels, `{"consumer": {"unique": "` + evidenceConsumedLabel + `.", "group": ["x"]}}`, false},
		{"options without a peer label", withoutLabels, evidenceConsumedOptions, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			useFakePeerCatalog(t, test.catalog)
			path := filepath.Join(t.TempDir(), "secondary.sqlite3")
			binding := testPDNSEngineBinding()
			if err := buildPDNSSwitchCandidate(context.Background(), path, manifest, binding); err != nil {
				t.Fatal(err)
			}
			db, err := openPDNSEngineDB(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO domains(name,type,master,catalog,account,options) VALUES(?, 'SLAVE', ?, ?, '', ?)`,
				evidenceConsumedMember, manifest.PeerIP, catalogDomain, test.options); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			err = verifyPDNSSwitchDatabase(context.Background(), path, manifest, binding)
			if (err == nil) != test.ok {
				t.Fatalf("err=%v", err)
			}
			if !test.ok && !strings.Contains(err.Error(), evidenceConsumedMember) {
				t.Fatalf("refusal does not name the member: %v", err)
			}
		})
	}
}

func TestLegacyPDNSConsumerSourceAcceptsConsumerOptions(t *testing.T) {
	manifest := testPDNSPairSecondaryReconfigureManifest(t)
	peerDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	peer := dnsCatalogAXFRResult{
		Serial: 17, Members: []string{evidenceConsumedMember},
		MemberLabels: map[string]string{evidenceConsumedMember: evidenceConsumedLabel},
	}
	for _, test := range []struct {
		name    string
		options string
		ok      bool
	}{
		{"native consumer options", evidenceConsumedOptions, true},
		{"another label", `{"consumer": {"unique": "` + strings.Repeat("d", 56) + `."}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := initializePDNSEngineDB(context.Background(), filepath.Join(t.TempDir(), "legacy.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`INSERT INTO domains(name,type,master,account) VALUES(?, 'CONSUMER', ?, ?)`,
				peerDomain, manifest.PeerIP, pdnsPeerCatalogAccount); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO domains(name,type,master,catalog,account,options) VALUES(?, 'SLAVE', ?, ?, '', ?)`,
				evidenceConsumedMember, manifest.PeerIP, peerDomain, test.options); err != nil {
				t.Fatal(err)
			}
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			err = verifyLegacyPDNSConsumerSourceTx(context.Background(), tx, manifest, peer)
			if (err == nil) != test.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

// freshPDNSSecondaryRollbackFixture builds this operation's staged candidate
// at the live path and then applies what PowerDNS's catalog consumer wrote in
// cells c3/c4: consumer last_check, the catalog's own records and one
// consumed member with its options and records.
func freshPDNSSecondaryRollbackFixture(t *testing.T) (dnsEngineSwitchJournal, string, string) {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch, "", transport.DNSEnginePowerDNS,
		0, 1, 1, transport.DNSTopologyPaired, transport.DNSPairRoleSecondary,
		"192.0.2.10", "ns2.s1-kill.test", "192.0.2.11", "ns1.s1-kill.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	catalogDomain, err := binddns.CatalogDomain(manifest.PeerIP)
	if err != nil {
		t.Fatal(err)
	}
	useFakePeerCatalog(t, dnsCatalogAXFRResult{
		Serial: 1, Members: []string{evidenceConsumedMember},
		MemberLabels: map[string]string{evidenceConsumedMember: evidenceConsumedLabel},
	})
	live, backup, candidate := stagePDNSRollbackDir(t)
	binding := transport.ServiceMutationBinding{
		MutationRequestID: "b9e7d7c1f680a84aebb1a6c9c8136fb3", MutationOwnerID: strings.Repeat("b", 32),
	}
	if err := buildPDNSSwitchCandidate(context.Background(), live, manifest, binding); err != nil {
		t.Fatal(err)
	}
	db, err := openPDNSEngineDB(live, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var consumerID int64
	if err := db.QueryRow(`SELECT id FROM domains WHERE name = ?`, catalogDomain).Scan(&consumerID); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE domains SET last_check = 1790690829 WHERE id = ?`, consumerID)
	for _, record := range [][3]any{
		{evidenceConsumedLabel + ".zones." + catalogDomain, "PTR", evidenceConsumedMember},
		{catalogDomain, "NS", "invalid"},
		{catalogDomain, "SOA", "invalid invalid 1 60 30 3600 30"},
		{"version." + catalogDomain, "TXT", `"2"`},
	} {
		exec(`INSERT INTO records(domain_id,name,type,content,ttl,auth) VALUES(?,?,?,?,60,1)`, consumerID, record[0], record[1], record[2])
	}
	exec(`INSERT INTO records(domain_id,name,type,content,ttl,auth) VALUES(?,?,NULL,NULL,NULL,1)`, consumerID, "zones."+catalogDomain)
	result, err := db.Exec(`INSERT INTO domains(name,master,last_check,type,notified_serial,account,options,catalog) VALUES(?,?,1790690844,'SLAVE',NULL,'',?,?)`,
		evidenceConsumedMember, manifest.PeerIP, evidenceConsumedOptions, catalogDomain)
	if err != nil {
		t.Fatal(err)
	}
	memberID, _ := result.LastInsertId()
	for _, record := range [][3]any{
		{"ns1." + evidenceConsumedMember, "A", "192.0.2.11"},
		{evidenceConsumedMember, "NS", "ns1." + evidenceConsumedMember},
		{evidenceConsumedMember, "SOA", "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600"},
		{"www." + evidenceConsumedMember, "A", "192.0.2.11"},
	} {
		exec(`INSERT INTO records(domain_id,name,type,content,ttl,auth) VALUES(?,?,?,?,300,1)`, memberID, record[0], record[1], record[2])
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseRollingBack,
		Mode: manifest.Mode, MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine, TargetEngine: manifest.TargetEngine,
		SourceEpoch: manifest.SourceEpoch, TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology, PairRole: manifest.PairRole, LocalIP: manifest.LocalIP, LocalNS: manifest.LocalNS,
		PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS, SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		PDNSCandidatePath: candidate, PDNSBackupPath: backup,
	}
	return journal, live, catalogDomain
}

func TestFreshPDNSSecondaryRollbackRemovesOnlyTheConsumersDatabase(t *testing.T) {
	journal, live, _ := freshPDNSSecondaryRollbackFixture(t)
	if !freshPDNSPairSecondaryRollbackJournal(journal) {
		t.Fatal("fixture is not the fresh paired-secondary shape")
	}
	// The peer is not consulted: the rollback must work while it is down.
	previous := probeDNSCatalogAXFR
	probeDNSCatalogAXFR = func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{}, errors.New("peer unreachable")
	}
	t.Cleanup(func() { probeDNSCatalogAXFR = previous })
	if err := restorePDNSDatabase(journal); err != nil {
		t.Fatalf("daemon-made consumer state was refused: %v", err)
	}
	if _, err := os.Lstat(live); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("live database was not removed: %v", err)
	}
}

func TestFreshPDNSSecondaryRollbackKeepsAnythingElse(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sql.DB, string) error
		want   string
	}{
		{"owner zone", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO domains(name,type) VALUES('owner.test','MASTER')`)
			return err
		}, "owner.test"},
		{"domain metadata", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO domainmetadata(domain_id,kind,content) VALUES(1,'ALLOW-AXFR-FROM','AUTO-NS')`)
			return err
		}, "domainmetadata"},
		{"TSIG key", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO tsigkeys(name,algorithm,secret) VALUES('k','hmac-sha256','c2VjcmV0')`)
			return err
		}, "tsigkeys"},
		{"comment", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO comments(domain_id,name,type,modified_at,comment) VALUES(1,'x','A',1,'owner note')`)
			return err
		}, "comments"},
		{"member options with another label", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`UPDATE domains SET options = ? WHERE name = ?`, `{"consumer": {"unique": "`+strings.Repeat("e", 56)+`."}}`, evidenceConsumedMember)
			return err
		}, evidenceConsumedMember},
		{"member not in the local catalog", func(db *sql.DB, catalog string) error {
			_, err := db.Exec(`INSERT INTO domains(name,master,type,account,catalog) VALUES('stray.test','192.0.2.11','SLAVE','',?)`, catalog)
			return err
		}, "stray.test"},
		{"member of another primary", func(db *sql.DB, catalog string) error {
			_, err := db.Exec(`UPDATE domains SET master = '198.51.100.9' WHERE name = ?`, evidenceConsumedMember)
			return err
		}, evidenceConsumedMember},
		{"consumer account changed", func(db *sql.DB, catalog string) error {
			_, err := db.Exec(`UPDATE domains SET account = 'owner' WHERE name = ?`, catalog)
			return err
		}, "consumer"},
		{"orphan record", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO records(domain_id,name,type,content) VALUES(999,'x.test','A','192.0.2.1')`)
			return err
		}, "belong to no zone"},
		{"extra table", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`CREATE TABLE owner_notes (note TEXT)`)
			return err
		}, "schema"},
		{"supermaster", func(db *sql.DB, _ string) error {
			_, err := db.Exec(`INSERT INTO supermasters(ip,nameserver,account) VALUES('192.0.2.11','ns1.s1-kill.test','x')`)
			return err
		}, "supermasters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal, live, catalog := freshPDNSSecondaryRollbackFixture(t)
			db, err := openPDNSEngineDB(live, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.mutate(db, catalog); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			err = restorePDNSDatabase(journal)
			if err == nil || !strings.Contains(err.Error(), test.want) ||
				!strings.Contains(err.Error(), "kept the live database") ||
				!strings.Contains(err.Error(), journal.MutationRequestID) {
				t.Fatalf("err=%v, want a refusal naming %q", err, test.want)
			}
			if _, statErr := os.Lstat(live); statErr != nil {
				t.Fatalf("refused rollback removed the database: %v", statErr)
			}
		})
	}
}

// Every other journal keeps the exact staged-target rule.
func TestFreshPDNSSecondaryRollbackShapeIsNarrow(t *testing.T) {
	journal, _, _ := freshPDNSSecondaryRollbackFixture(t)
	for name, mutate := range map[string]func(*dnsEngineSwitchJournal){
		"prior database":   func(j *dnsEngineSwitchJournal) { j.PDNSBackupSHA256 = strings.Repeat("a", 64) },
		"source engine":    func(j *dnsEngineSwitchJournal) { j.SourceEngine = transport.DNSEngineBIND },
		"prior receipt":    func(j *dnsEngineSwitchJournal) { j.StateBefore.Exists = true },
		"primary role":     func(j *dnsEngineSwitchJournal) { j.PairRole = transport.DNSPairRolePrimary },
		"standalone":       func(j *dnsEngineSwitchJournal) { j.Topology = transport.DNSTopologyStandalone },
		"adoption":         func(j *dnsEngineSwitchJournal) { j.Mode = transport.DNSEngineSwitchModeAdopt },
		"BIND target":      func(j *dnsEngineSwitchJournal) { j.TargetEngine = transport.DNSEngineBIND },
		"source epoch set": func(j *dnsEngineSwitchJournal) { j.SourceEpoch = 1 },
	} {
		candidate := journal
		mutate(&candidate)
		if freshPDNSPairSecondaryRollbackJournal(candidate) {
			t.Errorf("%s was admitted as a fresh paired secondary", name)
		}
	}
}
