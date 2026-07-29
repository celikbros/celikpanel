package systemsqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createTestSQLite(t *testing.T, path string, journalMode string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", sqliteURI(path, "rwc"))
	if err != nil {
		t.Fatal(err)
	}
	if journalMode != "" {
		if _, err := database.Exec("PRAGMA journal_mode=" + journalMode); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`
		PRAGMA user_version=7;
		PRAGMA foreign_keys=ON;
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id));
		INSERT INTO parent(id) VALUES (1);
		INSERT INTO child(id, parent_id) VALUES (1, 1);
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database
}

func testDefinition(id, path string, mutable bool) Definition {
	return Definition{
		ID: id, Name: "Test database", Purpose: "Test only.", Kind: "test",
		Path: path, PathHint: "managed-data / test.sqlite3",
		Mutable: mutable, Optimizable: mutable, SnapshotAllowed: true,
	}
}

type recordingMutableOperations struct {
	inspectCalls   int
	checkCalls     int
	snapshotCalls  int
	optimizeCalls  int
	snapshotData   []byte
	snapshotLimits SnapshotLimits
}

func (operations *recordingMutableOperations) Inspect(
	context.Context,
	Definition,
) (MutableInspection, error) {
	operations.inspectCalls++
	return MutableInspection{JournalMode: "delete", UserVersion: 7}, nil
}

func (operations *recordingMutableOperations) Check(
	_ context.Context,
	definition Definition,
) (CheckResult, error) {
	operations.checkCalls++
	return CheckResult{
		DatabaseID: definition.ID, IntegrityOK: true, IntegrityMessage: "ok", ForeignKeysOK: true,
	}, nil
}

func (operations *recordingMutableOperations) Snapshot(
	_ context.Context,
	definition Definition,
	destination *os.File,
	limits SnapshotLimits,
) error {
	operations.snapshotCalls++
	operations.snapshotLimits = limits
	maxBytes := limits.MaxBytes
	if operations.snapshotData != nil {
		if int64(len(operations.snapshotData)) > maxBytes {
			return errors.New("recorded snapshot exceeds limit")
		}
		if _, err := destination.Write(operations.snapshotData); err != nil {
			return err
		}
		return destination.Sync()
	}
	source, err := os.Open(definition.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	if _, err := io.Copy(destination, io.LimitReader(source, maxBytes+1)); err != nil {
		return err
	}
	return destination.Sync()
}

func (operations *recordingMutableOperations) Optimize(context.Context, Definition) error {
	operations.optimizeCalls++
	return nil
}

func newTestManager(t *testing.T, definitions []Definition, options Options) *Manager {
	t.Helper()
	if options.SnapshotRoot == "" {
		options.SnapshotRoot = filepath.Join(t.TempDir(), "snapshots")
	}
	if options.AvailableBytes == nil {
		options.AvailableBytes = func(string) (int64, error) { return math.MaxInt64, nil }
	}
	if options.MutableOperations == nil {
		options.MutableOperations = directMutableOperations{
			skipOwnerCheckForTests: true,
			capacityProbe: func(*os.File) (snapshotFilesystemCapacity, error) {
				return snapshotFilesystemCapacity{ID: 1, AvailableBytes: math.MaxInt64}, nil
			},
		}
	}
	manager, err := NewManager(definitions, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return manager
}

func readSnapshotBytes(t *testing.T, manager *Manager, token string) []byte {
	t.Helper()
	var result []byte
	var offset int64
	for {
		chunk, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{
			Token: token, Offset: offset, MaxBytes: 257,
		})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, chunk.Data...)
		offset = chunk.NextOffset
		if chunk.EOF {
			break
		}
	}
	return result
}

func TestMutableOperationsAlwaysUseIsolationBoundary(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mutable.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	operations := &recordingMutableOperations{}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePowerDNS, databasePath, true),
	}, Options{MutableOperations: operations})

	inventory, err := manager.List(context.Background())
	if err != nil || len(inventory) != 1 || inventory[0].Status != "ready" {
		t.Fatalf("List() = %+v, %v", inventory, err)
	}
	if _, err := manager.Check(context.Background(), DatabasePowerDNS); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Optimize(context.Background(), DatabasePowerDNS); err != nil {
		t.Fatal(err)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
	if operations.inspectCalls != 1 || operations.checkCalls != 1 ||
		operations.snapshotCalls != 1 || operations.optimizeCalls != 1 {
		t.Fatalf("isolation calls = %+v", operations)
	}
	if operations.snapshotLimits != (SnapshotLimits{
		MaxBytes:       defaultMaxSnapshotBytes,
		FreeSpaceFloor: defaultFreeSpaceFloor,
	}) {
		t.Fatalf("snapshot limits = %+v", operations.snapshotLimits)
	}
}

func TestManagerTreatsMutableWorkerSnapshotAsValidatedOpaqueBytes(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mutable.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("worker-validated opaque snapshot")
	operations := &recordingMutableOperations{snapshotData: payload}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePowerDNS, databasePath, true),
	}, Options{MutableOperations: operations})

	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatal(err)
	}
	if got := readSnapshotBytes(t, manager, snapshot.Token); !bytes.Equal(got, payload) {
		t.Fatalf("snapshot bytes = %q, want %q", got, payload)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestMutableOperationsFailClosedWithoutIsolationBoundary(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "mutable.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(
		[]Definition{testDefinition(DatabasePowerDNS, databasePath, true)},
		Options{
			SnapshotRoot: filepath.Join(t.TempDir(), "snapshots"),
			AvailableBytes: func(string) (int64, error) {
				return math.MaxInt64, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	inventory, err := manager.List(context.Background())
	if err != nil || len(inventory) != 1 || inventory[0].Status != "error" ||
		len(inventory[0].Actions) != 0 {
		t.Fatalf("fail-closed List() = %+v, %v", inventory, err)
	}
	if _, err := manager.Check(context.Background(), DatabasePowerDNS); err == nil {
		t.Fatal("mutable check succeeded without isolation")
	}
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS); err == nil {
		t.Fatal("mutable snapshot succeeded without isolation")
	}
	if _, err := manager.Optimize(context.Background(), DatabasePowerDNS); err == nil {
		t.Fatal("mutable optimize succeeded without isolation")
	}
}

func TestManagerRejectsSnapshotLimitAboveHardMaximum(t *testing.T) {
	_, err := NewManager(
		[]Definition{testDefinition(
			DatabaseComponentCatalog,
			filepath.Join(t.TempDir(), "catalog.sqlite3"),
			false,
		)},
		Options{MaxSnapshotBytes: defaultMaxSnapshotBytes + 1},
	)
	if err == nil {
		t.Fatal("snapshot limit above 2 GiB was accepted")
	}
}

func TestListReportsSafeMetadataAndMissingDatabase(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "WAL")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(root, "missing.sqlite3")
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
		testDefinition(DatabasePowerDNS, missingPath, true),
	}, Options{})

	items, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d", len(items))
	}
	if !items[0].Available || items[0].UserVersion != 7 || items[0].JournalMode == "" {
		t.Fatalf("available metadata = %+v", items[0])
	}
	if got := strings.Join(items[0].Actions, ","); got != "check,snapshot,optimize" {
		t.Fatalf("actions = %q", got)
	}
	if items[1].Available || items[1].Status != "missing" || len(items[1].Actions) != 0 {
		t.Fatalf("missing metadata = %+v", items[1])
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), databasePath) {
		t.Fatalf("inventory leaked a managed path: %s", encoded)
	}
}

func TestSnapshotAllowedIsEnforcedBeforeStorageWork(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabasePanel, databasePath, true)
	definition.SnapshotAllowed = false
	capacityChecks := 0
	snapshotRoot := filepath.Join(t.TempDir(), "snapshots")
	manager := newTestManager(t, []Definition{definition}, Options{
		SnapshotRoot: snapshotRoot,
		AvailableBytes: func(string) (int64, error) {
			capacityChecks++
			return math.MaxInt64, nil
		},
	})

	items, err := manager.List(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("List() = %+v, %v", items, err)
	}
	if got := strings.Join(items[0].Actions, ","); got != "check,optimize" {
		t.Fatalf("actions = %q", got)
	}
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("snapshot-disabled database accepted a snapshot")
	}
	if capacityChecks != 0 {
		t.Fatalf("capacity checks = %d, want 0", capacityChecks)
	}
	if _, err := os.Stat(snapshotRoot); !os.IsNotExist(err) {
		t.Fatalf("snapshot storage was touched: %v", err)
	}
}

func TestOnlyOneSnapshotMayBeActiveAndReleaseOpensTheSlot(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	capacityChecks := 0
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{AvailableBytes: func(string) (int64, error) {
		capacityChecks++
		return math.MaxInt64, nil
	}})

	first, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("a second active snapshot was accepted")
	}
	if capacityChecks != 1 {
		t.Fatalf("capacity checks = %d, want 1", capacityChecks)
	}
	if released, err := manager.ReleaseSnapshot(first.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
	second, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatalf("snapshot after release failed: %v", err)
	}
	if capacityChecks != 2 {
		t.Fatalf("capacity checks after release = %d, want 2", capacityChecks)
	}
	if released, err := manager.ReleaseSnapshot(second.Token); err != nil || !released {
		t.Fatalf("second ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestSnapshotSlotIsHeldWhileCreationIsInProgress(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	capacityEntered := make(chan struct{})
	continueCreation := make(chan struct{})
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{AvailableBytes: func(string) (int64, error) {
		close(capacityEntered)
		<-continueCreation
		return math.MaxInt64, nil
	}})
	type snapshotResult struct {
		info SnapshotInfo
		err  error
	}
	created := make(chan snapshotResult, 1)
	go func() {
		info, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
		created <- snapshotResult{info: info, err: err}
	}()
	<-capacityEntered
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		close(continueCreation)
		t.Fatal("a second snapshot was accepted while creation was in progress")
	}
	close(continueCreation)
	result := <-created
	if result.err != nil {
		t.Fatal(result.err)
	}
	if released, err := manager.ReleaseSnapshot(result.info.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestSnapshotResourceDefaults(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{})
	if manager.maxSnapshotBytes != int64(2<<30) {
		t.Fatalf("default max snapshot bytes = %d", manager.maxSnapshotBytes)
	}
	if manager.freeSpaceFloor != int64(512<<20) {
		t.Fatalf("default free-space floor = %d", manager.freeSpaceFloor)
	}
}

func TestSnapshotCreationFailureOpensTheSlotAfterCleanup(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	if err := os.WriteFile(databasePath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{})

	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("malformed database snapshot unexpectedly succeeded")
	}
	if err := os.Remove(databasePath); err != nil {
		t.Fatal(err)
	}
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatalf("slot remained held after cleaned creation failure: %v", err)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestSnapshotCapacityPreflightRejectsInsufficientSpace(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	const maxBytes = int64(1 << 20)
	const floor = int64(4096)
	available := maxBytes + floor - 1
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{
		MaxSnapshotBytes: maxBytes,
		FreeSpaceFloor:   floor,
		AvailableBytes:   func(string) (int64, error) { return available, nil },
	})

	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("snapshot passed an insufficient-capacity preflight")
	}
	available++
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatalf("exact required capacity was rejected: %v", err)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestSnapshotStorageFailureIsLazyAndDoesNotDisableChecks(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{SnapshotRoot: "relative-snapshot-root"})
	if _, err := manager.Check(context.Background(), DatabasePanel); err != nil {
		t.Fatalf("Check() was disabled by snapshot storage: %v", err)
	}
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("snapshot with unsafe storage unexpectedly succeeded")
	}
}

func TestFirstSnapshotCleansOrphanedPrivateDirectories(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	snapshotRoot := filepath.Join(t.TempDir(), "snapshots")
	if err := os.Mkdir(snapshotRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(snapshotRoot, "celikpanel-system-sqlite-orphan")
	if err := os.Mkdir(orphan, 0o700); err != nil {
		t.Fatal(err)
	}
	nonmatchingDirectory := filepath.Join(snapshotRoot, "operator-notes")
	if err := os.Mkdir(nonmatchingDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	nonmatchingFile := filepath.Join(snapshotRoot, "README")
	if err := os.WriteFile(nonmatchingFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{SnapshotRoot: snapshotRoot})
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan snapshot directory still exists: %v", err)
	}
	for _, path := range []string{nonmatchingDirectory, nonmatchingFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("nonmatching entry was changed: %s: %v", path, err)
		}
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestFirstSnapshotRejectsUnsafeMatchingOrphan(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	snapshotRoot := filepath.Join(t.TempDir(), "snapshots")
	if err := os.Mkdir(snapshotRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafeEntry := filepath.Join(snapshotRoot, snapshotDirectoryPrefix+"not-a-directory")
	if err := os.WriteFile(unsafeEntry, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{SnapshotRoot: snapshotRoot})
	if _, err := manager.CreateSnapshot(context.Background(), DatabasePanel); err == nil {
		t.Fatal("unsafe matching orphan was accepted")
	}
	if data, err := os.ReadFile(unsafeEntry); err != nil || string(data) != "keep" {
		t.Fatalf("unsafe matching orphan was changed: %q, %v", data, err)
	}
}

func TestOrphanCleanupRejectsMatchingSymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	sentinel := filepath.Join(target, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, snapshotDirectoryPrefix+"link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are unavailable in this test environment: %v", err)
	}
	if err := cleanupOrphanedSnapshotDirectories(root); err == nil {
		t.Fatal("matching snapshot symlink was accepted")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("symlink target was changed: %q, %v", data, err)
	}
}

func TestMutableSnapshotUsesOnlineBackupAndProducesStandaloneDigest(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "WAL")
	defer database.Close()
	if _, err := database.Exec("INSERT INTO parent(id) VALUES (2), (3)"); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{})

	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatal(err)
	}
	decodedDigest, digestErr := hex.DecodeString(snapshot.SHA256)
	if len(snapshot.Token) != 64 || snapshot.SizeBytes <= 0 || digestErr != nil || len(decodedDigest) != 32 {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	data := readSnapshotBytes(t, manager, snapshot.Token)
	digest := sha256.Sum256(data)
	if got := hex.EncodeToString(digest[:]); got != snapshot.SHA256 {
		t.Fatalf("SHA-256 = %s, want %s", got, snapshot.SHA256)
	}
	restoredPath := filepath.Join(root, "download.sqlite3")
	if err := os.WriteFile(restoredPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", sqliteURI(restoredPath, "ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var count int
	if err := restored.QueryRow("SELECT COUNT(*) FROM parent").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("snapshot row count = %d, want 3", count)
	}
	var journalMode string
	if err := restored.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "delete") {
		t.Fatalf("snapshot journal_mode = %q", journalMode)
	}
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{
		Token: snapshot.Token, MaxBytes: MaxChunkSize + 1,
	}); err == nil {
		t.Fatal("oversized chunk was accepted")
	}
	released, err := manager.ReleaseSnapshot(snapshot.Token)
	if err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{Token: snapshot.Token}); err == nil {
		t.Fatal("released snapshot remained readable")
	}
}

func TestSnapshotAllowsPreExistingForeignKeyViolationsForRecovery(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "recovery.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if _, err := database.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO child(id, parent_id) VALUES (2, 999)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePowerDNS, databasePath, true),
	}, Options{})
	check, err := manager.Check(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatal(err)
	}
	if check.ForeignKeysOK || check.ForeignKeyViolations == 0 {
		t.Fatalf("foreign-key check = %+v", check)
	}
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatalf("recovery snapshot was blocked: %v", err)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestOnlineBackupChecksTargetGrowthAgainstTheLimit(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := openManagedSource(databasePath, false)
	if err != nil {
		t.Fatal(err)
	}
	defer source.close()
	target := filepath.Join(root, "snapshot.sqlite3")
	if err := createOnlineSnapshot(context.Background(), source, target, 1); err == nil {
		t.Fatal("online backup target grew beyond the limit without failing")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 1 {
		t.Fatalf("target size = %d, test did not cross the limit", info.Size())
	}
}

func TestImmutableCatalogSnapshotPreservesExactBytes(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, "components-v2.db")
	database := createTestSQLite(t, catalogPath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabaseComponentCatalog, catalogPath, false)
	definition.Optimizable = false
	manager := newTestManager(t, []Definition{definition}, Options{})

	snapshot, err := manager.CreateSnapshot(context.Background(), DatabaseComponentCatalog)
	if err != nil {
		t.Fatal(err)
	}
	got := readSnapshotBytes(t, manager, snapshot.Token)
	if string(got) != string(original) {
		t.Fatal("immutable snapshot did not preserve the catalog bytes exactly")
	}
	digest := sha256.Sum256(original)
	if snapshot.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("snapshot SHA-256 = %s", snapshot.SHA256)
	}
	if _, err := manager.Optimize(context.Background(), DatabaseComponentCatalog); err == nil {
		t.Fatal("immutable catalog accepted optimize")
	}
}

func TestSnapshotChunksCarryAuthoritativeDatabaseIDForDataAndEOF(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "components-v2.db")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	definition := testDefinition(DatabaseComponentCatalog, databasePath, false)
	definition.Optimizable = false
	manager := newTestManager(t, []Definition{definition}, Options{})

	snapshot, err := manager.CreateSnapshot(context.Background(), DatabaseComponentCatalog)
	if err != nil {
		t.Fatal(err)
	}
	dataChunk, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{
		Token: snapshot.Token, MaxBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dataChunk.DatabaseID != DatabaseComponentCatalog || len(dataChunk.Data) != 1 {
		t.Fatalf("data chunk = %+v", dataChunk)
	}
	eofChunk, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{
		Token: snapshot.Token, Offset: snapshot.SizeBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if eofChunk.DatabaseID != DatabaseComponentCatalog || !eofChunk.EOF {
		t.Fatalf("EOF chunk = %+v", eofChunk)
	}
	if released, err := manager.ReleaseSnapshot(snapshot.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestCheckOptimizeAndExpiredSnapshot(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "pdns.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePowerDNS, databasePath, true),
	}, Options{SnapshotTTL: time.Minute, Now: func() time.Time { return now }})

	check, err := manager.Check(context.Background(), DatabasePowerDNS)
	if err != nil || !check.IntegrityOK || !check.ForeignKeysOK {
		t.Fatalf("Check() = %+v, %v", check, err)
	}
	if _, err := manager.Optimize(context.Background(), DatabasePowerDNS); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{Token: snapshot.Token}); err == nil {
		t.Fatal("expired snapshot remained readable")
	}
	replacement, err := manager.CreateSnapshot(context.Background(), DatabasePowerDNS)
	if err != nil {
		t.Fatalf("slot remained held after expiry cleanup: %v", err)
	}
	if released, err := manager.ReleaseSnapshot(replacement.Token); err != nil || !released {
		t.Fatalf("ReleaseSnapshot() = %v, %v", released, err)
	}
}

func TestSnapshotExpiryExtendsWhileChunksAreRead(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "WAL")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{SnapshotTTL: time.Minute, Now: func() time.Time { return now }})

	snapshot, err := manager.CreateSnapshot(context.Background(), DatabasePanel)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(45 * time.Second)
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{Token: snapshot.Token, Offset: 0, MaxBytes: 1}); err != nil {
		t.Fatalf("first active chunk read failed: %v", err)
	}
	now = now.Add(45 * time.Second)
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{Token: snapshot.Token, Offset: 1, MaxBytes: 1}); err != nil {
		t.Fatalf("second active chunk read failed: %v", err)
	}
	now = now.Add(61 * time.Second)
	if _, err := manager.ReadSnapshotChunk(ReadSnapshotChunkRequest{Token: snapshot.Token, Offset: 2, MaxBytes: 1}); err == nil {
		t.Fatal("idle snapshot remained readable after the refreshed expiry")
	}
}

func TestUnsafeAndMalformedFilesNeverLeakManagedPath(t *testing.T) {
	root := t.TempDir()
	malformedPath := filepath.Join(root, "secret-control.sqlite3")
	if err := os.WriteFile(malformedPath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, malformedPath, true),
	}, Options{})
	_, err := manager.Check(context.Background(), DatabasePanel)
	if err == nil {
		t.Fatal("malformed database passed Check")
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), malformedPath) {
		t.Fatalf("error leaked managed path: %v", err)
	}
	pathError := &os.PathError{Op: "open", Path: malformedPath, Err: errors.New("private detail")}
	if got := publicDatabaseError(pathError).Error(); strings.Contains(got, malformedPath) || strings.Contains(got, "private detail") {
		t.Fatalf("publicDatabaseError leaked details: %q", got)
	}
	items, listErr := manager.List(context.Background())
	if listErr != nil || len(items) != 1 {
		t.Fatalf("List() = %+v, %v", items, listErr)
	}
	if got := strings.Join(items[0].Actions, ","); got != "" {
		t.Fatalf("malformed database actions = %q", got)
	}
	encoded, marshalErr := json.Marshal(items[0])
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), root) || strings.Contains(string(encoded), malformedPath) {
		t.Fatalf("malformed inventory leaked a managed path: %s", encoded)
	}
}

func TestSnapshotCleanupCannotEscapePrivateRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "celikpanel-system-sqlite-victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(victim, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateSnapshotDirectory(root, victim); err == nil {
		t.Fatal("cleanup accepted a directory outside the private root")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("outside sentinel was changed: %v", err)
	}

	valid, err := os.MkdirTemp(root, "celikpanel-system-sqlite-")
	if err != nil {
		t.Fatal(err)
	}
	if err := removePrivateSnapshotDirectory(root, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(valid); !os.IsNotExist(err) {
		t.Fatalf("valid private snapshot directory still exists: %v", err)
	}
}

func TestCleanupFailureKeepsSnapshotSlotFailClosedUntilRetrySucceeds(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "panel.sqlite3")
	database := createTestSQLite(t, databasePath, "DELETE")
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	snapshotRoot := filepath.Join(t.TempDir(), "snapshots")
	if err := os.Mkdir(snapshotRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, []Definition{
		testDefinition(DatabasePanel, databasePath, true),
	}, Options{SnapshotRoot: snapshotRoot})
	token := strings.Repeat("a", 64)
	unsafePath := filepath.Join(snapshotRoot, snapshotDirectoryPrefix+"cleanup-failure")
	if err := os.WriteFile(unsafePath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := &snapshotEntry{
		info:      SnapshotInfo{Token: token, ExpiresAt: time.Now().Add(time.Minute)},
		root:      snapshotRoot,
		directory: unsafePath,
	}
	manager.mu.Lock()
	manager.snapshots[token] = entry
	manager.snapshotSlotHeld = true
	manager.mu.Unlock()

	if released, err := manager.ReleaseSnapshot(token); err == nil || released {
		t.Fatalf("ReleaseSnapshot() = %v, %v; want fail-closed cleanup error", released, err)
	}
	if err := manager.claimSnapshotSlot(); err == nil {
		t.Fatal("cleanup failure opened the global snapshot slot")
	}
	if err := os.Remove(unsafePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(unsafePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if released, err := manager.ReleaseSnapshot(token); err != nil || !released {
		t.Fatalf("cleanup retry ReleaseSnapshot() = %v, %v", released, err)
	}
	if err := manager.claimSnapshotSlot(); err != nil {
		t.Fatalf("successful cleanup retry did not open the slot: %v", err)
	}
	manager.releaseSnapshotSlot()
}
