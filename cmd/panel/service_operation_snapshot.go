package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	sqlite "modernc.org/sqlite"
)

type serviceOperationSnapshotSchema string

const (
	serviceOperationSnapshotBasename                                       = "celikpanel.db"
	serviceOperationSnapshotSchemaNormal    serviceOperationSnapshotSchema = "normal"
	serviceOperationSnapshotSchemaPreLedger serviceOperationSnapshotSchema = "pre-ledger"
	knownLegacySchemaMigrationsSQL                                         = `CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT DEFAULT (datetime('now'))
		)`
)

type sqliteOnlineBackupConnection interface {
	NewBackup(string) (*sqlite.Backup, error)
}

func parseServiceOperationSnapshotSchema(value string) (serviceOperationSnapshotSchema, error) {
	switch serviceOperationSnapshotSchema(strings.TrimSpace(value)) {
	case serviceOperationSnapshotSchemaNormal:
		return serviceOperationSnapshotSchemaNormal, nil
	case serviceOperationSnapshotSchemaPreLedger:
		return serviceOperationSnapshotSchemaPreLedger, nil
	default:
		return "", fmt.Errorf("snapshot schema must be exactly normal or pre-ledger")
	}
}

// createServiceOperationSnapshot copies the live SQLite database with SQLite's
// online backup API, validates the standalone copy, and publishes it without
// replacing an existing destination.
// createServiceOperationSnapshot, canlı SQLite veritabanını SQLite'ın çevrim içi
// yedekleme API'siyle kopyalar, bağımsız kopyayı doğrular ve mevcut hedefin
// üzerine yazmadan yayımlar.
func createServiceOperationSnapshot(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
) (returnErr error) {
	return createServiceOperationSnapshotWithCopy(
		sourcePath,
		destinationPath,
		schema,
		copySQLiteDatabaseOnline,
	)
}

func createQuarantinedServiceOperationSnapshot(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
) error {
	return createServiceOperationSnapshotWithCopy(
		sourcePath,
		destinationPath,
		schema,
		copySQLiteDatabaseImmutable,
	)
}

func createVerifiedQuarantinedServiceOperationSnapshot(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	verifySource func() error,
) error {
	return createServiceOperationSnapshotWithCopyAndVerify(
		sourcePath,
		destinationPath,
		schema,
		copySQLiteDatabaseImmutable,
		verifySource,
	)
}

func createServiceOperationSnapshotWithCopy(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	copyDatabase func(string, string) error,
) (returnErr error) {
	return createServiceOperationSnapshotWithCopyAndVerify(
		sourcePath,
		destinationPath,
		schema,
		copyDatabase,
		nil,
	)
}

func createServiceOperationSnapshotWithCopyAndVerify(
	sourcePath string,
	destinationPath string,
	schema serviceOperationSnapshotSchema,
	copyDatabase func(string, string) error,
	verifySource func() error,
) (returnErr error) {
	if schema != serviceOperationSnapshotSchemaNormal &&
		schema != serviceOperationSnapshotSchemaPreLedger {
		return fmt.Errorf("unsupported snapshot schema %q", schema)
	}

	sourcePath, err := filepath.Abs(filepath.Clean(sourcePath))
	if err != nil {
		return fmt.Errorf("resolve panel database source: %w", err)
	}
	destination, err := prepareServiceOperationSnapshotDestination(destinationPath)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, destination.cleanup())
		}
		returnErr = errors.Join(returnErr, destination.close())
	}()

	stagePath, err := destination.createStage()
	if err != nil {
		return err
	}
	if err := copyDatabase(sourcePath, stagePath); err != nil {
		return fmt.Errorf("create SQLite online backup: %w", err)
	}
	if err := canonicalizeKnownLegacySnapshotSchemaMigrations(stagePath); err != nil {
		return fmt.Errorf("canonicalize known legacy snapshot schema: %w", err)
	}
	if err := destination.syncAndVerifyStage(); err != nil {
		return err
	}

	if err := destination.validateStage(schema); err != nil {
		return err
	}
	if verifySource != nil {
		if err := verifySource(); err != nil {
			return fmt.Errorf("revalidate quarantined SQLite source before snapshot publish: %w", err)
		}
	}
	if err := destination.publish(); err != nil {
		return err
	}
	return nil
}

