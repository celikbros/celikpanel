package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestControlPlaneModesAreMutuallyExclusive(t *testing.T) {
	newModes := []struct {
		name  string
		apply func(*panelCommandModes)
	}{
		{
			name:  "generate-control-plane-key",
			apply: func(modes *panelCommandModes) { modes.generateControlPlaneKey = true },
		},
		{
			name:  "create-control-plane-archive",
			apply: func(modes *panelCommandModes) { modes.createControlPlaneArchive = true },
		},
		{
			name:  "restore-control-plane-archive",
			apply: func(modes *panelCommandModes) { modes.restoreControlPlaneArchive = true },
		},
	}

	// Each new mode is a valid one-shot mode on its own.
	for _, mode := range newModes {
		t.Run("alone "+mode.name, func(t *testing.T) {
			modes := panelCommandModes{}
			mode.apply(&modes)
			if err := validatePanelCommandModes(modes); err != nil {
				t.Fatalf("%s alone was refused: %v", mode.name, err)
			}
		})
	}

	// No two of them may be combined.
	for _, left := range newModes {
		for _, right := range newModes {
			if left.name == right.name {
				continue
			}
			t.Run(left.name+" with "+right.name, func(t *testing.T) {
				modes := panelCommandModes{}
				left.apply(&modes)
				right.apply(&modes)
				err := validatePanelCommandModes(modes)
				if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("error=%v, want a mutual-exclusion refusal", err)
				}
				if !strings.Contains(err.Error(), left.name) || !strings.Contains(err.Error(), right.name) {
					t.Fatalf("error=%v, want both mode names", err)
				}
			})
		}
	}

	// Nor may any of them be combined with an existing one-shot mode.
	existing := []struct {
		name  string
		apply func(*panelCommandModes)
	}{
		{name: "create-admin", apply: func(modes *panelCommandModes) { modes.createAdmin = true }},
		{name: "count-users", apply: func(modes *panelCommandModes) { modes.countUsers = true }},
		{name: "migrate-only", apply: func(modes *panelCommandModes) { modes.migrateOnly = true }},
		{
			name:  "service-operation-snapshot-create-or-restore",
			apply: func(modes *panelCommandModes) { modes.createOrRestore = true },
		},
		{
			name:  "validate-admin-credentials-file",
			apply: func(modes *panelCommandModes) { modes.validateAdminCredentials = true },
		},
	}
	for _, mode := range newModes {
		for _, other := range existing {
			t.Run(mode.name+" with "+other.name, func(t *testing.T) {
				modes := panelCommandModes{}
				mode.apply(&modes)
				other.apply(&modes)
				err := validatePanelCommandModes(modes)
				if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
					t.Fatalf("error=%v, want a mutual-exclusion refusal", err)
				}
				if !strings.Contains(err.Error(), mode.name) || !strings.Contains(err.Error(), other.name) {
					t.Fatalf("error=%v, want both mode names", err)
				}
			})
		}
	}

	// And none of them may be combined with a runtime flag such as demo.
	for _, mode := range newModes {
		for _, runtimeFlag := range []struct {
			name  string
			apply func(*panelCommandModes)
		}{
			{name: "demo", apply: func(modes *panelCommandModes) { modes.demo = true }},
			{name: "insecure-cookies", apply: func(modes *panelCommandModes) { modes.insecureCookies = true }},
		} {
			t.Run(mode.name+" with "+runtimeFlag.name, func(t *testing.T) {
				modes := panelCommandModes{}
				mode.apply(&modes)
				runtimeFlag.apply(&modes)
				err := validatePanelCommandModes(modes)
				if err == nil || !strings.Contains(err.Error(), "runtime flags") {
					t.Fatalf("error=%v, want a runtime-flag refusal", err)
				}
				if !strings.Contains(err.Error(), mode.name) || !strings.Contains(err.Error(), runtimeFlag.name) {
					t.Fatalf("error=%v, want both names", err)
				}
			})
		}
	}
}

