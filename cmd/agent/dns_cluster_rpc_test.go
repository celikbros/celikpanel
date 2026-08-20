package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func prepareManagedDNSReadinessTest(t *testing.T, databasePath string) string {
	t.Helper()
	oldLookPath := dnsClusterLookPath
	oldManagedConf := dnsManagedConf
	oldMainConf := dnsMainConf
	oldClusterConf := dnsClusterConf
	oldRequiredOwnerUID := dnsClusterConfigRequiredOwnerUID
	oldOwnerUID := dnsClusterConfigOwnerUID
	dir := t.TempDir()
	dnsManagedConf = filepath.Join(dir, "celikpanel.conf")
	dnsMainConf = filepath.Join(dir, "pdns.conf")
	dnsClusterConf = filepath.Join(dir, "celikpanel-cluster.conf")
	dnsClusterLookPath = func(name string) (string, error) { return name, nil }
	if runtime.GOOS == "linux" {
		dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid())
	}
	managed := "launch=gsqlite3\ngsqlite3-dnssec=yes\ngsqlite3-database=" +
		databasePath +
		"\nlocal-address=192.0.2.10\nzone-cache-refresh-interval=0\nwebserver=no\napi=no\n"
	if err := os.WriteFile(dnsManagedConf, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsMainConf, []byte("include-dir="+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dnsClusterLookPath = oldLookPath
		dnsManagedConf = oldManagedConf
		dnsMainConf = oldMainConf
		dnsClusterConf = oldClusterConf
		dnsClusterConfigRequiredOwnerUID = oldRequiredOwnerUID
		dnsClusterConfigOwnerUID = oldOwnerUID
	})
	return dir
}

