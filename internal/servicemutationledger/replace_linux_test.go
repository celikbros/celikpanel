//go:build linux

package servicemutationledger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func exactEvidenceFixture(t *testing.T) (string, FileOwner) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "dns-engine-switch-journal.json")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	return path, FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
}

func TestReplaceFileExactPublishesAndVerifiesPrivateCheckpoint(t *testing.T) {
	path, owner := exactEvidenceFixture(t)
	if err := ReplaceFileExact(path, []byte("before"), []byte("after"), 128, owner); err != nil {
		t.Fatal(err)
	}
	got, exists, err := ReadFile(path, 128, owner)
	if err != nil || !exists || !bytes.Equal(got, []byte("after")) {
		t.Fatalf("checkpoint not verified: %q %v %v", got, exists, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("checkpoint left stage residue: %v %v", entries, err)
	}
}

func TestReplaceFileExactRefusesMissingUnsafeOrDifferentPreimage(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, string) string{
		"missing":   func(t *testing.T, path string) string { return path + "-missing" },
		"different": func(t *testing.T, path string) string { return path },
		"readable": func(t *testing.T, path string) string {
			if err := os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}
			return path
		},
		"symlink": func(t *testing.T, path string) string {
			if err := os.Symlink(path, path+"-link"); err != nil {
				t.Fatal(err)
			}
			return path + "-link"
		},
		"hardlink": func(t *testing.T, path string) string {
			if err := os.Link(path, path+"-link"); err != nil {
				t.Fatal(err)
			}
			return path
		},
		"untrusted parent": func(t *testing.T, path string) string {
			if err := os.Chmod(filepath.Dir(path), 0750); err != nil {
				t.Fatal(err)
			}
			return path
		},
	} {
		t.Run(name, func(t *testing.T) {
			path, owner := exactEvidenceFixture(t)
			original := path
			path = edit(t, path)
			before := []byte("before")
			if name == "different" {
				before = []byte("other")
			}
			if err := ReplaceFileExact(path, before, []byte("after"), 128, owner); err == nil {
				t.Fatal("unsafe evidence accepted")
			}
			got, err := os.ReadFile(original)
			if err != nil || !bytes.Equal(got, []byte("before")) {
				t.Fatalf("refusal changed existing evidence: %q %v", got, err)
			}
		})
	}
}

func TestReplaceFileExactRefusesPrepublicationOwnerEdit(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, string){
		"content": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("owner-edit"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"inode": func(t *testing.T, path string) {
			if err := os.WriteFile(path+"-new", []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+"-new", path); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, path string) {
			parent := filepath.Dir(path)
			if err := os.Rename(parent, parent+"-old"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Rename(parent+"-old", parent) })
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(parent) })
		},
	} {
		t.Run(name, func(t *testing.T) {
			path, owner := exactEvidenceFixture(t)
			err := replaceFileExact(path, []byte("before"), []byte("after"), 128, owner, func() { edit(t, path) })
			if err == nil {
				t.Fatal("owner edit accepted before publication")
			}
		})
	}
}
