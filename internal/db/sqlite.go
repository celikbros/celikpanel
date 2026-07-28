package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const createSchemaMigrationsSQL = `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT DEFAULT (datetime('now'))
	)`

type SQLiteDB struct {
	db *sql.DB
}

// NewSQLiteDB creates a new SQLite connection
func NewSQLiteDB(path string) (*SQLiteDB, error) {
	// Enable WAL and foreign keys via connection string parameters
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to open database: %v", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("unable to ping database: %v", err)
	}

	sqliteDB := &SQLiteDB{db: db}

	// Run migrations
	if err := sqliteDB.RunMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %v", err)
	}

	return sqliteDB, nil
}

// Close closes the database connection
func (db *SQLiteDB) Close() {
	db.db.Close()
}

// RunMigrations executes all migration files in order
func (db *SQLiteDB) RunMigrations() error {
	ctx := context.Background()

	// Create migrations table if not exists
	_, err := db.db.ExecContext(ctx, createSchemaMigrationsSQL)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %v", err)
	}

	// Read all migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %v", err)
	}

	// Filter and sort .sql files
	var migrations []string
	appliedCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			migrations = append(migrations, entry.Name())
		}
	}
	sort.Strings(migrations)

	// Apply each migration
	for _, filename := range migrations {
		// Extract version number
		version := 0
		fmt.Sscanf(filename, "%d_", &version)

		// Check if already applied
		var exists bool
		err := db.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %v", err)
		}

		if exists {
			continue // Already applied
		}

		// Read migration file
		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %v", filename, err)
		}

		// Execute migration in a transaction
		tx, err := db.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %v", filename, err)
		}

		// Execute SQL
		_, err = tx.ExecContext(ctx, string(content))
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to apply migration %s: %v", filename, err)
		}

		// Record migration
		_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %v", filename, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %v", filename, err)
		}

		log.Printf("Applied migration: %s", filename)
		appliedCount++
	}

	if appliedCount > 0 {
		log.Printf("Successfully applied %d migrations", appliedCount)
	}

	return nil
}

// GetDB returns the underlying *sql.DB for queries
func (db *SQLiteDB) GetDB() *sql.DB {
	return db.db
}
