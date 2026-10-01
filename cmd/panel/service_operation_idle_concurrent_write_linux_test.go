//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// upd4 F4: the only refusal the updater may read once more is a concurrent
// write to the live database or its -wal/-shm (exit 75). A busy operation
// queue, a rollback journal, a replaced or re-moded file, a wrapped private-copy
// failure and a joined error keep exit 1 and are never re-read.
func TestConcurrentPanelWriteIsTheOnlyRetryableIdleRefusal(t *testing.T) {
	fixture := func(t *testing.T, sidecars ...string) (string, *pinnedPanelDatabase) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "celikpanel.db")
		for _, suffix := range append([]string{""}, sidecars...) {
			if err := os.WriteFile(path+suffix, []byte("pinned"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		pinned, err := pinWALAwarePanelDatabase(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pinned.close)
		if err := pinned.verifyPath(); err != nil {
			t.Fatalf("unchanged pin refused: %v", err)
		}
		return path, pinned
	}
	write := func(t *testing.T, path string) {
		t.Helper()
		time.Sleep(10 * time.Millisecond)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("+commit"); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	retryable := func(t *testing.T, name string, err error) {
		t.Helper()
		if !errors.Is(err, errServiceOperationsNotIdle) || !isConcurrentPanelDatabaseWrite(err) ||
			serviceOperationIdleExitCode(err) != serviceOperationIdleConcurrentWriteExitCode {
			t.Fatalf("%s: %v (exit %d) is not the concurrent-write refusal", name, err, serviceOperationIdleExitCode(err))
		}
	}
	final := func(t *testing.T, name string, err error) {
		t.Helper()
		if err == nil || isConcurrentPanelDatabaseWrite(err) || serviceOperationIdleExitCode(err) != 1 {
			t.Fatalf("%s: %v (exit %d) must stay a final refusal", name, err, serviceOperationIdleExitCode(err))
		}
	}

	t.Run("shm rewritten", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		write(t, path+"-shm")
		err := pinned.verifyPath()
		retryable(t, "shm", err)
		if err.Error() != "service operations are not idle: SQLite sidecar -shm changed after pinning" {
			t.Fatalf("message changed: %q", err)
		}
	})
	t.Run("wal appended", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		write(t, path+"-wal")
		retryable(t, "wal", pinned.verifyPath())
	})
	t.Run("wal removed on last close", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		if err := os.Remove(path + "-wal"); err != nil {
			t.Fatal(err)
		}
		retryable(t, "wal removed", pinned.verifyPath())
	})
	t.Run("wal recreated", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		if err := os.Remove(path + "-wal"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+"-wal", []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		retryable(t, "wal recreated", pinned.verifyPath())
	})
	t.Run("shm appeared", func(t *testing.T) {
		path, pinned := fixture(t)
		if err := os.WriteFile(path+"-shm", []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		retryable(t, "shm appeared", pinned.verifyPath())
	})
	t.Run("database checkpoint", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		write(t, path)
		retryable(t, "database", pinned.verifyPath())
	})
	t.Run("rollback journal appeared", func(t *testing.T) {
		path, pinned := fixture(t)
		if err := os.WriteFile(path+"-journal", []byte("journal"), 0o600); err != nil {
			t.Fatal(err)
		}
		final(t, "journal", pinned.verifyPath())
	})
	t.Run("database replaced", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		if err := os.WriteFile(path+".new", []byte("other"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".new", path); err != nil {
			t.Fatal(err)
		}
		final(t, "replaced", pinned.verifyPath())
	})
	t.Run("database re-moded", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		final(t, "chmod", pinned.verifyPath())
	})
	t.Run("sidecar re-moded", func(t *testing.T) {
		path, pinned := fixture(t, "-wal", "-shm")
		if err := os.Chmod(path+"-shm", 0o644); err != nil {
			t.Fatal(err)
		}
		final(t, "chmod sidecar", pinned.verifyPath())
	})
	t.Run("wrapped and joined", func(t *testing.T) {
		changed := &panelDatabaseChangedError{detail: "SQLite sidecar -shm changed after pinning"}
		final(t, "wrapped", errors.Join(changed, errors.New("remove private live SQLite snapshot")))
		final(t, "private copy", fmt.Errorf("validate private live SQLite snapshot: %w", changed))
	})
	t.Run("busy queue", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database := openWALAwareTestDatabase(t, path)
		defer database.Close()
		panel := &Panel{db: database}
		if _, err := panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{}); err != nil {
			t.Fatal(err)
		}
		requireNonEmptyWAL(t, path)
		err := checkWALAwareServiceOperationsIdle(path)
		if !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("active row err=%v", err)
		}
		final(t, "busy", err)
	})
}
