package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

var errServiceOperationsNotIdle = errors.New("service operations are not idle")

// checkServiceOperationsIdle opens the panel database in SQLite read-only mode.
// Release tooling uses it after stopping the panel; it never migrates, repairs,
// deletes or completes an operation row.
// checkServiceOperationsIdle panel veritabanını SQLite salt-okunur modunda
// açar. Sürüm araçları bunu paneli durdurduktan sonra kullanır; hiçbir işlem
// satırını migrate etmez, onarmaz, silmez veya tamamlamaz.
func checkServiceOperationsIdle(databasePath string) error {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		absolutePath, err := filepath.Abs(databasePath)
		if err != nil {
			return fmt.Errorf("%w: resolve panel database path: %v", errServiceOperationsNotIdle, err)
		}
		databasePath = absolutePath
	}
	info, err := os.Lstat(databasePath)
	if err != nil {
		return fmt.Errorf("%w: inspect panel database: %v", errServiceOperationsNotIdle, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: panel database must be a regular file", errServiceOperationsNotIdle)
	}

	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(databasePath)}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return fmt.Errorf("%w: open panel database read-only: %v", errServiceOperationsNotIdle, err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("%w: read panel database: %v", errServiceOperationsNotIdle, err)
	}
	var tableCount int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='service_operations'`,
	).Scan(&tableCount); err != nil {
		return fmt.Errorf("%w: inspect service operation schema: %v", errServiceOperationsNotIdle, err)
	}
	if tableCount != 1 {
		return fmt.Errorf("%w: service operation schema is unavailable", errServiceOperationsNotIdle)
	}
	var id, status string
	err = database.QueryRowContext(
		ctx,
		`SELECT id, status FROM service_operations
		 WHERE status IN (?, ?)
		 ORDER BY created_at ASC LIMIT 1`,
		serviceOperationQueued,
		serviceOperationRunning,
	).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: inspect active service operations: %v", errServiceOperationsNotIdle, err)
	}
	return fmt.Errorf("%w: operation %s is %s", errServiceOperationsNotIdle, id, status)
}
