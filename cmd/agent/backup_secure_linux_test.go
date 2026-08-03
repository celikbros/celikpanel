//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testTarEntry struct {
	name     string
	typeflag byte
	linkname string
	content  string
}

func writeTestBackupArchive(
	t *testing.T,
	base, scope, name string,
	entries []testTarEntry,
) {
	t.Helper()
	file, cleanup, err := secureCreateBackupFile(base, scope, name)
	if err != nil {
		t.Fatal(err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			cleanup()
		}
	}()
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     entry.name,
			Typeflag: entry.typeflag,
			Linkname: entry.linkname,
			Mode:     0o644,
			Size:     int64(len(entry.content)),
		}
		if entry.typeflag == tar.TypeDir {
			header.Size = 0
			header.Mode = 0o755
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.content != "" {
			if _, err := io.WriteString(tarWriter, entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	keep = true
}

func TestSecureBackupRoundTripAndTenantIsolation(t *testing.T) {
	base := filepath.Join(t.TempDir(), "backups")
	source := filepath.Join(t.TempDir(), "source")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(target, "nested", "hello.txt"), []byte("old"), 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "hello.txt"), []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	const (
		scopeA = "subscriptions/10/domains/20"
		scopeB = "subscriptions/10/domains/21"
		name   = "files_20260727_120000.tar.gz"
	)
	size, err := secureCreateFilesBackup(source, base, scopeA, name)
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 {
		t.Fatalf("backup size = %d", size)
	}
	records, err := secureListBackupFiles(base, scopeA)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != name {
		t.Fatalf("scope A records = %+v", records)
	}
	records, err = secureListBackupFiles(base, scopeB)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("scope B saw scope A backups: %+v", records)
	}
	if _, _, err := secureReadBackupFile(base, scopeB, name, 1<<20); err == nil {
		t.Fatal("other tenant scope read a backup")
	}
	if err := secureRestoreFilesBackup(target, base, scopeA, name); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(target, "nested", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello" {
		t.Fatalf("restored content = %q", content)
	}
}

func TestSecureBackupCreationRejectsSymlinkAndHardlinkEscapes(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, source, outside string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, source, outside string) {
				t.Helper()
				if err := os.Symlink(
					filepath.Join(outside, "secret.txt"),
					filepath.Join(source, "escape"),
				); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hardlink",
			setup: func(t *testing.T, source, outside string) {
				t.Helper()
				if err := os.Link(
					filepath.Join(outside, "secret.txt"),
					filepath.Join(source, "escape"),
				); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "backups")
			root := t.TempDir()
			source := filepath.Join(root, "source")
			outside := filepath.Join(root, "outside")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			test.setup(t, source, outside)
			if _, err := secureCreateFilesBackup(
				source, base, "subscriptions/1/domains/2",
				"files_20260727_120001.tar.gz",
			); err == nil {
				t.Fatalf("%s escape was archived", test.name)
			}
			records, err := secureListBackupFiles(base, "subscriptions/1/domains/2")
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 0 {
				t.Fatalf("partial unsafe backup was retained: %+v", records)
			}
		})
	}
}

func TestSecureRestoreRejectsArchiveTraversalAndLinkEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []testTarEntry
	}{
		{
			name: "traversal",
			entries: []testTarEntry{{
				name: "../escaped.txt", typeflag: tar.TypeReg, content: "pwned",
			}},
		},
		{
			name: "absolute",
			entries: []testTarEntry{{
				name: "/tmp/escaped.txt", typeflag: tar.TypeReg, content: "pwned",
			}},
		},
		{
			name: "symlink",
			entries: []testTarEntry{{
				name: "escape", typeflag: tar.TypeSymlink, linkname: "../outside",
			}},
		},
		{
			name: "hardlink",
			entries: []testTarEntry{{
				name: "escape", typeflag: tar.TypeLink, linkname: "../outside",
			}},
		},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "backups")
			target := filepath.Join(t.TempDir(), "target")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			name := "files_20260727_12000" + string(rune('0'+i)) + ".tar.gz"
			writeTestBackupArchive(
				t, base, "subscriptions/1/domains/2", name, test.entries,
			)
			if err := secureRestoreFilesBackup(
				target, base, "subscriptions/1/domains/2", name,
			); err == nil {
				t.Fatalf("%s archive entry was accepted", test.name)
			}
		})
	}
}

func TestSecureRestoreRejectsPreexistingSymlinkAndHardlinkTargets(t *testing.T) {
	t.Run("symlink parent", func(t *testing.T) {
		root := t.TempDir()
		base := filepath.Join(root, "backups")
		target := filepath.Join(root, "target")
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(target, "escape")); err != nil {
			t.Fatal(err)
		}
		const name = "files_20260727_120010.tar.gz"
		writeTestBackupArchive(t, base, "subscriptions/1/domains/2", name, []testTarEntry{{
			name: "escape/pwned.txt", typeflag: tar.TypeReg, content: "pwned",
		}})
		if err := secureRestoreFilesBackup(
			target, base, "subscriptions/1/domains/2", name,
		); err == nil {
			t.Fatal("restore followed a preexisting symlink parent")
		}
		if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(err) {
			t.Fatalf("outside file was created: %v", err)
		}
	})

	t.Run("hardlink file", func(t *testing.T) {
		root := t.TempDir()
		base := filepath.Join(root, "backups")
		target := filepath.Join(root, "target")
		outside := filepath.Join(root, "outside.txt")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(outside, filepath.Join(target, "victim.txt")); err != nil {
			t.Fatal(err)
		}
		const name = "files_20260727_120011.tar.gz"
		writeTestBackupArchive(t, base, "subscriptions/1/domains/2", name, []testTarEntry{{
			name: "victim.txt", typeflag: tar.TypeReg, content: "pwned",
		}})
		if err := secureRestoreFilesBackup(
			target, base, "subscriptions/1/domains/2", name,
		); err == nil {
			t.Fatal("restore overwrote a preexisting hardlink")
		}
		content, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "original" {
			t.Fatalf("outside hardlink target changed: %q", content)
		}
	})
}

func TestBackupStorageRejectsSymlinkAndHardlinkFiles(t *testing.T) {
	for _, linkType := range []string{"symlink", "hardlink"} {
		t.Run(linkType, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, "backups")
			scope := "subscriptions/1/domains/2"
			scopeDir := filepath.Join(base, filepath.FromSlash(scope))
			if err := os.MkdirAll(scopeDir, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(root, "outside.tar.gz")
			if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
				t.Fatal(err)
			}
			name := "files_20260727_120020.tar.gz"
			target := filepath.Join(scopeDir, name)
			var err error
			if linkType == "symlink" {
				err = os.Symlink(outside, target)
			} else {
				err = os.Link(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := secureOpenBackupFile(base, scope, name); err == nil {
				t.Fatalf("%s backup storage entry was opened", linkType)
			}
			if err := secureDeleteBackupFile(base, scope, name); err == nil {
				t.Fatalf("%s backup storage entry was deleted through RPC", linkType)
			}
			content, err := os.ReadFile(outside)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), "secret") {
				t.Fatalf("outside content changed: %q", content)
			}
		})
	}
}

func TestBackupBaseSymlinkFailsClosed(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "backups-link")
	if err := os.Symlink(outside, base); err != nil {
		t.Fatal(err)
	}
	if _, _, err := secureCreateBackupFile(
		base, "subscriptions/1/domains/2", "files_20260727_120030.tar.gz",
	); err == nil {
		t.Fatal("symlink backup base was accepted")
	}
}
