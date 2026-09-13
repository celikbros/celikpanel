package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPDNSV3SwitchStatePreservesUnreceiptedNativeZones(t *testing.T) {
	for _, kind := range []string{"MASTER", "NATIVE"} {
		for _, action := range []string{"sync", "delete"} {
			t.Run(kind+"/"+action, func(t *testing.T) {
				path, commitment, binding, state := prepareDirectionalPDNSV3Fixture(t)
				db, err := openPDNSEngineDB(path, false)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				result, err := db.Exec(`INSERT INTO domains (name,type,account) VALUES (?,?,?)`, strings.ToUpper(commitment.Domain), kind, "server-owner")
				if err != nil {
					t.Fatal(err)
				}
				domainID, err := result.LastInsertId()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO records (domain_id,name,type,content,ttl,prio,disabled,auth) VALUES (?,?,'TXT','owner-data',600,0,0,1)`, domainID, commitment.Domain); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO domainmetadata (domain_id,kind,content) VALUES (?,'OWNER','preserve')`, domainID); err != nil {
					t.Fatal(err)
				}
				if action == "delete" {
					commitment, err = mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, commitment.EngineEpoch, commitment.DesiredGeneration, commitment.Domain, true, "MASTER", nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := applyPDNSV3ZoneDatabaseForState(context.Background(), path, commitment, binding, state); err == nil || !strings.Contains(err.Error(), "management receipt") {
					t.Fatalf("unreceipted owner zone was not rejected: %v", err)
				}
				var preserved int
				if err := db.QueryRow(`SELECT COUNT(*) FROM domains d JOIN records r ON r.domain_id=d.id JOIN domainmetadata m ON m.domain_id=d.id WHERE d.id=? AND d.type=? AND d.account='server-owner' AND r.content='owner-data' AND m.content='preserve'`, domainID, kind).Scan(&preserved); err != nil {
					t.Fatal(err)
				}
				if preserved != 1 {
					t.Fatal("owner zone data or metadata changed on rejected sync")
				}
				var receipts int
				if err := db.QueryRow(`SELECT COUNT(*) FROM celikpanel_dns_zone_sync_v3_receipts WHERE domain=?`, commitment.Domain).Scan(&receipts); err != nil {
					t.Fatal(err)
				}
				if receipts != 0 {
					t.Fatal("rejected owner-zone collision wrote a receipt")
				}
			})
		}
	}
}

func TestPDNSV3SwitchStateAllowsNewAndReceiptedZones(t *testing.T) {
	path, commitment, binding, state := prepareDirectionalPDNSV3Fixture(t)
	if err := applyPDNSV3ZoneDatabaseForState(context.Background(), path, commitment, binding, state); err != nil {
		t.Fatalf("new zone rejected: %v", err)
	}
	updatedRecords := append(testPDNSEngineRecords(commitment.Domain), transport.ZoneRecord{Name: commitment.Domain, Type: "TXT", Content: "reviewed addition", TTL: 300})
	updated, err := mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, commitment.EngineEpoch, commitment.DesiredGeneration+1, commitment.Domain, false, "MASTER", updatedRecords)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneDatabaseForState(context.Background(), path, updated, binding, state); err != nil {
		t.Fatalf("owned zone update rejected: %v", err)
	}
	deleted, err := mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, commitment.EngineEpoch, commitment.DesiredGeneration+2, commitment.Domain, true, "MASTER", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPDNSV3ZoneDatabaseForState(context.Background(), path, deleted, binding, state); err != nil {
		t.Fatalf("owned zone deletion rejected: %v", err)
	}
	assertPDNSTestZoneAbsent(t, path, commitment.Domain)
}

func TestPDNSV3SwitchStatePreservesOwnerEditsAfterReceipt(t *testing.T) {
	for _, change := range []string{"content", "ttl", "disabled", "added_record", "removed_record", "zone_type", "missing_zone", "recreated_zone"} {
		for _, action := range []string{"sync", "delete"} {
			t.Run(change+"/"+action, func(t *testing.T) {
				path, prior, binding, state := prepareDirectionalPDNSV3Fixture(t)
				ctx := context.Background()
				if err := applyPDNSV3ZoneDatabaseForState(ctx, path, prior, binding, state); err != nil {
					t.Fatal(err)
				}
				if change == "recreated_zone" {
					deleted, err := mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, prior.EngineEpoch, prior.DesiredGeneration+1, prior.Domain, true, prior.ZoneType, nil)
					if err != nil {
						t.Fatal(err)
					}
					if err := applyPDNSV3ZoneDatabaseForState(ctx, path, deleted, binding, state); err != nil {
						t.Fatal(err)
					}
					prior = deleted
				}
				db, err := openPDNSEngineDB(path, false)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var statement string
				switch change {
				case "content":
					statement = `UPDATE records SET content='server-owner change' WHERE type='TXT' AND domain_id=(SELECT id FROM domains WHERE name=?)`
				case "ttl":
					statement = `UPDATE records SET ttl=900 WHERE type='TXT' AND domain_id=(SELECT id FROM domains WHERE name=?)`
				case "disabled":
					statement = `UPDATE records SET disabled=0 WHERE type='TXT' AND domain_id=(SELECT id FROM domains WHERE name=?)`
				case "added_record":
					statement = `INSERT INTO records (domain_id,name,type,content,ttl,prio,disabled,auth) SELECT id,name,'TXT','server-owner addition',300,0,0,1 FROM domains WHERE name=?`
				case "removed_record":
					statement = `DELETE FROM records WHERE type='TXT' AND domain_id=(SELECT id FROM domains WHERE name=?)`
				case "zone_type":
					statement = `UPDATE domains SET type='NATIVE' WHERE name=?`
				case "missing_zone":
					if _, err := db.Exec(`DELETE FROM records WHERE domain_id=(SELECT id FROM domains WHERE name=?)`, prior.Domain); err != nil {
						t.Fatal(err)
					}
					statement = `DELETE FROM domains WHERE name=?`
				case "recreated_zone":
					statement = `INSERT INTO domains (name,type,account) VALUES (?,'MASTER','server-owner')`
				}
				if _, err := db.Exec(statement, prior.Domain); err != nil {
					t.Fatal(err)
				}
				before := pdnsOwnershipSnapshot(t, db)
				var records []transport.ZoneRecord
				if action == "sync" {
					records = testPDNSEngineRecords(prior.Domain)
				}
				next, err := mutationpayload.CanonicalDNSZoneSyncV3(transport.DNSEnginePowerDNS, prior.EngineEpoch, prior.DesiredGeneration+1, prior.Domain, action == "delete", prior.ZoneType, records)
				if err != nil {
					t.Fatal(err)
				}
				if err := applyPDNSV3ZoneDatabaseForState(ctx, path, next, binding, state); err == nil {
					t.Fatal("owner change was silently overwritten")
				}
				if after := pdnsOwnershipSnapshot(t, db); after != before {
					t.Fatal("rejected write changed native rows, catalog, or receipts")
				}
			})
		}
	}
}

func pdnsOwnershipSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var snapshot strings.Builder
	for _, query := range []string{
		`SELECT * FROM domains ORDER BY id`,
		`SELECT * FROM records ORDER BY id`,
		`SELECT * FROM domainmetadata ORDER BY id`,
		`SELECT * FROM celikpanel_dns_zone_sync_v3_receipts ORDER BY domain`,
	} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		fmt.Fprintln(&snapshot, query)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			fmt.Fprintf(&snapshot, "%#v\n", values)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return snapshot.String()
}
