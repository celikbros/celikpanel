package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const createSchemaMigrationsSQL = `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		filename TEXT,
		sha256 TEXT,
		applied_at TEXT DEFAULT (datetime('now'))
	)`

const (
	sqliteBusyTimeout        = 5 * time.Second
	sqlitePingTimeout        = 5 * time.Second
	sqliteMigrationTimeout   = 2 * time.Minute
	sqliteMaxOpenConnections = 4
	sqliteMaxIdleConnections = 4
	sqliteConnectionIdleTime = 5 * time.Minute
)

type SQLiteDB struct {
	db *sql.DB
}

// NewSQLiteDB creates a new SQLite connection
func NewSQLiteDB(path string) (*SQLiteDB, error) {
	// PRAGMAs in the DSN are applied to every connection opened by database/sql.
	// WAL permits concurrent readers but SQLite still serializes writers, so a
	// small bounded pool prevents unbounded file descriptors and writer queues.
	// The busy timeout lets short write contention resolve without immediate
	// "database is locked" failures.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)",
		path, sqliteBusyTimeout.Milliseconds())

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to open database: %w", err)
	}
	db.SetMaxOpenConns(sqliteMaxOpenConnections)
	db.SetMaxIdleConns(sqliteMaxIdleConnections)
	db.SetConnMaxIdleTime(sqliteConnectionIdleTime)

	// Test connection without allowing a broken filesystem or driver to stall
	// startup indefinitely. Close the partially initialized pool on every error.
	pingCtx, cancelPing := context.WithTimeout(context.Background(), sqlitePingTimeout)
	err = db.PingContext(pingCtx)
	cancelPing()
	if err != nil {
		return nil, closeSQLiteInitialization(db, fmt.Errorf("unable to ping database: %w", err))
	}

	sqliteDB := &SQLiteDB{db: db}

	// Run migrations
	if err := sqliteDB.RunMigrations(); err != nil {
		return nil, closeSQLiteInitialization(db, fmt.Errorf("failed to run migrations: %w", err))
	}

	return sqliteDB, nil
}

func closeSQLiteInitialization(db *sql.DB, cause error) error {
	if err := db.Close(); err != nil {
		return errors.Join(cause, fmt.Errorf("close partially initialized database: %w", err))
	}
	return cause
}

// Close closes the database connection
func (db *SQLiteDB) Close() {
	db.db.Close()
}

// RunMigrations executes all migration files in order
func (db *SQLiteDB) RunMigrations() error {
	ctx, cancel := context.WithTimeout(context.Background(), sqliteMigrationTimeout)
	defer cancel()
	return db.runMigrations(ctx)
}

