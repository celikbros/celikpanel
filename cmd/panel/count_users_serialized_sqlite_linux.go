//go:build linux

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
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

	userColumns, err := readOnlyAdmissionUserColumns(ctx, connection)
	if err != nil {
		return 0, err
	}
	for _, required := range []string{"username", "password_hash", "email", "role"} {
		if !userColumns[required] {
			return 0, fmt.Errorf("in-memory SQLite users schema has no %s column", required)
		}
	}
	predicates := []string{"role = 'admin'"}
	if userColumns["status"] {
		// Migration 004 documents active/suspended but deliberately leaves
		// enforcement to code. Match the login/session gate exactly: NULL is
		// legacy-active and only suspended is denied. A stricter active-only
		// probe would refuse rows that the current authentication path accepts.
		predicates = append(predicates, "COALESCE(status, 'active') != 'suspended'")
	}
	if userColumns["account_type"] {
		// Empty/NULL is the repository's explicit pre-migration legacy account
		// representation. Any persisted non-account marker fails closed.
		predicates = append(predicates,
			"(account_type = 'account' OR account_type = '' OR account_type IS NULL)")
	}
	rows, err := connection.QueryContext(ctx,
		"SELECT password_hash FROM users WHERE "+strings.Join(predicates, " AND "))
	if err != nil {
		return 0, fmt.Errorf("select administrator credentials in in-memory SQLite database: %w", err)
	}
	// TOTP state is intentionally not part of this row-level admission count.
	// A parseable password hash proves eligibility for the first login step;
	// neither possession of a current TOTP code nor a usable secret can be
	// established by a read-only database probe.
	defer rows.Close()
	count := 0
	for rows.Next() {
		var passwordHash string
		if err := rows.Scan(&passwordHash); err != nil {
			return 0, fmt.Errorf("read administrator credential in in-memory SQLite database: %w", err)
		}
		if auth.ValidatePasswordHash(passwordHash) == nil {
			count++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate administrator credentials in in-memory SQLite database: %w", err)
	}
	if count < 0 {
		return 0, fmt.Errorf("in-memory SQLite database returned an invalid user count")
	}
	return count, nil
}

func readOnlyAdmissionUserColumns(
	ctx context.Context,
	connection *sql.Conn,
) (map[string]bool, error) {
	rows, err := connection.QueryContext(ctx, `PRAGMA table_xinfo('users')`)
	if err != nil {
		return nil, fmt.Errorf("inspect in-memory SQLite users schema: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var (
			columnID     int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
			hidden       int
		)
		if err := rows.Scan(
			&columnID,
			&name,
			&columnType,
			&notNull,
			&defaultValue,
			&primaryKey,
			&hidden,
		); err != nil {
			return nil, fmt.Errorf("read in-memory SQLite users schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate in-memory SQLite users schema: %w", err)
	}
	return columns, nil
}
