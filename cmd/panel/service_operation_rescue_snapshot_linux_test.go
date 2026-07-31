//go:build linux

package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type rescueSnapshotFileState struct {
	content []byte
	stat    unix.Stat_t
}

func TestEnsureServiceOperationRescueSnapshotPreservesCommittedWALAndCanonicalMetadata(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-wal")
	createUncleanCommittedWAL(t, fixture, 0o644)
	assertProbeAbsentFromMainDatabase(t, fixture.sourcePath)

	canonicalBefore := map[string]rescueSnapshotFileState{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		canonicalBefore[suffix] = captureRescueSnapshotFileState(t, fixture.sourcePath+suffix)
	}
	if err := ensureServiceOperationRescueSnapshotWithOwner(
		fixture.sourcePath,
		destination,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("ensure rescue snapshot from committed WAL: %v", err)
	}

	assertWALProbe(t, destination)
	assertProbeAbsentFromMainDatabase(t, fixture.sourcePath)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		assertRescueSnapshotFileStateUnchanged(
			t,
			fixture.sourcePath+suffix,
			canonicalBefore[suffix],
		)
	}
	assertReleaseServiceOperationSnapshotParentRestored(t, fixture)
}

func TestEnsureServiceOperationRescueSnapshotCanonicalizesKnownHistoricalMigrationDDLWithoutMutatingSource(
	t *testing.T,
) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-legacy-ddl")
	rebuildSchemaMigrationsDDLForTest(
		t,
		fixture.sourcePath,
		knownLegacySchemaMigrationsSQL,
	)
	setSchemaMigrationAppliedAtForTest(t, fixture.sourcePath, 1, sql.NullString{})

	sourceBefore := captureRescueSnapshotFileState(t, fixture.sourcePath)
	sourceSQLBefore, sourceRowsBefore := readSchemaMigrationsStateForTest(
		t,
		fixture.sourcePath,
	)
	if sourceSQLBefore != knownLegacySchemaMigrationsSQL {
		t.Fatalf("historical source DDL=%q", sourceSQLBefore)
	}

	if err := ensureServiceOperationRescueSnapshotWithOwner(
		fixture.sourcePath,
		destination,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("ensure rescue snapshot from historical DDL: %v", err)
	}

	assertRescueSnapshotFileStateUnchanged(t, fixture.sourcePath, sourceBefore)
	sourceSQLAfter, sourceRowsAfter := readSchemaMigrationsStateForTest(
		t,
		fixture.sourcePath,
	)
	if sourceSQLAfter != sourceSQLBefore ||
		!equalServiceOperationSnapshotMigrationRows(sourceRowsBefore, sourceRowsAfter) {
		t.Fatal("canonical source schema or migration rows changed")
	}

	destinationSQL, destinationRows := readSchemaMigrationsStateForTest(t, destination)
	if len(destinationRows) == 0 {
		t.Fatal("canonical rescue snapshot has no migration rows")
	}
	canonicalSQL := referenceSchemaMigrationsSQLForTest(
		t,
		destinationRows[len(destinationRows)-1].version,
	)
	if destinationSQL != canonicalSQL {
		t.Fatalf("rescue snapshot DDL=%q want %q", destinationSQL, canonicalSQL)
	}
	if !equalServiceOperationSnapshotMigrationRows(sourceRowsBefore, destinationRows) {
		t.Fatal("rescue snapshot migration rows changed during canonicalization")
	}
	assertSchemaMigrationAppliedAtStorageClassesForTest(t, destinationRows)
	assertStandaloneSnapshot(t, destination)
	assertReleaseServiceOperationSnapshotParentRestored(t, fixture)
}

func TestEnsureServiceOperationRescueSnapshotRejectsHistoricalMigrationDDLWithBlobAppliedAtWithoutMutatingSource(
	t *testing.T,
) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-legacy-blob")
	rebuildSchemaMigrationsDDLForTest(
		t,
		fixture.sourcePath,
		knownLegacySchemaMigrationsSQL,
	)
	setSchemaMigrationAppliedAtBlobForTest(t, fixture.sourcePath, 1)

	sourceBefore := captureRescueSnapshotFileState(t, fixture.sourcePath)
	sourceSQLBefore, sourceRowsBefore := readSchemaMigrationsStateForTest(
		t,
		fixture.sourcePath,
	)
	if sourceSQLBefore != knownLegacySchemaMigrationsSQL {
		t.Fatalf("historical source DDL=%q", sourceSQLBefore)
	}
	if sourceRowsBefore[0].appliedAtStorageClass != "blob" {
		t.Fatalf(
			"historical source applied_at storage class=%q want blob",
			sourceRowsBefore[0].appliedAtStorageClass,
		)
	}

	err := ensureServiceOperationRescueSnapshotWithOwner(
		fixture.sourcePath,
		destination,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "unsupported applied_at storage class") {
		t.Fatalf("error=%v want applied_at storage class rejection", err)
	}

	assertRescueSnapshotFileStateUnchanged(t, fixture.sourcePath, sourceBefore)
	sourceSQLAfter, sourceRowsAfter := readSchemaMigrationsStateForTest(
		t,
		fixture.sourcePath,
	)
	if sourceSQLAfter != sourceSQLBefore ||
		!equalServiceOperationSnapshotMigrationRows(sourceRowsBefore, sourceRowsAfter) {
		t.Fatal("rejected canonical source schema, rows, or storage classes changed")
	}
	assertSnapshotDirectoryEmpty(t, filepath.Dir(destination))
	assertReleaseServiceOperationSnapshotParentRestored(t, fixture)
}