func copySQLiteDatabaseOnline(sourcePath string, destinationPath string) error {
	return copySQLiteDatabaseFromURI(sqliteSnapshotURI(sourcePath, true), destinationPath)
}

func copySQLiteDatabaseImmutable(sourcePath string, destinationPath string) error {
	sourceURI, err := url.Parse(sqliteSnapshotURI(sourcePath, true))
	if err != nil {
		return fmt.Errorf("build immutable SQLite source URI: %w", err)
	}
	query := sourceURI.Query()
	query.Set("immutable", "1")
	sourceURI.RawQuery = query.Encode()
	return copySQLiteDatabaseFromURI(sourceURI.String(), destinationPath)
}

func copySQLiteDatabaseFromURI(sourceURI string, destinationPath string) error {
	database, err := sql.Open("sqlite", sourceURI)
	if err != nil {
		return fmt.Errorf("open source read-only: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("read source database: %w", err)
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve source connection: %w", err)
	}
	defer connection.Close()

	destinationURI := sqliteSnapshotURI(destinationPath, false)
	if err := connection.Raw(func(driverConnection any) error {
		onlineBackupConnection, ok := driverConnection.(sqliteOnlineBackupConnection)
		if !ok {
			return fmt.Errorf("SQLite driver does not expose the online backup API")
		}
		backup, err := onlineBackupConnection.NewBackup(destinationURI)
		if err != nil {
			return fmt.Errorf("start SQLite online backup: %w", err)
		}
		more, stepErr := backup.Step(-1)
		if stepErr == nil && more {
			stepErr = fmt.Errorf("SQLite online backup did not reach a complete snapshot")
		}
		finishErr := backup.Finish()
		return errors.Join(stepErr, finishErr)
	}); err != nil {
		return err
	}
	return normalizeStandaloneSQLiteSnapshot(destinationPath)
}

func normalizeStandaloneSQLiteSnapshot(databasePath string) (returnErr error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, false))
	if err != nil {
		return fmt.Errorf("open completed SQLite backup: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, database.Close())
	}()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var journalMode string
	if err := database.QueryRowContext(ctx, `PRAGMA journal_mode=DELETE`).Scan(&journalMode); err != nil {
		return fmt.Errorf("set standalone SQLite journal mode: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "delete") {
		return fmt.Errorf("standalone SQLite journal mode is %q, expected delete", journalMode)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		return fmt.Errorf("set standalone SQLite synchronous mode: %w", err)
	}
	return nil
}

type serviceOperationSnapshotMigrationRow struct {
	version               int
	appliedAt             sql.NullString
	appliedAtStorageClass string
}

// canonicalizeKnownLegacySnapshotSchemaMigrations rewrites only the copied
// snapshot stage when schema_migrations has the exact token sequence emitted
// by CelikPanel but differs from the embedded contract only in insignificant
// whitespace. The table shape is independently proved through SQLite's schema
// PRAGMAs, and every other schema object must already match the embedded
// contract. Semantic variants are deliberately left untouched so the normal
// exact-schema validator rejects them.
//
// This function must only receive a private standalone copy. It is never
// called with the canonical live database path.
func canonicalizeKnownLegacySnapshotSchemaMigrations(
	databasePath string,
) (returnErr error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, false))
	if err != nil {
		return fmt.Errorf("open copied SQLite snapshot: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, database.Close())
	}()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("read copied SQLite snapshot: %w", err)
	}

	actualObjects, err := paneldb.ReadSQLiteUserSchema(ctx, database)
	if err != nil {
		return fmt.Errorf("read copied snapshot schema contract: %w", err)
	}
	legacyIndex := -1
	for index, object := range actualObjects {
		if object.Type == "table" &&
			object.Name == "schema_migrations" &&
			object.TableName == "schema_migrations" {
			legacyIndex = index
			break
		}
	}
	if legacyIndex < 0 {
		return nil
	}

	var maximumVersion int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&maximumVersion); err != nil {
		return fmt.Errorf("inspect copied snapshot migration version: %w", err)
	}
	expectedObjects, err := paneldb.ReferenceSQLiteUserSchema(ctx, maximumVersion)
	if err != nil {
		return fmt.Errorf("build copied snapshot reference schema: %w", err)
	}
	canonicalSQL := ""
	for _, object := range expectedObjects {
		if object.Type == "table" &&
			object.Name == "schema_migrations" &&
			object.TableName == "schema_migrations" {
			canonicalSQL = object.SQL
			break
		}
	}
	if canonicalSQL == "" {
		return fmt.Errorf("embedded migration ledger schema is unavailable")
	}
	if actualObjects[legacyIndex].SQL == canonicalSQL {
		return nil
	}
	if compactSQLiteSchemaWhitespace(actualObjects[legacyIndex].SQL) !=
		compactSQLiteSchemaWhitespace(knownLegacySchemaMigrationsSQL) {
		return nil
	}

	// Prove that migration-ledger formatting is the only schema difference before
	// dropping the copied ledger table. This prevents the rebuild from erasing
	// an unexpected index or trigger and accidentally hiding schema drift.
	comparableObjects := append([]paneldb.SQLiteSchemaObject(nil), actualObjects...)
	comparableObjects[legacyIndex].SQL = canonicalSQL
	if err := compareServiceOperationSnapshotSchemaObjects(
		expectedObjects,
		comparableObjects,
	); err != nil {
		return fmt.Errorf("validate compatible copied schema: %w", err)
	}
	if err := validateCanonicalizableSnapshotSchemaMigrations(ctx, database); err != nil {
		return fmt.Errorf("validate copied migration ledger shape: %w", err)
	}

	var invalidAppliedAtStorageClasses int
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM schema_migrations
		WHERE typeof(applied_at) NOT IN ('null', 'text')
	`).Scan(&invalidAppliedAtStorageClasses); err != nil {
		return fmt.Errorf("inspect copied migration ledger applied_at storage classes: %w", err)
	}
	if invalidAppliedAtStorageClasses != 0 {
		return fmt.Errorf(
			"copied migration ledger contains %d unsupported applied_at storage class value(s)",
			invalidAppliedAtStorageClasses,
		)
	}

	migrationRows, err := readServiceOperationSnapshotMigrationRows(ctx, database)
	if err != nil {
		return err
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin copied migration ledger canonicalization: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = transaction.Rollback()
		}
	}()
	if _, err := transaction.ExecContext(ctx, `DROP TABLE schema_migrations`); err != nil {
		return fmt.Errorf("drop copied legacy migration ledger: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, canonicalSQL); err != nil {
		return fmt.Errorf("create canonical copied migration ledger: %w", err)
	}
	statement, err := transaction.PrepareContext(
		ctx,
		`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare copied migration ledger restore: %w", err)
	}
	for _, row := range migrationRows {
		if _, err := statement.ExecContext(ctx, row.version, row.appliedAt); err != nil {
			_ = statement.Close()
			return fmt.Errorf("restore copied migration ledger version %d: %w", row.version, err)
		}
	}
	if err := statement.Close(); err != nil {
		return fmt.Errorf("close copied migration ledger restore: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit copied migration ledger canonicalization: %w", err)
	}

	var storedSQL string
	if err := database.QueryRowContext(ctx, `
		SELECT sql
		FROM sqlite_schema
		WHERE type = 'table'
		  AND name = 'schema_migrations'
		  AND tbl_name = 'schema_migrations'
	`).Scan(&storedSQL); err != nil {
		return fmt.Errorf("verify canonical copied migration ledger schema: %w", err)
	}
	if storedSQL != canonicalSQL {
		return fmt.Errorf("canonical copied migration ledger schema differs after rebuild")
	}
	restoredRows, err := readServiceOperationSnapshotMigrationRows(ctx, database)
	if err != nil {
		return err
	}
	if !equalServiceOperationSnapshotMigrationRows(migrationRows, restoredRows) {
		return fmt.Errorf("copied migration ledger changed during canonicalization")
	}
	return nil
}

