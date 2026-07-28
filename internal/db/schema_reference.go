package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type SQLiteSchemaObject struct {
	Type      string
	Name      string
	TableName string
	SQL       string
}

// ReferenceSQLiteUserSchema builds the exact empty schema produced by the
// embedded migrations through targetVersion and returns its user objects.
// ReferenceSQLiteUserSchema, gömülü migration'ların targetVersion sürümüne
// kadar ürettiği tam boş şemayı kurar ve kullanıcı nesnelerini döndürür.
func ReferenceSQLiteUserSchema(
	ctx context.Context,
	targetVersion int,
) (objects []SQLiteSchemaObject, returnErr error) {
	if targetVersion < 1 {
		return nil, fmt.Errorf("reference schema target version must be positive")
	}
	database, err := sql.Open(
		"sqlite",
		"file:celikpanel-reference-schema?mode=memory&cache=private&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		return nil, fmt.Errorf("open reference schema database: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, database.Close())
	}()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open reference schema connection: %w", err)
	}
	if _, err := database.ExecContext(ctx, createSchemaMigrationsSQL); err != nil {
		return nil, fmt.Errorf("create reference migration ledger: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	expectedVersion := 1
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return nil, fmt.Errorf("parse embedded migration %q: %w", name, err)
		}
		if version > targetVersion {
			continue
		}
		if version != expectedVersion {
			return nil, fmt.Errorf(
				"embedded migration sequence expected version %d, got %d",
				expectedVersion,
				version,
			)
		}
		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %q: %w", name, err)
		}
		transaction, err := database.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin reference migration %q: %w", name, err)
		}
		if _, err := transaction.ExecContext(ctx, string(content)); err != nil {
			_ = transaction.Rollback()
			return nil, fmt.Errorf("apply reference migration %q: %w", name, err)
		}
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO schema_migrations(version) VALUES (?)`,
			version,
		); err != nil {
			_ = transaction.Rollback()
			return nil, fmt.Errorf("record reference migration %q: %w", name, err)
		}
		if err := transaction.Commit(); err != nil {
			return nil, fmt.Errorf("commit reference migration %q: %w", name, err)
		}
		expectedVersion++
	}
	if expectedVersion != targetVersion+1 {
		return nil, fmt.Errorf("embedded migration version %d is unavailable", targetVersion)
	}
	return ReadSQLiteUserSchema(ctx, database)
}

// ReadSQLiteUserSchema returns every user-created table, index, trigger and
// view in deterministic order; SQLite-owned objects are excluded.
// ReadSQLiteUserSchema, kullanıcı tarafından oluşturulan her tablo, indeks,
// tetikleyici ve görünümü belirli sırada döndürür; SQLite nesnelerini dışlar.
func ReadSQLiteUserSchema(
	ctx context.Context,
	database *sql.DB,
) ([]SQLiteSchemaObject, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT type, name, tbl_name, sql
		FROM sqlite_schema
		WHERE type IN ('table', 'index', 'trigger', 'view')
		  AND name NOT GLOB 'sqlite_*'
		  AND sql IS NOT NULL
		ORDER BY type ASC, name ASC, tbl_name ASC`)
	if err != nil {
		return nil, fmt.Errorf("read SQLite user schema: %w", err)
	}
	defer rows.Close()

	objects := make([]SQLiteSchemaObject, 0)
	for rows.Next() {
		var object SQLiteSchemaObject
		if err := rows.Scan(
			&object.Type,
			&object.Name,
			&object.TableName,
			&object.SQL,
		); err != nil {
			return nil, fmt.Errorf("scan SQLite schema object: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SQLite user schema: %w", err)
	}
	return objects, nil
}
