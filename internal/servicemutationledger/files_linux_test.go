//go:build linux

package servicemutationledger

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func evidenceFixture(t *testing.T) (string, FileOwner) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "evidence.json")
	if err := os.WriteFile(path, []byte("retained-owner-evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	return path, FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
}
func TestReadEvidenceDistinguishesAbsenceAndUncertainty(t *testing.T) {
	path, owner := evidenceFixture(t)
	raw, found, err := ReadFile(path, 512, owner)
	if err != nil || !found || string(raw) != "retained-owner-evidence" {
		t.Fatalf("read: %q %v %v", raw, found, err)
	}
	raw, found, err = ReadFile(filepath.Join(filepath.Dir(path), "absent"), 512, owner)
	if err != nil || found || raw != nil {
		t.Fatalf("absent final: %q %v %v", raw, found, err)
	}
	raw, found, err = ReadFile(filepath.Join(filepath.Dir(path), "absent-parent", "absent"), 512, owner)
	if err == nil || found || raw != nil {
		t.Fatal("missing parent interpreted as absent evidence")
	}
}
func TestReadEvidenceRejectsUnsafeMetadataAndPaths(t *testing.T) {
	for name, edit := range map[string]func(string) string{
		"readable file": func(p string) string {
			if e := os.Chmod(p, 0640); e != nil {
				t.Fatal(e)
			}
			return p
		},
		"setuid file": func(p string) string {
			if e := os.Chmod(p, 0600|os.ModeSetuid); e != nil {
				t.Fatal(e)
			}
			return p
		},
		"parent readable": func(p string) string {
			if e := os.Chmod(filepath.Dir(p), 0750); e != nil {
				t.Fatal(e)
			}
			return p
		},
		"final symlink": func(p string) string {
			if e := os.Symlink(p, p+"-link"); e != nil {
				t.Fatal(e)
			}
			return p + "-link"
		},
		"parent symlink": func(p string) string {
			alias := filepath.Dir(p) + "-link"
			if e := os.Symlink(filepath.Dir(p), alias); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.Remove(alias) })
			return filepath.Join(alias, filepath.Base(p))
		},
		"hard link": func(p string) string {
			if e := os.Link(p, p+"-link"); e != nil {
				t.Fatal(e)
			}
			return p
		},
		"directory": func(p string) string {
			if e := os.Remove(p); e != nil {
				t.Fatal(e)
			}
			if e := os.Mkdir(p, 0600); e != nil {
				t.Fatal(e)
			}
			return p
		},
		"relative": func(p string) string { return "evidence.json" },
	} {
		t.Run(name, func(t *testing.T) {
			p, o := evidenceFixture(t)
			p = edit(p)
			raw, found, err := ReadFile(p, 512, o)
			if err == nil || found || raw != nil {
				t.Fatalf("unsafe path accepted: %v %v", found, err)
			}
		})
	}
	p, o := evidenceFixture(t)
	for _, size := range []int64{-1, 0, 4} {
		if _, _, err := ReadFile(p, size, o); err == nil {
			t.Fatal("unsafe size accepted")
		}
	}
	o.GID++
	if _, _, err := ReadFile(p, 512, o); err == nil {
		t.Fatal("unestablished identity accepted")
	}
}
func TestReadEvidenceRefusesChangesObservedAfterRead(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, string){
		"content": func(t *testing.T, p string) {
			if e := os.WriteFile(p, []byte("different-owner-value"), 0600); e != nil {
				t.Fatal(e)
			}
		},
		"inode": func(t *testing.T, p string) {
			if e := os.WriteFile(p+"-new", []byte("retained-owner-evidence"), 0600); e != nil {
				t.Fatal(e)
			}
			if e := os.Rename(p+"-new", p); e != nil {
				t.Fatal(e)
			}
		},
		"mode": func(t *testing.T, p string) {
			if e := os.Chmod(p, 0640); e != nil {
				t.Fatal(e)
			}
		},
		"link count": func(t *testing.T, p string) {
			if e := os.Link(p, p+"-link"); e != nil {
				t.Fatal(e)
			}
		},
		"parent": func(t *testing.T, p string) {
			parent := filepath.Dir(p)
			if e := os.Rename(parent, parent+"-old"); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.Rename(parent+"-old", parent) })
			if e := os.Mkdir(parent, 0700); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.Remove(parent) })
		},
		"parent symlink": func(t *testing.T, p string) {
			parent := filepath.Dir(p)
			if e := os.Rename(parent, parent+"-old"); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.Rename(parent+"-old", parent) })
			if e := os.Symlink(parent+"-old", parent); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { os.Remove(parent) })
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, o := evidenceFixture(t)
			raw, found, err := readFile(p, 512, o, func() { edit(t, p) })
			if err == nil || found || raw != nil {
				t.Fatalf("changed evidence accepted: %v %v", found, err)
			}
		})
	}
}
func TestReadEvidenceFIFOIsBounded(t *testing.T) {
	if path := os.Getenv("CELIKPANEL_LEDGER_FIFO_TEST"); path != "" {
		_, _, err := ReadFile(path, 512, FileOwner{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())})
		if err == nil {
			t.Fatal("FIFO accepted")
		}
		return
	}
	p, _ := evidenceFixture(t)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadEvidenceFIFOIsBounded$")
	cmd.Env = append(os.Environ(), "CELIKPANEL_LEDGER_FIFO_TEST="+p)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO proof failed: %v %s", err, output)
	}
	if ctx.Err() != nil {
		t.Fatal("FIFO read hung")
	}
}
func TestReadEvidenceNeverNormalizesOwnerChanges(t *testing.T) {
	p, o := evidenceFixture(t)
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0640); e != nil {
		t.Fatal(e)
	}
	_, _, err := ReadFile(p, 512, o)
	if err == nil || strings.Contains(err.Error(), string(before)) {
		t.Fatal("unsafe acceptance or data disclosed")
	}
	after, e := os.ReadFile(p)
	info, statErr := os.Stat(p)
	if e != nil || statErr != nil || !bytes.Equal(before, after) || info.Mode().Perm() != 0640 {
		t.Fatal("reader normalized owner evidence")
	}
}

func TestEmptyInitialDirectoryCannotSupplyAlternateOwnerEvidence(t *testing.T) {
	p, o := evidenceFixture(t)
	dir := filepath.Dir(p)
	if err := VerifyEmptyDirectory(dir, o); err == nil {
		t.Fatal("nonempty residue accepted")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEmptyDirectory(dir, o); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("alternate owner ledger"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEmptyDirectory(dir, o); err == nil {
		t.Fatal("new evidence admitted as empty residue")
	}
}
