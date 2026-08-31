//go:build linux

package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type sqliteDeserializer interface {
	Deserialize([]byte) error
}

func countUsableUsersInSerializedSQLiteDatabase(serialized []byte) (int, error) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return 0, fmt.Errorf("open in-memory SQLite database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deserializeConnection, err := database.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("open SQLite deserialize connection: %w", err)
	}
	if err := deserializeConnection.Raw(func(driverConnection any) error {
		deserializer, ok := driverConnection.(sqliteDeserializer)
		if !ok {
			return fmt.Errorf("SQLite driver does not expose Deserialize")
		}
		return deserializer.Deserialize(serialized)
	}); err != nil {
		_ = deserializeConnection.Close()
		return 0, fmt.Errorf("deserialize pinned SQLite database in memory: %w", err)
	}
	if err := deserializeConnection.Close(); err != nil {
		return 0, fmt.Errorf("release SQLite deserialize connection: %w", err)
	}

	// modernc/sqlite documents that Deserialize invalidates every connection
	// that existed at the call. Acquire a new connection for every query below.
	connection, err := database.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("open deserialized SQLite connection: %w", err)
	}
	// Do not return this post-Deserialize connection to database/sql's idle
	// pool. modernc/sqlite v1.40.1 double-frees the Deserialize allocation when
	// that pool later closes it (including on Go 1.26). This mode is a dedicated
	// short-lived installer probe; DB.Close marks the pool closed and process
	// exit reclaims the one checked-out connection without touching the source.
	if _, err := connection.ExecContext(ctx, `PRAGMA temp_store = MEMORY`); err != nil {
		return 0, fmt.Errorf("keep SQLite temporary state in memory: %w", err)
	}
	var tempStore int
	if err := connection.QueryRowContext(ctx, `PRAGMA temp_store`).Scan(&tempStore); err != nil {
		return 0, fmt.Errorf("verify SQLite temporary-state storage: %w", err)
	}
	if tempStore != 2 {
		return 0, fmt.Errorf("SQLite temporary-state storage is not memory-only")
	}
	if _, err := connection.ExecContext(ctx, `PRAGMA query_only = ON`); err != nil {
		return 0, fmt.Errorf("make deserialized SQLite database query-only: %w", err)
	}
	var quickCheck string
	if err := connection.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return 0, fmt.Errorf("run in-memory SQLite database quick check: %w", err)
	}
	if quickCheck != "ok" {
		return 0, fmt.Errorf("in-memory SQLite database quick check failed")
	}

	var count int
	if err := connection.QueryRowContext(ctx, `
		SELECT COUNT(*) - COALESCE(SUM(CASE
			WHEN username = 'admin'
			 AND role = 'admin'
			 AND password_hash = ?
			THEN 1
			ELSE 0
		END), 0)
		FROM users
	`, deadPlaceholderAdminPasswordHash).Scan(&count); err != nil {
		return 0, fmt.Errorf("count usable users in in-memory SQLite database: %w", err)
	}
	if count < 0 {
		return 0, fmt.Errorf("in-memory SQLite database returned an invalid user count")
	}
	return count, nil
}
