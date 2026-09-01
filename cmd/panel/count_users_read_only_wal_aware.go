package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
)

const deadPlaceholderAdminPasswordHash = "$2a$10$rVQ8K5h6Z.Zg0qX7J3K3KuF7pB3vZ8mN9lD5qE0wY0kX0H0L0M0N0"

// This command runs as a short-lived root admission probe. Keep the complete
// private image plus modernc/sqlite's Deserialize copy below a predictable
// memory ceiling; an unexpectedly large existing database fails closed before
// the installer is allowed to mutate the host.
const maxReadOnlyAdmissionDatabaseBytes = int64(256 << 20)

const maxReadOnlyWALAwareUserCountAttempts = 3

func countUsableUsersReadOnlyWALAware(databasePath string) (int, error) {
	return countUsableUsersReadOnlyWALAwareWithAttemptHook(databasePath, nil)
}

// readOnlyWALAwareUserCountAttemptHook is a package-private deterministic test
// seam. Production callers always pass nil; the hook cannot be selected by a
// flag, environment variable, or external package.
type readOnlyWALAwareUserCountAttemptHook func(attempt int) error

func countUsableUsersReadOnlyWALAwareWithAttemptHook(
	databasePath string,
	afterPin readOnlyWALAwareUserCountAttemptHook,
) (int, error) {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		absolutePath, err := filepath.Abs(databasePath)
		if err != nil {
			return 0, fmt.Errorf("resolve panel database path: %w", err)
		}
		databasePath = absolutePath
	}
	var lastErr error
	for attempt := 1; attempt <= maxReadOnlyWALAwareUserCountAttempts; attempt++ {
		count, err := countUsableUsersReadOnlyWALAwareAttempt(databasePath, attempt, afterPin)
		if err == nil {
			return count, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf(
		"count login-capable administrators from a coherent read-only SQLite snapshot after %d attempts: %w",
		maxReadOnlyWALAwareUserCountAttempts,
		lastErr,
	)
}

func countUsableUsersReadOnlyWALAwareAttempt(
	databasePath string,
	attempt int,
	afterPin readOnlyWALAwareUserCountAttemptHook,
) (int, error) {
	// Admission must not create even a private on-disk snapshot: this mode runs
	// before the installer is allowed to mutate the host. Pin the database and
	// all SQLite sidecars, recover the last committed WAL frame into memory, and
	// fail closed if any source metadata moves while the count is in flight.
	pinned, err := pinNoAtimeWALAwarePanelDatabase(databasePath)
	if err != nil {
		return 0, fmt.Errorf("pin WAL-aware panel database for user count: %w", err)
	}
	defer pinned.close()
	if afterPin != nil {
		if err := afterPin(attempt); err != nil {
			return 0, fmt.Errorf("run read-only user-count attempt hook: %w", err)
		}
	}

	serialized, err := materializePinnedSQLiteDatabaseInMemory(pinned)
	if err != nil {
		return 0, err
	}
	defer clear(serialized)
	if err := pinned.verifyPath(); err != nil {
		return 0, fmt.Errorf("verify WAL-aware panel database after memory snapshot: %w", err)
	}

	count, err := countUsableUsersInSerializedSQLiteDatabase(serialized)
	if err != nil {
		return 0, err
	}
	if err := pinned.verifyPath(); err != nil {
		return 0, fmt.Errorf("verify WAL-aware panel database after user count: %w", err)
	}
	return count, nil
}

func materializePinnedSQLiteDatabaseInMemory(pinned *pinnedPanelDatabase) ([]byte, error) {
	if pinned == nil || pinned.file == nil || pinned.info == nil ||
		!pinned.info.Mode().IsRegular() || pinned.info.Size() < 100 {
		return nil, fmt.Errorf("pinned SQLite database is not a valid regular file")
	}
	if pinned.info.Size() > maxReadOnlyAdmissionDatabaseBytes {
		return nil, fmt.Errorf("pinned SQLite database exceeds the read-only admission limit")
	}
	const sqliteHeaderBytes = int64(100)
	header := make([]byte, sqliteHeaderBytes)
	if _, err := io.ReadFull(io.NewSectionReader(pinned.file, 0, sqliteHeaderBytes), header); err != nil {
		clear(header)
		return nil, fmt.Errorf("read pinned SQLite database: %w", err)
	}
	mainPageSize, err := validateSQLiteDatabaseHeader(header)
	if err != nil {
		clear(header)
		return nil, fmt.Errorf("pinned SQLite database header is invalid: %w", err)
	}
	if pinned.info.Size()%int64(mainPageSize) != 0 {
		clear(header)
		return nil, fmt.Errorf("pinned SQLite database size is not page-aligned")
	}
	clear(header)

	wal := pinned.sidecars["-wal"]
	if wal == nil || wal.info == nil || wal.info.Size() == 0 {
		serialized := make([]byte, int(pinned.info.Size()))
		if _, err := io.ReadFull(io.NewSectionReader(pinned.file, 0, pinned.info.Size()), serialized); err != nil {
			clear(serialized)
			return nil, fmt.Errorf("read pinned SQLite database: %w", err)
		}
		serialized[18] = 1
		serialized[19] = 1
		return serialized, nil
	}
	if wal.info.Size() > maxReadOnlyAdmissionDatabaseBytes {
		return nil, fmt.Errorf("pinned SQLite WAL exceeds the read-only admission limit")
	}
	committedEnd, err := recoverPinnedLiveSQLiteWAL(wal)
	if err != nil {
		return nil, fmt.Errorf("recover pinned SQLite WAL in memory: %w", err)
	}
	if committedEnd == 0 {
		serialized := make([]byte, int(pinned.info.Size()))
		if _, err := io.ReadFull(io.NewSectionReader(pinned.file, 0, pinned.info.Size()), serialized); err != nil {
			clear(serialized)
			return nil, fmt.Errorf("read pinned SQLite database: %w", err)
		}
		serialized[18] = 1
		serialized[19] = 1
		return serialized, nil
	}

	const walHeaderSize = int64(32)
	const walFrameHeaderSize = int64(24)
	walHeader := make([]byte, walHeaderSize)
	if _, err := io.ReadFull(io.NewSectionReader(wal.file, 0, walHeaderSize), walHeader); err != nil {
		clear(walHeader)
		return nil, fmt.Errorf("read pinned SQLite WAL header: %w", err)
	}
	walPageSize := binary.BigEndian.Uint32(walHeader[8:12])
	clear(walHeader)
	if walPageSize == 1 {
		walPageSize = 65536
	}
	if walPageSize != mainPageSize {
		return nil, fmt.Errorf("pinned SQLite database and WAL page sizes differ")
	}
	frameSize := walFrameHeaderSize + int64(walPageSize)
	if committedEnd < walHeaderSize+frameSize || (committedEnd-walHeaderSize)%frameSize != 0 {
		return nil, fmt.Errorf("recovered SQLite WAL commit boundary is invalid")
	}
	frame := make([]byte, frameSize)
	defer clear(frame)
	if _, err := io.ReadFull(io.NewSectionReader(wal.file, committedEnd-frameSize, frameSize), frame); err != nil {
		return nil, fmt.Errorf("read final committed SQLite WAL frame: %w", err)
	}
	committedPages := binary.BigEndian.Uint32(frame[4:8])
	if committedPages == 0 {
		return nil, fmt.Errorf("recovered SQLite WAL has no committed database size")
	}
	committedSize := int64(committedPages) * int64(walPageSize)
	if committedSize < sqliteHeaderBytes || committedSize > maxReadOnlyAdmissionDatabaseBytes {
		return nil, fmt.Errorf("recovered SQLite WAL database size is invalid")
	}
	serialized := make([]byte, int(committedSize))
	mainBytes := pinned.info.Size()
	if committedSize < mainBytes {
		mainBytes = committedSize
	}
	if _, err := io.ReadFull(io.NewSectionReader(pinned.file, 0, mainBytes), serialized[:int(mainBytes)]); err != nil {
		clear(serialized)
		return nil, fmt.Errorf("read pinned SQLite database prefix: %w", err)
	}
	for offset := walHeaderSize; offset < committedEnd; offset += frameSize {
		if _, err := io.ReadFull(io.NewSectionReader(wal.file, offset, frameSize), frame); err != nil {
			clear(serialized)
			return nil, fmt.Errorf("read committed SQLite WAL frame: %w", err)
		}
		pageNumber := binary.BigEndian.Uint32(frame[0:4])
		pageEnd := int64(pageNumber) * int64(walPageSize)
		if pageNumber == 0 || pageEnd > maxReadOnlyAdmissionDatabaseBytes {
			clear(serialized)
			return nil, fmt.Errorf("committed SQLite WAL page is outside the admission limit")
		}
		// A later commit may shrink the database. Frames for pages above the
		// final committed size belong to the superseded larger image.
		if pageNumber > committedPages {
			continue
		}
		pageStart := pageEnd - int64(walPageSize)
		copy(serialized[int(pageStart):int(pageEnd)], frame[walFrameHeaderSize:])
	}
	finalPageSize, err := validateSQLiteDatabaseHeader(serialized)
	if err != nil {
		clear(serialized)
		return nil, fmt.Errorf("recovered SQLite database header is invalid: %w", err)
	}
	if finalPageSize != mainPageSize {
		clear(serialized)
		return nil, fmt.Errorf("recovered SQLite database changed its page size")
	}
	binary.BigEndian.PutUint32(serialized[28:32], committedPages)
	serialized[18] = 1
	serialized[19] = 1
	return serialized, nil
}

func validateSQLiteDatabaseHeader(header []byte) (uint32, error) {
	if len(header) < 100 || string(header[:16]) != "SQLite format 3\x00" {
		return 0, fmt.Errorf("magic is invalid")
	}
	pageSize := uint32(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		return 0, fmt.Errorf("page size is invalid")
	}
	if (header[18] != 1 && header[18] != 2) || (header[19] != 1 && header[19] != 2) {
		return 0, fmt.Errorf("read/write format is invalid")
	}
	return pageSize, nil
}