func (db *SQLiteDB) runMigrations(ctx context.Context) error {
	// Create migrations table if not exists
	_, err := db.db.ExecContext(ctx, createSchemaMigrationsSQL)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}
	if err := db.ensureMigrationLedgerIntegrityColumns(ctx); err != nil {
		return err
	}

	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}
	if err := db.verifyMigrationLedgerCoverage(ctx, migrations); err != nil {
		return err
	}

	appliedCount := 0
	// Apply each migration
	for _, migration := range migrations {
		exists, err := db.verifyOrBackfillAppliedMigration(ctx, migration)
		if err != nil {
			return err
		}
		if exists {
			continue // Already applied
		}

		// Execute migration in a transaction
		tx, err := db.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", migration.filename, err)
		}

		// Execute SQL
		_, err = tx.ExecContext(ctx, string(migration.content))
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to apply migration %s: %w", migration.filename, err)
		}

		// Record the immutable identity of the exact SQL that was applied. A
		// released migration may be superseded by a new version, never edited in
		// place under an already-published version number.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO schema_migrations (version, filename, sha256)
			VALUES (?, ?, ?)`, migration.version, migration.filename, migration.sha256)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", migration.filename, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", migration.filename, err)
		}

		log.Printf("Applied migration: %s", migration.filename)
		appliedCount++
	}

	if appliedCount > 0 {
		log.Printf("Successfully applied %d migrations", appliedCount)
	}

	return nil
}

type embeddedMigration struct {
	version  int
	filename string
	sha256   string
	content  []byte
}

// HighestEmbeddedMigrationVersion returns the newest migration this build
// ships. It reads the same embedded list RunMigrations applies at startup, so
// the number can never drift from what the binary can actually run. A restore
// uses it to refuse a database from a newer release before placing it.
// HighestEmbeddedMigrationVersion, bu yapının taşıdığı en yeni migration'ı
// döndürür. RunMigrations'ın başlangıçta uyguladığı gömülü listenin aynısını
// okur; böylece sayı, ikili dosyanın gerçekten çalıştırabildiğinden sapamaz.
func HighestEmbeddedMigrationVersion() (int, error) {
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, errors.New("this release embeds no migrations")
	}
	// loadEmbeddedMigrations returns them sorted ascending by version.
	return migrations[len(migrations)-1].version, nil
}

func loadEmbeddedMigrations() ([]embeddedMigration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	migrations := make([]embeddedMigration, 0, len(entries))
	versions := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := parseMigrationFilename(entry.Name())
		if err != nil {
			return nil, err
		}
		if previous, exists := versions[version]; exists {
			return nil, fmt.Errorf(
				"duplicate migration version %d in %q and %q",
				version, previous, entry.Name(),
			)
		}
		versions[version] = entry.Name()

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", entry.Name(), err)
		}
		digest := sha256.Sum256(content)
		migrations = append(migrations, embeddedMigration{
			version:  version,
			filename: entry.Name(),
			sha256:   fmt.Sprintf("%x", digest),
			content:  content,
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func parseMigrationFilename(filename string) (int, error) {
	underscore := strings.IndexByte(filename, '_')
	if underscore <= 0 ||
		!strings.HasSuffix(filename, ".sql") ||
		underscore+1 >= len(filename)-len(".sql") {
		return 0, fmt.Errorf(
			"invalid migration filename %q: expected <positive-version>_<name>.sql",
			filename,
		)
	}
	for _, character := range filename[:underscore] {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf(
				"invalid migration filename %q: version must be decimal digits",
				filename,
			)
		}
	}
	version, err := strconv.Atoi(filename[:underscore])
	if err != nil || version <= 0 {
		return 0, fmt.Errorf(
			"invalid migration filename %q: version must be a positive integer",
			filename,
		)
	}
	return version, nil
}

func (db *SQLiteDB) ensureMigrationLedgerIntegrityColumns(ctx context.Context) error {
	rows, err := db.db.QueryContext(ctx, `PRAGMA table_info(schema_migrations)`)
	if err != nil {
		return fmt.Errorf("inspect migration ledger schema: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var (
			columnID   int
			name       string
			columnType string
			notNull    int
			defaultSQL sql.NullString
			primaryKey int
		)
		if err := rows.Scan(
			&columnID,
			&name,
			&columnType,
			&notNull,
			&defaultSQL,
			&primaryKey,
		); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read migration ledger schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate migration ledger schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close migration ledger schema query: %w", err)
	}
	if !columns["version"] {
		return errors.New("migration ledger is missing its version column")
	}

	missing := make([]string, 0, 2)
	for _, name := range []string{"filename", "sha256"} {
		if !columns[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration ledger upgrade: %w", err)
	}
	for _, name := range missing {
		if _, err := tx.ExecContext(
			ctx,
			"ALTER TABLE schema_migrations ADD COLUMN "+name+" TEXT",
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("add migration ledger %s column: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration ledger upgrade: %w", err)
	}
	return nil
}

func (db *SQLiteDB) verifyMigrationLedgerCoverage(
	ctx context.Context,
	migrations []embeddedMigration,
) error {
	embeddedByVersion := make(map[int]string, len(migrations))
	for _, migration := range migrations {
		embeddedByVersion[migration.version] = migration.filename
	}

	rows, err := db.db.QueryContext(ctx, `
		SELECT version
		FROM schema_migrations
		ORDER BY version`)
	if err != nil {
		return fmt.Errorf("inspect migration ledger coverage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("read migration ledger coverage: %w", err)
		}
		if _, exists := embeddedByVersion[version]; !exists {
			return fmt.Errorf(
				"migration ledger contains applied version %d but this release has no matching embedded migration",
				version,
			)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration ledger coverage: %w", err)
	}
	return nil
}

func (db *SQLiteDB) verifyOrBackfillAppliedMigration(
	ctx context.Context,
	migration embeddedMigration,
) (bool, error) {
	var recordedFilename, recordedSHA256 sql.NullString
	err := db.db.QueryRowContext(ctx, `
		SELECT filename, sha256
		FROM schema_migrations
		WHERE version = ?`, migration.version).Scan(&recordedFilename, &recordedSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %s ledger entry: %w", migration.filename, err)
	}

	// Rows created by versions before the integrity ledger had no identity
	// columns. Backfill both values together once; a partially populated row is
	// corruption and must not be guessed at.
	if !recordedFilename.Valid && !recordedSHA256.Valid {
		result, err := db.db.ExecContext(ctx, `
			UPDATE schema_migrations
			SET filename = ?, sha256 = ?
			WHERE version = ? AND filename IS NULL AND sha256 IS NULL`,
			migration.filename, migration.sha256, migration.version)
		if err != nil {
			return false, fmt.Errorf("backfill migration %s identity: %w", migration.filename, err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("confirm migration %s identity backfill: %w", migration.filename, err)
		}
		if changed != 1 {
			return false, fmt.Errorf("migration %s identity changed concurrently", migration.filename)
		}
		log.Printf("Recorded legacy migration identity: %s", migration.filename)
		return true, nil
	}
	if !recordedFilename.Valid ||
		!recordedSHA256.Valid ||
		recordedFilename.String == "" ||
		recordedSHA256.String == "" {
		return false, fmt.Errorf(
			"migration ledger entry %d has incomplete identity",
			migration.version,
		)
	}
	if recordedFilename.String != migration.filename ||
		recordedSHA256.String != migration.sha256 {
		return false, fmt.Errorf(
			"migration integrity mismatch for version %d: ledger has %q/%s, embedded release has %q/%s",
			migration.version,
			recordedFilename.String,
			recordedSHA256.String,
			migration.filename,
			migration.sha256,
		)
	}
	return true, nil
}

// GetDB returns the underlying *sql.DB for queries
func (db *SQLiteDB) GetDB() *sql.DB {
	return db.db
}
