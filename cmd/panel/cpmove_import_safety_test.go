package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
	_ "modernc.org/sqlite"
)

func TestCanonicalCpmoveImportDomainMustBeDeclared(t *testing.T) {
	preview := &cpmovePreview{
		MainDomain: "Example.COM.",
		Domains:    []string{"www.example.com", "shop.example.com"},
	}
	got, err := canonicalCpmoveImportDomain(preview, "")
	if err != nil || got != "example.com" {
		t.Fatalf("default domain = %q, %v", got, err)
	}
	if _, err := canonicalCpmoveImportDomain(preview, "unrelated.example"); err == nil {
		t.Fatal("undeclared import domain was accepted")
	}
}

func TestNormalizeCpmoveDNSRecordsRejectsUnsafeAndConflictingData(t *testing.T) {
	tests := []struct {
		name    string
		records []transport.CpmoveDNSRecord
		want    string
	}{
		{
			name: "outside zone",
			records: []transport.CpmoveDNSRecord{{
				Name: "attacker.example.", Type: "A", Content: "192.0.2.1", TTL: 300,
			}},
			want: "outside this DNS zone",
		},
		{
			name: "invalid address",
			records: []transport.CpmoveDNSRecord{{
				Name: "@", Type: "A", Content: "not-an-address", TTL: 300,
			}},
			want: "invalid A address",
		},
		{
			name: "invalid ttl",
			records: []transport.CpmoveDNSRecord{{
				Name: "@", Type: "A", Content: "192.0.2.1", TTL: -1,
			}},
			want: "TTL",
		},
		{
			name: "cname coexistence",
			records: []transport.CpmoveDNSRecord{
				{Name: "www", Type: "CNAME", Content: "example.com", TTL: 300},
				{Name: "www", Type: "TXT", Content: "conflict", TTL: 300},
			},
			want: "cannot coexist",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeCpmoveDNSRecords("example.com", test.records)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestReplaceCpmoveDNSRecordsIsAtomic(t *testing.T) {
	db := openCpmoveSafetyDB(t)
	mustExecCpmoveSafety(t, db, `
		CREATE TABLE pdns_records (
			id INTEGER PRIMARY KEY,
			domain_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			ttl INTEGER NOT NULL,
			prio INTEGER
		);
		INSERT INTO pdns_records(domain_id,name,type,content,ttl)
		VALUES (1,'example.com','SOA','soa',3600),
		       (1,'example.com','NS','ns1.example.net',3600),
		       (1,'old.example.com','A','192.0.2.9',300);
		CREATE TRIGGER reject_imported_txt
		BEFORE INSERT ON pdns_records WHEN NEW.type = 'TXT'
		BEGIN SELECT RAISE(ABORT, 'test insert failure'); END;
	`)

	_, err := replaceCpmoveDNSRecords(context.Background(), db, 1, "example.com", []transport.CpmoveDNSRecord{
		{Name: "@", Type: "A", Content: "192.0.2.1", TTL: 300},
		{Name: "@", Type: "TXT", Content: "must rollback", TTL: 300},
	})
	if err == nil {
		t.Fatal("trigger failure unexpectedly succeeded")
	}
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM pdns_records
		WHERE domain_id=1 AND name='old.example.com' AND type='A' AND content='192.0.2.9'
	`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("old DNS record count = %d, want 1 after rollback", count)
	}
}

func TestSetCpmoveImportStatusRollsBackBothRows(t *testing.T) {
	db := openCpmoveSafetyDB(t)
	mustExecCpmoveSafety(t, db, `
		CREATE TABLE domains(id INTEGER PRIMARY KEY, status TEXT NOT NULL);
		CREATE TABLE sites(id INTEGER PRIMARY KEY, status TEXT NOT NULL);
		INSERT INTO domains VALUES(1,'active');
		INSERT INTO sites VALUES(2,'active');
		CREATE TRIGGER reject_site_pending
		BEFORE UPDATE ON sites WHEN NEW.status='pending'
		BEGIN SELECT RAISE(ABORT, 'test status failure'); END;
	`)
	if err := setCpmoveImportStatus(context.Background(), db, 1, 2, "pending"); err == nil {
		t.Fatal("trigger failure unexpectedly succeeded")
	}
	var domainStatus, siteStatus string
	if err := db.QueryRow(`SELECT status FROM domains WHERE id=1`).Scan(&domainStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM sites WHERE id=2`).Scan(&siteStatus); err != nil {
		t.Fatal(err)
	}
	if domainStatus != "active" || siteStatus != "active" {
		t.Fatalf("statuses = %q/%q, want active/active after rollback", domainStatus, siteStatus)
	}
}

func openCpmoveSafetyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecCpmoveSafety(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
