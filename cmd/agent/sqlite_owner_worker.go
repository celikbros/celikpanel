package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/alicelik/celikpanel/internal/systemsqlite"
)

const systemSQLiteOwnerWorkerMode = "--system-sqlite-owner-worker"

// handleSystemSQLiteOwnerWorker runs before the privileged RPC service is initialized.
// handleSystemSQLiteOwnerWorker, ayrıcalıklı RPC servisi başlatılmadan önce çalışır.
func handleSystemSQLiteOwnerWorker() (bool, error) {
	if len(os.Args) < 2 || os.Args[1] != systemSQLiteOwnerWorkerMode {
		return false, nil
	}
	if len(os.Args) != 4 && len(os.Args) != 6 {
		return true, errors.New("invalid isolated SQLite worker invocation")
	}
	// Apply the privilege and resource boundary before resolving accounts, paths, or database definitions.
	// Hesapları, yolları veya veritabanı tanımlarını çözmeden önce yetki ve kaynak sınırını uygula.
	if err := systemsqlite.PrepareOwnerWorkerProcess(); err != nil {
		return true, err
	}
	action, databaseID := os.Args[2], os.Args[3]
	timeout := systemSQLiteCheckTimeout
	switch action {
	case systemsqlite.OwnerWorkerInspect:
		timeout = systemSQLiteListTimeout
	case systemsqlite.OwnerWorkerCheck:
		timeout = systemSQLiteCheckTimeout
	case systemsqlite.OwnerWorkerSnapshot:
		timeout = systemSQLiteSnapshotTimeout
	case systemsqlite.OwnerWorkerOptimize:
		timeout = systemSQLiteOptimizeTimeout
	default:
		return true, errors.New("unknown isolated SQLite worker operation")
	}

	var destination *os.File
	var workspace *os.File
	var limits systemsqlite.SnapshotLimits
	if action == systemsqlite.OwnerWorkerSnapshot {
		if len(os.Args) != 6 {
			return true, errors.New("isolated SQLite snapshot capacity limits are required")
		}
		maxBytes, err := strconv.ParseInt(os.Args[4], 10, 64)
		if err != nil {
			return true, errors.New("isolated SQLite snapshot size limit is invalid")
		}
		freeSpaceFloor, err := strconv.ParseInt(os.Args[5], 10, 64)
		if err != nil {
			return true, errors.New("isolated SQLite snapshot free-space reserve is invalid")
		}
		limits = systemsqlite.SnapshotLimits{
			MaxBytes:       maxBytes,
			FreeSpaceFloor: freeSpaceFloor,
		}
		destination = os.NewFile(uintptr(systemsqlite.OwnerWorkerDestinationFD), "system-sqlite-snapshot")
		if destination == nil {
			return true, errors.New("isolated SQLite snapshot descriptor is missing")
		}
		defer destination.Close()
		workspace = os.NewFile(uintptr(systemsqlite.OwnerWorkerWorkspaceFD), "system-sqlite-workspace")
		if workspace == nil {
			return true, errors.New("isolated SQLite workspace descriptor is missing")
		}
		defer workspace.Close()
	} else if len(os.Args) != 4 {
		return true, errors.New("unexpected isolated SQLite worker argument")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()
	response := systemsqlite.RunOwnerWorkerOperation(
		ctx,
		systemSQLiteDefinitions(),
		action,
		databaseID,
		destination,
		workspace,
		limits,
	)
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		return true, errors.New("could not encode isolated SQLite worker response")
	}
	return true, nil
}
