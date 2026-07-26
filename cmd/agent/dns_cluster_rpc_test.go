package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareDNSClusterRuntimeTest(t *testing.T) string {
	t.Helper()
	oldConf := dnsClusterConf
	oldLookPath := dnsClusterLookPath
	oldRestart := dnsClusterRestart
	oldRetrieve := dnsClusterRetrieve
	oldPurge := dnsClusterPurge
	oldApply := dnsClusterApplyAutoprimaryTx
	oldSetType := dnsClusterSetLocalZoneTypeTx
	t.Cleanup(func() {
		dnsClusterConf = oldConf
		dnsClusterLookPath = oldLookPath
		dnsClusterRestart = oldRestart
		dnsClusterRetrieve = oldRetrieve
		dnsClusterPurge = oldPurge
		dnsClusterApplyAutoprimaryTx = oldApply
		dnsClusterSetLocalZoneTypeTx = oldSetType
	})

	dnsClusterConf = filepath.Join(t.TempDir(), "celikpanel-cluster.conf")
	dnsClusterLookPath = func(string) (string, error) { return "pdns_server", nil }
	dnsClusterRestart = func() ([]byte, error) { return nil, nil }
	dnsClusterRetrieve = func(string) ([]byte, error) { return nil, nil }
	dnsClusterPurge = func(string) ([]byte, error) { return nil, nil }
	dnsClusterApplyAutoprimaryTx = applyAutoprimaryTx
	dnsClusterSetLocalZoneTypeTx = setLocalZoneTypeTx
	return dnsClusterConf
}