func compactSQLiteSchemaWhitespace(value string) string {
	var compact strings.Builder
	compact.Grow(len(value))
	for _, character := range canonicalExactSQLiteSchemaSQL(value) {
		if unicode.IsSpace(character) {
			continue
		}
		compact.WriteRune(character)
	}
	return compact.String()
}

func validateCanonicalizableSnapshotSchemaMigrations(
	ctx context.Context,
	database *sql.DB,
) error {
	type migrationColumn struct {
		cid          int
		name         string
		declaredType string
		notNull      int
		defaultValue sql.NullString
		primaryKey   int
		hidden       int
	}
	expectedColumns := []migrationColumn{
		{
			cid:          0,
			name:         "version",
			declaredType: "INTEGER",
			notNull:      0,
			primaryKey:   1,
			hidden:       0,
		},
		{
			cid:          1,
			name:         "applied_at",
			declaredType: "TEXT",
			notNull:      0,
			defaultValue: sql.NullString{
				String: "datetime('now')",
				Valid:  true,
			},
			primaryKey: 0,
			hidden:     0,
		},
	}

	columnRows, err := database.QueryContext(ctx, `
		SELECT cid, name, type, "notnull", dflt_value, pk, hidden
		FROM pragma_table_xinfo(?)
		ORDER BY cid ASC
	`, "schema_migrations")
	if err != nil {
		return fmt.Errorf("inspect migration ledger columns: %w", err)
	}
	actualColumns := make([]migrationColumn, 0, len(expectedColumns))
	for columnRows.Next() {
		var column migrationColumn
		if err := columnRows.Scan(
			&column.cid,
			&column.name,
			&column.declaredType,
			&column.notNull,
			&column.defaultValue,
			&column.primaryKey,
			&column.hidden,
		); err != nil {
			columnRows.Close()
			return fmt.Errorf("read migration ledger column: %w", err)
		}
		actualColumns = append(actualColumns, column)
	}
	if err := columnRows.Err(); err != nil {
		columnRows.Close()
		return fmt.Errorf("iterate migration ledger columns: %w", err)
	}
	if err := columnRows.Close(); err != nil {
		return fmt.Errorf("close migration ledger columns: %w", err)
	}
	if len(actualColumns) != len(expectedColumns) {
		return fmt.Errorf(
			"migration ledger has %d columns, expected %d",
			len(actualColumns),
			len(expectedColumns),
		)
	}
	for index := range expectedColumns {
		if actualColumns[index] != expectedColumns[index] {
			return fmt.Errorf(
				"migration ledger column %d differs from the canonical contract",
				index,
			)
		}
	}

	tableRows, err := database.QueryContext(ctx, `
		SELECT "schema", name, type, ncol, wr, strict
		FROM pragma_table_list(?)
		ORDER BY "schema" ASC, name ASC
	`, "schema_migrations")
	if err != nil {
		return fmt.Errorf("inspect migration ledger table flags: %w", err)
	}
	tableCount := 0
	for tableRows.Next() {
		var schemaName, tableName, tableType string
		var columnCount, withoutRowID, strict int
		if err := tableRows.Scan(
			&schemaName,
			&tableName,
			&tableType,
			&columnCount,
			&withoutRowID,
			&strict,
		); err != nil {
			tableRows.Close()
			return fmt.Errorf("read migration ledger table flags: %w", err)
		}
		tableCount++
		if schemaName != "main" ||
			tableName != "schema_migrations" ||
			tableType != "table" ||
			columnCount != 2 ||
			withoutRowID != 0 ||
			strict != 0 {
			tableRows.Close()
			return fmt.Errorf("migration ledger table flags differ from the canonical contract")
		}
	}
	if err := tableRows.Err(); err != nil {
		tableRows.Close()
		return fmt.Errorf("iterate migration ledger table flags: %w", err)
	}
	if err := tableRows.Close(); err != nil {
		return fmt.Errorf("close migration ledger table flags: %w", err)
	}
	if tableCount != 1 {
		return fmt.Errorf("migration ledger table count is %d, expected 1", tableCount)
	}

	var indexCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM pragma_index_list(?)`,
		"schema_migrations",
	).Scan(&indexCount); err != nil {
		return fmt.Errorf("inspect migration ledger indexes: %w", err)
	}
	if indexCount != 0 {
		return fmt.Errorf("migration ledger has %d unexpected index(es)", indexCount)
	}

	var foreignKeyCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM pragma_foreign_key_list(?)`,
		"schema_migrations",
	).Scan(&foreignKeyCount); err != nil {
		return fmt.Errorf("inspect migration ledger foreign keys: %w", err)
	}
	if foreignKeyCount != 0 {
		return fmt.Errorf(
			"migration ledger has %d unexpected foreign key(s)",
			foreignKeyCount,
		)
	}
	return nil
}