func prepareDNSClusterRuntimeTest(t *testing.T) string {
	t.Helper()
	oldConf := dnsClusterConf
	oldManagedConf := dnsManagedConf
	oldMainConf := dnsMainConf
	oldLookPath := dnsClusterLookPath
	oldRestart := dnsClusterRestart
	oldRetrieve := dnsClusterRetrieve
	oldPurge := dnsClusterPurge
	oldCatalogProbe := probeDNSCatalogAXFR
	oldApply := dnsClusterApplyAutoprimaryTx
	oldSetType := dnsClusterSetLocalZoneTypeTx
	oldRequiredOwnerUID := dnsClusterConfigRequiredOwnerUID
	oldOwnerUID := dnsClusterConfigOwnerUID
	oldHostAddresses := dnsPairHostOwnedAddresses
	t.Cleanup(func() {
		dnsClusterConf = oldConf
		dnsManagedConf = oldManagedConf
		dnsMainConf = oldMainConf
		dnsClusterLookPath = oldLookPath
		dnsClusterRestart = oldRestart
		dnsClusterRetrieve = oldRetrieve
		dnsClusterPurge = oldPurge
		probeDNSCatalogAXFR = oldCatalogProbe
		dnsClusterApplyAutoprimaryTx = oldApply
		dnsClusterSetLocalZoneTypeTx = oldSetType
		dnsClusterConfigRequiredOwnerUID = oldRequiredOwnerUID
		dnsClusterConfigOwnerUID = oldOwnerUID
		dnsPairHostOwnedAddresses = oldHostAddresses
	})

	dir := t.TempDir()
	if runtime.GOOS == "linux" {
		dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid())
	}
	dnsClusterConf = filepath.Join(dir, "celikpanel-cluster.conf")
	dnsManagedConf = filepath.Join(dir, "celikpanel.conf")
	dnsMainConf = filepath.Join(dir, "pdns.conf")
	managed := "launch=gsqlite3\ngsqlite3-dnssec=yes\ngsqlite3-database=" +
		pdnsDBPath() +
		"\nlocal-address=192.0.2.10\nzone-cache-refresh-interval=0\nwebserver=no\napi=no\n"
	if err := os.WriteFile(dnsManagedConf, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsMainConf, []byte("include-dir="+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dnsClusterLookPath = func(string) (string, error) { return "pdns_server", nil }
	dnsClusterRestart = func(context.Context) ([]byte, error) { return nil, nil }
	dnsClusterRetrieve = func(context.Context, string) ([]byte, error) { return nil, nil }
	dnsClusterPurge = func(context.Context, string) ([]byte, error) { return nil, nil }
	probeDNSCatalogAXFR = func(_ context.Context, _ string, domain string) (dnsCatalogAXFRResult, error) {
		db, err := openPDNSEngineDB(pdnsDBPath(), true)
		if err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		defer db.Close()
		rows, err := db.Query(`
			SELECT name FROM domains WHERE catalog = ? COLLATE NOCASE
			ORDER BY name COLLATE BINARY
		`, domain)
		if err != nil {
			return dnsCatalogAXFRResult{}, err
		}
		defer rows.Close()
		result := dnsCatalogAXFRResult{Serial: 1}
		for rows.Next() {
			var member string
			if err := rows.Scan(&member); err != nil {
				return dnsCatalogAXFRResult{}, err
			}
			result.Members = append(result.Members, member)
		}
		return result, rows.Err()
	}
	dnsClusterApplyAutoprimaryTx = applyAutoprimaryTx
	dnsClusterSetLocalZoneTypeTx = setLocalZoneTypeTx
	dnsPairHostOwnedAddresses = func() []string {
		return []string{"192.0.2.10"}
	}
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

func TestManagedPowerDNSMainConfigAcceptsOnlyOneExactStockEmptyLaunch(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	dir := prepareManagedDNSReadinessTest(t, databasePath)

	tests := []struct {
		name   string
		config string
		valid  bool
	}{
		{
			name:   "fresh main file without legacy launch",
			config: "include-dir={managed}\n",
			valid:  true,
		},
		{
			name:   "stock empty launch before managed include",
			config: "launch=\ninclude-dir={managed}\n",
			valid:  true,
		},
		{
			name:   "commented inactive launch does not override",
			config: "# launch=bind\ninclude-dir={managed}\n",
			valid:  true,
		},
		{
			name: "Debian stock empty launch after managed include",
			config: "# include-dir=\n" +
				"include-dir={managed}\n" +
				"# launch=\n" +
				"launch=\n",
			valid: true,
		},
		{
			name:   "Debian stock empty launch after managed include with CRLF",
			config: "include-dir={managed}\r\nlaunch=\r\n",
			valid:  true,
		},
		{
			name:   "gsqlite3 launch override",
			config: "launch=gsqlite3\ninclude-dir={managed}\n",
		},
		{
			name:   "bind launch override",
			config: "launch=bind\ninclude-dir={managed}\n",
		},
		{
			name:   "custom launch override",
			config: "launch=custom\ninclude-dir={managed}\n",
		},
		{
			name:   "alternate append launch key",
			config: "launch+=bind\ninclude-dir={managed}\n",
		},
		{
			name:   "space before append launch separator",
			config: "launch +=bind\ninclude-dir={managed}\n",
		},
		{
			name:   "tab before append launch separator",
			config: "launch\t+=bind\ninclude-dir={managed}\n",
		},
		{
			name:   "empty append launch",
			config: "launch +=\ninclude-dir={managed}\n",
		},
		{
			name:   "duplicate empty launch",
			config: "launch=\nlaunch=\ninclude-dir={managed}\n",
		},
		{
			name:   "duplicate empty launch around managed include",
			config: "launch=\ninclude-dir={managed}\nlaunch=\n",
		},
		{
			name:   "nonempty launch after managed include",
			config: "include-dir={managed}\nlaunch=gsqlite3\n",
		},
		{
			name:   "append launch after managed include",
			config: "include-dir={managed}\nlaunch+=bind\n",
		},
		{
			name:   "whitespace launch after managed include",
			config: "include-dir={managed}\nlaunch= \n",
		},
		{
			name:   "inline comment launch after managed include",
			config: "include-dir={managed}\nlaunch=# not empty\n",
		},
		{
			name:   "whitespace around separator",
			config: "launch =\ninclude-dir={managed}\n",
		},
		{
			name:   "trailing whitespace",
			config: "launch= \ninclude-dir={managed}\n",
		},
		{
			name:   "inline comment is not empty",
			config: "launch=# not a stock empty directive\ninclude-dir={managed}\n",
		},
		{
			name:   "spaced inline comment is not byte exact",
			config: "launch= # not a stock empty directive\ninclude-dir={managed}\n",
		},
		{
			name:   "malformed launch without separator",
			config: "launch gsqlite3\ninclude-dir={managed}\n",
		},
		{
			name:   "malformed append launch without separator",
			config: "launch+ bind\ninclude-dir={managed}\n",
		},
		{
			name:   "other empty managed directive",
			config: "gsqlite3-database=\ninclude-dir={managed}\n",
		},
		{
			name:   "gsqlite3 database append override",
			config: "gsqlite3-database+=/evil\ninclude-dir={managed}\n",
		},
		{
			name:   "spaced gsqlite3 database append override",
			config: "gsqlite3-database +=/evil\ninclude-dir={managed}\n",
		},
		{
			name:   "include directory append override",
			config: "include-dir +=/evil\ninclude-dir={managed}\n",
		},
		{
			name:   "vertical tab before include directory value",
			config: "include-dir=\v{managed}\n",
		},
		{
			name:   "form feed before include directory value",
			config: "include-dir=\f{managed}\n",
		},
		{
			name:   "non-breaking space before include directory value",
			config: "include-dir=\u00a0{managed}\n",
		},
		{
			name:   "non-breaking space after include directory value",
			config: "include-dir={managed}\u00a0\n",
		},
		{
			name:   "non-breaking space before include directory key",
			config: "\u00a0include-dir={managed}\n",
		},
		{
			name:   "noncanonical include directory alias",
			config: "include-dir={managed}/link/..\n",
		},
		{
			name:   "bare api directive",
			config: "api\ninclude-dir={managed}\n",
		},
		{
			name:   "bare primary directive with inline comment",
			config: "primary # enabled\ninclude-dir={managed}\n",
		},
		{
			name:   "bare launch directive with inline comment",
			config: "launch # enabled\ninclude-dir={managed}\n",
		},
		{
			name:   "local address override",
			config: "local-address=0.0.0.0\ninclude-dir={managed}\n",
		},
		{
			name:   "zone cache override",
			config: "zone-cache-refresh-interval=60\ninclude-dir={managed}\n",
		},
		{
			name:   "webserver override",
			config: "webserver=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "api override",
			config: "api=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "arbitrary gsqlite3 query override",
			config: "gsqlite3-basic-query=select evil\ninclude-dir={managed}\n",
		},
		{
			name:   "local port override",
			config: "local-port=5353\ninclude-dir={managed}\n",
		},
		{
			name:   "disable AXFR override",
			config: "disable-axfr=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "DNS update override",
			config: "dnsupdate=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "DNS update TSIG override",
			config: "dnsupdate-require-tsig=no\ninclude-dir={managed}\n",
		},
		{
			name:   "DNS update source override",
			config: "allow-dnsupdate-from=0.0.0.0/0\ninclude-dir={managed}\n",
		},
		{
			name:   "forward DNS update override",
			config: "forward-dnsupdate=192.0.2.1\ninclude-dir={managed}\n",
		},
		{
			name:   "legacy superslave override",
			config: "superslave=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "bare legacy superslave override",
			config: "superslave # enabled\ninclude-dir={managed}\n",
		},
		{
			name:   "incremental legacy superslave override",
			config: "superslave +=yes\ninclude-dir={managed}\n",
		},
		{
			name:   "uppercase launch is not canonical",
			config: "LAUNCH=\ninclude-dir={managed}\n",
		},
		{
			name:   "uppercase include is not canonical",
			config: "launch=\nINCLUDE-DIR={managed}\n",
		},
		{
			name:   "split launch key continuation",
			config: "laun\\\nch=bind\ninclude-dir={managed}\n",
		},
		{
			name:   "split database key continuation",
			config: "gsqlite3-data\\\nbase=/evil\ninclude-dir={managed}\n",
		},
		{
			name:   "split include key continuation",
			config: "include-\\\ndir=/evil\ninclude-dir={managed}\n",
		},
		{
			name:   "CRLF launch continuation",
			config: "laun\\\r\nch=bind\r\ninclude-dir={managed}\r\n",
		},
		{
			name:   "comment continuation",
			config: "# comment\\\nlaunch=bind\ninclude-dir={managed}\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := strings.ReplaceAll(test.config, "{managed}", dir)
			hasInclude, validationErr := validateManagedPowerDNSMainConfig(config, dir)
			if test.valid {
				if validationErr != nil || !hasInclude {
					t.Fatalf("main config validation=(include=%v, err=%v), want accepted", hasInclude, validationErr)
				}
			} else if validationErr == nil {
				t.Fatalf("main config validation=(include=%v, err=nil), want hard failure", hasInclude)
			}

			if err := os.WriteFile(dnsMainConf, []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}
			effective, _, effectiveErr := effectiveManagedPowerDNSConfig()
			if test.valid {
				if effectiveErr != nil || !effective {
					t.Fatalf("effective config=(effective=%v, err=%v), want accepted", effective, effectiveErr)
				}
			} else if effectiveErr == nil {
				t.Fatalf("effective config=(effective=%v, err=nil), want hard failure", effective)
			}
		})
	}
}

func TestManagedPowerDNSConfigRejectsParserAmbiguity(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "pdns.sqlite3")
	databaseAlias := filepath.Join(filepath.Dir(databasePath), "link") +
		"/../" + filepath.Base(databasePath)
	baseline := "launch=gsqlite3\n" +
		"gsqlite3-dnssec=yes\n" +
		"gsqlite3-database=" + databasePath + "\n" +
		"local-address=192.0.2.10\n" +
		"zone-cache-refresh-interval=0\n" +
		"webserver=no\n" +
		"api=no\n"
	if !validManagedPowerDNSConfig(baseline, databasePath) {
		t.Fatal("canonical managed PowerDNS configuration was rejected")
	}
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "bare launch appended",
			config: baseline + "launch\n",
		},
		{
			name:   "bare api with inline comment appended",
			config: baseline + "api # enabled\n",
		},
		{
			name: "comment continuation swallows required launch",
			config: strings.Replace(
				baseline, "launch=gsqlite3", "# comment\\\nlaunch=gsqlite3", 1,
			),
		},
		{
			name: "split required database key",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-data\\\nbase="+databasePath,
				1,
			),
		},
		{
			name: "incremental launch",
			config: strings.Replace(
				baseline, "launch=gsqlite3", "launch +=gsqlite3", 1,
			),
		},
		{
			name: "incremental database",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database +=/evil",
				1,
			),
		},
		{
			name: "uppercase launch",
			config: strings.Replace(
				baseline, "launch=gsqlite3", "LAUNCH=gsqlite3", 1,
			),
		},
		{
			name: "vertical tab before database path",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database=\v"+databasePath,
				1,
			),
		},
		{
			name: "form feed before database path",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database=\f"+databasePath,
				1,
			),
		},
		{
			name: "non-breaking space before database path",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database=\u00a0"+databasePath,
				1,
			),
		},
		{
			name: "non-breaking space after database path",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database="+databasePath+"\u00a0",
				1,
			),
		},
		{
			name: "non-breaking space before database key",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"\u00a0gsqlite3-database="+databasePath,
				1,
			),
		},
		{
			name: "noncanonical database alias",
			config: strings.Replace(
				baseline,
				"gsqlite3-database="+databasePath,
				"gsqlite3-database="+databaseAlias,
				1,
			),
		},
		{
			name: "local address with port",
			config: strings.Replace(
				baseline, "local-address=192.0.2.10", "local-address=192.0.2.10:5353", 1,
			),
		},
		{
			name: "invalid local address",
			config: strings.Replace(
				baseline, "local-address=192.0.2.10", "local-address=not-an-ip", 1,
			),
		},
		{
			name: "empty local address token",
			config: strings.Replace(
				baseline, "local-address=192.0.2.10", "local-address=192.0.2.10,", 1,
			),
		},
		{
			name: "duplicate local address",
			config: strings.Replace(
				baseline,
				"local-address=192.0.2.10",
				"local-address=192.0.2.10,192.0.2.10",
				1,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if validManagedPowerDNSConfig(test.config, databasePath) {
				t.Fatal("ambiguous managed PowerDNS configuration was accepted")
			}
		})
	}
}

