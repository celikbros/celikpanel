//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

var defaultMailTLSTestNow = time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)

func defaultMailTLSTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "mail-tls")
	return filepath.Join(dir, "default-cert.pem"), filepath.Join(dir, "default-key.pem")
}

func requireDefaultMailTLSPair(t *testing.T, certPath, keyPath string) {
	t.Helper()
	if err := validateDefaultMailCertPair(
		certPath,
		keyPath,
		"mail.example.test",
		defaultMailTLSTestNow,
	); err != nil {
		t.Fatalf("default mail TLS pair is invalid: %v", err)
	}
	for path, wantMode := range map[string]os.FileMode{
		certPath: 0o644,
		keyPath:  0o600,
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			t.Fatalf("%s mode = %v, want a non-symlink regular file", path, info.Mode())
		}
		if got := info.Mode().Perm(); got != wantMode {
			t.Fatalf("%s mode = %04o, want %04o", path, got, wantMode)
		}
		if links, ok := mailTLSFileLinkCount(info); ok && links != 1 {
			t.Fatalf("%s link count = %d, want 1", path, links)
		}
	}
}

func TestEnsureDefaultMailCertPairIsStableAndSecure(t *testing.T) {
	certPath, keyPath := defaultMailTLSTestPaths(t)
	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
	); err != nil {
		t.Fatal(err)
	}
	requireDefaultMailTLSPair(t, certPath, keyPath)
	beforeCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
	); err != nil {
		t.Fatal(err)
	}
	afterCert, _ := os.ReadFile(certPath)
	afterKey, _ := os.ReadFile(keyPath)
	if !bytes.Equal(beforeCert, afterCert) || !bytes.Equal(beforeKey, afterKey) {
		t.Fatal("a valid fallback pair was unexpectedly regenerated")
	}
	entries, err := os.ReadDir(filepath.Dir(certPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("atomic publication left unexpected artifacts: %v", entries)
	}
}

func TestEnsureDefaultMailCertPairRegeneratesForHostnameChange(t *testing.T) {
	certPath, keyPath := defaultMailTLSTestPaths(t)
	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "old.example.test", defaultMailTLSTestNow, secureWriteConfig,
	); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
	); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Fatal("hostname change did not regenerate the fallback certificate")
	}
	requireDefaultMailTLSPair(t, certPath, keyPath)
}

