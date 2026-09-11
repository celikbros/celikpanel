//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCertbotReissuanceNormalizesOnlyTrustedManagedDirectories(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root metadata")
	}
	root := t.TempDir()
	lineage := panelCertLineageName("panel.example.test")
	const gid = 65534
	for _, path := range []string{"live", "archive", "renewal", "renewal-hooks", "renewal-hooks/deploy", "live/" + lineage, "archive/" + lineage, "archive/unrelated"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(full, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(full, 0, gid); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(root, "archive", lineage, "privkey1.pem")
	if err := os.WriteFile(old, []byte("unchanged previous private material"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(old, 0, gid); err != nil {
		t.Fatal(err)
	}
	if err := normalizeManagedCertbotSourceDirectories(root, lineage, gid); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"live", "archive", "renewal", "renewal-hooks", "renewal-hooks/deploy", "live/" + lineage, "archive/" + lineage} {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Sys().(*syscall.Stat_t).Gid != 0 {
			t.Fatalf("managed directory retained old group: %s", path)
		}
	}
	for _, path := range []string{old, filepath.Join(root, "archive/unrelated")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Sys().(*syscall.Stat_t).Gid != gid {
			t.Fatal("repair crossed the exact managed directory boundary")
		}
	}
	if contents, err := os.ReadFile(old); err != nil || string(contents) != "unchanged previous private material" {
		t.Fatal("old certificate source was adopted or modified")
	}
}

func TestCertbotOwnershipRepairRejectsUnsafePathsBeforeAnyChange(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root metadata")
	}
	for _, kind := range []string{"symlink", "wrong-owner", "wrong-group", "writable", "renewal-symlink", "deploy-writable"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			lineage := mailHostCertLineageName("mail.example.test")
			const gid = 65534
			for _, path := range []string{"live", "archive", "live/" + lineage} {
				if err := os.MkdirAll(filepath.Join(root, path), 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.Chown(filepath.Join(root, path), 0, gid); err != nil {
					t.Fatal(err)
				}
			}
			bad := filepath.Join(root, "archive", lineage)
			if kind == "renewal-symlink" {
				bad = filepath.Join(root, "renewal")
			}
			if kind == "deploy-writable" {
				if err := os.MkdirAll(filepath.Join(root, "renewal-hooks"), 0750); err != nil {
					t.Fatal(err)
				}
				bad = filepath.Join(root, "renewal-hooks", "deploy")
			}
			if kind == "symlink" || kind == "renewal-symlink" {
				if err := os.Symlink(t.TempDir(), bad); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(bad, 0750); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "wrong-owner":
					if err := os.Chown(bad, gid, gid); err != nil {
						t.Fatal(err)
					}
				case "wrong-group":
					if err := os.Chown(bad, 0, gid-1); err != nil {
						t.Fatal(err)
					}
				case "writable", "deploy-writable":
					if err := os.Chmod(bad, 0770); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := normalizeManagedCertbotSourceDirectories(root, lineage, gid); err == nil {
				t.Fatal("unsafe Certbot tree was adopted")
			}
			info, err := os.Stat(filepath.Join(root, "live"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Sys().(*syscall.Stat_t).Gid != gid {
				t.Fatal("failed preflight changed an earlier directory")
			}
		})
	}
}
