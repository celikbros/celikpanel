//go:build linux

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// createVerifiedSnapshotFromPinnedWAL first materializes a standalone,
// transaction-consistent copy from the pinned main database and the last
// checksum-valid committed WAL frame. The published release snapshot is
// durable and fully validated before the canonical database is modified.
func (s *quarantinedServiceOperationSnapshotSource) createVerifiedSnapshotFromPinnedWAL(
	destinationPath string,
	schema serviceOperationSnapshotSchema,
) (returnErr error) {
	if err := s.verify(); err != nil {
		return err
	}
	workspace, err := createSecureLiveIdleTemporaryDirectory()
	if err != nil {
		return fmt.Errorf("create private release snapshot workspace: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(workspace); cleanupErr != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("remove private release snapshot workspace: %w", cleanupErr),
			)
		}
	}()

	privatePath := filepath.Join(workspace, s.baseName)
	databaseInfo, err := s.database.file.Stat()
	if err != nil {
		return fmt.Errorf("inspect pinned canonical database: %w", err)
	}
	if err := copyPinnedLiveSQLiteFile(
		s.database.file,
		databaseInfo,
		privatePath,
	); err != nil {
		return fmt.Errorf("copy pinned canonical database: %w", err)
	}
	if wal := s.sidecars["-wal"]; wal != nil {
		walInfo, err := wal.file.Stat()
		if err != nil {
			return fmt.Errorf("inspect pinned canonical SQLite WAL: %w", err)
		}
		if walInfo.Size() != 0 {
			committedSize, err := recoverPinnedLiveSQLiteWAL(&pinnedSQLiteSidecar{
				file: wal.file,
				info: walInfo,
			})
			if err != nil {
				return fmt.Errorf("recover pinned canonical SQLite WAL: %w", err)
			}
			if committedSize != 0 {
				if err := copyPinnedLiveSQLiteFilePrefix(
					wal.file,
					walInfo,
					committedSize,
					privatePath+"-wal",
				); err != nil {
					return fmt.Errorf("copy committed canonical SQLite WAL: %w", err)
				}
			}
		}
	}
	if err := syncLiveIdleSnapshotDirectory(workspace); err != nil {
		return fmt.Errorf("sync private release snapshot workspace: %w", err)
	}
	if err := s.verify(); err != nil {
		return err
	}
	if err := normalizeStandaloneSQLiteSnapshot(privatePath); err != nil {
		return fmt.Errorf("normalize private release snapshot: %w", err)
	}
	if err := canonicalizeKnownLegacySnapshotSchemaMigrations(privatePath); err != nil {
		return fmt.Errorf("canonicalize known legacy private release snapshot: %w", err)
	}
	if err := requireSQLiteSidecarsAbsent(privatePath, "private release snapshot"); err != nil {
		return err
	}
	if err := syncLiveIdleSnapshotFile(privatePath); err != nil {
		return fmt.Errorf("sync normalized private release snapshot: %w", err)
	}
	if err := syncLiveIdleSnapshotDirectory(workspace); err != nil {
		return fmt.Errorf("sync normalized private release snapshot workspace: %w", err)
	}
	if err := validateServiceOperationSnapshot(privatePath, schema); err != nil {
		return fmt.Errorf("validate private release snapshot: %w", err)
	}
	if err := s.verify(); err != nil {
		return err
	}
	return createVerifiedQuarantinedServiceOperationSnapshot(
		privatePath,
		destinationPath,
		schema,
		s.verify,
	)
}

// normalizeCanonicalDatabase is allowed to run only after the verified release
// snapshot above has been durably published. The parent remains root-only and
// every source inode is pinned while legacy-safe modes are narrowed to 0600.
// SQLite itself checkpoints and removes its WAL/SHM; sidecars are never
// unlinked by application code.
func (s *quarantinedServiceOperationSnapshotSource) normalizeCanonicalDatabase(
	schema serviceOperationSnapshotSchema,
) error {
	if err := s.verify(); err != nil {
		return err
	}
	originalDatabase := s.database.stat
	if err := s.makePinnedSQLiteEntriesPrivate(); err != nil {
		return err
	}
	if err := s.closePinnedEntries(); err != nil {
		return fmt.Errorf("close pinned canonical SQLite entries before checkpoint: %w", err)
	}

	canonicalPath := fmt.Sprintf(
		"/proc/self/fd/%d/%s",
		s.parent.Fd(),
		s.baseName,
	)
	if err := normalizeCanonicalSQLiteDatabase(canonicalPath); err != nil {
		return err
	}
	if err := requireSQLiteSidecarsAbsent(
		canonicalPath,
		"normalized canonical SQLite source",
	); err != nil {
		return err
	}

	var currentDatabase unix.Stat_t
	if err := unix.Fstatat(
		int(s.parent.Fd()),
		s.baseName,
		&currentDatabase,
		unix.AT_SYMLINK_NOFOLLOW,
	); err != nil {
		return fmt.Errorf("inspect normalized canonical SQLite source: %w", err)
	}
	if !sameUnixFileIdentity(originalDatabase, currentDatabase) {
		return fmt.Errorf("canonical SQLite source inode changed during normalization")
	}
	if err := validatePanelOwnedDatabaseFile(
		currentDatabase,
		s.owner,
		"normalized canonical SQLite source",
	); err != nil {
		return err
	}

	s.sidecars = make(map[string]*exactPinnedSQLiteFile, 3)
	database, err := s.pinEntry("")
	if err != nil {
		return err
	}
	if database == nil {
		return fmt.Errorf("normalized canonical SQLite source disappeared")
	}
	if err := validatePanelOwnedDatabaseFile(
		database.stat,
		s.owner,
		"pinned normalized canonical SQLite source",
	); err != nil {
		_ = database.file.Close()
		return err
	}
	s.database = *database
	s.initial = database.stat
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		s.sidecars[suffix] = nil
	}
	if err := validateServiceOperationSnapshot(s.sqlitePath(), schema); err != nil {
		return fmt.Errorf("validate normalized canonical SQLite source: %w", err)
	}
	if err := s.database.file.Sync(); err != nil {
		return fmt.Errorf("sync normalized canonical SQLite source: %w", err)
	}
	if err := s.parent.Sync(); err != nil {
		return fmt.Errorf("sync normalized canonical SQLite directory: %w", err)
	}
	return s.verify()
}

