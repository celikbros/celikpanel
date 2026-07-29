package systemsqlite

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MutableInspection contains the bounded SQLite metadata read by an isolated worker.
// MutableInspection, yalıtılmış çalışanın okuduğu sınırlı SQLite meta verisini içerir.
type MutableInspection struct {
	JournalMode string `json:"journal_mode"`
	UserVersion int    `json:"user_version"`
}

// MutableOperations keeps every SQLite open of a service-writable database behind an isolation boundary.
// MutableOperations, servis tarafından yazılabilen veritabanlarının her SQLite açılışını bir yalıtım sınırının arkasında tutar.
type MutableOperations interface {
	Inspect(context.Context, Definition) (MutableInspection, error)
	Check(context.Context, Definition) (CheckResult, error)
	Snapshot(context.Context, Definition, *os.File, SnapshotLimits) error
	Optimize(context.Context, Definition) error
}

type unavailableMutableOperations struct{}

func (unavailableMutableOperations) Inspect(context.Context, Definition) (MutableInspection, error) {
	return MutableInspection{}, errors.New("owner-isolated mutable SQLite operations are unavailable")
}

func (unavailableMutableOperations) Check(context.Context, Definition) (CheckResult, error) {
	return CheckResult{}, errors.New("owner-isolated mutable SQLite operations are unavailable")
}

func (unavailableMutableOperations) Snapshot(context.Context, Definition, *os.File, SnapshotLimits) error {
	return errors.New("owner-isolated mutable SQLite operations are unavailable")
}

func (unavailableMutableOperations) Optimize(context.Context, Definition) error {
	return errors.New("owner-isolated mutable SQLite operations are unavailable")
}

type directMutableOperations struct {
	skipOwnerCheckForTests bool
	capacityProbe          snapshotCapacityProbe
	workspaceDirectory     *os.File
}

func (operations directMutableOperations) verifySource(
	source *managedSource,
	definition Definition,
) error {
	if operations.skipOwnerCheckForTests {
		return nil
	}
	return verifyOwnerWorkerSource(source, definition)
}

func (operations directMutableOperations) Inspect(
	ctx context.Context,
	definition Definition,
) (MutableInspection, error) {
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return MutableInspection{}, err
	}
	defer source.close()
	if err := operations.verifySource(source, definition); err != nil {
		return MutableInspection{}, err
	}
	database, err := openSQLite(ctx, source.databasePath(), "ro")
	if err != nil {
		return MutableInspection{}, err
	}
	defer database.Close()
	var result MutableInspection
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&result.JournalMode); err != nil {
		return MutableInspection{}, err
	}
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.UserVersion); err != nil {
		return MutableInspection{}, err
	}
	if err := source.verifyIdentity(); err != nil {
		return MutableInspection{}, err
	}
	return result, nil
}

func (operations directMutableOperations) Check(
	ctx context.Context,
	definition Definition,
) (CheckResult, error) {
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return CheckResult{}, err
	}
	defer source.close()
	if err := operations.verifySource(source, definition); err != nil {
		return CheckResult{}, err
	}
	database, err := openSQLite(ctx, source.databasePath(), "ro")
	if err != nil {
		return CheckResult{}, err
	}
	defer database.Close()

	result := CheckResult{DatabaseID: definition.ID}
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&integrity); err != nil {
		return CheckResult{}, err
	}
	result.IntegrityMessage = boundedMessage(integrity)
	result.IntegrityOK = strings.EqualFold(strings.TrimSpace(integrity), "ok")

	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return CheckResult{}, err
	}
	for rows.Next() {
		var tableName any
		var rowID any
		var parentName any
		var foreignKeyID any
		if err := rows.Scan(&tableName, &rowID, &parentName, &foreignKeyID); err != nil {
			_ = rows.Close()
			return CheckResult{}, err
		}
		result.ForeignKeyViolations++
		if result.ForeignKeyViolations >= maxForeignKeyViolations {
			result.ForeignKeyCheckTruncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return CheckResult{}, err
	}
	if err := rows.Close(); err != nil {
		return CheckResult{}, err
	}
	result.ForeignKeysOK = result.ForeignKeyViolations == 0
	if err := source.verifyIdentity(); err != nil {
		return CheckResult{}, err
	}
	return result, nil
}

