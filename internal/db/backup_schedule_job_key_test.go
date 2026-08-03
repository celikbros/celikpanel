package db

import (
	"path/filepath"
	"testing"
)

func TestBackupScheduleJobKeyMigrationIsApplied(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	rows, err := database.GetDB().Query(`PRAGMA table_info(backup_schedules)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "active_job_key" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("backup_schedules.active_job_key migration was not applied")
	}
	var applied int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE version = 27`,
	).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("migration 27 applied count=%d, want 1", applied)
	}
}
