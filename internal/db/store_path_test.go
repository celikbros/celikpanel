package db

import (
	"path/filepath"
	"testing"
)

func TestStoreManagePathConstraintRejectsExternalAndTraversalPaths(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer database.Close()

	invalidPaths := []string{
		"https://evil.example",
		"//evil.example/path",
		`\services\wireguard`,
		"/services/../settings",
		"/services/\nwireguard",
	}
	for _, path := range invalidPaths {
		_, err := database.GetDB().Exec(`
			INSERT INTO store_offerings
				(id, kind, category, vendor, release_state, entitlement_mode, manage_path, metadata_json)
			VALUES
				('invalid_path', 'feature', 'test', 'test', 'available', 'included', ?, '{}')`,
			path,
		)
		if err == nil {
			t.Fatalf("unsafe manage path %q passed the database constraint", path)
		}
	}
}