func (operations directMutableOperations) Snapshot(
	ctx context.Context,
	definition Definition,
	destination *os.File,
	limits SnapshotLimits,
) error {
	if destination == nil || limits.validate() != nil {
		return errors.New("invalid isolated snapshot destination")
	}
	maxBytes := limits.MaxBytes
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return err
	}
	defer source.close()
	if err := operations.verifySource(source, definition); err != nil {
		return err
	}
	if source.info.Size() > maxBytes {
		return errors.New("managed database exceeds the snapshot size limit")
	}

	temporaryDirectory := ""
	workspaceDirectory := operations.workspaceDirectory
	closeWorkspaceDirectory := false
	cleanupTemporaryDirectory := false
	if workspaceDirectory == nil {
		temporaryRoot := "/tmp"
		if operations.skipOwnerCheckForTests {
			temporaryRoot = ""
		}
		temporaryDirectory, err = os.MkdirTemp(temporaryRoot, ".celikpanel-sqlite-owner-")
		if err != nil {
			return errors.New("could not create isolated SQLite workspace")
		}
		cleanupTemporaryDirectory = true
		defer func() {
			if cleanupTemporaryDirectory {
				_ = os.RemoveAll(temporaryDirectory)
			}
		}()
		if err := os.Chmod(temporaryDirectory, 0o700); err != nil {
			return errors.New("could not protect isolated SQLite workspace")
		}
		workspaceDirectory, err = os.Open(temporaryDirectory)
		if err != nil {
			return errors.New("could not verify isolated SQLite snapshot capacity")
		}
		closeWorkspaceDirectory = true
	}
	capacityProbe := operations.capacityProbe
	if capacityProbe == nil {
		capacityProbe = snapshotFilesystemCapacityForFile
	}
	temporaryCapacity, temporaryCapacityErr := capacityProbe(workspaceDirectory)
	temporaryCloseErr := error(nil)
	if closeWorkspaceDirectory {
		temporaryCloseErr = workspaceDirectory.Close()
	}
	destinationCapacity, destinationCapacityErr := capacityProbe(destination)
	if temporaryCapacityErr != nil || temporaryCloseErr != nil || destinationCapacityErr != nil {
		return errors.New("could not verify isolated SQLite snapshot capacity")
	}
	if err := ensureMutableSnapshotCapacity(temporaryCapacity, destinationCapacity, limits); err != nil {
		return err
	}
	temporarySnapshot := "snapshot.sqlite3"
	backupTarget := temporarySnapshot
	if temporaryDirectory != "" {
		temporarySnapshot = filepath.Join(temporaryDirectory, temporarySnapshot)
		backupTarget = temporarySnapshot
	} else {
		backupTarget, err = pinnedManagedDirectoryEntryPath(workspaceDirectory, temporarySnapshot)
		if err != nil {
			return err
		}
	}
	if err := createOnlineSnapshot(ctx, source, backupTarget, maxBytes); err != nil {
		return err
	}
	if err := operations.normalizeSnapshot(ctx, temporarySnapshot); err != nil {
		return err
	}
	if err := source.verifyIdentity(); err != nil {
		return err
	}

	snapshot, err := operations.openSnapshotSource(temporarySnapshot, false)
	if err != nil {
		return err
	}
	defer snapshot.close()
	if snapshot.info.Size() > maxBytes {
		return errors.New("SQLite snapshot exceeds the size limit")
	}
	if err := destination.Truncate(0); err != nil {
		return errors.New("could not prepare isolated snapshot destination")
	}
	if _, err := destination.Seek(0, io.SeekStart); err != nil {
		return errors.New("could not prepare isolated snapshot destination")
	}
	written, err := io.Copy(destination, io.LimitReader(snapshot.file, maxBytes+1))
	if err != nil {
		return errors.New("could not stream isolated SQLite snapshot")
	}
	if written != snapshot.info.Size() || written > maxBytes {
		return errors.New("isolated SQLite snapshot size changed")
	}
	if err := destination.Sync(); err != nil {
		return errors.New("could not persist isolated SQLite snapshot")
	}
	if err := snapshot.verifyIdentity(); err != nil {
		return err
	}
	snapshot.close()
	if temporaryDirectory != "" {
		if err := os.RemoveAll(temporaryDirectory); err != nil {
			return errors.New("could not remove isolated SQLite workspace")
		}
		cleanupTemporaryDirectory = false
	}
	return nil
}

func (operations directMutableOperations) openSnapshotSource(
	path string,
	writable bool,
) (*managedSource, error) {
	if operations.workspaceDirectory != nil {
		return openManagedSourceInCurrentDirectory(path, writable)
	}
	return openManagedSource(path, writable)
}

func (operations directMutableOperations) normalizeSnapshot(ctx context.Context, path string) error {
	if operations.workspaceDirectory == nil {
		return normalizeAndCheckSnapshot(ctx, path)
	}
	source, err := openManagedSourceInCurrentDirectory(path, true)
	if err != nil {
		return err
	}
	defer source.close()
	return normalizeAndCheckSnapshotSource(ctx, source, path)
}

func (operations directMutableOperations) Optimize(ctx context.Context, definition Definition) error {
	source, err := openManagedSource(definition.Path, true)
	if err != nil {
		return err
	}
	defer source.close()
	if err := operations.verifySource(source, definition); err != nil {
		return err
	}
	database, err := openSQLite(ctx, source.databasePath(), "rw")
	if err != nil {
		return err
	}
	_, optimizeErr := database.ExecContext(ctx, "PRAGMA optimize")
	closeErr := database.Close()
	if optimizeErr != nil {
		return optimizeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return source.verifyIdentity()
}
