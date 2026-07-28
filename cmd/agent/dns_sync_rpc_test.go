package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncDNSZonePreservesDomainIdentityAndDNSSECState(t *testing.T) {
	originalSyncCommand, originalDNSSECCommand := dnsSyncCommand, dnssecCommand
	var publicationCalls []string
	dnsSyncCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		publicationCalls = append(publicationCalls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	dnssecCommand = func(name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		publicationCalls = append(publicationCalls, call)
		switch strings.Join(args, " ") {
		case "zone show biovision.health":
			return []byte("DS = biovision.health. IN DS 12345 13 2 AABBCC\\n"), nil
		case "zone rectify biovision.health":
			return nil, nil
		default:
			return nil, errors.New("unexpected DNSSEC command")
		}
	}
	t.Cleanup(func() {
		dnsSyncCommand, dnssecCommand = originalSyncCommand, originalDNSSECCommand
	})
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains (id, name, type, notified_serial, options)
		VALUES (7, 'biovision.health', 'MASTER', 2026072601, 'keep-me');
		INSERT INTO records (domain_id, name, type, content, ttl, auth)
		VALUES (7, 'biovision.health', 'SOA', 'old.invalid hostmaster.biovision.health 1 10800 3600 604800 3600', 3600, 1);
		INSERT INTO domainmetadata (domain_id, kind, content)
		VALUES (7, 'NSEC3PARAM', '1 0 0 -');
		INSERT INTO cryptokeys (domain_id, flags, active, published, content)
		VALUES (7, 257, 1, 1, 'private-key-material');
		INSERT INTO comments (domain_id, name, type, modified_at, comment)
		VALUES (7, 'biovision.health', 'SOA', 1, 'keep this comment');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	req := &SyncDNSZoneRequest{
		Domain:   "biovision.health",
		ZoneType: "MASTER",
		Records: []ZoneRecord{
			{Name: "biovision.health", Type: "SOA", Content: "ns2.celikhost.com hostmaster.biovision.health 2026072602 10800 3600 604800 3600", TTL: 3600},
			{Name: "biovision.health", Type: "NS", Content: "ns1.celikhost.com", TTL: 3600},
			{Name: "biovision.health", Type: "NS", Content: "ns2.celikhost.com", TTL: 3600},
		},
	}
	var resp SyncDNSZoneResponse
	if err := (&Agent{}).syncDNSZone(context.Background(), req, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || !resp.Synced {
		t.Fatalf("sync failed: %+v", resp)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id, notified int
	var zoneType, options string
	if err := db.QueryRow(`SELECT id, type, notified_serial, options FROM domains WHERE name = ?`, req.Domain).
		Scan(&id, &zoneType, &notified, &options); err != nil {
		t.Fatal(err)
	}
	if id != 7 || zoneType != "MASTER" || notified != 2026072601 || options != "keep-me" {
		t.Errorf("domain identity/state changed: id=%d type=%q notified=%d options=%q", id, zoneType, notified, options)
	}
	for table, want := range map[string]int{"domainmetadata": 1, "cryptokeys": 1, "comments": 1, "records": 3} {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE domain_id = 7`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	position := func(needle string) int {
		for i, call := range publicationCalls {
			if call == needle {
				return i
			}
		}
		return -1
	}
	rectifyAt := position("pdnsutil zone rectify biovision.health")
	notifyAt := position("pdns_control notify biovision.health")
	if rectifyAt < 0 || notifyAt < 0 || rectifyAt >= notifyAt {
		t.Fatalf("signed publication order = %v; rectify must finish before NOTIFY", publicationCalls)
	}
}

func TestSyncDNSZoneRefusesPeerOwnedZone(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains (id, name, type, master) VALUES (9, 'peer.example', 'SLAVE', '192.0.2.10');
		INSERT INTO records (domain_id, name, type, content, ttl, auth)
		VALUES (9, 'peer.example', 'SOA', 'ns.example hostmaster.example 1 10800 3600 604800 3600', 3600, 1);
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	var resp SyncDNSZoneResponse
	if err := (&Agent{}).syncDNSZone(context.Background(), &SyncDNSZoneRequest{
		Domain:   "peer.example",
		ZoneType: "MASTER",
		Records:  []ZoneRecord{{Name: "peer.example", Type: "A", Content: "192.0.2.20", TTL: 300}},
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" || resp.Synced {
		t.Fatalf("peer-owned zone was writable: %+v", resp)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var recordType string
	if err := db.QueryRow(`SELECT type FROM records WHERE domain_id = 9`).Scan(&recordType); err != nil {
		t.Fatal(err)
	}
	if recordType != "SOA" {
		t.Errorf("peer-owned records changed to %q", recordType)
	}
}

func TestSyncDNSZoneRefusesDeletingPeerOwnedZone(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains (id, name, type, master) VALUES (10, 'peer-delete.example', 'SECONDARY', '192.0.2.10');
		INSERT INTO records (domain_id, name, type, content, ttl, auth)
		VALUES (10, 'peer-delete.example', 'SOA', 'ns.example hostmaster.example 1 10800 3600 604800 3600', 3600, 1);
		INSERT INTO domainmetadata (domain_id, kind, content) VALUES (10, 'PRESIGNED', '1');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	var resp SyncDNSZoneResponse
	if err := (&Agent{}).syncDNSZone(context.Background(), &SyncDNSZoneRequest{
		Domain: "peer-delete.example",
		Delete: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == "" || resp.Synced {
		t.Fatalf("peer-owned zone was deleted: %+v", resp)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var zoneType, master string
	if err := db.QueryRow(`SELECT type, master FROM domains WHERE id = 10`).Scan(&zoneType, &master); err != nil {
		t.Fatalf("peer-owned domain was removed: %v", err)
	}
	if zoneType != "SECONDARY" || master != "192.0.2.10" {
		t.Errorf("peer-owned domain changed: type=%q master=%q", zoneType, master)
	}
	for table, want := range map[string]int{"records": 1, "domainmetadata": 1} {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE domain_id = 10`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
}

func TestSyncDNSZoneReportsNotifyFailure(t *testing.T) {
	originalSyncCommand, originalDNSSECCommand := dnsSyncCommand, dnssecCommand
	dnsSyncCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pdns_control" && len(args) > 0 && args[0] == "notify" {
			return []byte("PowerDNS control socket unavailable\\n"), errors.New("exit status 1")
		}
		return nil, nil
	}
	dnssecCommand = func(string, ...string) ([]byte, error) {
		return []byte("Zone is not secured\\n"), nil
	}
	t.Cleanup(func() {
		dnsSyncCommand, dnssecCommand = originalSyncCommand, originalDNSSECCommand
	})
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))

	var resp SyncDNSZoneResponse
	if err := (&Agent{}).syncDNSZone(context.Background(), &SyncDNSZoneRequest{
		Domain:   "notify.example",
		ZoneType: "MASTER",
		Records: []ZoneRecord{{
			Name: "notify.example", Type: "SOA",
			Content: "ns1.example hostmaster.example 2026072601 10800 3600 604800 3600", TTL: 3600,
		}},
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Synced || !strings.Contains(resp.Error, "notify peer") {
		t.Fatalf("notify failure was hidden: %+v", resp)
	}
}
