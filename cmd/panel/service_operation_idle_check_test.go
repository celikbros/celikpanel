package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestServiceOperationsIdleCheckIsReadOnlyAndFailClosed(t *testing.T) {
	t.Run("idle database", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); err != nil {
			t.Fatalf("idle database rejected: %v", err)
		}
	})

	t.Run("queued operation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		panel := &Panel{db: database}
		if _, err := panel.createServiceOperation(
			context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
		); err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("queued operation err=%v want not idle", err)
		}
	})

	t.Run("terminal operation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.sqlite")
		database, err := paneldb.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		panel := &Panel{db: database}
		op, err := panel.createServiceOperation(
			context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := panel.markServiceOperationRunning(context.Background(), op.ID, "installing"); err != nil {
			t.Fatal(err)
		}
		if err := panel.finishServiceOperationSucceeded(
			context.Background(), op.ID, serviceOperationResult{"success": true},
		); err != nil {
			t.Fatal(err)
		}
		database.Close()
		if err := checkServiceOperationsIdle(path); err != nil {
			t.Fatalf("terminal database rejected: %v", err)
		}
	})

	t.Run("missing database", func(t *testing.T) {
		err := checkServiceOperationsIdle(filepath.Join(t.TempDir(), "missing.sqlite"))
		if !errors.Is(err, errServiceOperationsNotIdle) {
			t.Fatalf("missing database err=%v want not idle", err)
		}
	})
}
