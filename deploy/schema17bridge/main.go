// Command schema17-bridge is a deliberately narrow release-recovery helper.
//
// It exists only to move the last pre-ledger CelikPanel database shape from
// the exact, contiguous migration sequence 1..17 to the exact pre-ledger
// sequence 1..20. It also creates and restores a standalone exact-17 SQLite
// snapshot. Unknown, newer, gapped, corrupt or partially migrated databases
// are rejected before any mutation.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sourceSchemaVersion = 17
	bridgeSchemaVersion = 20
	operationTimeout    = 2 * time.Minute
)

var bridgeMigrations = map[int]string{
	18: "018_hostname_namespace.sql",
	19: "019_hsts_retirement.sql",
	20: "020_ssl_lineage_identity.sql",
}

// These objects/columns are introduced by migrations 21 through 24. Their
// presence on an exact bridge ledger is evidence of a partial/manual later
// migration and must fail closed.
var postBridgeObjects = []string{
	"service_operations",
	"idx_service_operations_one_active",
	"idx_service_operations_recent",
	"idx_service_operations_request_id",
	"store_offerings",
	"store_offering_components",
	"idx_store_offerings_release",
	"idx_store_offering_components_component",
	"vpn_sync_state",
	"idx_vpn_peers_desired_sync",
	"vpn_peers_sync_insert",
	"vpn_peers_sync_update",
	"vpn_peers_sync_delete",
	"vpn_entitlements_sync_insert",
	"vpn_entitlements_sync_update",
	"vpn_entitlements_sync_delete",
	"vpn_offering_sync_update",
}

var postBridgeColumns = []columnRef{
	{table: "service_operations", column: "request_id"},
	{table: "vpn_peers", column: "desired_state"},
	{table: "vpn_peers", column: "sync_state"},
	{table: "vpn_peers", column: "sync_error"},
	{table: "vpn_peers", column: "updated_at"},
	{table: "vpn_peers", column: "provisioning_state"},
	{table: "vpn_peers", column: "delivery_token_hash"},
	{table: "vpn_peers", column: "delivery_expires_at"},
}

// These objects/columns are introduced by migrations 18 through 24. Their
// presence on a database whose ledger still ends at 17 is evidence of a
// partial/manual migration and must fail closed.
var post17Objects = append([]string{
	"hostname_reservations",
	"idx_hostname_reservations_source",
	"idx_hostname_reservations_domain",
	"idx_domains_name_canonical",
	"idx_domain_aliases_alias_canonical",
	"trg_hostname_reservations_reject_invalid",
	"trg_hostname_reservations_reject_conflict",
	"trg_domains_hostname_canonical_insert",
	"trg_domains_hostname_canonical_update",
	"trg_domain_aliases_hostname_canonical_insert",
	"trg_domain_aliases_hostname_canonical_update",
	"trg_domains_hostname_reserve_insert",
	"trg_domains_hostname_reserve_update",
	"trg_domain_aliases_hostname_reserve_insert",
	"trg_domain_aliases_hostname_reserve_update",
	"trg_domain_aliases_hostname_reserve_delete",
	"idx_ssl_certificates_lineage",
}, postBridgeObjects...)

type columnRef struct {
	table  string
	column string
}

var post17Columns = append([]columnRef{
	{table: "sites", column: "hsts_retire_after"},
	{table: "ssl_certificates", column: "lineage_name"},
	{table: "ssl_certificates", column: "acme_provider_id"},
}, postBridgeColumns...)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "schema17-bridge: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "check":
		flags := flag.NewFlagSet("check", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		databasePath := flags.String("db", "", "absolute panel database path")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError()
		}
		if flags.NArg() != 0 || *databasePath == "" {
			return usageError()
		}
		return checkDatabaseFile(*databasePath, sourceSchemaVersion)
	case "snapshot":
		flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		databasePath := flags.String("db", "", "absolute panel database path")
		outputPath := flags.String("out", "", "absolute snapshot output path")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError()
		}
		if flags.NArg() != 0 || *databasePath == "" || *outputPath == "" {
			return usageError()
		}
		return createSnapshot(*databasePath, *outputPath)
	case "migrate":
		flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		databasePath := flags.String("db", "", "absolute panel database path")
		migrationsRoot := flags.String("migrations-root", "", "absolute trusted migration directory")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError()
		}
		if flags.NArg() != 0 || *databasePath == "" || *migrationsRoot == "" {
			return usageError()
		}
		return migrate(*databasePath, *migrationsRoot)
	case "restore":
		flags := flag.NewFlagSet("restore", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		databasePath := flags.String("db", "", "absolute panel database path")
		snapshotPath := flags.String("snapshot", "", "absolute verified snapshot path")
		if err := flags.Parse(args[1:]); err != nil {
			return usageError()
		}
		if flags.NArg() != 0 || *databasePath == "" || *snapshotPath == "" {
			return usageError()
		}
		return restoreSnapshot(*databasePath, *snapshotPath)
	default:
		return usageError()
	}
}

