package main

import (
	"strings"
	"testing"
)

func TestManagedBINDZoneIncludeIsExactAndIdempotent(t *testing.T) {
	base := "// operator configuration\n"
	path := "/var/cache/bind/celikpanel/current/zones.conf"
	configured, err := managedBINDZoneInclude(base, path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(configured, bindZonesMarkerBegin) != 1 ||
		!strings.Contains(configured, `include "`+path+`";`) {
		t.Fatalf("configured=%q", configured)
	}
	again, err := managedBINDZoneInclude(configured, path)
	if err != nil || again != configured {
		t.Fatalf("idempotent result changed err=%v\n%s", err, again)
	}
	modified := strings.Replace(configured, "zones.conf", "attacker.conf", 1)
	if _, err := managedBINDZoneInclude(modified, path); err == nil {
		t.Fatal("modified managed zone block was accepted")
	}
	if _, err := managedBINDZoneInclude(base, "/safe\ninclude \"/tmp/x\";"); err == nil {
		t.Fatal("newline include injection was accepted")
	}
}

func TestManagedBINDOptionsDisablesPublicRecursionInsideSingleOptionsBlock(t *testing.T) {
	base := `
options {
    directory "/var/cache/bind";
    // braces and options { recursion yes; }; inside comments are inert
    listen-on-v6 { any; };
};
`
	configured, err := managedBINDOptions(base, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"recursion no;",
		"allow-recursion { none; };",
		"allow-query-cache { none; };",
		"allow-transfer { none; };",
	} {
		if strings.Count(configured, want) != 1 {
			t.Fatalf("configured options missing %q:\n%s", want, configured)
		}
	}
	again, err := managedBINDOptions(configured, "")
	if err != nil || again != configured {
		t.Fatalf("managed options not idempotent: %v\n%s", err, again)
	}
}

func TestManagedBINDOptionsRejectsAmbiguousOrOperatorOwnedRecursion(t *testing.T) {
	tests := []string{
		`options { recursion yes; };`,
		`options { allow-recursion { any; }; };`,
		`options { }; options { };`,
		`zone "example" { type master; };`,
		"options {\n" + bindOptionsMarkerBegin + "\n};",
	}
	for _, config := range tests {
		if _, err := managedBINDOptions(config, ""); err == nil {
			t.Fatalf("unsafe configuration was accepted: %q", config)
		}
	}
}

func TestBINDConfigurationLexerIgnoresQuotedAndCommentedSyntax(t *testing.T) {
	config := `
// options { recursion yes; };
/* options { allow-recursion { any; }; }; */
options {
    directory "/value/with/{/and/options";
    # recursion yes;
};
`
	open, close, err := bindOptionsBlock(config)
	if err != nil || open >= close || config[open] != '{' || config[close] != '}' {
		t.Fatalf("open=%d close=%d err=%v", open, close, err)
	}
	if _, err := managedBINDOptions(config, ""); err != nil {
		t.Fatal(err)
	}
}

func TestManagedBINDOptionsAllowsTransfersOnlyFromPairedPrimary(t *testing.T) {
	base := "options {\n    directory \"/var/cache/bind\";\n};\n"
	configured, err := managedBINDOptions(base, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(configured, "allow-transfer { 192.0.2.10/32; };") != 1 ||
		strings.Contains(configured, "allow-transfer { none; };") {
		t.Fatalf("paired-secondary transfer ACL is not exact:\n%s", configured)
	}
	again, err := managedBINDOptions(configured, "192.0.2.10")
	if err != nil || again != configured {
		t.Fatalf("paired-secondary options are not idempotent: %v\n%s", err, again)
	}
	if _, err := managedBINDOptions(configured, "192.0.2.11"); err == nil {
		t.Fatal("an existing managed transfer peer was silently retargeted")
	}
	for _, peer := range []string{"192.0.2.010", "127.0.0.1", "2001:db8::1"} {
		if _, err := managedBINDOptions(base, peer); err == nil {
			t.Fatalf("unsafe transfer peer %q was accepted", peer)
		}
	}
}

func TestManagedBINDOptionsMigratesOnlyExactLegacyBlock(t *testing.T) {
	legacy := `options {
	// BEGIN CELIKPANEL MANAGED BIND OPTIONS
	recursion no;
	allow-recursion { none; };
	allow-query-cache { none; };
	// END CELIKPANEL MANAGED BIND OPTIONS
};
`
	configured, err := managedBINDOptions(legacy, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(configured, "allow-transfer { 192.0.2.10/32; };") != 1 {
		t.Fatalf("legacy block was not upgraded exactly:\n%s", configured)
	}
	modified := strings.Replace(legacy, "recursion no;", "recursion yes;", 1)
	if _, err := managedBINDOptions(modified, "192.0.2.10"); err == nil {
		t.Fatal("modified legacy block was accepted")
	}
}

func TestManagedBINDOptionsRejectsExternalTransferDirective(t *testing.T) {
	base := "options { allow-transfer { any; }; };"
	if _, err := managedBINDOptions(base, "192.0.2.10"); err == nil {
		t.Fatal("operator-owned allow-transfer was accepted")
	}
	configured, err := managedBINDOptions("options { };", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	duplicated := strings.Replace(
		configured, "options {", "options { allow-transfer { any; };", 1,
	)
	if _, err := managedBINDOptions(duplicated, "192.0.2.10"); err == nil {
		t.Fatal("external allow-transfer beside the managed block was accepted")
	}
}