func TestManagedPowerDNSClusterConfigRejectsParserAmbiguity(t *testing.T) {
	baseline := "primary=yes\n" +
		"secondary=yes\n" +
		"autosecondary=yes\n" +
		"allow-axfr-ips=192.0.2.10\n" +
		"also-notify=192.0.2.10\n"
	if !validDNSClusterPowerDNSConfig(baseline) ||
		!validManagedDNSClusterPowerDNSConfig(baseline) {
		t.Fatal("canonical managed PowerDNS cluster configuration was rejected")
	}
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "comment continuation swallows required primary",
			config: "# comment\\\n" + baseline,
		},
		{
			name: "split required primary key",
			config: strings.Replace(
				baseline, "primary=yes", "prim\\\nary=yes", 1,
			),
		},
		{
			name: "incremental primary",
			config: strings.Replace(
				baseline, "primary=yes", "primary +=yes", 1,
			),
		},
		{
			name: "incremental also-notify",
			config: strings.Replace(
				baseline,
				"also-notify=192.0.2.10",
				"also-notify +=192.0.2.10",
				1,
			),
		},
		{
			name: "bare primary with inline comment",
			config: strings.Replace(
				baseline, "primary=yes", "primary # enabled", 1,
			),
		},
		{
			name:   "bare foreign setting",
			config: baseline + "api # enabled\n",
		},
		{
			name: "uppercase primary",
			config: strings.Replace(
				baseline, "primary=yes", "PRIMARY=yes", 1,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if validDNSClusterPowerDNSConfig(test.config) {
				t.Fatal("ambiguous legacy cluster configuration was accepted")
			}
			if validManagedDNSClusterPowerDNSConfig(test.config) {
				t.Fatal("ambiguous managed cluster configuration was accepted")
			}
		})
	}
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

