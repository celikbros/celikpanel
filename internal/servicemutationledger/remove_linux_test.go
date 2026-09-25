//go:build linux

package servicemutationledger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveFileExactRequiresFullPrivatePreimage(t *testing.T) {
	path, owner := exactEvidenceFixture(t)
	if err := RemoveFileExact(path, []byte("wrong"), 128, owner); err == nil {
		t.Fatal("foreign preimage was removed")
	}
	if err := RemoveFileExact(path, []byte("before"), 128, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("exact evidence still present: %v", err)
	}
	if err := RemoveFileExact(path, []byte("before"), 128, owner); err == nil {
		t.Fatal("missing evidence treated as completed removal")
	}
}

func TestRemoveFileExactRefusesUnsafeOrChangedEvidence(t *testing.T) {
	for _, name := range []string{"symlink", "hardlink", "mode", "owner", "content", "inode", "directory"} {
		t.Run(name, func(t *testing.T) {
			path, owner := exactEvidenceFixture(t)
			requested := path
			var hook func()
			switch name {
			case "symlink":
				requested = path + "-link"
				if err := os.Symlink(path, requested); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+"-link"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
			case "owner":
				owner.GID++
			case "content":
				hook = func() {
					if err := os.WriteFile(path, []byte("owner-edit"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "inode":
				hook = func() {
					if err := os.WriteFile(path+"-new", []byte("before"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(path+"-new", path); err != nil {
						t.Fatal(err)
					}
				}
			case "directory":
				hook = func() {
					parent := filepath.Dir(path)
					if err := os.Rename(parent, parent+"-old"); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Rename(parent+"-old", parent) })
					if err := os.Mkdir(parent, 0700); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Remove(parent) })
				}
			}
			if err := removeFileExact(requested, []byte("before"), 128, owner, hook); err == nil {
				t.Fatal("unsafe evidence removed")
			}
			if name != "directory" {
				got, err := os.ReadFile(path)
				if err != nil || len(got) == 0 || bytes.Equal(got, []byte("after")) {
					t.Fatalf("refusal lost evidence: %q %v", got, err)
				}
			}
		})
	}
}
