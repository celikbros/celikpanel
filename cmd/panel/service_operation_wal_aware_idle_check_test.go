//go:build linux

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestWALAwareServiceOperationsIdleAcceptsValidNonEmptyWALWithoutChangingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database := openWALAwareTestDatabase(t, path)
	defer database.Close()
	writeWALAwareIdleMarker(t, database, "wal-aware-idle")
	requireNonEmptyWAL(t, path)

	before := captureSQLiteSourceStates(t, path)
	if err := checkWALAwareServiceOperationsIdle(path); err != nil {
		t.Fatalf("valid WAL-backed idle database rejected: %v", err)
	}
	after := captureSQLiteSourceStates(t, path)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if before[suffix] != after[suffix] {
			t.Fatalf(
				"source SQLite file %q changed\nbefore: %#v\nafter:  %#v",
				path+suffix,
				before[suffix],
				after[suffix],
			)
		}
	}
}

func TestWALAwareServiceOperationsIdleRejectsActiveRowOnlyInWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database := openWALAwareTestDatabase(t, path)
	defer database.Close()
	panel := &Panel{db: database}
	if _, err := panel.createServiceOperation(
		context.Background(),
		serviceOperationKindInstall,
		"certbot",
		"",
		serviceOperationActor{},
	); err != nil {
		t.Fatal(err)
	}
	requireNonEmptyWAL(t, path)

	if err := checkWALAwareServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
		t.Fatalf("active WAL-only row err=%v, want not idle", err)
	}
}

func TestWALAwarePreLedgerServiceOperationsIdleAcceptsValidWAL(t *testing.T) {
	path := createPreLedgerPanelDatabase(t)
	database, err := sql.Open(
		"sqlite",
		fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Ping(); err != nil {
		t.Fatal(err)
	}
	prepareWALAwareConnection(t, database)
	if _, err := database.Exec(
		`INSERT OR REPLACE INTO panel_settings(key, value) VALUES (?, ?)`,
		"wal-aware-pre-ledger",
		"ready",
	); err != nil {
		t.Fatal(err)
	}
	requireNonEmptyWAL(t, path)

	if err := checkWALAwarePreLedgerServiceOperationsIdle(path); err != nil {
		t.Fatalf("valid pre-ledger WAL rejected: %v", err)
	}
}

func TestWALAwarePreLedgerSnapshotLogicalEquivalenceAcceptsCommittedWALWithoutChangingSource(t *testing.T) {
	root := newSecureSnapshotTestRoot(t)
	path := createPreLedgerPanelDatabaseInDirectory(t, filepath.Join(root, "source"))
	database, err := sql.Open(
		"sqlite",
		fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Ping(); err != nil {
		t.Fatal(err)
	}
	prepareWALAwareConnection(t, database)
	if _, err := database.Exec(
		`INSERT OR REPLACE INTO panel_settings(key, value) VALUES (?, ?)`,
		"wal-aware-equivalence",
		"committed-only-in-wal",
	); err != nil {
		t.Fatal(err)
	}
	requireNonEmptyWAL(t, path)

	snapshotDirectory := filepath.Join(root, "snapshot")
	mustMkdirSnapshotTestDirectory(t, snapshotDirectory, 0o700)
	snapshotPath := filepath.Join(snapshotDirectory, serviceOperationSnapshotBasename)
	if err := createServiceOperationSnapshot(
		path,
		snapshotPath,
		serviceOperationSnapshotSchemaPreLedger,
	); err != nil {
		t.Fatalf("create standalone pre-ledger snapshot: %v", err)
	}

	before := captureSQLiteSourceStates(t, path)
	if err := proveWALAwarePreLedgerSnapshotLogicalEquivalence(path, snapshotPath); err != nil {
		t.Fatalf("equivalent WAL-aware pre-ledger snapshot rejected: %v", err)
	}
	after := captureSQLiteSourceStates(t, path)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if before[suffix] != after[suffix] {
			t.Fatalf(
				"source SQLite file %q changed\\nbefore: %#v\\nafter:  %#v",
				path+suffix,
				before[suffix],
				after[suffix],
			)
		}
	}
}

func TestWALAwareServiceOperationsIdleRejectsCorruptWALChecksums(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func([]byte)
	}{
		{
			name: "header checksum",
			mutate: func(wal []byte) {
				wal[24] ^= 0x80
			},
		},
		{
			name: "current frame data checksum",
			mutate: func(wal []byte) {
				wal[32+24] ^= 0x80
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := copiedWALAwareDatabase(t)
			walPath := path + "-wal"
			wal, err := os.ReadFile(walPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(wal) <= 32+24 {
				t.Fatalf("WAL is unexpectedly short: %d", len(wal))
			}
			testCase.mutate(wal)
			if err := os.WriteFile(walPath, wal, 0o600); err != nil {
				t.Fatal(err)
			}

			err = checkWALAwareServiceOperationsIdle(path)
			if !errors.Is(err, errServiceOperationsNotIdle) {
				t.Fatalf("corrupt WAL err=%v, want not idle", err)
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "checksum") {
				t.Fatalf("corrupt WAL did not report a checksum failure: %v", err)
			}
		})
	}
}

func TestWALAwareServiceOperationsIdleRejectsIncompleteCurrentFrame(t *testing.T) {
	path := copiedWALAwareDatabase(t)
	walPath := path + "-wal"
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 33 {
		t.Fatalf("WAL is unexpectedly short: %d", info.Size())
	}
	if err := os.Truncate(walPath, info.Size()-1); err != nil {
		t.Fatal(err)
	}

	err = checkWALAwareServiceOperationsIdle(path)
	if !errors.Is(err, errServiceOperationsNotIdle) {
		t.Fatalf("incomplete WAL frame err=%v, want not idle", err)
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "incomplete") {
		t.Fatalf("incomplete WAL frame did not report an incomplete tail: %v", err)
	}
}

func TestWALAwareServiceOperationsIdleHandlesReusedWALWithStaleTail(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		active bool
	}{
		{name: "idle current prefix"},
		{name: "active current prefix", active: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "panel.sqlite")
			database := openWALAwareTestDatabase(t, path)
			defer database.Close()
			for index := 0; index < 40; index++ {
				writeWALAwareIdleMarker(t, database, fmt.Sprintf("stale-tail-%03d", index))
			}
			oldWAL, err := os.ReadFile(path + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			checkpointWAL(t, database.GetDB(), "RESTART")
			if testCase.active {
				panel := &Panel{db: database}
				if _, err := panel.createServiceOperation(
					context.Background(),
					serviceOperationKindInstall,
					"nginx",
					"",
					serviceOperationActor{},
				); err != nil {
					t.Fatal(err)
				}
			} else {
				writeWALAwareIdleMarker(t, database, "current-idle-prefix")
			}
			newWAL, err := os.ReadFile(path + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			if len(newWAL) != len(oldWAL) {
				t.Fatalf("SQLite did not retain the reused WAL tail: old=%d new=%d", len(oldWAL), len(newWAL))
			}
			if string(newWAL[16:24]) == string(oldWAL[16:24]) {
				t.Fatal("SQLite did not rotate the WAL salt during RESTART reuse")
			}

			pinned, err := pinWALAwarePanelDatabase(path)
			if err != nil {
				t.Fatal(err)
			}
			recoveredBytes, recoverErr := recoverPinnedLiveSQLiteWAL(pinned.sidecars["-wal"])
			pinned.close()
			if recoverErr != nil {
				t.Fatal(recoverErr)
			}
			if recoveredBytes == 0 || recoveredBytes >= int64(len(newWAL)) {
				t.Fatalf("recovered prefix=%d, physical WAL=%d; stale tail was not excluded", recoveredBytes, len(newWAL))
			}

			err = checkWALAwareServiceOperationsIdle(path)
			if testCase.active {
				if !errors.Is(err, errServiceOperationsNotIdle) {
					t.Fatalf("active reused-WAL prefix err=%v, want not idle", err)
				}
			} else if err != nil {
				t.Fatalf("idle reused-WAL prefix rejected: %v", err)
			}
		})
	}
}