func TestDNSClusterReadinessReportsPowerDNSAvailability(t *testing.T) {
	oldLookPath := dnsClusterLookPath
	oldReadFile := dnsClusterReadFile
	oldStat := dnsClusterStat
	oldReadDir := dnsClusterReadDir
	oldManagedConf := dnsManagedConf
	oldMainConf := dnsMainConf
	oldRequiredOwnerUID := dnsClusterConfigRequiredOwnerUID
	oldOwnerUID := dnsClusterConfigOwnerUID
	t.Cleanup(func() {
		dnsClusterLookPath = oldLookPath
		dnsClusterReadFile = oldReadFile
		dnsClusterStat = oldStat
		dnsClusterReadDir = oldReadDir
		dnsManagedConf = oldManagedConf
		dnsMainConf = oldMainConf
		dnsClusterConfigRequiredOwnerUID = oldRequiredOwnerUID
		dnsClusterConfigOwnerUID = oldOwnerUID
	})
	dir := t.TempDir()
	if runtime.GOOS == "linux" {
		dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid())
	}
	databasePath := filepath.Join(dir, "pdns.sqlite3")
	dnsManagedConf = filepath.Join(dir, "celikpanel.conf")
	dnsMainConf = filepath.Join(dir, "pdns.conf")
	t.Setenv("CELIKPANEL_PDNS_DB", databasePath)
	dnsClusterReadFile = os.ReadFile
	dnsClusterStat = os.Lstat
	dnsClusterReadDir = os.ReadDir

	t.Run("missing tooling", func(t *testing.T) {
		dnsClusterLookPath = func(string) (string, error) { return "", errors.New("not found") }
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err != nil {
			t.Fatal(err)
		}
		if got.Ready || !strings.Contains(got.Detail, "not installed") {
			t.Fatalf("missing tooling readiness = %+v", got)
		}
	})

	dnsClusterLookPath = func(name string) (string, error) { return "/usr/sbin/" + name, nil }
	t.Run("installed but unconfigured", func(t *testing.T) {
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err != nil {
			t.Fatal(err)
		}
		if got.Ready || !strings.Contains(got.Detail, "not been configured") {
			t.Fatalf("unconfigured readiness = %+v", got)
		}
	})

	if err := os.WriteFile(dnsManagedConf, []byte(
		"launch=gsqlite3\ngsqlite3-dnssec=yes\ngsqlite3-database="+databasePath+
			"\nlocal-address=192.0.2.10\nzone-cache-refresh-interval=0\nwebserver=no\napi=no\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsMainConf, []byte(
		"launch=\ninclude-dir="+dir+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, []byte("sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("configured", func(t *testing.T) {
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err != nil {
			t.Fatal(err)
		}
		if !got.Ready || !strings.Contains(got.Detail, "ready") {
			t.Fatalf("configured readiness = %+v", got)
		}
	})

	for _, test := range []struct {
		name       string
		targetBase string
		prepare    func(t *testing.T)
	}{
		{
			name:       "unexpected managed configuration owner is hard",
			targetBase: filepath.Base(dnsManagedConf),
		},
		{
			name:       "unexpected main configuration owner is hard",
			targetBase: filepath.Base(dnsMainConf),
		},
		{
			name:       "unexpected loaded configuration owner is hard",
			targetBase: "owner-override.conf",
			prepare: func(t *testing.T) {
				if err := os.WriteFile(
					filepath.Join(dir, "owner-override.conf"),
					[]byte("# owner trust probe\n"),
					0o644,
				); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = os.Remove(filepath.Join(dir, "owner-override.conf"))
				})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.prepare != nil {
				test.prepare(t)
			}
			oldOwner := dnsClusterConfigOwnerUID
			dnsClusterConfigOwnerUID = func(
				info os.FileInfo,
			) (uint32, bool) {
				if info.Name() == test.targetBase {
					return dnsClusterConfigRequiredOwnerUID + 1, true
				}
				return dnsClusterConfigRequiredOwnerUID, true
			}
			defer func() { dnsClusterConfigOwnerUID = oldOwner }()
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &got,
			); err == nil {
				t.Fatalf("unexpected owner readiness=%+v", got)
			}
			if got.Ready {
				t.Fatal("unexpected configuration owner reported ready")
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("group writable include directory is hard before enumeration", func(t *testing.T) {
			if err := os.Chmod(dir, 0o770); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(dir, 0o700)
			readDirCalls := 0
			dnsClusterReadDir = func(path string) ([]os.DirEntry, error) {
				readDirCalls++
				return os.ReadDir(path)
			}
			defer func() { dnsClusterReadDir = os.ReadDir }()
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &got,
			); err == nil {
				t.Fatalf("writable include directory readiness=%+v", got)
			}
			if got.Ready || readDirCalls != 0 {
				t.Fatalf("writable include directory ready=%v read-dir calls=%d",
					got.Ready, readDirCalls)
			}
		})

		t.Run("symlinked include directory is hard before enumeration", func(t *testing.T) {
			linkDir := filepath.Join(t.TempDir(), "pdns.d")
			if err := os.Symlink(dir, linkDir); err != nil {
				t.Fatal(err)
			}
			oldManaged, oldCluster := dnsManagedConf, dnsClusterConf
			dnsManagedConf = filepath.Join(linkDir, filepath.Base(oldManaged))
			dnsClusterConf = filepath.Join(linkDir, filepath.Base(dnsClusterConf))
			if err := os.WriteFile(
				dnsMainConf,
				[]byte("include-dir="+linkDir+"\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
			defer func() {
				dnsManagedConf, dnsClusterConf = oldManaged, oldCluster
				_ = os.WriteFile(
					dnsMainConf,
					[]byte("include-dir="+dir+"\n"),
					0o644,
				)
			}()
			readDirCalls := 0
			dnsClusterReadDir = func(path string) ([]os.DirEntry, error) {
				readDirCalls++
				return os.ReadDir(path)
			}
			defer func() { dnsClusterReadDir = os.ReadDir }()
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &got,
			); err == nil {
				t.Fatalf("symlinked include directory readiness=%+v", got)
			}
			if got.Ready || readDirCalls != 0 {
				t.Fatalf("symlinked include directory ready=%v read-dir calls=%d",
					got.Ready, readDirCalls)
			}
		})

		t.Run("world writable main configuration is hard", func(t *testing.T) {
			if err := os.Chmod(dnsMainConf, 0o666); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(dnsMainConf, 0o644)
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
				t.Fatalf("world-writable main configuration readiness=%+v", got)
			}
		})

		if runtime.GOOS == "linux" {
			t.Run("unexpected include directory owner is hard before enumeration", func(t *testing.T) {
				oldExpected := dnsClusterConfigRequiredOwnerUID
				dnsClusterConfigRequiredOwnerUID = uint32(os.Geteuid() + 1)
				defer func() {
					dnsClusterConfigRequiredOwnerUID = oldExpected
				}()
				readDirCalls := 0
				dnsClusterReadDir = func(path string) ([]os.DirEntry, error) {
					readDirCalls++
					return os.ReadDir(path)
				}
				defer func() { dnsClusterReadDir = os.ReadDir }()
				var got DNSClusterReadinessResponse
				if err := (&Agent{}).DNSClusterReadiness(
					&transport.Empty{}, &got,
				); err == nil {
					t.Fatalf("unexpected include owner readiness=%+v", got)
				}
				if got.Ready || readDirCalls != 0 {
					t.Fatalf("unexpected include owner ready=%v read-dir calls=%d",
						got.Ready, readDirCalls)
				}
			})
		}

		t.Run("world writable loaded include is hard", func(t *testing.T) {
			unsafe := filepath.Join(dir, "unsafe.conf")
			if err := os.WriteFile(unsafe, []byte("# loaded include\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(unsafe, 0o666); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(unsafe)
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
				t.Fatalf("world-writable loaded include readiness=%+v", got)
			}
		})

		t.Run("symlinked main configuration is hard", func(t *testing.T) {
			realMain := dnsMainConf + ".real"
			if err := os.Rename(dnsMainConf, realMain); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(realMain, dnsMainConf); err != nil {
				_ = os.Rename(realMain, dnsMainConf)
				t.Fatal(err)
			}
			defer func() {
				_ = os.Remove(dnsMainConf)
				_ = os.Rename(realMain, dnsMainConf)
			}()
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
				t.Fatalf("symlinked main configuration readiness=%+v", got)
			}
		})

		t.Run("symlinked loaded include is hard", func(t *testing.T) {
			target := filepath.Join(dir, "include-target")
			if err := os.WriteFile(target, []byte("# target\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "symlink.conf")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(link)
			defer os.Remove(target)
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
				t.Fatalf("symlinked loaded include readiness=%+v", got)
			}
		})
	}

	t.Run("configuration read ambiguity is hard", func(t *testing.T) {
		dnsClusterReadFile = func(string) ([]byte, error) {
			return nil, errors.New("forced read ambiguity")
		}
		defer func() { dnsClusterReadFile = os.ReadFile }()
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
			t.Fatal("ambiguous managed configuration read reported ordinary not-ready")
		}
	})

	t.Run("later topology override is hard", func(t *testing.T) {
		override := filepath.Join(dir, "zz-override.conf")
		if err := os.WriteFile(override, []byte("also-notify=198.51.100.7\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(override)
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
			t.Fatalf("loaded topology override readiness=%+v", got)
		}
	})

	for _, test := range []struct {
		name      string
		directive string
	}{
		{
			name:      "loaded local address override is hard",
			directive: "local-address=0.0.0.0\n",
		},
		{
			name:      "loaded zone cache override is hard",
			directive: "zone-cache-refresh-interval=60\n",
		},
		{
			name:      "loaded append launch override is hard",
			directive: "launch+=bind\n",
		},
		{
			name:      "loaded spaced append launch override is hard",
			directive: "launch +=bind\n",
		},
		{
			name:      "loaded append database override is hard",
			directive: "gsqlite3-database+=/evil\n",
		},
		{
			name:      "loaded spaced append database override is hard",
			directive: "gsqlite3-database +=/evil\n",
		},
		{
			name:      "loaded append include override is hard",
			directive: "include-dir +=/evil\n",
		},
		{
			name:      "loaded arbitrary gsqlite3 query override is hard",
			directive: "gsqlite3-basic-query=select evil\n",
		},
		{
			name:      "loaded local port override is hard",
			directive: "local-port=5353\n",
		},
		{
			name:      "loaded disable AXFR override is hard",
			directive: "disable-axfr=yes\n",
		},
		{
			name:      "loaded DNS update override is hard",
			directive: "dnsupdate=yes\n",
		},
		{
			name:      "loaded DNS update TSIG override is hard",
			directive: "dnsupdate-require-tsig=no\n",
		},
		{
			name:      "loaded DNS update source override is hard",
			directive: "allow-dnsupdate-from=0.0.0.0/0\n",
		},
		{
			name:      "loaded forward DNS update override is hard",
			directive: "forward-dnsupdate=192.0.2.1\n",
		},
		{
			name:      "loaded legacy superslave override is hard",
			directive: "superslave=yes\n",
		},
		{
			name:      "loaded bare legacy superslave override is hard",
			directive: "superslave # enabled\n",
		},
		{
			name:      "loaded incremental legacy superslave override is hard",
			directive: "superslave +=yes\n",
		},
		{
			name:      "loaded bare api override is hard",
			directive: "api # enabled\n",
		},
		{
			name:      "loaded bare primary override is hard",
			directive: "primary # enabled\n",
		},
		{
			name:      "loaded split launch continuation is hard",
			directive: "laun\\\nch=bind\n",
		},
		{
			name:      "loaded split database continuation is hard",
			directive: "gsqlite3-data\\\nbase=/evil\n",
		},
		{
			name:      "loaded comment continuation is hard",
			directive: "# comment\\\nlaunch=bind\n",
		},
		{
			name:      "loaded malformed launch override is hard",
			directive: "launch bind\n",
		},
		{
			name:      "loaded malformed append launch override is hard",
			directive: "launch+ bind\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			override := filepath.Join(dir, "zz-managed-override.conf")
			if err := os.WriteFile(
				override, []byte(test.directive), 0o644,
			); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(override)
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &got,
			); err == nil {
				t.Fatalf("loaded managed override readiness=%+v", got)
			}
			if got.Ready {
				t.Fatal("loaded managed override reported ready")
			}
		})
	}

	clusterBaseline := "primary=yes\n" +
		"secondary=yes\n" +
		"autosecondary=yes\n" +
		"allow-axfr-ips=192.0.2.10\n" +
		"also-notify=192.0.2.10\n"
	for _, test := range []struct {
		name   string
		config string
	}{
		{
			name: "loaded cluster continuation is hard",
			config: strings.Replace(
				clusterBaseline, "primary=yes", "prim\\\nary=yes", 1,
			),
		},
		{
			name: "loaded cluster incremental directive is hard",
			config: strings.Replace(
				clusterBaseline, "primary=yes", "primary +=yes", 1,
			),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster := filepath.Join(dir, filepath.Base(dnsClusterConf))
			if err := os.WriteFile(cluster, []byte(test.config), 0o644); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(cluster)
			var got DNSClusterReadinessResponse
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &got,
			); err == nil {
				t.Fatalf("ambiguous loaded cluster readiness=%+v", got)
			}
			if got.Ready {
				t.Fatal("ambiguous loaded cluster reported ready")
			}
		})
	}

	t.Run("standalone stray topology is hard", func(t *testing.T) {
		stray := filepath.Join(dir, "stray.conf")
		if err := os.WriteFile(stray, []byte("primary=yes\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(stray)
		var got DNSClusterReadinessResponse
		if err := (&Agent{}).DNSClusterReadiness(&transport.Empty{}, &got); err == nil {
			t.Fatalf("stray topology readiness=%+v", got)
		}
	})
}

func TestDNSClusterReadinessHidesStandbyPowerDNSDuringDurableEngineAuthority(t *testing.T) {
	for _, authority := range []string{"bind-state", "switch-journal"} {
		t.Run(authority, func(t *testing.T) {
			oldAuthority := legacyPowerDNSDurableAuthorityCheck
			oldLookPath := dnsClusterLookPath
			raw := "raw " + authority + " detail must stay in logs"
			legacyPowerDNSDurableAuthorityCheck = func(bool) error {
				return errors.New(raw)
			}
			hostCalls := 0
			dnsClusterLookPath = func(string) (string, error) {
				hostCalls++
				return "", errors.New("unexpected host lookup")
			}
			t.Cleanup(func() {
				legacyPowerDNSDurableAuthorityCheck = oldAuthority
				dnsClusterLookPath = oldLookPath
			})
			response := DNSClusterReadinessResponse{
				Ready: true, Detail: "stale authority",
			}
			if err := (&Agent{}).DNSClusterReadiness(
				&transport.Empty{}, &response,
			); err != nil {
				t.Fatal(err)
			}
			if response.Ready ||
				response.Detail != "PowerDNS is not the active DNS engine on this server" ||
				strings.Contains(response.Detail, raw) ||
				hostCalls != 0 {
				t.Fatalf("readiness=%+v hostCalls=%d", response, hostCalls)
			}
		})
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

func TestConfigureDNSClusterV2LeavesForwardRecoveryAuthorityAtEveryFailureStage(t *testing.T) {
	for _, stage := range []string{"apply-autoprimary", "set-zone-types", "restart"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("CELIKPANEL_PDNS_DB", filepath.Join(t.TempDir(), "pdns.sqlite3"))
			confPath := prepareDNSClusterRuntimeTest(t)
			oldConfig := []byte(dnsClusterConfig(&DNSClusterRequest{
				Role: dnsRolePaired, PeerIP: "192.0.2.1",
			}))
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

			var applyCalls, setTypeCalls, restartCalls int
			dnsClusterApplyAutoprimaryTx = func(tx *sql.Tx, req *DNSClusterRequest) error {
				applyCalls++
				if stage == "apply-autoprimary" {
					if _, err := tx.Exec(`DELETE FROM supermasters WHERE account = 'celikpanel'`); err != nil {
						return err
					}
					if _, err := tx.Exec(`INSERT INTO supermasters VALUES ('203.0.113.9', 'mutated.example', 'celikpanel')`); err != nil {
						return err
					}
					return errors.New("injected apply-autoprimary failure")
				}
				return applyAutoprimaryTx(tx, req)
			}
			dnsClusterSetLocalZoneTypeTx = func(tx *sql.Tx, req *DNSClusterRequest) ([]string, error) {
				setTypeCalls++
				if stage == "set-zone-types" {
					if _, err := tx.Exec(`UPDATE domains SET type = 'MASTER', master = 'mutated' WHERE id = 1`); err != nil {
						return nil, err
					}
					return nil, errors.New("injected set-zone-types failure")
				}
				return setLocalZoneTypeTx(tx, req)
			}
			dnsClusterRestart = func(context.Context) ([]byte, error) {
				restartCalls++
				if stage == "restart" {
					return []byte("new configuration rejected\n"), errors.New("exit status 1")
				}
				return nil, nil
			}

			req := &DNSClusterRequest{
				Role: dnsRolePaired, PeerIP: "203.0.113.9", PeerNS: "new-peer.example",
			}
			commitment, err := mutationpayload.CanonicalDNSClusterConfig(
				req.Role, req.PeerIP, req.PeerNS,
			)
			if err != nil {
				t.Fatal(err)
			}
			_, convergeErr := convergeDNSClusterConfig(context.Background(), commitment)
			if convergeErr == nil {
				t.Fatalf("failure stage %q was reported as success", stage)
			}
			expectedError := map[string]string{
				"apply-autoprimary": "injected apply-autoprimary failure",
				"set-zone-types":    "injected set-zone-types failure",
				"restart":           "restart PowerDNS after cluster convergence",
			}[stage]
			if !strings.Contains(convergeErr.Error(), expectedError) {
				t.Fatalf("failure stage %q error = %v, want %q", stage, convergeErr, expectedError)
			}
			expectedCalls := map[string][3]int{
				"apply-autoprimary": {1, 0, 0},
				"set-zone-types":    {1, 1, 0},
				"restart":           {1, 1, 1},
			}[stage]
			if got := [3]int{applyCalls, setTypeCalls, restartCalls}; got != expectedCalls {
				t.Fatalf("failure stage %q calls = %v, want %v", stage, got, expectedCalls)
			}
			after := dnsClusterDatabaseSnapshot(t)
			if stage == "restart" {
				if after == before {
					t.Fatal("restart failure lost the committed forward database state")
				}
			} else if after != before {
				t.Fatalf("precommit database failure leaked rows\nbefore: %s\n after: %s", before, after)
			}
			gotConfig, err := os.ReadFile(confPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotConfig) != dnsClusterConfig(req) {
				t.Fatalf("desired forward configuration was not retained: %q", gotConfig)
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
		INSERT INTO records (domain_id, name, type, content, ttl, prio, auth) VALUES
		  (1, 'local.example', 'SOA',
		   'ns.local.example hostmaster.local.example 1 60 30 3600 30', 60, 0, 1);
	`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	var calls []string
	dnsClusterRestart = func(context.Context) ([]byte, error) {
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
		if master != "203.0.113.9" {
			return nil, fmt.Errorf("committed master was not visible during restart: %s", master)
		}
		return nil, nil
	}
	dnsClusterRetrieve = func(_ context.Context, zone string) ([]byte, error) {
		calls = append(calls, "retrieve "+zone)
		return nil, nil
	}

	req := &DNSClusterRequest{
		Role: dnsRolePaired, PeerIP: "203.0.113.9", PeerNS: "new-peer.example",
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		req.Role, req.PeerIP, req.PeerNS,
	)
	if err != nil {
		t.Fatal(err)
	}
	peerZones, err := convergeDNSClusterConfig(context.Background(), commitment)
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}
	for _, zone := range peerZones {
		_, _ = dnsClusterRetrieve(context.Background(), zone)
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
	dnsClusterRestart = func(context.Context) ([]byte, error) {
		calls = append(calls, "restart")
		return nil, nil
	}
	dnsClusterPurge = func(_ context.Context, zone string) ([]byte, error) {
		calls = append(calls, "purge "+zone+"$")
		return nil, nil
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		dnsRoleStandalone, "", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	peerZones, err := convergeDNSClusterConfig(context.Background(), commitment)
	if err != nil {
		t.Fatalf("standalone transition failed: %v", err)
	}
	for _, zone := range peerZones {
		_, _ = dnsClusterPurge(context.Background(), zone)
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