func dnsClusterDatabaseSnapshot(t *testing.T) string {
	t.Helper()
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var parts []string
	rows, err := db.Query(`
		SELECT id, name, type, COALESCE(master, '<NULL>'),
		       COALESCE(CAST(last_check AS TEXT), '<NULL>'), COALESCE(account, '<NULL>')
		FROM domains ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int
		var name, zoneType, master, lastCheck, account string
		if err := rows.Scan(&id, &name, &zoneType, &master, &lastCheck, &account); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("domain:%d:%s:%s:%s:%s:%s", id, name, zoneType, master, lastCheck, account))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = db.Query(`SELECT ip, nameserver, account FROM supermasters ORDER BY ip, nameserver, account`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ip, nameserver, account string
		if err := rows.Scan(&ip, &nameserver, &account); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		parts = append(parts, "supermaster:"+ip+":"+nameserver+":"+account)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("%s:%d", table, count))
	}
	return strings.Join(parts, "|")
}

func TestDNSClusterConfigUsesSymmetricPair(t *testing.T) {
	req := &DNSClusterRequest{
		Role:   dnsRolePaired,
		PeerIP: "2.25.80.4",
		PeerNS: "ns2.celikhost.com",
	}
	got := dnsClusterConfig(req)
	for _, want := range []string{
		"primary=yes",
		"secondary=yes",
		"autosecondary=yes",
		"allow-axfr-ips=2.25.80.4",
		"also-notify=2.25.80.4",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("paired config does not contain %q:\\n%s", want, got)
		}
	}
	if strings.Contains(got, "allow-axfr-ip=") {
		t.Fatalf("paired config uses PowerDNS's invalid singular setting:\\n%s", got)
	}
}

func TestNormalizeAgentDNSRoleMigratesLegacyValues(t *testing.T) {
	for _, legacy := range []string{dnsRolePrimary, dnsRoleSecondary, dnsRolePaired} {
		if got := normalizeAgentDNSRole(legacy); got != dnsRolePaired {
			t.Errorf("normalizeAgentDNSRole(%q) = %q, want %q", legacy, got, dnsRolePaired)
		}
	}
	if got := normalizeAgentDNSRole("not-a-role"); got != "" {
		t.Errorf("invalid role normalized to %q, want empty", got)
	}
}

func TestApplyAutoprimaryTrustsOnlyTheConfiguredPeer(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	req := &DNSClusterRequest{Role: dnsRolePaired, PeerIP: "2.25.80.4", PeerNS: "ns2.celikhost.com."}
	if err := applyAutoprimary(req); err != nil {
		t.Fatal(err)
	}
	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	var ip, nameserver string
	if err := db.QueryRow(`SELECT ip, nameserver FROM supermasters WHERE account = 'celikpanel'`).Scan(&ip, &nameserver); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if ip != req.PeerIP || nameserver != "ns2.celikhost.com" {
		t.Errorf("autoprimary = %s/%s, want %s/ns2.celikhost.com", ip, nameserver, req.PeerIP)
	}

	if err := applyAutoprimary(&DNSClusterRequest{Role: dnsRoleStandalone}); err != nil {
		t.Fatal(err)
	}
	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM supermasters WHERE account = 'celikpanel'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("standalone left %d trusted peer rows", count)
	}
}

func TestConfigureDNSClusterRollsBackDatabaseOnEveryFailureStage(t *testing.T) {
	for _, stage := range []string{"apply-autoprimary", "set-zone-types", "restart"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
			confPath := prepareDNSClusterRuntimeTest(t)
			oldConfig := []byte("old cluster configuration\n")
			if err := os.WriteFile(confPath, oldConfig, 0o644); err != nil {
				t.Fatal(err)
			}

			db, err := openPdnsDB()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`
				INSERT INTO supermasters (ip, nameserver, account) VALUES
				  ('192.0.2.1', 'old-peer.example', 'celikpanel'),
				  ('198.51.100.1', 'manual-peer.example', 'manual');
				INSERT INTO domains (id, name, type, master, last_check, account) VALUES
				  (1, 'local.example', 'NATIVE', 'native-marker', 11, NULL),
				  (2, 'peer.example', 'SLAVE', '192.0.2.1', 22, 'celikpanel'),
				  (3, 'manual.example', 'SECONDARY', '198.51.100.1', 33, 'manual');
				INSERT INTO records (domain_id, name, type, content, ttl, auth)
				VALUES (2, 'peer.example', 'SOA', 'old-peer.example hostmaster.example 1 1 1 1 1', 300, 1);
				INSERT INTO comments (domain_id, name, type, modified_at, comment)
				VALUES (2, 'peer.example', 'SOA', 1, 'keep');
				INSERT INTO domainmetadata (domain_id, kind, content) VALUES (2, 'PRESIGNED', '1');
				INSERT INTO cryptokeys (domain_id, flags, active, published, content)
				VALUES (2, 257, 1, 1, 'keep-key');
			`); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			before := dnsClusterDatabaseSnapshot(t)

			var restartCalls int
			switch stage {
			case "apply-autoprimary":
				dnsClusterApplyAutoprimaryTx = func(tx *sql.Tx, _ *DNSClusterRequest) error {
					if _, err := tx.Exec(`DELETE FROM supermasters WHERE account = 'celikpanel'`); err != nil {
						return err
					}
					if _, err := tx.Exec(`INSERT INTO supermasters VALUES ('203.0.113.9', 'mutated.example', 'celikpanel')`); err != nil {
						return err
					}
					return errors.New("injected apply-autoprimary failure")
				}
			case "set-zone-types":
				dnsClusterSetLocalZoneTypeTx = func(tx *sql.Tx, _ *DNSClusterRequest) ([]string, error) {
					if _, err := tx.Exec(`UPDATE domains SET type = 'MASTER', master = 'mutated' WHERE id = 1`); err != nil {
						return nil, err
					}
					return nil, errors.New("injected set-zone-types failure")
				}
			case "restart":
				dnsClusterRestart = func() ([]byte, error) {
					restartCalls++
					if restartCalls == 1 {
						return []byte("new configuration rejected\n"), errors.New("exit status 1")
					}
					return nil, nil
				}
			}

			var resp DNSClusterResponse
			if err := (&Agent{}).ConfigureDNSCluster(&DNSClusterRequest{
				Role: dnsRolePaired, PeerIP: "203.0.113.9", PeerNS: "new-peer.example",
			}, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Applied || resp.Error == "" {
				t.Fatalf("failure stage %q was reported as success: %+v", stage, resp)
			}
			if after := dnsClusterDatabaseSnapshot(t); after != before {
				t.Fatalf("database was not rolled back\nbefore: %s\n after: %s", before, after)
			}
			gotConfig, err := os.ReadFile(confPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotConfig) != string(oldConfig) {
				t.Fatalf("configuration was not restored: %q", gotConfig)
			}
			if stage == "restart" && restartCalls != 2 {
				t.Fatalf("restart calls = %d, want failed new start plus restored start", restartCalls)
			}
			if stage != "restart" && restartCalls != 0 {
				t.Fatalf("restart called before database preparation completed: %d", restartCalls)
			}
		})
	}
}

func TestConfigureDNSClusterRetargetsOnlyManagedSecondariesAfterCommit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", dbPath)
	prepareDNSClusterRuntimeTest(t)
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO domains (id, name, type, master, last_check, account) VALUES
		  (1, 'local.example', 'NATIVE', 'native-marker', 11, NULL),
		  (2, 'managed.example', 'SLAVE', '192.0.2.1', 22, 'celikpanel'),
		  (3, 'already-current.example', 'SECONDARY', '203.0.113.9', 33, 'celikpanel'),
		  (4, 'manual.example', 'SLAVE', '192.0.2.1', 44, 'manual');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	var calls []string
	dnsClusterRestart = func() ([]byte, error) {
		calls = append(calls, "restart")
		readDB, err := sql.Open("sqlite", "file:"+dbPath+"?_busy_timeout=100")
		if err != nil {
			return nil, err
		}
		defer readDB.Close()
		var master string
		if err := readDB.QueryRow(`SELECT master FROM domains WHERE id = 2`).Scan(&master); err != nil {
			return nil, err
		}
		if master != "192.0.2.1" {
			return nil, fmt.Errorf("uncommitted master became visible during restart: %s", master)
		}
		return nil, nil
	}
	dnsClusterRetrieve = func(zone string) ([]byte, error) {
		calls = append(calls, "retrieve "+zone)
		return nil, nil
	}

	var resp DNSClusterResponse
	if err := (&Agent{}).ConfigureDNSCluster(&DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: "203.0.113.9", PeerNS: "new-peer.example",
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Applied || resp.Error != "" {
		t.Fatalf("pairing failed: %+v", resp)
	}
	if got := strings.Join(calls, "|"); got != "restart|retrieve managed.example" {
		t.Fatalf("post-commit calls = %q", got)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var zoneType string
	var master sql.NullString
	if err := db.QueryRow(`SELECT type, master FROM domains WHERE id = 1`).Scan(&zoneType, &master); err != nil {
		t.Fatal(err)
	}
	if zoneType != "MASTER" || master.Valid {
		t.Errorf("local zone = type %q master %+v, want MASTER/NULL", zoneType, master)
	}
	var lastCheck sql.NullInt64
	if err := db.QueryRow(`SELECT master, last_check FROM domains WHERE id = 2`).Scan(&master, &lastCheck); err != nil {
		t.Fatal(err)
	}
	if !master.Valid || master.String != "203.0.113.9" || lastCheck.Valid {
		t.Errorf("managed secondary = master %+v last_check %+v", master, lastCheck)
	}
	if err := db.QueryRow(`SELECT master, last_check FROM domains WHERE id = 3`).Scan(&master, &lastCheck); err != nil {
		t.Fatal(err)
	}
	if master.String != "203.0.113.9" || !lastCheck.Valid || lastCheck.Int64 != 33 {
		t.Errorf("already-current secondary changed: master %+v last_check %+v", master, lastCheck)
	}
	if err := db.QueryRow(`SELECT master, last_check FROM domains WHERE id = 4`).Scan(&master, &lastCheck); err != nil {
		t.Fatal(err)
	}
	if master.String != "192.0.2.1" || lastCheck.Int64 != 44 {
		t.Errorf("manual secondary changed: master %+v last_check %+v", master, lastCheck)
	}
}

func TestConfigureDNSClusterStandaloneRemovesOnlyManagedSecondaries(t *testing.T) {
	t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
	confPath := prepareDNSClusterRuntimeTest(t)
	if err := os.WriteFile(confPath, []byte("paired config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO supermasters (ip, nameserver, account) VALUES
		  ('192.0.2.1', 'old-peer.example', 'celikpanel'),
		  ('198.51.100.1', 'manual-peer.example', 'manual');
		INSERT INTO domains (id, name, type, master, account) VALUES
		  (1, 'local.example', 'MASTER', NULL, NULL),
		  (2, 'managed-a.example', 'SLAVE', '192.0.2.1', 'celikpanel'),
		  (3, 'managed-b.example', 'SECONDARY', '192.0.2.1', 'celikpanel'),
		  (4, 'manual.example', 'SLAVE', '198.51.100.1', 'manual');
		INSERT INTO records (domain_id, name, type, content, ttl, auth) VALUES
		  (2, 'managed-a.example', 'SOA', 'ns hostmaster 1 1 1 1 1', 300, 1),
		  (4, 'manual.example', 'SOA', 'ns hostmaster 1 1 1 1 1', 300, 1);
		INSERT INTO comments (domain_id, name, type, modified_at, comment) VALUES
		  (2, 'managed-a.example', 'SOA', 1, 'remove'),
		  (4, 'manual.example', 'SOA', 1, 'keep');
		INSERT INTO domainmetadata (domain_id, kind, content) VALUES
		  (2, 'PRESIGNED', '1'), (4, 'PRESIGNED', '1');
		INSERT INTO cryptokeys (domain_id, flags, active, published, content) VALUES
		  (2, 257, 1, 1, 'remove'), (4, 257, 1, 1, 'keep');
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	var calls []string
	dnsClusterRestart = func() ([]byte, error) {
		calls = append(calls, "restart")
		return nil, nil
	}
	dnsClusterPurge = func(zone string) ([]byte, error) {
		calls = append(calls, "purge "+zone+"$")
		return nil, nil
	}
	var resp DNSClusterResponse
	if err := (&Agent{}).ConfigureDNSCluster(&DNSClusterRequest{Role: dnsRoleStandalone}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Applied || resp.Error != "" {
		t.Fatalf("standalone transition failed: %+v", resp)
	}
	if got := strings.Join(calls, "|"); got != "restart|purge managed-a.example$|purge managed-b.example$" {
		t.Fatalf("post-commit calls = %q", got)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Fatalf("cluster configuration still exists or stat failed: %v", err)
	}

	db, err = openPdnsDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var zoneType string
	if err := db.QueryRow(`SELECT type FROM domains WHERE id = 1`).Scan(&zoneType); err != nil {
		t.Fatal(err)
	}
	if zoneType != "NATIVE" {
		t.Errorf("local zone type = %q, want NATIVE", zoneType)
	}
	var managedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM domains WHERE account = 'celikpanel'`).Scan(&managedCount); err != nil {
		t.Fatal(err)
	}
	if managedCount != 0 {
		t.Errorf("managed secondaries remaining = %d", managedCount)
	}
	var manualMaster string
	if err := db.QueryRow(`SELECT master FROM domains WHERE id = 4`).Scan(&manualMaster); err != nil {
		t.Fatal(err)
	}
	if manualMaster != "198.51.100.1" {
		t.Errorf("manual secondary master = %q", manualMaster)
	}
	for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE domain_id = 4`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("manual %s rows = %d, want 1", table, count)
		}
	}
	var panelSupermasters int
	if err := db.QueryRow(`SELECT COUNT(*) FROM supermasters WHERE account = 'celikpanel'`).Scan(&panelSupermasters); err != nil {
		t.Fatal(err)
	}
	if panelSupermasters != 0 {
		t.Errorf("celikpanel supermasters remaining = %d", panelSupermasters)
	}
}