func openWALAwareTestDatabase(t *testing.T, path string) *paneldb.SQLiteDB {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	prepareWALAwareConnection(t, database.GetDB())
	return database
}

func prepareWALAwareConnection(t *testing.T, database *sql.DB) {
	t.Helper()
	checkpointWAL(t, database, "TRUNCATE")
	if _, err := database.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
}

func checkpointWAL(t *testing.T, database *sql.DB, mode string) {
	t.Helper()
	var busy, logFrames, checkpointedFrames int
	if err := database.QueryRow(`PRAGMA wal_checkpoint(`+mode+`)`).Scan(
		&busy,
		&logFrames,
		&checkpointedFrames,
	); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("WAL checkpoint %s was busy: log=%d checkpointed=%d", mode, logFrames, checkpointedFrames)
	}
}

func writeWALAwareIdleMarker(t *testing.T, database *paneldb.SQLiteDB, key string) {
	t.Helper()
	if _, err := database.GetDB().Exec(
		`INSERT OR REPLACE INTO panel_settings(key, value) VALUES (?, ?)`,
		key,
		"ready",
	); err != nil {
		t.Fatal(err)
	}
}

func requireNonEmptyWAL(t *testing.T, databasePath string) {
	t.Helper()
	info, err := os.Stat(databasePath + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 32 {
		t.Fatalf("expected a WAL with frames, size=%d", info.Size())
	}
}

func copiedWALAwareDatabase(t *testing.T) string {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, sourcePath)
	writeWALAwareIdleMarker(t, database, "copy-for-corruption")
	requireNonEmptyWAL(t, sourcePath)
	destinationPath := filepath.Join(t.TempDir(), "copy.sqlite")
	for _, suffix := range []string{"", "-wal"} {
		content, err := os.ReadFile(sourcePath + suffix)
		if err != nil {
			database.Close()
			t.Fatal(err)
		}
		if err := os.WriteFile(destinationPath+suffix, content, 0o600); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	database.Close()
	return destinationPath
}

type sqliteSourceState struct {
	exists         bool
	size           int64
	mode           os.FileMode
	modificationNS int64
	contentSHA256  [sha256.Size]byte
}

func captureSQLiteSourceStates(t *testing.T, databasePath string) map[string]sqliteSourceState {
	t.Helper()
	states := make(map[string]sqliteSourceState, 3)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := databasePath + suffix
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			states[suffix] = sqliteSourceState{}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		states[suffix] = sqliteSourceState{
			exists:         true,
			size:           info.Size(),
			mode:           info.Mode(),
			modificationNS: info.ModTime().UnixNano(),
			contentSHA256:  sha256.Sum256(content),
		}
	}
	return states
}