func TestControlPlaneCommandFlags(t *testing.T) {
	inherited := inheritedControlPlaneKeyFileFlag{set: true, value: "-"}
	wrongValue := inheritedControlPlaneKeyFileFlag{set: true, value: "/root/key.txt"}

	tests := []struct {
		name      string
		generate  bool
		create    string
		restore   string
		keyFile   inheritedControlPlaneKeyFileFlag
		wantError string
	}{
		{name: "nothing requested"},
		{name: "generate alone", generate: true},
		{name: "create with key", create: "/root/a.cpbak", keyFile: inherited},
		{name: "restore with key", restore: "/root/a.cpbak", keyFile: inherited},
		{
			name:      "create without key",
			create:    "/root/a.cpbak",
			wantError: "requires --control-plane-key-file=-",
		},
		{
			name:      "restore without key",
			restore:   "/root/a.cpbak",
			wantError: "requires --control-plane-key-file=-",
		},
		{
			name:      "create and restore together",
			create:    "/root/a.cpbak",
			restore:   "/root/b.cpbak",
			keyFile:   inherited,
			wantError: "mutually exclusive",
		},
		{
			name:      "key without a mode",
			keyFile:   inherited,
			wantError: "requires an archive create or restore mode",
		},
		{
			name:      "key with generate",
			generate:  true,
			create:    "/root/a.cpbak",
			keyFile:   inherited,
			wantError: "never reads a key",
		},
		{
			name:      "key file that is not stdin",
			create:    "/root/a.cpbak",
			keyFile:   wrongValue,
			wantError: "inherited on stdin",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateControlPlaneCommandFlags(
				test.generate,
				test.create,
				test.restore,
				test.keyFile,
			)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestControlPlaneKeyFileArgumentSpellings(t *testing.T) {
	for _, argument := range []string{
		"--control-plane-key-file",
		"--control-plane-key-file=/root/key.txt",
		"--control-plane-key-file=",
		"--control-plane-key-file=--",
		"-control-plane-key-file=-",
	} {
		t.Run(argument, func(t *testing.T) {
			if err := validateControlPlaneKeyFileArgumentSpellings([]string{argument}); err == nil {
				t.Fatalf("the spelling %q was accepted", argument)
			}
		})
	}
	for _, arguments := range [][]string{
		{},
		{"--generate-control-plane-key"},
		{"--create-control-plane-archive=/root/a.cpbak", controlPlaneKeyFileArgument},
	} {
		if err := validateControlPlaneKeyFileArgumentSpellings(arguments); err != nil {
			t.Fatalf("the arguments %v were refused: %v", arguments, err)
		}
	}
}

func TestControlPlaneKeyFlagIsSetOnlyOnce(t *testing.T) {
	var flagValue inheritedControlPlaneKeyFileFlag
	if err := flagValue.Set("-"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := flagValue.Set("-"); err == nil {
		t.Fatal("the key file flag was accepted twice")
	}
	if flagValue.String() != "-" {
		t.Fatalf("flag value %q", flagValue.String())
	}
}

func TestControlPlaneGenerateKeyCommandPrintsOneUsableKey(t *testing.T) {
	var output bytes.Buffer
	if err := runGenerateControlPlaneKey(&output); err != nil {
		t.Fatalf("generate: %v", err)
	}
	lines := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("printed %d lines, want exactly one", len(lines))
	}
	if _, err := parseControlPlaneKey(lines[0]); err != nil {
		t.Fatalf("the printed key does not parse: %v", err)
	}
}

func TestControlPlaneCommandContractNamesEveryMode(t *testing.T) {
	contract := controlPlaneCommandContract()
	for _, expected := range []string{
		generateControlPlaneKeyArgument,
		"--create-control-plane-archive=",
		"--restore-control-plane-archive=",
		controlPlaneKeyFileArgument,
		"root",
	} {
		if !strings.Contains(contract, expected) {
			t.Fatalf("the contract does not mention %q: %s", expected, contract)
		}
	}
}
