package db

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreMigrationSeedsTypedHonestOfferings(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer database.Close()

	var tables int
	if err := database.GetDB().QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table'
		  AND name IN ('store_offerings', 'store_offering_components')`,
	).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 2 {
		t.Fatalf("Store table count = %d, want 2", tables)
	}

	rows, err := database.GetDB().Query(`
		SELECT id, release_state, entitlement_mode, metadata_json
		FROM store_offerings ORDER BY id`,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var id, releaseState, entitlementMode, rawMetadata string
		if err := rows.Scan(&id, &releaseState, &entitlementMode, &rawMetadata); err != nil {
			t.Fatal(err)
		}
		seen[id] = releaseState + "/" + entitlementMode
		var metadata map[string]any
		if err := json.Unmarshal([]byte(rawMetadata), &metadata); err != nil {
			t.Fatalf("%s metadata is invalid JSON: %v", id, err)
		}
		for key := range metadata {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "command") || strings.Contains(lower, "recipe") ||
				strings.Contains(lower, "sql") || strings.Contains(lower, "path") {
				t.Fatalf("%s metadata contains operational key %q", id, key)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if seen["app_installer"] != "available/grant" || seen["vpn"] != "available/grant" {
		t.Fatalf("real grantable offerings were not seeded honestly: %v", seen)
	}
	for _, id := range []string{"firewall", "business_email", "extra_ip", "ai_agent"} {
		if seen[id] != "coming_soon/grant" {
			t.Fatalf("%s state = %q, want coming_soon/grant", id, seen[id])
		}
	}
}
