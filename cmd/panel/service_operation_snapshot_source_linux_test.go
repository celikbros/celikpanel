//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"golang.org/x/sys/unix"
)

func TestCreateReleaseServiceOperationSnapshotWithOwnerQuarantinesAndRestoresSource(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	if err := createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("create release snapshot: %v", err)
	}
	assertReleaseServiceOperationSnapshotFixture(t, fixture)
}

func TestCreateReleaseServiceOperationSnapshotWithOwnerRecoversQuarantineStates(t *testing.T) {
	tests := []struct {
		name       string
		rootOwned  bool
		parentMode os.FileMode
	}{
		{name: "root 0700", rootOwned: true, parentMode: 0o700},
		{name: "root 0750", rootOwned: true, parentMode: 0o750},
		{name: "panel 0700", rootOwned: false, parentMode: 0o700},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReleaseServiceOperationSnapshotFixture(t)
			if test.rootOwned {
				if err := os.Chown(fixture.sourceDirectory, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(fixture.sourceDirectory, test.parentMode); err != nil {
				t.Fatal(err)
			}
			if err := createReleaseServiceOperationSnapshotWithOwner(
				fixture.sourcePath,
				fixture.destinationPath,
				serviceOperationSnapshotSchemaNormal,
				fixture.owner,
			); err != nil {
				t.Fatalf("create snapshot from recoverable quarantine: %v", err)
			}
			assertReleaseServiceOperationSnapshotFixture(t, fixture)
		})
	}
}

func TestCreateReleaseServiceOperationSnapshotWithOwnerRejectsSidecarWithoutDeleting(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	sidecarPath := fixture.sourcePath + "-wal"
	sidecarContent := []byte("uncheckpointed data")
	if err := os.WriteFile(sidecarPath, sidecarContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(sidecarPath, int(fixture.owner.uid), int(fixture.owner.gid)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sidecarPath, 0o600); err != nil {
		t.Fatal(err)
	}
	err := createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	)
	if err == nil || !strings.Contains(err.Error(), "must be absent before release snapshot") {
		t.Fatalf("error=%v want source sidecar rejection", err)
	}
	content, readErr := os.ReadFile(sidecarPath)
	if readErr != nil || !bytes.Equal(content, sidecarContent) {
		t.Fatalf("source sidecar changed: content=%q err=%v", content, readErr)
	}
	if _, statErr := os.Lstat(fixture.destinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after sidecar rejection: %v", statErr)
	}
	assertReleaseServiceOperationSnapshotSourceRestored(t, fixture)
}

type releaseServiceOperationSnapshotFixture struct {
	sourceDirectory string
	sourcePath      string
	destinationPath string
	owner           serviceOperationRestoreOwner
}

func newReleaseServiceOperationSnapshotFixture(t *testing.T) releaseServiceOperationSnapshotFixture {
	t.Helper()
	testRoot := newSecureSnapshotTestRoot(t)
	owner := unusedServiceOperationRestoreOwner(t)
	sourceDirectory := filepath.Join(testRoot, "canonical")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o750)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	if err := os.Chown(sourcePath, int(owner.uid), int(owner.gid)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourcePath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(sourceDirectory, int(owner.uid), int(owner.gid)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sourceDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	destinationDirectory := filepath.Join(testRoot, "release-20260728")
	mustMkdirSnapshotTestDirectory(t, destinationDirectory, 0o700)
	return releaseServiceOperationSnapshotFixture{
		sourceDirectory: sourceDirectory,
		sourcePath:      sourcePath,
		destinationPath: filepath.Join(destinationDirectory, serviceOperationSnapshotBasename),
		owner:           owner,
	}
}

func assertReleaseServiceOperationSnapshotFixture(
	t *testing.T,
	fixture releaseServiceOperationSnapshotFixture,
) {
	t.Helper()
	if err := validateServiceOperationSnapshot(
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
	); err != nil {
		t.Fatalf("validate release snapshot: %v", err)
	}
	assertStandaloneSnapshot(t, fixture.destinationPath)
	assertReleaseServiceOperationSnapshotSourceRestored(t, fixture)
}

func assertReleaseServiceOperationSnapshotSourceRestored(
	t *testing.T,
	fixture releaseServiceOperationSnapshotFixture,
) {
	t.Helper()
	parentInfo, err := os.Stat(fixture.sourceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0o750 ||
		parentStat.Uid != fixture.owner.uid || parentStat.Gid != fixture.owner.gid {
		t.Fatalf("source parent metadata mode=%v stat=%+v", parentInfo.Mode(), parentStat)
	}
	var sourceStat unix.Stat_t
	if err := unix.Lstat(fixture.sourcePath, &sourceStat); err != nil {
		t.Fatal(err)
	}
	if sourceStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		sourceStat.Mode&0o777 != 0o600 ||
		sourceStat.Nlink != 1 ||
		sourceStat.Uid != fixture.owner.uid ||
		sourceStat.Gid != fixture.owner.gid {
		t.Fatalf("source database metadata=%+v", sourceStat)
	}
}