func readServiceOperationSnapshotMigrationRows(
	ctx context.Context,
	database *sql.DB,
) ([]serviceOperationSnapshotMigrationRow, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT version, applied_at, typeof(applied_at)
		FROM schema_migrations
		ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("read copied migration ledger: %w", err)
	}
	defer rows.Close()

	result := make([]serviceOperationSnapshotMigrationRow, 0)
	for rows.Next() {
		var row serviceOperationSnapshotMigrationRow
		if err := rows.Scan(
			&row.version,
			&row.appliedAt,
			&row.appliedAtStorageClass,
		); err != nil {
			return nil, fmt.Errorf("scan copied migration ledger: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate copied migration ledger: %w", err)
	}
	return result, nil
}

func equalServiceOperationSnapshotMigrationRows(
	left []serviceOperationSnapshotMigrationRow,
	right []serviceOperationSnapshotMigrationRow,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].version != right[index].version ||
			left[index].appliedAt.Valid != right[index].appliedAt.Valid ||
			left[index].appliedAt.String != right[index].appliedAt.String ||
			left[index].appliedAtStorageClass != right[index].appliedAtStorageClass {
			return false
		}
	}
	return true
}

func validateServiceOperationSnapshot(
	databasePath string,
	schema serviceOperationSnapshotSchema,
) error {
	switch schema {
	case serviceOperationSnapshotSchemaNormal:
		if err := checkServiceOperationsIdle(databasePath); err != nil {
			return err
		}
		return validateServiceOperationSnapshotSchema(
			databasePath,
			durableServiceOperationSchemaVersion,
			false,
		)
	case serviceOperationSnapshotSchemaPreLedger:
		if err := checkPreLedgerServiceOperationsIdle(databasePath); err != nil {
			return err
		}
		return validateServiceOperationSnapshotSchema(
			databasePath,
			preLedgerServiceOperationSchemaVersion,
			true,
		)
	default:
		return fmt.Errorf("unsupported snapshot schema %q", schema)
	}
}

