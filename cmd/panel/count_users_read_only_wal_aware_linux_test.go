//go:build linux

package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCountUsableUsersReadOnlyWALAwareIncludesCommittedWALWithoutChangingSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database := openWALAwareTestDatabase(t, path)
	defer database.Close()
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('real-admin', 'real-password-hash', 'real@example.test', 'admin'),
		       ('admin', ?, 'placeholder@example.test', 'admin')
	`, deadPlaceholderAdminPasswordHash); err != nil {
		t.Fatalf("write WAL-only users: %v", err)
	}
	requireNonEmptyWAL(t, path)

	before := captureSQLiteSourceStates(t, path)
	assertNoNamedAdmissionWorkspace := watchForNamedAdmissionWorkspaces(t)
	count, err := countUsableUsersReadOnlyWALAware(path)
	assertNoNamedAdmissionWorkspace()
	if err != nil {
		t.Fatalf("count usable users: %v", err)
	}
	if count != 1 {
		t.Fatalf("usable user count = %d, want 1", count)
	}
	assertSQLiteSourceStatesUnchanged(t, path, before)
}

func watchForNamedAdmissionWorkspaces(t *testing.T) func() {
	t.Helper()
	admissionTemp := t.TempDir()
	t.Setenv("TMPDIR", admissionTemp)
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatalf("open /tmp admission workspace watcher: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if _, err := unix.InotifyAddWatch(fd, admissionTemp, unix.IN_CREATE|unix.IN_MOVED_TO); err != nil {
		t.Fatalf("watch admission temporary directory: %v", err)
	}
	return func() {
		t.Helper()
		buffer := make([]byte, 64*1024)
		for {
			read, err := unix.Read(fd, buffer)
			if err == unix.EAGAIN {
				return
			}
			if err != nil {
				t.Fatalf("read /tmp admission workspace events: %v", err)
			}
			for offset := 0; offset+16 <= read; {
				nameLength := int(binary.NativeEndian.Uint32(buffer[offset+12 : offset+16]))
				eventEnd := offset + 16 + nameLength
				if eventEnd > read {
					t.Fatal("truncated /tmp admission workspace event")
				}
				name := strings.TrimRight(string(buffer[offset+16:eventEnd]), "\x00")
				t.Fatalf("read-only admission created temporary path %q", name)
				offset = eventEnd
			}
		}
	}
}

func TestCountUsableUsersReadOnlyWALAwareExcludesMigrationOnePlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration-one.sqlite")
	database, err := sql.Open(
		"sqlite",
		fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path),
	)
	if err != nil {
		t.Fatalf("open migration-one database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE users (
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create migration-one users table: %v", err)
	}
	prepareWALAwareConnection(t, database)
	if _, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('admin', ?, 'admin@example.com', 'admin')
	`, deadPlaceholderAdminPasswordHash); err != nil {
		t.Fatalf("write migration-one placeholder: %v", err)
	}
	requireNonEmptyWAL(t, path)

	before := captureSQLiteSourceStates(t, path)
	count, err := countUsableUsersReadOnlyWALAware(path)
	if err != nil {
		t.Fatalf("count migration-one placeholder: %v", err)
	}
	if count != 0 {
		t.Fatalf("usable user count = %d, want 0 for the dead placeholder", count)
	}
	assertSQLiteSourceStatesUnchanged(t, path, before)
}

func TestCountUsableUsersReadOnlyWALAwareFailsClosedOnUnusableDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(path, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}
	if _, err := countUsableUsersReadOnlyWALAware(path); err == nil {
		t.Fatal("unusable database was accepted")
	}
}

func TestCountUsableUsersReadOnlyWALAwareKeepsForcedTemporaryStateInMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temp-state.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		CREATE TABLE raw_users (
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			email TEXT NOT NULL,
			role TEXT NOT NULL
		);
		CREATE VIEW users AS
			SELECT username,
			       MAX(password_hash) AS password_hash,
			       MAX(email) AS email,
			       MAX(role) AS role
			FROM raw_users
			GROUP BY username
			ORDER BY email;
	`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	transaction, err := database.Begin()
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	statement, err := transaction.Prepare(`
		INSERT INTO raw_users(username, password_hash, email, role)
		VALUES (?, ?, ?, 'admin')
	`)
	if err != nil {
		_ = transaction.Rollback()
		_ = database.Close()
		t.Fatal(err)
	}
	padding := strings.Repeat("x", 1024)
	const rows = 4096
	for index := 0; index < rows; index++ {
		if _, err := statement.Exec(
			fmt.Sprintf("admin-%06d", index),
			padding,
			fmt.Sprintf("admin-%06d@example.test", rows-index),
		); err != nil {
			_ = statement.Close()
			_ = transaction.Rollback()
			_ = database.Close()
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		_ = transaction.Rollback()
		_ = database.Close()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	assertNoTemporaryPath := watchForNamedAdmissionWorkspaces(t)
	count, err := countUsableUsersReadOnlyWALAware(path)
	assertNoTemporaryPath()
	if err != nil {
		t.Fatalf("count users through forced grouping/sort view: %v", err)
	}
	if count != rows {
		t.Fatalf("usable user count = %d, want %d", count, rows)
	}
}

func assertSQLiteSourceStatesUnchanged(t *testing.T, path string, before map[string]sqliteSourceState) {
	t.Helper()
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
