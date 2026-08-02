package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSQLiteDBConfiguresBoundedWALPool(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)

	raw := database.GetDB()
	if got := raw.Stats().MaxOpenConnections; got != sqliteMaxOpenConnections {
		t.Fatalf("MaxOpenConnections=%d, want %d", got, sqliteMaxOpenConnections)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connections := make([]*sql.Conn, 0, sqliteMaxOpenConnections)
	t.Cleanup(func() {
		for _, conn := range connections {
			_ = conn.Close()
		}
	})
	for range sqliteMaxOpenConnections {
		conn, err := raw.Conn(ctx)
		if err != nil {
			t.Fatalf("acquire connection: %v", err)
		}
		connections = append(connections, conn)

		var journalMode string
		if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
			t.Fatalf("journal_mode: %v", err)
		}
		if journalMode != "wal" {
			t.Fatalf("journal_mode=%q, want wal", journalMode)
		}

		var foreignKeys int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
			t.Fatalf("foreign_keys: %v", err)
		}
		if foreignKeys != 1 {
			t.Fatalf("foreign_keys=%d, want 1", foreignKeys)
		}

		var busyTimeout int64
		if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatalf("busy_timeout: %v", err)
		}
		if want := sqliteBusyTimeout.Milliseconds(); busyTimeout != want {
			t.Fatalf("busy_timeout=%d, want %d", busyTimeout, want)
		}
	}
	blockedCtx, cancelBlocked := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelBlocked()
	if _, err := raw.Conn(blockedCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fifth connection error=%v, want deadline exceeded", err)
	}
}