func validateServiceOperationSnapshotSchema(
	databasePath string,
	requiredVersion int,
	exactVersion bool,
) error {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, true))
	if err != nil {
		return fmt.Errorf("open snapshot schema read-only: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("read snapshot schema: %w", err)
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return fmt.Errorf("run snapshot quick check: %w", err)
	}
	if quickCheck != "ok" {
		return fmt.Errorf("snapshot quick check returned %q", quickCheck)
	}
	foreignKeyRows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run snapshot foreign key check: %w", err)
	}
	hasForeignKeyViolation := foreignKeyRows.Next()
	if err := foreignKeyRows.Err(); err != nil {
		foreignKeyRows.Close()
		return fmt.Errorf("read snapshot foreign key check: %w", err)
	}
	if err := foreignKeyRows.Close(); err != nil {
		return fmt.Errorf("close snapshot foreign key check: %w", err)
	}
	if hasForeignKeyViolation {
		return fmt.Errorf("snapshot foreign key check found a violation")
	}

	var maximumVersion int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&maximumVersion); err != nil {
		return fmt.Errorf("inspect snapshot migration version: %w", err)
	}
	if exactVersion && maximumVersion != requiredVersion {
		return fmt.Errorf(
			"snapshot schema version must be exactly %d, got %d",
			requiredVersion,
			maximumVersion,
		)
	}
	if !exactVersion && maximumVersion < requiredVersion {
		return fmt.Errorf(
			"snapshot schema version must be at least %d, got %d",
			requiredVersion,
			maximumVersion,
		)
	}
	actualObjects, err := paneldb.ReadSQLiteUserSchema(ctx, database)
	if err != nil {
		return fmt.Errorf("read snapshot schema contract: %w", err)
	}
	expectedObjects, err := paneldb.ReferenceSQLiteUserSchema(ctx, maximumVersion)
	if err != nil {
		return fmt.Errorf("build reference schema contract: %w", err)
	}
	return compareServiceOperationSnapshotSchemaObjects(expectedObjects, actualObjects)
}

type normalizedServiceOperationSnapshotSchemaObject struct {
	objectType string
	name       string
	tableName  string
	sql        string
}