func TestEnsureServiceOperationRescueSnapshotAcceptsValidExistingIdempotently(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-existing")
	if err := ensureServiceOperationRescueSnapshotWithOwner(
		fixture.sourcePath,
		destination,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("create rescue snapshot: %v", err)
	}
	before := captureRescueSnapshotFileState(t, destination)

	missingSource := filepath.Join(fixture.sourceDirectory, "missing", serviceOperationSnapshotBasename)
	if err := ensureServiceOperationRescueSnapshotWithOwner(
		missingSource,
		destination,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
	); err != nil {
		t.Fatalf("accept valid existing rescue snapshot without source: %v", err)
	}
	assertRescueSnapshotFileStateUnchanged(t, destination, before)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(destination + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected existing rescue sidecar %s: %v", suffix, err)
		}
	}
}

func TestEnsureServiceOperationRescueSnapshotRejectsInvalidExistingArtifacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "mode 0644",
			mutate: func(t *testing.T, destination string) {
				if err := os.Chmod(destination, 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "root-owned single-link 0600",
		},
		{
			name: "hard link",
			mutate: func(t *testing.T, destination string) {
				if err := os.Link(destination, destination+".other-link"); err != nil {
					t.Fatal(err)
				}
			},
			want: "root-owned single-link 0600",
		},
		{
			name: "symlink",
			mutate: func(t *testing.T, destination string) {
				if err := os.Remove(destination); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/null", destination); err != nil {
					t.Fatal(err)
				}
			},
			want: "root-owned single-link 0600",
		},
		{
			name: "invalid database",
			mutate: func(t *testing.T, destination string) {
				if err := os.WriteFile(destination, []byte("not sqlite"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "validate existing rescue snapshot",
		},
		{
			name: "WAL sidecar",
			mutate: func(t *testing.T, destination string) {
				if err := os.WriteFile(destination+"-wal", []byte("sidecar"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "sidecar -wal must be absent",
		},
		{
			name: "sidecar symlink",
			mutate: func(t *testing.T, destination string) {
				if err := os.Symlink("/dev/null", destination+"-shm"); err != nil {
					t.Fatal(err)
				}
			},
			want: "sidecar -shm must be absent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReleaseServiceOperationSnapshotFixture(t)
			destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-invalid")
			writeValidExistingRescueSnapshot(t, fixture.sourcePath, destination)
			test.mutate(t, destination)
			err := ensureServiceOperationRescueSnapshotWithOwner(
				fixture.sourcePath,
				destination,
				serviceOperationSnapshotSchemaNormal,
				fixture.owner,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateExistingServiceOperationRescueSnapshotRejectsUnsafeParentChain(t *testing.T) {
	fixture := newReleaseServiceOperationSnapshotFixture(t)
	destination := bindRescueSnapshotFixture(t, fixture, "release-rescue-parent")
	writeValidExistingRescueSnapshot(t, fixture.sourcePath, destination)
	if err := os.Chmod(filepath.Dir(destination), 0o770); err != nil {
		t.Fatal(err)
	}
	_, err := validateExistingServiceOperationRescueSnapshot(
		destination,
		serviceOperationSnapshotSchemaNormal,
	)
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("error=%v want unsafe parent rejection", err)
	}
}

func bindRescueSnapshotFixture(
	t *testing.T,
	fixture releaseServiceOperationSnapshotFixture,
	snapshot string,
) string {
	t.Helper()
	testRoot := filepath.Dir(filepath.Dir(fixture.destinationPath))
	root := filepath.Join(testRoot, "recovery-snapshots")
	mustMkdirSnapshotTestDirectory(t, root, 0o700)
	mustMkdirSnapshotTestDirectory(t, filepath.Join(root, snapshot), 0o700)
	previousRoot := serviceOperationRescueSnapshotRoot
	serviceOperationRescueSnapshotRoot = root
	t.Cleanup(func() { serviceOperationRescueSnapshotRoot = previousRoot })
	return filepath.Join(root, snapshot, serviceOperationSnapshotBasename)
}

func captureRescueSnapshotFileState(t *testing.T, path string) rescueSnapshotFileState {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return rescueSnapshotFileState{content: content, stat: stat}
}

func assertRescueSnapshotFileStateUnchanged(
	t *testing.T,
	path string,
	want rescueSnapshotFileState,
) {
	t.Helper()
	got := captureRescueSnapshotFileState(t, path)
	if !bytes.Equal(got.content, want.content) {
		t.Fatalf("%s content changed", path)
	}
	if !sameExactUnixFileMetadata(got.stat, want.stat) {
		t.Fatalf("%s metadata changed: before=%+v after=%+v", path, want.stat, got.stat)
	}
}

func writeValidExistingRescueSnapshot(t *testing.T, sourcePath string, destination string) {
	t.Helper()
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(destination, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		t.Fatal(err)
	}
}