func (s *quarantinedServiceOperationSnapshotSource) makePinnedSQLiteEntriesPrivate() error {
	entries := []*exactPinnedSQLiteFile{&s.database}
	for _, suffix := range []string{"-wal", "-shm"} {
		if sidecar := s.sidecars[suffix]; sidecar != nil {
			entries = append(entries, sidecar)
		}
	}
	for _, entry := range entries {
		if entry == nil || entry.file == nil {
			return fmt.Errorf("canonical SQLite entry is not pinned")
		}
		original := entry.stat
		if err := unix.Fchmod(int(entry.file.Fd()), 0o600); err != nil {
			return fmt.Errorf("protect pinned canonical SQLite entry: %w", err)
		}
		if err := entry.file.Sync(); err != nil {
			return fmt.Errorf("sync protected canonical SQLite entry: %w", err)
		}
		var current unix.Stat_t
		if err := unix.Fstat(int(entry.file.Fd()), &current); err != nil {
			return fmt.Errorf("inspect protected canonical SQLite entry: %w", err)
		}
		if !sameUnixFileIdentity(original, current) {
			return fmt.Errorf("canonical SQLite entry inode changed while protecting it")
		}
		if err := validatePanelOwnedDatabaseFile(
			current,
			s.owner,
			"protected canonical SQLite entry",
		); err != nil {
			return err
		}
		entry.stat = current
	}
	s.initial = s.database.stat
	return s.verify()
}

func normalizeCanonicalSQLiteDatabase(databasePath string) (returnErr error) {
	previousUmask := unix.Umask(0o077)
	defer unix.Umask(previousUmask)

	database, err := sql.Open("sqlite", canonicalSQLiteNormalizationURI(databasePath))
	if err != nil {
		return fmt.Errorf("open canonical SQLite source for normalization: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, database.Close())
	}()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("open canonical SQLite source: %w", err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		return fmt.Errorf("set canonical SQLite synchronous mode: %w", err)
	}
	var busy, logFrames, checkpointedFrames int
	if err := database.QueryRowContext(
		ctx,
		"PRAGMA wal_checkpoint(TRUNCATE)",
	).Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("checkpoint canonical SQLite WAL: %w", err)
	}
	if busy != 0 {
		return fmt.Errorf("canonical SQLite WAL checkpoint remained busy")
	}
	if logFrames >= 0 && checkpointedFrames != logFrames {
		return fmt.Errorf(
			"canonical SQLite WAL checkpoint copied %d of %d frames",
			checkpointedFrames,
			logFrames,
		)
	}

	var journalMode string
	if err := database.QueryRowContext(
		ctx,
		"PRAGMA journal_mode=DELETE",
	).Scan(&journalMode); err != nil {
		return fmt.Errorf("set canonical SQLite journal mode: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "delete") {
		return fmt.Errorf(
			"canonical SQLite journal mode is %q, expected delete",
			journalMode,
		)
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheck); err != nil {
		return fmt.Errorf("run canonical SQLite quick check: %w", err)
	}
	if quickCheck != "ok" {
		return fmt.Errorf("canonical SQLite quick check returned %q", quickCheck)
	}
	foreignKeyRows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("run canonical SQLite foreign key check: %w", err)
	}
	hasForeignKeyViolation := foreignKeyRows.Next()
	if err := foreignKeyRows.Err(); err != nil {
		foreignKeyRows.Close()
		return fmt.Errorf("read canonical SQLite foreign key check: %w", err)
	}
	if err := foreignKeyRows.Close(); err != nil {
		return fmt.Errorf("close canonical SQLite foreign key check: %w", err)
	}
	if hasForeignKeyViolation {
		return fmt.Errorf("canonical SQLite foreign key check found a violation")
	}
	return nil
}

func canonicalSQLiteNormalizationURI(databasePath string) string {
	path := filepath.ToSlash(databasePath)
	uri := &url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func requireSQLiteSidecarsAbsent(databasePath string, purpose string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_, err := os.Lstat(databasePath + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s%s: %w", purpose, suffix, err)
		}
		return fmt.Errorf("%s%s must be absent", purpose, suffix)
	}
	return nil
}