func compareServiceOperationSnapshotSchemaObjects(
	expected []paneldb.SQLiteSchemaObject,
	actual []paneldb.SQLiteSchemaObject,
) error {
	expectedByKey, err := normalizeServiceOperationSnapshotSchemaObjects(expected)
	if err != nil {
		return fmt.Errorf("normalize reference schema contract: %w", err)
	}
	actualByKey, err := normalizeServiceOperationSnapshotSchemaObjects(actual)
	if err != nil {
		return fmt.Errorf("normalize snapshot schema contract: %w", err)
	}
	for _, key := range sortedServiceOperationSnapshotSchemaKeys(expectedByKey) {
		expectedObject := expectedByKey[key]
		actualObject, ok := actualByKey[key]
		if !ok {
			return fmt.Errorf(
				"snapshot schema contract is missing %s %s",
				expectedObject.objectType,
				expectedObject.name,
			)
		}
		if actualObject.sql != expectedObject.sql {
			return fmt.Errorf(
				"snapshot schema contract for %s %s differs from the embedded migration contract",
				expectedObject.objectType,
				expectedObject.name,
			)
		}
	}
	for _, key := range sortedServiceOperationSnapshotSchemaKeys(actualByKey) {
		if _, ok := expectedByKey[key]; ok {
			continue
		}
		actualObject := actualByKey[key]
		return fmt.Errorf(
			"snapshot schema contract contains unexpected %s %s",
			actualObject.objectType,
			actualObject.name,
		)
	}
	return nil
}

func normalizeServiceOperationSnapshotSchemaObjects(
	objects []paneldb.SQLiteSchemaObject,
) (map[string]normalizedServiceOperationSnapshotSchemaObject, error) {
	normalized := make(map[string]normalizedServiceOperationSnapshotSchemaObject, len(objects))
	for _, object := range objects {
		normalizedObject := normalizedServiceOperationSnapshotSchemaObject{
			objectType: strings.ToLower(strings.TrimSpace(object.Type)),
			name:       strings.ToLower(strings.TrimSpace(object.Name)),
			tableName:  strings.ToLower(strings.TrimSpace(object.TableName)),
			sql:        canonicalExactSQLiteSchemaSQL(object.SQL),
		}
		switch normalizedObject.objectType {
		case "table", "index", "trigger", "view":
		default:
			return nil, fmt.Errorf("unsupported schema object type %q", object.Type)
		}
		if normalizedObject.name == "" || normalizedObject.tableName == "" || normalizedObject.sql == "" {
			return nil, fmt.Errorf("schema object metadata is incomplete")
		}
		key := normalizedObject.objectType + "\x00" + normalizedObject.name + "\x00" + normalizedObject.tableName
		if _, exists := normalized[key]; exists {
			return nil, fmt.Errorf("schema object %s %s is duplicated", normalizedObject.objectType, normalizedObject.name)
		}
		normalized[key] = normalizedObject
	}
	return normalized, nil
}

func canonicalExactSQLiteSchemaSQL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, ";") {
		value = strings.TrimSpace(strings.TrimSuffix(value, ";"))
	}
	return value
}

func sortedServiceOperationSnapshotSchemaKeys(
	objects map[string]normalizedServiceOperationSnapshotSchemaObject,
) []string {
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sqliteSnapshotURI(databasePath string, source bool) string {
	path := filepath.ToSlash(databasePath)
	if volume := filepath.VolumeName(databasePath); volume != "" &&
		!strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := &url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	if source {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	} else {
		query.Add("_pragma", "journal_mode(DELETE)")
		query.Add("_pragma", "synchronous(FULL)")
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func validateServiceOperationSnapshotRequest(
	destinationPath string,
	schemaValue string,
	conflictingMode bool,
) (serviceOperationSnapshotSchema, bool, error) {
	destinationPath = strings.TrimSpace(destinationPath)
	schemaValue = strings.TrimSpace(schemaValue)
	requested := destinationPath != "" || schemaValue != ""
	if !requested {
		return "", false, nil
	}
	if destinationPath == "" {
		return "", true, fmt.Errorf("snapshot destination is required")
	}
	if schemaValue == "" {
		return "", true, fmt.Errorf("snapshot schema is required")
	}
	if conflictingMode {
		return "", true, fmt.Errorf("snapshot mode is mutually exclusive with other panel modes")
	}
	schema, err := parseServiceOperationSnapshotSchema(schemaValue)
	if err != nil {
		return "", true, err
	}
	return schema, true, nil
}

func snapshotPathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}
