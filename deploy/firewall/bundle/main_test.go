//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/firewallruntime"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "firewall-restore")
	if err := os.WriteFile(binary, []byte("reviewed binary"), 0755); err != nil {
		t.Fatal(err)
	}
	return binary, filepath.Join(root, "firewall-runtime")
}
func TestAssembleDeterministicAndRetainsPreviousBuild(t *testing.T) {
	binary, out := fixture(t)
	first, err := assemble(binary, out)
	if err != nil {
		t.Fatal(err)
	}
	again, err := assemble(binary, out)
	if err != nil || first != again {
		t.Fatalf("not repeatable: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, firewallruntime.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binary, []byte("next reviewed binary"), 0755); err != nil {
		t.Fatal(err)
	}
	next, err := assemble(binary, out)
	if err != nil || next == first {
		t.Fatalf("no distinct next artifact: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(out), ".firewall-runtime.build-*"))
	if err != nil || len(matches) != 1 {
		t.Fatal("old build was not retained")
	}
	old, err := os.ReadFile(filepath.Join(matches[0], firewallruntime.ManifestName))
	if err != nil || string(old) != string(raw) {
		t.Fatal("retained evidence differs")
	}
	if id, err := verify(out); err != nil || id != next {
		t.Fatal("next artifact not verified")
	}
}
func TestUnrecognizedOutputAndLinkedInputArePreserved(t *testing.T) {
	binary, out := fixture(t)
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(out, "owner-data")
	if err := os.WriteFile(note, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := assemble(binary, out); err == nil {
		t.Fatal("unknown output replaced")
	}
	if raw, err := os.ReadFile(note); err != nil || string(raw) != "keep" {
		t.Fatal("owner file changed")
	}

}
func TestLinkedInputRefusedBeforeOutputExists(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			binary, out := fixture(t)
			linked := binary + ".link"
			var err error
			if kind == "symlink" {
				err = os.Symlink(binary, linked)
			} else {
				err = os.Link(binary, linked)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = assemble(linked, out); err == nil {
				t.Fatal("linked source accepted")
			}
			if _, err = os.Lstat(out); !os.IsNotExist(err) {
				t.Fatal("output created after refused input")
			}
			if raw, err := os.ReadFile(binary); err != nil || string(raw) != "reviewed binary" {
				t.Fatal("source changed")
			}
		})
	}

}
func TestChangedArtifactIsNotOverwritten(t *testing.T) {
	binary, out := fixture(t)
	if _, err := assemble(binary, out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "celikpanel-firewall-restore.service")
	if err := os.WriteFile(path, []byte("owner edit"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := assemble(binary, out); err == nil {
		t.Fatal("edited unit overwritten")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "owner edit" {
		t.Fatal("edit lost")
	}
}