func usageError() error {
	return errors.New("usage: schema17-bridge check --db PATH | snapshot --db PATH --out PATH | migrate --db PATH --migrations-root DIR | restore --db PATH --snapshot PATH")
}

func canonicalRegularFile(path, label string) (string, os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", nil, fmt.Errorf("%s path must be absolute and clean", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("%s must be a regular non-symlink file", label)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, fmt.Errorf("resolve %s: %w", label, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("make %s path absolute: %w", label, err)
	}
	if filepath.Clean(canonical) != path {
		return "", nil, fmt.Errorf("%s path contains a symlink or alias", label)
	}
	return canonical, info, nil
}

func canonicalDirectory(path, label string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("%s path must be absolute and clean", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s must be a non-symlink directory", label)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("make %s path absolute: %w", label, err)
	}
	if filepath.Clean(canonical) != path {
		return "", fmt.Errorf("%s path contains a symlink or alias", label)
	}
	return canonical, nil
}

func sqliteURI(path, mode string, immutable bool) string {
	sqlitePath := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(sqlitePath, "/") {
		sqlitePath = "/" + sqlitePath
	}
	uri := &url.URL{Scheme: "file", Path: sqlitePath}
	query := uri.Query()
	query.Set("mode", mode)
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "trusted_schema(0)")
	if immutable {
		query.Set("immutable", "1")
		query.Add("_pragma", "query_only(1)")
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func openDatabase(path, mode string, immutable bool) (*sql.DB, error) {
	database, err := sql.Open("sqlite", sqliteURI(path, mode, immutable))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func checkDatabaseFile(path string, expectedVersion int) error {
	canonical, _, err := canonicalRegularFile(path, "database")
	if err != nil {
		return err
	}
	for _, suffix := range []string{"-journal"} {
		if _, err := os.Lstat(canonical + suffix); err == nil {
			return fmt.Errorf("unsafe SQLite sidecar is present: %s", suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect SQLite sidecar %s: %w", suffix, err)
		}
	}
	database, err := openDatabase(canonical, "ro", false)
	if err != nil {
		return fmt.Errorf("open database read-only: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := checkDatabase(ctx, database, expectedVersion); err != nil {
		return err
	}
	return nil
}

func checkDatabase(ctx context.Context, database *sql.DB, expectedVersion int) error {
	var quickCheck string
	if err := database.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return fmt.Errorf("run SQLite quick_check: %w", err)
	}
	if quickCheck != "ok" {
		return fmt.Errorf("SQLite quick_check returned %q", quickCheck)
	}

	rows, err := database.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return fmt.Errorf("inspect migration ledger: %w", err)
	}
	expected := 1
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("read migration ledger: %w", err)
		}
		if version != expected {
			rows.Close()
			return fmt.Errorf("migration ledger is not contiguous at version %d", expected)
		}
		expected++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate migration ledger: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration ledger: %w", err)
	}
	if expected != expectedVersion+1 {
		return fmt.Errorf("database schema is %d; exact schema %d is required", expected-1, expectedVersion)
	}

	if expectedVersion == sourceSchemaVersion {
		if err := rejectPost17Shape(ctx, database); err != nil {
			return err
		}
	}
	if expectedVersion == bridgeSchemaVersion {
		if err := requireBridgeShape(ctx, database); err != nil {
			return err
		}
	}

	foreignRows, err := database.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("run SQLite foreign_key_check: %w", err)
	}
	if foreignRows.Next() {
		foreignRows.Close()
		return errors.New("SQLite foreign_key_check reported a violation")
	}
	if err := foreignRows.Err(); err != nil {
		foreignRows.Close()
		return fmt.Errorf("iterate SQLite foreign_key_check: %w", err)
	}
	if err := foreignRows.Close(); err != nil {
		return fmt.Errorf("close SQLite foreign_key_check: %w", err)
	}
	return nil
}

