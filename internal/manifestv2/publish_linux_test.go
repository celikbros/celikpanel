//go:build linux

package manifestv2

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCatalogPublishDirectoryRejectsUnsafePermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o777, 0o775} {
		t.Run(mode.String(), func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, mode); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(parent, "catalog.db")
			_, err := BuildCatalog(
				context.Background(),
				destination,
				testCatalogDocument("release-key-1"),
			)
			if err == nil || !strings.Contains(err.Error(), "permits group or other writes") {
				t.Fatalf("BuildCatalog mode %04o error = %v", mode, err)
			}
			entries, readErr := os.ReadDir(parent)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("unsafe parent contains %d build artifacts", len(entries))
			}
		})
	}
}

func TestCatalogPublishDirectoryRejectsSymlink(t *testing.T) {
	realParent := t.TempDir()
	linkRoot := t.TempDir()
	linkParent := filepath.Join(linkRoot, "publish-link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Fatal(err)
	}
	_, err := BuildCatalog(
		context.Background(),
		filepath.Join(linkParent, "catalog.db"),
		testCatalogDocument("release-key-1"),
	)
	if err == nil {
		t.Fatal("BuildCatalog accepted a symlink publish parent")
	}
	if _, statErr := os.Stat(filepath.Join(realParent, "catalog.db")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink parent received a catalog: %v", statErr)
	}
}

func TestCatalogSigningRejectsSymlinkAndWritableArtifact(t *testing.T) {
	parent := t.TempDir()
	privateKey := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	expectedDigest := strings.Repeat("0", 64)
	target := filepath.Join(parent, "target.db")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "catalog.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SignCatalog(link, expectedDigest, "release-key-1", privateKey); err == nil {
		t.Fatal("signing accepted a symlink catalog artifact")
	}

	writable := filepath.Join(parent, "writable.db")
	if err := os.WriteFile(writable, []byte("writable"), 0o620); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o620); err != nil {
		t.Fatal(err)
	}
	if _, err := SignCatalog(writable, expectedDigest, "release-key-1", privateKey); err == nil {
		t.Fatal("signing accepted a group-writable catalog artifact")
	}
}

func TestCatalogFilesystemOwnershipValidation(t *testing.T) {
	effectiveUID := os.Geteuid()
	untrustedUID := uint32(effectiveUID + 1)
	if untrustedUID == 0 {
		untrustedUID++
	}
	if err := validateCatalogDirectoryStat(unix.Stat_t{
		Mode: unix.S_IFDIR | 0o700,
		Uid:  untrustedUID,
	}, effectiveUID); err == nil {
		t.Fatal("directory owned by an unrelated uid was accepted")
	}
	if err := validateCatalogArtifactStat(unix.Stat_t{
		Mode: unix.S_IFREG | 0o600,
		Uid:  untrustedUID,
	}, effectiveUID); err == nil {
		t.Fatal("artifact owned by an unrelated uid was accepted")
	}
	for _, uid := range []uint32{0, uint32(effectiveUID)} {
		if err := validateCatalogDirectoryStat(unix.Stat_t{
			Mode: unix.S_IFDIR | 0o700,
			Uid:  uid,
		}, effectiveUID); err != nil {
			t.Fatalf("trusted directory uid %d rejected: %v", uid, err)
		}
		if err := validateCatalogArtifactStat(unix.Stat_t{
			Mode: unix.S_IFREG | 0o600,
			Uid:  uid,
		}, effectiveUID); err != nil {
			t.Fatalf("trusted artifact uid %d rejected: %v", uid, err)
		}
	}
}

func TestCatalogBasenameRejectsRootAndSeparators(t *testing.T) {
	backslash := string(rune(92))
	for _, name := range []string{"/", backslash, "nested/name", "nested" + backslash + "name"} {
		if err := validateCatalogBasename(name); err == nil {
			t.Errorf("catalog basename %q was accepted", name)
		}
	}
	if err := validateCatalogBasename("catalog.db"); err != nil {
		t.Fatalf("ordinary catalog basename rejected: %v", err)
	}

	parent := t.TempDir()
	sourcePath := filepath.Join(parent, "source.db")
	if err := os.WriteFile(sourcePath, []byte("catalog"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	directory, err := openCatalogPublishDirectory(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := publishCatalog(source, "/", "/", directory, syncCatalogDirectory); err == nil {
		t.Fatal("publication accepted an absolute root basename")
	}

	privateKey := make(ed25519.PrivateKey, ed25519.PrivateKeySize)
	if _, err := SignCatalog(
		"/",
		strings.Repeat("0", 64),
		"release-key-1",
		privateKey,
	); err == nil || !strings.Contains(err.Error(), "invalid catalog basename") {
		t.Fatalf("root signing path error = %v", err)
	}
}

func TestPinnedPublishDirectorySurvivesPathReplacement(t *testing.T) {
	root := t.TempDir()
	publishPath := filepath.Join(root, "publish")
	if err := os.Mkdir(publishPath, 0o700); err != nil {
		t.Fatal(err)
	}
	directory, err := openCatalogPublishDirectory(publishPath)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()

	movedPath := filepath.Join(root, "publish-pinned")
	if err := os.Rename(publishPath, movedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(publishPath, 0o700); err != nil {
		t.Fatal(err)
	}

	workspace, err := createCatalogBuildWorkspace(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cleanupErr := cleanupCatalogBuildWorkspace(workspace); cleanupErr != nil {
			t.Errorf("cleanup workspace: %v", cleanupErr)
		}
	}()
	payload := []byte("pinned-inode")
	if _, err := workspace.database.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := workspace.database.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := publishCatalog(
		workspace.database,
		"catalog.db",
		filepath.Join(movedPath, "catalog.db"),
		directory,
		syncCatalogDirectory,
	); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(movedPath, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("pinned destination content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(publishPath, "catalog.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement pathname unexpectedly received catalog: %v", err)
	}
}
