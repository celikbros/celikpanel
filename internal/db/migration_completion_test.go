package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestVerifyCompletedMigrationHistoryIsReadOnlyAndExact(t *testing.T) {
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"complete", "missing", "gap", "future", "filename", "digest", "null", "legacy-columns"} {
		t.Run(kind, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			database.SetMaxOpenConns(1)
			schema := "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, filename TEXT, sha256 TEXT)"
			if kind == "legacy-columns" {
				schema = "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"
			}
			if _, err := database.Exec(schema); err != nil {
				t.Fatal(err)
			}
			if kind != "legacy-columns" {
				for _, m := range migrations {
					if _, err := database.Exec("INSERT INTO schema_migrations(version,filename,sha256) VALUES(?,?,?)", m.version, m.filename, m.sha256); err != nil {
						t.Fatal(err)
					}
				}
			}
			last := migrations[len(migrations)-1].version
			statement := ""
			switch kind {
			case "missing":
				statement = fmt.Sprintf("DELETE FROM schema_migrations WHERE version=%d", last)
			case "gap":
				statement = "DELETE FROM schema_migrations WHERE version=2"
			case "future":
				statement = fmt.Sprintf("INSERT INTO schema_migrations VALUES(%d,'future','future')", last+1)
			case "filename":
				statement = "UPDATE schema_migrations SET filename='changed' WHERE version=1"
			case "digest":
				statement = "UPDATE schema_migrations SET sha256='changed' WHERE version=1"
			case "null":
				statement = "UPDATE schema_migrations SET filename=NULL,sha256=NULL WHERE version=1"
			}
			if statement != "" {
				if _, err := database.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := database.Exec("PRAGMA query_only=ON"); err != nil {
				t.Fatal(err)
			}
			for retry := 0; retry < 2; retry++ {
				err := VerifyCompletedMigrationHistory(context.Background(), database)
				if (err == nil) != (kind == "complete") {
					t.Fatalf("result=%v", err)
				}
			}
		})
	}
}
