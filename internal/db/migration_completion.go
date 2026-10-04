package db

import (
	"context"
	"database/sql"
	"fmt"
)

// VerifyCompletedMigrationHistory proves that every migration embedded in this
// reader has already been recorded with its exact identity. It performs SELECTs
// only: missing legacy identity columns/values, gaps, and newer or altered rows
// are refusal states, never an invitation to migrate or backfill.
func VerifyCompletedMigrationHistory(ctx context.Context, database *sql.DB) error {
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("completion reader embeds no migrations")
	}
	rows, err := database.QueryContext(ctx, `SELECT version, filename, sha256 FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read completed migration history: %w", err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var version int
		var filename, sha sql.NullString
		if err := rows.Scan(&version, &filename, &sha); err != nil {
			return fmt.Errorf("read completed migration identity: %w", err)
		}
		if index >= len(migrations) {
			return fmt.Errorf("completed database contains an unsupported migration version")
		}
		expected := migrations[index]
		if expected.version != index+1 || version != expected.version || !filename.Valid || !sha.Valid || filename.String != expected.filename || sha.String != expected.sha256 {
			return fmt.Errorf("completed migration identity differs at version %d", expected.version)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate completed migration history: %w", err)
	}
	if index != len(migrations) {
		return fmt.Errorf("database migrations are incomplete: found %d of %d", index, len(migrations))
	}
	return nil
}