func rejectPost17Shape(ctx context.Context, database *sql.DB) error {
	return rejectSchemaAdditions(ctx, database, post17Objects, post17Columns, "post-17")
}

func rejectSchemaAdditions(
	ctx context.Context,
	database *sql.DB,
	objects []string,
	columns []columnRef,
	label string,
) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(objects)), ",")
	args := make([]any, len(objects))
	for i, name := range objects {
		args[i] = name
	}
	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE name IN (`+placeholders+`)`,
		args...,
	).Scan(&count); err != nil {
		return fmt.Errorf("inspect %s schema objects: %w", label, err)
	}
	if count != 0 {
		return fmt.Errorf("database contains a partial %s schema object", label)
	}
	for _, ref := range columns {
		exists, err := hasColumn(ctx, database, ref.table, ref.column)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("database contains partial %s column %s.%s", label, ref.table, ref.column)
		}
	}
	return nil
}

func requireBridgeShape(ctx context.Context, database *sql.DB) error {
	requiredObjects := []string{
		"hostname_reservations",
		"idx_hostname_reservations_source",
		"idx_hostname_reservations_domain",
		"idx_domains_name_canonical",
		"idx_domain_aliases_alias_canonical",
		"idx_ssl_certificates_lineage",
	}
	for _, name := range requiredObjects {
		var count int
		if err := database.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`,
			name,
		).Scan(&count); err != nil {
			return fmt.Errorf("inspect bridge object %s: %w", name, err)
		}
		if count != 1 {
			return fmt.Errorf("required bridge object %s is missing or duplicated", name)
		}
	}
	for _, ref := range []columnRef{
		{table: "sites", column: "hsts_retire_after"},
		{table: "ssl_certificates", column: "lineage_name"},
		{table: "ssl_certificates", column: "acme_provider_id"},
	} {
		exists, err := hasColumn(ctx, database, ref.table, ref.column)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("required bridge column %s.%s is missing", ref.table, ref.column)
		}
	}
	if err := rejectSchemaAdditions(
		ctx,
		database,
		postBridgeObjects,
		postBridgeColumns,
		"post-bridge",
	); err != nil {
		return err
	}
	return nil
}

func hasColumn(ctx context.Context, database *sql.DB, table, column string) (bool, error) {
	// Table and column names are constants in this helper. pragma_table_info(?)
	// keeps even those constants out of SQL text.
	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
		table,
		column,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect column %s.%s: %w", table, column, err)
	}
	if count > 1 {
		return false, fmt.Errorf("column %s.%s is duplicated", table, column)
	}
	return count == 1, nil
}

func createSnapshot(databasePath, outputPath string) (returnErr error) {
	source, _, err := canonicalRegularFile(databasePath, "database")
	if err != nil {
		return err
	}
	if err := checkDatabaseFile(source, sourceSchemaVersion); err != nil {
		return fmt.Errorf("source database is not exact schema 17: %w", err)
	}
	output, parent, err := prepareNewOutputPath(outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(output)
			_ = os.Remove(output + "-wal")
			_ = os.Remove(output + "-shm")
			_ = os.Remove(output + "-journal")
		}
	}()

	database, err := openDatabase(source, "ro", false)
	if err != nil {
		return fmt.Errorf("open source database for snapshot: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := database.ExecContext(ctx, `VACUUM INTO ?`, output); err != nil {
		database.Close()
		return fmt.Errorf("create transaction-consistent SQLite snapshot: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close source database after snapshot: %w", err)
	}
	if err := os.Chmod(output, 0o600); err != nil {
		return fmt.Errorf("protect snapshot: %w", err)
	}
	if err := checkStandaloneSQLite(output); err != nil {
		return err
	}
	if err := checkDatabaseFile(output, sourceSchemaVersion); err != nil {
		return fmt.Errorf("verify exact schema-17 snapshot: %w", err)
	}
	if err := syncPath(output); err != nil {
		return fmt.Errorf("make snapshot durable: %w", err)
	}
	if err := syncPath(parent); err != nil {
		return fmt.Errorf("make snapshot directory durable: %w", err)
	}
	return nil
}

