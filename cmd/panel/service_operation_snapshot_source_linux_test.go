//go:build linux

package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"golang.org/x/sys/unix"
)

const releaseServiceOperationWALWriterDatabaseEnv = "CELIKPANEL_TEST_RELEASE_WAL_DATABASE"

func TestReleaseServiceOperationSnapshotWALWriterProcess(t *testing.T) {
	databasePath := os.Getenv(releaseServiceOperationWALWriterDatabaseEnv)
	if databasePath == "" {
		return
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open WAL writer database: %v", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	var journalMode string
	if err := database.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		t.Fatalf("enable WAL: %v", err)
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
		t.Fatalf("journal mode=%q want WAL", journalMode)
	}
	if _, err := database.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatalf("disable automatic WAL checkpoints: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO panel_settings(key, value)
		VALUES ('release_snapshot_wal_probe', 'committed-only-in-wal')
	`); err != nil {
		t.Fatalf("commit WAL-only probe: %v", err)
	}
	var marker string
	if err := database.QueryRow(
		"SELECT value FROM panel_settings WHERE key='release_snapshot_wal_probe'",
	).Scan(&marker); err != nil {
		t.Fatalf("read committed WAL-only probe: %v", err)
	}
	if marker != "committed-only-in-wal" {
		t.Fatalf("marker=%q", marker)
	}

	// Do not close the connection. os.Exit deliberately simulates an unclean
	// panel stop so the committed transaction remains in the WAL.
	os.Exit(0)
}

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

func TestCreateReleaseServiceOperationSnapshotWithOwnerPreservesCommittedWAL(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	createUncleanCommittedWAL(t, fixture, 0o600)
	assertProbeAbsentFromMainDatabase(t, fixture.sourcePath)

	if err := createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("create release snapshot from committed WAL: %v", err)
	}

	assertReleaseServiceOperationSnapshotFixture(t, fixture)
	assertWALProbe(t, fixture.destinationPath)
	assertWALProbe(t, fixture.sourcePath)
	assertSQLiteSourceSidecarsAbsent(t, fixture.sourcePath)
}

func TestCreateReleaseServiceOperationSnapshotWithOwnerNormalizesLegacy0644SQLiteFiles(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	createUncleanCommittedWAL(t, fixture, 0o644)

	if err := createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("create release snapshot from legacy 0644 SQLite files: %v", err)
	}

	assertReleaseServiceOperationSnapshotFixture(t, fixture)
	assertWALProbe(t, fixture.destinationPath)
	assertWALProbe(t, fixture.sourcePath)
	assertSQLiteSourceSidecarsAbsent(t, fixture.sourcePath)
}

func TestCreateReleaseServiceOperationSnapshotWithOwnerRejectsSidecarWithoutDeleting(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	sourceContent, err := os.ReadFile(fixture.sourcePath)
	if err != nil {
		t.Fatal(err)
	}
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
	err = createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	)
	if err == nil || !strings.Contains(err.Error(), "recover pinned canonical SQLite WAL") {
		t.Fatalf("error=%v want corrupt WAL rejection", err)
	}
	currentSourceContent, sourceReadErr := os.ReadFile(fixture.sourcePath)
	if sourceReadErr != nil || !bytes.Equal(currentSourceContent, sourceContent) {
		t.Fatalf("source database changed: content_equal=%v err=%v", bytes.Equal(currentSourceContent, sourceContent), sourceReadErr)
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

func TestCreateReleaseServiceOperationSnapshotWithOwnerRejects0660Database(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	if err := os.Chmod(fixture.sourcePath, 0o660); err != nil {
		t.Fatal(err)
	}

	err := createReleaseServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		fixture.destinationPath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	)
	if err == nil || !strings.Contains(err.Error(), "0600, 0640, or 0644") {
		t.Fatalf("error=%v want unsafe-mode rejection", err)
	}
	if _, statErr := os.Lstat(fixture.destinationPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after unsafe-mode rejection: %v", statErr)
	}
	assertReleaseServiceOperationSnapshotParentRestored(t, fixture)
	var sourceStat unix.Stat_t
	if err := unix.Lstat(fixture.sourcePath, &sourceStat); err != nil {
		t.Fatal(err)
	}
	if sourceStat.Mode&0o777 != 0o660 {
		t.Fatalf("rejected source mode=%#o want 0660", sourceStat.Mode&0o777)
	}
}

func createUncleanCommittedWAL(
	t *testing.T,
	fixture releaseServiceOperationSnapshotFixture,
	mode os.FileMode,
) {
	t.Helper()
	command := exec.Command(
		os.Args[0],
		"-test.run=^TestReleaseServiceOperationSnapshotWALWriterProcess$",
	)
	command.Env = append(
		os.Environ(),
		releaseServiceOperationWALWriterDatabaseEnv+"="+fixture.sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create unclean committed WAL: %v\n%s", err, output)
	}

	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := fixture.sourcePath + suffix
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("inspect generated SQLite source %s: %v", suffix, err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("generated SQLite source %s is not regular", suffix)
		}
		if err := os.Chown(path, int(fixture.owner.uid), int(fixture.owner.gid)); err != nil {
			t.Fatalf("chown generated SQLite source %s: %v", suffix, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("chmod generated SQLite source %s: %v", suffix, err)
		}
	}
}

func assertProbeAbsentFromMainDatabase(t *testing.T, sourcePath string) {
	t.Helper()
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	mainOnlyPath := filepath.Join(t.TempDir(), "main-only.db")
	if err := os.WriteFile(mainOnlyPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", mainOnlyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var marker string
	err = database.QueryRow(
		"SELECT value FROM panel_settings WHERE key='release_snapshot_wal_probe'",
	).Scan(&marker)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("main-only WAL probe query error=%v marker=%q, want no rows", err, marker)
	}
}

func assertWALProbe(t *testing.T, databasePath string) {
	t.Helper()
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var marker string
	if err := database.QueryRow(
		"SELECT value FROM panel_settings WHERE key='release_snapshot_wal_probe'",
	).Scan(&marker); err != nil {
		t.Fatalf("read preserved WAL probe from %s: %v", databasePath, err)
	}
	if marker != "committed-only-in-wal" {
		t.Fatalf("preserved WAL probe=%q", marker)
	}
}

func assertSQLiteSourceSidecarsAbsent(t *testing.T, databasePath string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(databasePath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("SQLite sidecar %s remains after normalization: %v", suffix, err)
		}
	}
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
	assertReleaseServiceOperationSnapshotParentRestored(t, fixture)
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

func assertReleaseServiceOperationSnapshotParentRestored(
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
}
