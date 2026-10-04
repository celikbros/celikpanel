//go:build linux

package dnsenginerecovery

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestProbePDNSAdoptionDatabaseChecksSQLiteRowsAndFrozenBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pdns.sqlite3")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT, type TEXT, master TEXT, account TEXT);
		CREATE TABLE records (id INTEGER PRIMARY KEY, domain_id INTEGER, name TEXT, type TEXT, content TEXT, ttl INTEGER, prio INTEGER, disabled INTEGER);
		CREATE TABLE supermasters (ip TEXT, nameserver TEXT, account TEXT);
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
		Mode: transport.DNSEngineSwitchModeAdopt, TargetEngine: transport.DNSEnginePowerDNS,
		Topology: transport.DNSTopologyStandalone, TargetEpoch: 1,
	}
	_, size, digest, err := InspectPDNSDatabaseFile(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, path, size, digest, manifest); err != nil {
		t.Fatalf("exact empty-zone database rejected: %v", err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, path, size, "0000000000000000000000000000000000000000000000000000000000000000", manifest); err == nil {
		t.Fatal("changed frozen digest admitted")
	}
	link := filepath.Join(t.TempDir(), "pdns.sqlite3")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, link, size, digest, manifest); err == nil {
		t.Fatal("symlinked database admitted")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO domains(id, name, type, master, account) VALUES (1, 'owner.example', 'NATIVE', '', '')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, size, digest, err = InspectPDNSDatabaseFile(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, path, size, digest, manifest); err == nil {
		t.Fatal("unowned extra zone admitted despite matching frozen bytes")
	}
	record := transport.ZoneRecord{Name: "owner.example", Type: "A", Content: "192.0.2.10", TTL: 300}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEnginePowerDNS, 1, 1, "owner.example", false,
		"NATIVE", []transport.ZoneRecord{record},
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Zones = []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: commitment.Domain, DesiredGeneration: commitment.DesiredGeneration,
		ZoneType: commitment.ZoneType, Records: commitment.Records,
		ZoneQualifier: commitment.Qualifier,
	}}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO records(domain_id, name, type, content, ttl, prio, disabled)
		VALUES (1, 'owner.example', 'A', '192.0.2.10', 300, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, size, digest, err = InspectPDNSDatabaseFile(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, path, size, digest, manifest); err != nil {
		t.Fatalf("exact frozen zone rejected: %v", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE records SET content = '192.0.2.11' WHERE domain_id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, size, digest, err = InspectPDNSDatabaseFile(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ProbePDNSAdoptionDatabase(ctx, path, size, digest, manifest); err == nil {
		t.Fatal("changed zone accepted after updating the frozen byte digest")
	}
}