func prepareNewOutputPath(path string) (string, string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", "", errors.New("output path must be absolute and clean")
	}
	if _, err := os.Lstat(path); err == nil {
		return "", "", errors.New("output path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect output path: %w", err)
	}
	parent, err := canonicalDirectory(filepath.Dir(path), "output parent")
	if err != nil {
		return "", "", err
	}
	if filepath.Join(parent, filepath.Base(path)) != path {
		return "", "", errors.New("output path contains an alias")
	}
	return path, parent, nil
}

func checkStandaloneSQLite(path string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return fmt.Errorf("standalone SQLite file has sidecar %s", suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect standalone SQLite sidecar %s: %w", suffix, err)
		}
	}
	return nil
}

func migrate(databasePath, migrationsRoot string) error {
	databasePath, _, err := canonicalRegularFile(databasePath, "database")
	if err != nil {
		return err
	}
	migrationsRoot, err = canonicalDirectory(migrationsRoot, "migrations root")
	if err != nil {
		return err
	}
	migrationSQL, err := loadBridgeMigrations(migrationsRoot)
	if err != nil {
		return err
	}

	database, err := openDatabase(databasePath, "rw", false)
	if err != nil {
		return fmt.Errorf("open database for migration: %w", err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := database.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		return fmt.Errorf("set durable SQLite mode: %w", err)
	}
	if err := checkDatabase(ctx, database, sourceSchemaVersion); err != nil {
		return fmt.Errorf("source database is not exact schema 17: %w", err)
	}
	for version := sourceSchemaVersion + 1; version <= bridgeSchemaVersion; version++ {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, migrationSQL[version]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", version, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`,
			version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", version, err)
		}
	}
	if err := checkDatabase(ctx, database, bridgeSchemaVersion); err != nil {
		return fmt.Errorf("verify exact bridge schema 20: %w", err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("checkpoint migrated database: %w", err)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close migrated database: %w", err)
	}
	if err := syncSQLiteFamily(databasePath); err != nil {
		return fmt.Errorf("make migrated database durable: %w", err)
	}
	if err := checkDatabaseFile(databasePath, bridgeSchemaVersion); err != nil {
		return fmt.Errorf("reopen and verify exact bridge schema 20: %w", err)
	}
	return nil
}

func loadBridgeMigrations(root string) (map[int]string, error) {
	versions := make([]int, 0, len(bridgeMigrations))
	for version := range bridgeMigrations {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	result := make(map[int]string, len(versions))
	for _, version := range versions {
		path := filepath.Join(root, bridgeMigrations[version])
		canonical, info, err := canonicalRegularFile(path, fmt.Sprintf("migration %d", version))
		if err != nil {
			return nil, err
		}
		if canonical != path {
			return nil, fmt.Errorf("migration %d path is not canonical", version)
		}
		if info.Size() <= 0 || info.Size() > 2*1024*1024 {
			return nil, fmt.Errorf("migration %d has an invalid size", version)
		}
		content, err := os.ReadFile(canonical)
		if err != nil {
			return nil, fmt.Errorf("read migration %d: %w", version, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			return nil, fmt.Errorf("migration %d is empty", version)
		}
		result[version] = string(content)
	}
	return result, nil
}

func restoreSnapshot(databasePath, snapshotPath string) (returnErr error) {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		return errors.New("database path must be absolute")
	}
	destination, destinationInfo, err := canonicalRegularFile(databasePath, "database")
	if err != nil {
		return err
	}
	snapshot, _, err := canonicalRegularFile(snapshotPath, "snapshot")
	if err != nil {
		return err
	}
	if destination == snapshot {
		return errors.New("database and snapshot paths must differ")
	}
	if err := checkStandaloneSQLite(snapshot); err != nil {
		return err
	}
	if err := checkDatabaseFile(snapshot, sourceSchemaVersion); err != nil {
		return fmt.Errorf("snapshot is not exact schema 17: %w", err)
	}

	parent, err := canonicalDirectory(filepath.Dir(destination), "database parent")
	if err != nil {
		return err
	}
	stagingFile, err := os.CreateTemp(parent, ".schema17-restore-*.sqlite")
	if err != nil {
		return fmt.Errorf("create restore staging file: %w", err)
	}
	staging := stagingFile.Name()
	if err := stagingFile.Close(); err != nil {
		os.Remove(staging)
		return fmt.Errorf("close restore staging file: %w", err)
	}
	if err := os.Remove(staging); err != nil {
		return fmt.Errorf("prepare restore staging path: %w", err)
	}
	defer func() {
		if returnErr != nil {
			_ = os.Remove(staging)
			_ = os.Remove(staging + "-wal")
			_ = os.Remove(staging + "-shm")
			_ = os.Remove(staging + "-journal")
		}
	}()

	source, err := openDatabase(snapshot, "ro", false)
	if err != nil {
		return fmt.Errorf("open snapshot for restore: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if _, err := source.ExecContext(ctx, `VACUUM INTO ?`, staging); err != nil {
		source.Close()
		return fmt.Errorf("stage transaction-consistent restore: %w", err)
	}
	if err := source.Close(); err != nil {
		return fmt.Errorf("close snapshot after restore staging: %w", err)
	}
	if err := checkStandaloneSQLite(staging); err != nil {
		return err
	}
	if err := checkDatabaseFile(staging, sourceSchemaVersion); err != nil {
		return fmt.Errorf("verify staged exact schema-17 restore: %w", err)
	}
	if err := applyFileMetadata(staging, destinationInfo); err != nil {
		return err
	}
	if err := syncPath(staging); err != nil {
		return fmt.Errorf("make staged restore durable: %w", err)
	}

	// The updater/rollback contract keeps both coordinators stopped while this
	// helper runs. Refuse a hot journal and checkpoint/truncate any harmless WAL
	// before replacing the database inode.
	current, err := openDatabase(destination, "rw", false)
	if err != nil {
		return fmt.Errorf("open current database before restore: %w", err)
	}
	if _, err := current.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		current.Close()
		return fmt.Errorf("checkpoint current database before restore: %w", err)
	}
	if err := current.Close(); err != nil {
		return fmt.Errorf("close current database before restore: %w", err)
	}
	if _, err := os.Lstat(destination + "-journal"); err == nil {
		return errors.New("current database has a hot rollback journal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect current rollback journal: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := removeRegularSidecar(destination + suffix); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, destination); err != nil {
		return fmt.Errorf("atomically publish restored database: %w", err)
	}
	if err := syncPath(parent); err != nil {
		return fmt.Errorf("make restored database entry durable: %w", err)
	}
	if err := checkStandaloneSQLite(destination); err != nil {
		return err
	}
	if err := checkDatabaseFile(destination, sourceSchemaVersion); err != nil {
		return fmt.Errorf("verify published exact schema-17 restore: %w", err)
	}
	return nil
}

func removeRegularSidecar(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect SQLite sidecar %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("SQLite sidecar %s is unsafe", filepath.Base(path))
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove SQLite sidecar %s: %w", filepath.Base(path), err)
	}
	return nil
}

func applyFileMetadata(path string, reference os.FileInfo) error {
	if err := os.Chmod(path, reference.Mode().Perm()); err != nil {
		return fmt.Errorf("restore database mode: %w", err)
	}
	uid, gid, ok := numericOwnership(reference)
	if ok {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("restore database ownership: %w", err)
		}
	}
	return nil
}

// numericOwnership avoids OS-specific build files while still preserving
// uid/gid on Unix. Windows test runs simply have no numeric ownership fields.
func numericOwnership(info os.FileInfo) (int, int, bool) {
	if info == nil || info.Sys() == nil {
		return 0, 0, false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, 0, false
	}
	uid := value.FieldByName("Uid")
	gid := value.FieldByName("Gid")
	if !uid.IsValid() || !gid.IsValid() || !uid.CanUint() || !gid.CanUint() {
		return 0, 0, false
	}
	return int(uid.Uint()), int(gid.Uint()), true
}

func syncSQLiteFamily(path string) error {
	if err := syncPath(path); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe SQLite sidecar %s", suffix)
		}
		if err := syncPath(path + suffix); err != nil {
			return err
		}
	}
	return syncPath(filepath.Dir(path))
}

func syncPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() && runtime.GOOS == "windows" {
		// Windows does not expose directory fsync. Linux, the only supported
		// deployment target for this release helper, still takes the durable
		// directory path below.
		return nil
	}
	flags := os.O_RDONLY
	if info.Mode().IsRegular() {
		flags = os.O_RDWR
	}
	file, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
