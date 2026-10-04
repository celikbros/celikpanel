package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// checkCompletedUpdateDatabaseWALAware inspects one pinned private DB/WAL copy.
// Queue idleness alone cannot prove that the target migrations ran. Completion
// additionally requires the latest embedded schema and every migration identity.
// Neither the source nor its sidecars are opened through SQLite or modified.
func checkCompletedUpdateDatabaseWALAware(databasePath string) error {
	return checkWALAwareServiceOperationsIdleWith(databasePath, checkCompletedUpdateDatabase)
}

func checkCompletedUpdateDatabase(databasePath string) error {
	if err := checkServiceOperationsIdle(databasePath); err != nil {
		return err
	}
	latest, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		return fmt.Errorf("read completed database contract: %w", err)
	}
	if err := validateServiceOperationSnapshotSchema(databasePath, latest, true); err != nil {
		return fmt.Errorf("completed update database schema is not verified: %w", err)
	}
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, true))
	if err != nil {
		return fmt.Errorf("open completed database identity read-only: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := paneldb.VerifyCompletedMigrationHistory(ctx, database); err != nil {
		return fmt.Errorf("completed update migration history is not verified: %w", err)
	}
	return nil
}
