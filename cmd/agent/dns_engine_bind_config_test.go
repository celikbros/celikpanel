package main

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestManagedBINDZoneIncludeRejectsCommentedOwnershipMarkers(t *testing.T) {
	path := "/var/cache/bind/celikpanel/current/zones.conf"
	active, err := managedBINDZoneInclude("", path)
	if err != nil {
		t.Fatal(err)
	}
	commented := "/*\n" + active + "*/\n"
	if _, err := managedBINDZoneInclude(commented, path); err == nil {
		t.Fatalf("commented managed include was accepted:\n%s", commented)
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

func TestManagedBINDOptionsRejectsCommentedOwnershipMarkers(t *testing.T) {
	configured, err := managedBINDOptions("options { };\n", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(configured, bindOptionsMarkerBegin)
	end := strings.Index(configured, bindOptionsMarkerEnd) + len(bindOptionsMarkerEnd)
	if start < 0 || end < start {
		t.Fatal("managed block was not rendered")
	}
	commented := configured[:start] + "/*\n" + configured[start:end] +
		"\n*/" + configured[end:]
	if _, err := managedBINDOptions(commented, "192.0.2.10"); err == nil {
		t.Fatalf("commented managed options were accepted:\n%s", commented)
	}
	if exactLegacyManagedBINDOptions(commented) {
		t.Fatalf("commented managed options were classified as released authority:\n%s", commented)
	}
}

func TestManagedBINDOptionsPreservesLexerStateAfterOwnedSpan(t *testing.T) {
	legacy := `options {
	// BEGIN CELIKPANEL MANAGED BIND OPTIONS
	recursion no;
	allow-recursion { none; };
	allow-query-cache { none; };
	// END CELIKPANEL MANAGED BIND OPTIONS /*
	allow-transfer { any; };
};
`
	if _, err := managedBINDOptions(legacy, "192.0.2.10"); err == nil {
		t.Fatal("active transfer ACL hidden behind an inert comment opener was accepted")
	}
	if exactLegacyManagedBINDOptions(legacy) {
		t.Fatal("active transfer ACL was classified as exact released policy")
	}
}

func TestManagedBINDLegacyOptionsRestoresOnlyExactDirectionalBlock(t *testing.T) {
	base := "options { };\n"
	directional, err := managedBINDOptions(base, "")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := managedBINDLegacyOptions(directional)
	if err != nil {
		t.Fatal(err)
	}
	if !exactLegacyManagedBINDOptions(legacy) ||
		strings.Contains(legacy, "allow-transfer") {
		t.Fatalf("released policy was not restored exactly:\n%s", legacy)
	}
	reapplied, err := managedBINDOptions(legacy, "")
	if err != nil || reapplied != directional {
		t.Fatalf("directional round trip changed: err=%v\n%s", err, reapplied)
	}
	tampered := strings.Replace(
		directional, "allow-transfer { none; };",
		"allow-transfer { any; };", 1,
	)
	if _, err := managedBINDLegacyOptions(tampered); err == nil {
		t.Fatal("tampered directional options were restored as released authority")
	}
}

func TestPrepareBINDLegacyConfigMutationHandlesSplitAndCombinedLayouts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve the exact Unix BIND config modes")
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned BIND configuration fixtures")
	}
	for _, combined := range []bool{false, true} {
		name := "split"
		if combined {
			name = "combined"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			optionsPath := filepath.Join(root, "named.conf.options")
			anchorPath := filepath.Join(root, "named.conf.local")
			if combined {
				optionsPath = filepath.Join(root, "named.conf")
				anchorPath = optionsPath
			}
			layout := bindHostLayout{
				GenerationRoot: "/var/cache/bind/celikpanel",
				OptionsConfig:  optionsPath, AnchorConfig: anchorPath,
			}
			options, err := managedBINDOptions("options { };\n", "")
			if err != nil {
				t.Fatal(err)
			}
			anchor := "// operator local config\n"
			if combined {
				anchor = options
			}
			anchor, err = managedBINDZoneInclude(
				anchor, filepath.ToSlash(filepath.Join(
					layout.GenerationRoot, "current", "zones.conf",
				)),
			)
			if err != nil {
				t.Fatal(err)
			}
			if combined {
				options = anchor
			}
			if err := os.WriteFile(optionsPath, []byte(options), 0o644); err != nil {
				t.Fatal(err)
			}
			if !combined {
				if err := os.WriteFile(anchorPath, []byte(anchor), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			mutation, err := prepareBINDLegacyConfigMutation(layout)
			if err != nil {
				t.Fatal(err)
			}
			if err := mutation.apply(); err != nil {
				t.Fatal(err)
			}
			if err := verifyManagedBINDConfigExact(layout, "", true); err != nil {
				t.Fatalf("exact released config rejected: %v", err)
			}
			if err := mutation.restore(); err != nil {
				t.Fatal(err)
			}
			if err := verifyManagedBINDConfigExact(layout, "", false); err != nil {
				t.Fatalf("exact directional config was not restored: %v", err)
			}
		})
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
