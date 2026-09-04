//go:build linux

package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The POSIX half of the contract: modes and owners survive the round trip, and
// an account the archive names but this host does not have stops the restore
// before anything is placed.
//
// Sözleşmenin POSIX yarısı: kipler ve sahipler gidiş-dönüşten sağ çıkar; arşivin
// adlandırdığı ama bu makinede olmayan bir hesap, hiçbir şey yerleştirilmeden
// geri yüklemeyi durdurur.

func TestControlPlaneArchiveRoundTripPreservesModesAndOwners(t *testing.T) {
	source := newControlPlaneTestTree(t)
	// One member with a deliberately different mode proves the mode really is
	// carried rather than assumed.
	readOnly := filepath.Join(source.Root.ConfDir, "panel.env")
	if err := os.Chmod(readOnly, 0o640); err != nil {
		t.Fatalf("chmod %s: %v", readOnly, err)
	}

	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		source.Root,
		io.Discard,
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	target := newControlPlaneTargetRoots(t)
	if _, err := restoreControlPlaneArchive(
		archivePath,
		controlPlaneTestKey,
		target,
		io.Discard,
	); err != nil {
		t.Fatalf("restore the archive: %v", err)
	}

	rebase, err := newControlPlaneRebase(source.Root, target)
	if err != nil {
		t.Fatalf("build the rebase: %v", err)
	}
	for path := range source.Files {
		placed, err := rebase(path)
		if err != nil {
			t.Fatalf("rebase %s: %v", path, err)
		}
		sourceStat := lstatControlPlaneUnix(t, path)
		targetStat := lstatControlPlaneUnix(t, placed)
		if sourceStat.Mode&0o7777 != targetStat.Mode&0o7777 {
			t.Fatalf(
				"restored %s with mode %04o, want %04o",
				placed,
				targetStat.Mode&0o7777,
				sourceStat.Mode&0o7777,
			)
		}
		if sourceStat.Uid != targetStat.Uid || sourceStat.Gid != targetStat.Gid {
			t.Fatalf(
				"restored %s owned by %d:%d, want %d:%d",
				placed,
				targetStat.Uid,
				targetStat.Gid,
				sourceStat.Uid,
				sourceStat.Gid,
			)
		}
	}
	restoredPanelEnv, err := rebase(readOnly)
	if err != nil {
		t.Fatalf("rebase %s: %v", readOnly, err)
	}
	if mode := lstatControlPlaneUnix(t, restoredPanelEnv).Mode & 0o7777; mode != 0o640 {
		t.Fatalf("restored panel.env with mode %04o, want 0640", mode)
	}
}

func TestControlPlaneOwnershipResolutionNamesAMissingAccount(t *testing.T) {
	missing := "celikpanel-account-that-does-not-exist"
	if _, _, err := controlPlaneResolveOwnership(missing, "root"); err == nil {
		t.Fatal("a missing user account was resolved")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error=%v, want it to name %q", err, missing)
	}
	if _, _, err := controlPlaneResolveOwnership("root", missing); err == nil {
		t.Fatal("a missing group was resolved")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error=%v, want it to name %q", err, missing)
	}
	// A numeric name is still resolvable, so an archive from a host whose
	// account names could not be read is not lost.
	uid, gid, err := controlPlaneResolveOwnership("0", "0")
	if err != nil || uid != 0 || gid != 0 {
		t.Fatalf("numeric ownership resolved to %d:%d err=%v", uid, gid, err)
	}
}

func TestControlPlaneRestoreRefusesAnAccountThisHostDoesNotHave(t *testing.T) {
	missing := "celikpanel-account-that-does-not-exist"
	target := newControlPlaneTargetRoots(t)
	content := []byte("not-a-real-secret-key\n")
	digest := sha256.Sum256(content)
	manifest := controlPlaneManifest{
		SchemaVersion:            durableServiceOperationSchemaVersion,
		DatabaseMigrationVersion: shippedControlPlaneMigrationVersion(t),
		PanelVersion:             buildVersion,
		PanelCommit:              buildCommit,
		Host:                     "host-a",
		CreatedAt:                "2026-09-03T00:00:00Z",
		Roots:                    target,
		Members: []controlPlaneManifestEntry{
			{
				Path:   filepath.Join(target.DataDir, controlPlaneSecretKeyBasename),
				Type:   controlPlaneManifestEntryFile,
				Owner:  missing,
				Group:  "root",
				Mode:   "0600",
				Size:   int64(len(content)),
				SHA256: hex.EncodeToString(digest[:]),
			},
		},
	}
	archivePath := filepath.Join(t.TempDir(), "foreign-account.cpbak")
	sealControlPlaneTestArchive(t, archivePath, controlPlaneTestKey, func(writer *tar.Writer) {
		writeControlPlaneTestManifest(t, writer, manifest)
		name, err := controlPlaneMemberName(manifest.Members[0].Path)
		if err != nil {
			t.Fatalf("build the member name: %v", err)
		}
		if err := writer.WriteHeader(&tar.Header{
			Format:   tar.FormatPAX,
			Typeflag: tar.TypeReg,
			Name:     name,
			Mode:     0o600,
			Size:     int64(len(content)),
			Uname:    missing,
			Gname:    "root",
		}); err != nil {
			t.Fatalf("write the member header: %v", err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatalf("write the member: %v", err)
		}
		writeControlPlaneTestManifestDigest(t, writer, manifest)
	})

	_, err := restoreControlPlaneArchive(archivePath, controlPlaneTestKey, target, io.Discard)
	if err == nil {
		t.Fatal("an archive naming an unknown account was restored")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error=%v, want it to name %q", err, missing)
	}
	assertControlPlaneTargetUntouched(t, target)
}

func lstatControlPlaneUnix(t *testing.T, path string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat %s carries no unix metadata", path)
	}
	return stat
}
