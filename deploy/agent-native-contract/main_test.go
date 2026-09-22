package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidBuildCannotCreateCompatibilityClaim(t *testing.T) {
	for _, kind := range []string{"arbitrary-bytes", "symbolic-input", "empty-input", "wrong-name", "unknown-source"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "agent")
			out := filepath.Join(root, "agent-native-contract.json")
			commit := strings.Repeat("a", 40)
			if err := os.WriteFile(binary, []byte("not a management Agent build"), 0755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symbolic-input":
				if err := os.Rename(binary, binary+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(binary+".saved", binary); err != nil {
					t.Fatal(err)
				}
			case "empty-input":
				if err := os.Truncate(binary, 0); err != nil {
					t.Fatal(err)
				}
			case "wrong-name":
				binary = filepath.Join(root, "panel")
			case "unknown-source":
				commit = "unknown"
			}
			if err := assemble(binary, commit, out); err == nil {
				t.Fatal("invalid input gained compatibility")
			}
			if _, err := os.Lstat(out); !os.IsNotExist(err) {
				t.Fatal("declaration published on refusal")
			}
		})
	}
}

// Explicit opt-in native artifact acceptance: ordinary unit fixtures cannot
// prove that the real management build is distinguishable from the mail helper.
func TestReviewedAgentAndHelperArtifacts(t *testing.T) {
	normal, helper := os.Getenv("CP_CONTRACT_AGENT"), os.Getenv("CP_CONTRACT_HELPER")
	if normal == "" || helper == "" {
		t.Skip("requires real freshly built management/helper artifacts")
	}
	for _, tt := range []struct {
		name, path string
		allow      bool
	}{{"management", normal, true}, {"renewal-helper", helper, false}} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			binary, out := filepath.Join(root, "agent"), filepath.Join(root, "agent-native-contract.json")
			raw, e := os.ReadFile(tt.path)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(binary, raw, 0755); e != nil {
				t.Fatal(e)
			}
			e = assemble(binary, strings.Repeat("a", 40), out)
			if (e == nil) != tt.allow {
				t.Fatalf("allow=%v: %v", tt.allow, e)
			}
			if !tt.allow {
				if _, e = os.Stat(out); !os.IsNotExist(e) {
					t.Fatal("helper gained management authority")
				}
				return
			}
			first, e := os.ReadFile(out)
			if e != nil {
				t.Fatal(e)
			}
			if e = assemble(binary, strings.Repeat("a", 40), out); e != nil {
				t.Fatal(e)
			}
			second, e := os.ReadFile(out)
			if e != nil || string(first) != string(second) {
				t.Fatal("nonrepeatable contract")
			}
			if e = os.WriteFile(out, []byte("owner evidence"), 0600); e != nil {
				t.Fatal(e)
			}
			if e = assemble(binary, strings.Repeat("a", 40), out); e == nil {
				t.Fatal("owner output overwritten")
			}
			retained, e := os.ReadFile(out)
			if e != nil || string(retained) != "owner evidence" {
				t.Fatal("owner evidence changed")
			}
		})
	}
}