func TestEnsureDefaultMailCertPairRepairsInvalidSafePairs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, certPath, keyPath string)
	}{
		{
			name: "missing key",
			mutate: func(t *testing.T, _, keyPath string) {
				if err := os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "corrupt certificate",
			mutate: func(t *testing.T, certPath, _ string) {
				if err := os.WriteFile(certPath, []byte("not a certificate"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mismatched private key",
			mutate: func(t *testing.T, _, keyPath string) {
				_, otherKey, err := generateDefaultMailCertPair("other.example.test", defaultMailTLSTestNow)
				if err != nil {
					t.Fatal(err)
				}
				if err := secureWriteConfig(keyPath, otherKey, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "expired pair",
			mutate: func(t *testing.T, certPath, keyPath string) {
				cert, key, err := generateDefaultMailCertPair(
					"old.example.test",
					defaultMailTLSTestNow.AddDate(-3, 0, 0),
				)
				if err != nil {
					t.Fatal(err)
				}
				if err := secureWriteConfig(certPath, cert, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := secureWriteConfig(keyPath, key, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			certPath, keyPath := defaultMailTLSTestPaths(t)
			if err := ensureDefaultMailCertPair(
				certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, certPath, keyPath)
			if err := ensureDefaultMailCertPair(
				certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
			); err != nil {
				t.Fatalf("repair failed: %v", err)
			}
			requireDefaultMailTLSPair(t, certPath, keyPath)
		})
	}
}

func TestEnsureDefaultMailCertPairConvergesAfterInterruptedPublish(t *testing.T) {
	certPath, keyPath := defaultMailTLSTestPaths(t)
	interrupted := errors.New("simulated crash before key publish")
	writes := 0
	writer := func(path string, content []byte, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return interrupted
		}
		return secureWriteConfig(path, content, mode)
	}
	err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, writer,
	)
	if !errors.Is(err, interrupted) {
		t.Fatalf("interrupted publish error = %v, want %v", err, interrupted)
	}
	if _, err := os.Lstat(certPath); err != nil {
		t.Fatalf("first atomic publish did not converge to a certificate: %v", err)
	}
	if _, err := os.Lstat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("interrupted key publish left an unexpected key: %v", err)
	}

	if err := ensureDefaultMailCertPair(
		certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
	); err != nil {
		t.Fatalf("retry after interrupted publish: %v", err)
	}
	requireDefaultMailTLSPair(t, certPath, keyPath)
}

func TestEnsureDefaultMailCertPairRefusesUnsafeExistingFiles(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		certPath, keyPath := defaultMailTLSTestPaths(t)
		if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(target, []byte("do not replace"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, certPath); err != nil {
			t.Fatal(err)
		}
		err := ensureDefaultMailCertPair(
			certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
		)
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("symlink refusal error = %v", err)
		}
		content, _ := os.ReadFile(target)
		if string(content) != "do not replace" {
			t.Fatalf("symlink target changed: %q", content)
		}
	})

	t.Run("hard link", func(t *testing.T) {
		certPath, keyPath := defaultMailTLSTestPaths(t)
		if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(filepath.Dir(certPath), "linked-source")
		if err := os.WriteFile(source, []byte("linked"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(source, certPath); err != nil {
			t.Fatal(err)
		}
		err := ensureDefaultMailCertPair(
			certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
		)
		if err == nil || !strings.Contains(err.Error(), "hard links") {
			t.Fatalf("hard-link refusal error = %v", err)
		}
	})

	t.Run("wrong mode", func(t *testing.T) {
		certPath, keyPath := defaultMailTLSTestPaths(t)
		if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Dir(certPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certPath, []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := ensureDefaultMailCertPair(
			certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig,
		)
		if err == nil || !strings.Contains(err.Error(), "unsafe permissions") {
			t.Fatalf("wrong-mode refusal error = %v", err)
		}
	})
}

func TestEnsureDefaultMailCertPairRefusesUnsafeDirectoryResolution(t *testing.T) {
	t.Run("non canonical", func(t *testing.T) {
		parent := t.TempDir()
		certPath := parent + string(filepath.Separator) + "mail-tls" + string(filepath.Separator) + ".." + string(filepath.Separator) + "mail-tls" + string(filepath.Separator) + "default-cert.pem"
		keyPath := filepath.Join(parent, "mail-tls", "default-key.pem")
		err := ensureDefaultMailCertPair(certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig)
		if err == nil || !strings.Contains(err.Error(), "canonical and absolute") {
			t.Fatalf("non-canonical path error = %v", err)
		}
	})

	t.Run("half production pair", func(t *testing.T) {
		err := ensureDefaultMailCertPair(defaultMailCert, filepath.Join(t.TempDir(), "default-key.pem"), "mail.example.test", defaultMailTLSTestNow, secureWriteConfig)
		if err == nil || !strings.Contains(err.Error(), "exact certificate and private-key pair") {
			t.Fatalf("half-production path error = %v", err)
		}
	})

	t.Run("unsafe immediate parent", func(t *testing.T) {
		parent := t.TempDir()
		if err := os.Chmod(parent, 0o777); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(parent, 0o700)
		certPath := filepath.Join(parent, "mail-tls", "default-cert.pem")
		keyPath := filepath.Join(parent, "mail-tls", "default-key.pem")
		err := ensureDefaultMailCertPair(certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig)
		if err == nil || !strings.Contains(err.Error(), "must not be writable") {
			t.Fatalf("unsafe-parent error = %v", err)
		}
		if _, statErr := os.Lstat(filepath.Dir(certPath)); !os.IsNotExist(statErr) {
			t.Fatalf("unsafe parent allowed target creation: %v", statErr)
		}
	})

	t.Run("symlink directory", func(t *testing.T) {
		parent := t.TempDir()
		target := t.TempDir()
		directory := filepath.Join(parent, "mail-tls")
		if err := os.Symlink(target, directory); err != nil {
			t.Fatal(err)
		}
		err := ensureDefaultMailCertPair(filepath.Join(directory, "default-cert.pem"), filepath.Join(directory, "default-key.pem"), "mail.example.test", defaultMailTLSTestNow, secureWriteConfig)
		if err == nil || (!strings.Contains(err.Error(), "symbolic link") &&
			!strings.Contains(err.Error(), "not a directory")) {
			t.Fatalf("symlink-directory error = %v", err)
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("symlink target was mutated: %v", entries)
		}
	})

	t.Run("openat2 unavailable", func(t *testing.T) {
		previous := mailTLSDirectoryOpenat2
		mailTLSDirectoryOpenat2 = func(int, string, *unix.OpenHow) (int, error) { return -1, unix.ENOSYS }
		defer func() { mailTLSDirectoryOpenat2 = previous }()
		parent := t.TempDir()
		certPath := filepath.Join(parent, "mail-tls", "default-cert.pem")
		keyPath := filepath.Join(parent, "mail-tls", "default-key.pem")
		err := ensureDefaultMailCertPair(certPath, keyPath, "mail.example.test", defaultMailTLSTestNow, secureWriteConfig)
		if err == nil || !strings.Contains(err.Error(), "requires Linux openat2") || !errors.Is(err, unix.ENOSYS) {
			t.Fatalf("ENOSYS error = %v", err)
		}
		if _, statErr := os.Lstat(filepath.Dir(certPath)); !os.IsNotExist(statErr) {
			t.Fatalf("ENOSYS allowed target creation: %v", statErr)
		}
	})
}

func TestPrepareDefaultMailTLSDirectoryAcceptsExistingRestrictiveDirectory(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "mail-tls")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(directory, "default-cert.pem")
	keyPath := filepath.Join(directory, "default-key.pem")
	if _, err := prepareDefaultMailTLSDirectory(certPath, keyPath); err != nil {
		t.Fatalf("existing restrictive directory: %v", err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("existing restrictive directory mode = %04o, want 0700", got)
	}
}

func TestProductionMailTLSDirectoryValidationRejectsNonRootOwnership(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "non-root-production-component")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(directory, 12345, 12345); err != nil {
			t.Fatalf("prepare non-root production component: %v", err)
		}
	}
	fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := validateMailTLSDirectoryFD(
		fd,
		mailTLSDirectoryOwner{uid: 0, gid: 0},
		"production mail TLS component",
	); err == nil || !strings.Contains(err.Error(), "must be owned by uid 0 gid 0") {
		t.Fatalf("non-root production ownership error = %v", err)
	}
}

func TestMailTLSFileSnapshotRestoresExistingAndAbsentFiles(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.conf")
	if err := os.WriteFile(existingPath, []byte("old state"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing, err := snapshotMailTLSFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingPath, []byte("new state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := existing.restore(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old state" {
		t.Fatalf("restored content = %q, want old state", content)
	}

	absentPath := filepath.Join(dir, "new.conf")
	absent, err := snapshotMailTLSFile(absentPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absentPath, []byte("temporary state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := absent.restore(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absentPath); !os.IsNotExist(err) {
		t.Fatalf("restoring an absent snapshot left the file behind: %v", err)
	}
}
