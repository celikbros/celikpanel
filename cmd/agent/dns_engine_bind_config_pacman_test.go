package main

import (
	"strings"
	"testing"
)

// The stock /etc/named.conf the pacman bind package ships, as captured on Arch
// on 3 September 2026 (register R-018).
// pacman bind paketinin gönderdiği stok /etc/named.conf, 3 Eylül 2026'da
// Arch'ta yakalandığı hâliyle (defter R-018).
const stockPacmanNamedConf = `// vim:set ts=4 sw=4 et:

options {
    directory "/var/named";
    pid-file "/run/named/named.pid";

    // Uncomment these to enable IPv6 connections support
    // IPv4 will still work:
    //  listen-on-v6 { any; };
    // Add this for no IPv4:
    //  listen-on { none; };

    allow-recursion { 127.0.0.1; ::1; };
    allow-transfer { none; };
    allow-update { none; };

    version none;
    hostname none;
    server-id none;
};

zone "localhost" IN {
    type master;
    file "localhost.zone";
};
`

func TestStockPacmanBINDOptionsAreSupersededByTheManagedBlock(t *testing.T) {
	stripped, err := stripStockPacmanBINDOptionDirectives(stockPacmanNamedConf)
	if err != nil {
		t.Fatal(err)
	}
	for _, directive := range pacmanStockBINDOptionDirectives {
		if strings.Contains(stripped, directive) {
			t.Fatalf("stock directive survived: %s", directive)
		}
	}
	for _, kept := range []string{`allow-update { none; };`, `directory "/var/named";`, `version none;`, `zone "localhost" IN {`} {
		if !strings.Contains(stripped, kept) {
			t.Fatalf("unrelated line was removed: %s", kept)
		}
	}
	managed, err := managedBINDOptions(stripped, "")
	if err != nil {
		t.Fatalf("managed options refused the stripped stock file: %v", err)
	}
	if !strings.Contains(managed, bindOptionsMarkerBegin) || !strings.Contains(managed, "allow-recursion { none; };") {
		t.Fatalf("managed block missing:\n%s", managed)
	}
	// Idempotent: the next switch sees the managed file and changes nothing.
	// Idempotent: sonraki geçiş yönetilen dosyayı görür ve hiçbir şeyi değiştirmez.
	again, err := stripStockPacmanBINDOptionDirectives(managed)
	if err != nil {
		t.Fatal(err)
	}
	final, err := managedBINDOptions(again, "")
	if err != nil || final != managed {
		t.Fatalf("second pass changed the managed file (err=%v)", err)
	}
}

func TestOperatorOwnedPacmanBINDOptionsStillRefuse(t *testing.T) {
	for name, custom := range map[string]string{
		"custom recursion": strings.Replace(stockPacmanNamedConf,
			"allow-recursion { 127.0.0.1; ::1; };", "allow-recursion { 10.0.0.0/8; };", 1),
		"custom transfer": strings.Replace(stockPacmanNamedConf,
			"allow-transfer { none; };", "allow-transfer { 192.0.2.9; };", 1),
		"explicit recursion yes": strings.Replace(stockPacmanNamedConf,
			"allow-update { none; };", "allow-update { none; };\n    recursion yes;", 1),
	} {
		t.Run(name, func(t *testing.T) {
			stripped, err := stripStockPacmanBINDOptionDirectives(custom)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := managedBINDOptions(stripped, ""); err == nil {
				t.Fatal("an operator-owned directive was silently superseded")
			}
		})
	}
}

func TestStockPacmanDirectivesOutsideTheOptionsBlockOrCommentedStay(t *testing.T) {
	config := stockPacmanNamedConf + "\n// allow-transfer { none; };\n"
	config = strings.Replace(config, "    allow-recursion { 127.0.0.1; ::1; };\n",
		"    // allow-recursion { 127.0.0.1; ::1; };\n", 1)
	stripped, err := stripStockPacmanBINDOptionDirectives(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stripped, "// allow-recursion { 127.0.0.1; ::1; };") ||
		!strings.Contains(stripped, "\n// allow-transfer { none; };\n") {
		t.Fatal("commented or out-of-block lines were removed")
	}
	if strings.Contains(stripped, "\n    allow-transfer { none; };\n") {
		t.Fatal("the in-block stock transfer line survived")
	}
}
