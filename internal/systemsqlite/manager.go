package systemsqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

const (
	defaultSnapshotRoot     = "/var/lib/celikpanel-agent-private/system-sqlite-snapshots"
	defaultSnapshotTTL      = 5 * time.Minute
	defaultCleanupInterval  = 30 * time.Second
	defaultMaxSnapshotBytes = int64(2 << 30)
	defaultFreeSpaceFloor   = int64(512 << 20)
	snapshotDirectoryPrefix = "celikpanel-system-sqlite-"
	maxForeignKeyViolations = 10_000
	maxIntegrityMessage     = 512
)

type Options struct {
	SnapshotRoot      string
	SnapshotTTL       time.Duration
	CleanupInterval   time.Duration
	MaxSnapshotBytes  int64
	FreeSpaceFloor    int64
	AvailableBytes    func(string) (int64, error)
	MutableOperations MutableOperations
	Now               func() time.Time
}

type snapshotEntry struct {
	mu            sync.Mutex
	info          SnapshotInfo
	root          string
	directory     string
	source        *managedSource
	releasing     bool
	cleanupFailed bool
}

type Manager struct {
	definitions map[string]Definition
	order       []string
	databaseMu  map[string]*sync.RWMutex

	snapshotRoot      string
	snapshotTTL       time.Duration
	cleanupInterval   time.Duration
	maxSnapshotBytes  int64
	freeSpaceFloor    int64
	availableBytes    func(string) (int64, error)
	mutableOperations MutableOperations
	now               func() time.Time
	snapshotMu        sync.Mutex
	snapshotReady     bool

	mu               sync.Mutex
	snapshots        map[string]*snapshotEntry
	snapshotSlotHeld bool
	closed           bool
	stop             chan struct{}
	done             chan struct{}
	closeOnce        sync.Once
}

func NewManager(definitions []Definition, options Options) (*Manager, error) {
	if len(definitions) == 0 {
		return nil, errors.New("at least one system SQLite database is required")
	}
	manager := &Manager{
		definitions:       make(map[string]Definition, len(definitions)),
		databaseMu:        make(map[string]*sync.RWMutex, len(definitions)),
		snapshotRoot:      strings.TrimSpace(options.SnapshotRoot),
		snapshotTTL:       options.SnapshotTTL,
		cleanupInterval:   options.CleanupInterval,
		maxSnapshotBytes:  options.MaxSnapshotBytes,
		freeSpaceFloor:    options.FreeSpaceFloor,
		availableBytes:    options.AvailableBytes,
		mutableOperations: options.MutableOperations,
		now:               options.Now,
		snapshots:         make(map[string]*snapshotEntry),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}
	if manager.snapshotTTL <= 0 {
		manager.snapshotTTL = defaultSnapshotTTL
	}
	if manager.cleanupInterval <= 0 {
		manager.cleanupInterval = defaultCleanupInterval
	}
	if manager.maxSnapshotBytes <= 0 {
		manager.maxSnapshotBytes = defaultMaxSnapshotBytes
	} else if manager.maxSnapshotBytes > defaultMaxSnapshotBytes {
		return nil, errors.New("system SQLite snapshot size limit exceeds the hard maximum")
	}
	if manager.freeSpaceFloor <= 0 {
		manager.freeSpaceFloor = defaultFreeSpaceFloor
	}
	if manager.availableBytes == nil {
		manager.availableBytes = snapshotAvailableBytes
	}
	if manager.mutableOperations == nil {
		manager.mutableOperations = unavailableMutableOperations{}
	}
	if manager.now == nil {
		manager.now = time.Now
	}
	if manager.snapshotRoot == "" {
		manager.snapshotRoot = defaultSnapshotRoot
	}
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Name = strings.TrimSpace(definition.Name)
		definition.Purpose = strings.TrimSpace(definition.Purpose)
		definition.Kind = strings.TrimSpace(definition.Kind)
		definition.Path = filepath.Clean(strings.TrimSpace(definition.Path))
		definition.PathHint = strings.TrimSpace(definition.PathHint)
		if !knownDatabaseID(definition.ID) {
			return nil, fmt.Errorf("unknown system SQLite database id %q", definition.ID)
		}
		if _, exists := manager.definitions[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate system SQLite database id %q", definition.ID)
		}
		if definition.Name == "" || definition.Purpose == "" || definition.Kind == "" ||
			!filepath.IsAbs(definition.Path) {
			return nil, fmt.Errorf("invalid definition for system SQLite database %q", definition.ID)
		}
		if definition.PathHint == "" || filepath.IsAbs(definition.PathHint) {
			return nil, fmt.Errorf("unsafe path hint for system SQLite database %q", definition.ID)
		}
		if definition.Optimizable && !definition.Mutable {
			return nil, fmt.Errorf("immutable system SQLite database %q cannot be optimizable", definition.ID)
		}
		manager.definitions[definition.ID] = definition
		manager.databaseMu[definition.ID] = &sync.RWMutex{}
		manager.order = append(manager.order, definition.ID)
	}
	sort.SliceStable(manager.order, func(left, right int) bool {
		return databaseOrder(manager.order[left]) < databaseOrder(manager.order[right])
	})

	go manager.cleanupLoop()
	return manager, nil
}

func ValidateProtocol(version int) error {
	if version != ProtocolVersion {
		return fmt.Errorf("unsupported system SQLite protocol version")
	}
	return nil
}

func (manager *Manager) List(ctx context.Context) ([]DatabaseInfo, error) {
	if err := manager.ensureOpen(); err != nil {
		return nil, err
	}
	result := make([]DatabaseInfo, 0, len(manager.order))
	for _, id := range manager.order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		definition := manager.definitions[id]
		result = append(result, inspectDefinition(ctx, definition, manager.mutableOperations))
	}
	return result, nil
}

func (manager *Manager) Check(ctx context.Context, databaseID string) (CheckResult, error) {
	definition, lock, err := manager.resolve(databaseID)
	if err != nil {
		return CheckResult{}, err
	}
	lock.RLock()
	defer lock.RUnlock()
	if definition.Mutable {
		result, err := manager.mutableOperations.Check(ctx, definition)
		if err != nil {
			return CheckResult{}, publicDatabaseError(err)
		}
		result.DatabaseID = databaseID
		result.CheckedAt = manager.now().UTC()
		return result, nil
	}
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	defer source.close()

	database, err := openSQLite(ctx, source.databasePath(), "ro")
	if err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	defer database.Close()

	result := CheckResult{
		DatabaseID: databaseID,
		CheckedAt:  manager.now().UTC(),
	}
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&integrity); err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	result.IntegrityMessage = boundedMessage(integrity)
	result.IntegrityOK = strings.EqualFold(strings.TrimSpace(integrity), "ok")

	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	for rows.Next() {
		var tableName any
		var rowID any
		var parentName any
		var foreignKeyID any
		if err := rows.Scan(&tableName, &rowID, &parentName, &foreignKeyID); err != nil {
			_ = rows.Close()
			return CheckResult{}, publicDatabaseError(err)
		}
		result.ForeignKeyViolations++
		if result.ForeignKeyViolations >= maxForeignKeyViolations {
			result.ForeignKeyCheckTruncated = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return CheckResult{}, publicDatabaseError(err)
	}
	if err := rows.Close(); err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	result.ForeignKeysOK = result.ForeignKeyViolations == 0
	if err := source.verifyIdentity(); err != nil {
		return CheckResult{}, publicDatabaseError(err)
	}
	return result, nil
}

func (manager *Manager) CreateSnapshot(
	ctx context.Context,
	databaseID string,
) (result SnapshotInfo, resultErr error) {
	definition, lock, err := manager.resolve(databaseID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	if !definition.SnapshotAllowed {
		return SnapshotInfo{}, errors.New("snapshots are not allowed for this system SQLite database")
	}
	if err := manager.claimSnapshotSlot(); err != nil {
		return SnapshotInfo{}, err
	}
	releaseSlot := true
	directory := ""
	defer func() {
		if !releaseSlot {
			return
		}
		if directory != "" {
			if err := removePrivateSnapshotDirectory(manager.snapshotRoot, directory); err != nil {
				result = SnapshotInfo{}
				resultErr = errors.New("could not clean private snapshot storage")
				return
			}
		}
		manager.releaseSnapshotSlot()
	}()
	lock.RLock()
	defer lock.RUnlock()
	if err := manager.ensureSnapshotStorage(); err != nil {
		return SnapshotInfo{}, errors.New("private snapshot storage is not available")
	}
	if err := manager.ensureSnapshotCapacity(); err != nil {
		return SnapshotInfo{}, err
	}

	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		return SnapshotInfo{}, publicDatabaseError(err)
	}
	defer source.close()
	if source.info.Size() > manager.maxSnapshotBytes {
		return SnapshotInfo{}, errors.New("managed database exceeds the snapshot size limit")
	}

	directory, err = os.MkdirTemp(manager.snapshotRoot, snapshotDirectoryPrefix)
	if err != nil {
		return SnapshotInfo{}, errors.New("could not create private snapshot storage")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return SnapshotInfo{}, errors.New("could not protect private snapshot storage")
	}
	target := filepath.Join(directory, "snapshot.sqlite3")

	if definition.Mutable {
		targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return SnapshotInfo{}, errors.New("could not create isolated SQLite snapshot destination")
		}
		operationErr := manager.mutableOperations.Snapshot(
			ctx,
			definition,
			targetFile,
			SnapshotLimits{
				MaxBytes:       manager.maxSnapshotBytes,
				FreeSpaceFloor: manager.freeSpaceFloor,
			},
		)
		closeErr := targetFile.Close()
		if operationErr != nil {
			return SnapshotInfo{}, publicDatabaseError(operationErr)
		}
		if closeErr != nil {
			return SnapshotInfo{}, errors.New("could not close isolated SQLite snapshot destination")
		}
	} else {
		if err := copyImmutableSnapshot(ctx, source, target, manager.maxSnapshotBytes); err != nil {
			return SnapshotInfo{}, publicDatabaseError(err)
		}
	}
	if err := source.verifyIdentity(); err != nil {
		return SnapshotInfo{}, publicDatabaseError(err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return SnapshotInfo{}, errors.New("could not protect the SQLite snapshot")
	}
	snapshotSource, err := openManagedSource(target, false)
	if err != nil {
		return SnapshotInfo{}, errors.New("could not pin the SQLite snapshot")
	}
	if snapshotSource.info.Size() > manager.maxSnapshotBytes {
		snapshotSource.close()
		return SnapshotInfo{}, errors.New("SQLite snapshot exceeds the size limit")
	}
	digest, err := digestPinnedSnapshot(snapshotSource)
	if err != nil {
		snapshotSource.close()
		return SnapshotInfo{}, errors.New("could not digest the SQLite snapshot")
	}

	now := manager.now().UTC()
	info := SnapshotInfo{
		DatabaseID: databaseID,
		SizeBytes:  snapshotSource.info.Size(),
		SHA256:     digest,
		CreatedAt:  now,
		ExpiresAt:  now.Add(manager.snapshotTTL),
	}
	token, err := manager.registerSnapshot(&snapshotEntry{
		info:      info,
		root:      manager.snapshotRoot,
		directory: directory,
		source:    snapshotSource,
	})
	if err != nil {
		snapshotSource.close()
		return SnapshotInfo{}, err
	}
	info.Token = token
	directory = ""
	releaseSlot = false
	return info, nil
}

type onlineBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

func createOnlineSnapshot(
	ctx context.Context,
	source *managedSource,
	target string,
	maxBytes int64,
) error {
	database, err := openSQLite(ctx, source.databasePath(), "ro")
	if err != nil {
		return err
	}
	defer database.Close()

	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return connection.Raw(func(driverConnection any) error {
		backuper, ok := driverConnection.(onlineBackuper)
		if !ok {
			return errors.New("SQLite driver does not support online backup")
		}
		backup, err := backuper.NewBackup(sqliteURI(target, "rwc"))
		if err != nil {
			return err
		}
		finished := false
		defer func() {
			if !finished {
				_ = backup.Finish()
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := backup.Step(256)
			if err != nil {
				return err
			}
			if err := ensureSnapshotTargetWithinLimit(target, maxBytes); err != nil {
				return err
			}
			if !more {
				break
			}
		}
		if err := backup.Finish(); err != nil {
			return err
		}
		finished = true
		return ensureSnapshotTargetWithinLimit(target, maxBytes)
	})
}

func ensureSnapshotTargetWithinLimit(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return errors.New("SQLite snapshot exceeds the size limit")
	}
	return nil
}

func normalizeAndCheckSnapshot(ctx context.Context, path string) error {
	source, err := openManagedSource(path, true)
	if err != nil {
		return err
	}
	defer source.close()
	return normalizeAndCheckSnapshotSource(ctx, source, path)
}

func normalizeAndCheckSnapshotSource(
	ctx context.Context,
	source *managedSource,
	sidecarPath string,
) error {
	if source == nil || source.file == nil {
		return errors.New("SQLite snapshot is not pinned")
	}
	database, err := openSQLite(ctx, source.databasePath(), "rw")
	if err != nil {
		return err
	}
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&journalMode); err != nil {
		_ = database.Close()
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(journalMode), "delete") {
		_ = database.Close()
		return errors.New("SQLite snapshot could not be normalized")
	}
	var quickCheck string
	if err := database.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&quickCheck); err != nil {
		_ = database.Close()
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(quickCheck), "ok") {
		_ = database.Close()
		return errors.New("SQLite snapshot quick check failed")
	}
	if err := database.Close(); err != nil {
		return err
	}
	if err := source.verifyIdentity(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(sidecarPath + suffix); err == nil {
			return errors.New("SQLite snapshot is not standalone")
		} else if !os.IsNotExist(err) {
			return errors.New("SQLite snapshot sidecar state could not be verified")
		}
	}
	return nil
}

func copyImmutableSnapshot(
	ctx context.Context,
	source *managedSource,
	target string,
	maxBytes int64,
) error {
	size := source.info.Size()
	if size < 0 || size > maxBytes {
		return errors.New("managed database exceeds the snapshot size limit")
	}
	destination, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("could not create the immutable SQLite snapshot")
	}
	copyErr := copyPinnedBytes(ctx, destination, source.file, size)
	if copyErr == nil {
		copyErr = destination.Sync()
	}
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func copyPinnedBytes(ctx context.Context, destination io.Writer, source *os.File, size int64) error {
	reader := io.NewSectionReader(source, 0, size)
	buffer := make([]byte, DefaultChunkSize)
	var copied int64
	for copied < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := int64(len(buffer))
		if remaining := size - copied; remaining < want {
			want = remaining
		}
		read, err := reader.Read(buffer[:want])
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if read == 0 {
			return errors.New("immutable SQLite source changed while copying")
		}
		written, err := destination.Write(buffer[:read])
		if err != nil {
			return err
		}
		if written != read {
			return io.ErrShortWrite
		}
		copied += int64(written)
	}
	return nil
}

func digestPinnedSnapshot(source *managedSource) (string, error) {
	digest := sha256.New()
	reader := io.NewSectionReader(source.file, 0, source.info.Size())
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (manager *Manager) ReadSnapshotChunk(
	request ReadSnapshotChunkRequest,
) (ReadSnapshotChunkResponse, error) {
	if request.Offset < 0 {
		return ReadSnapshotChunkResponse{}, errors.New("snapshot offset must not be negative")
	}
	maxBytes := request.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultChunkSize
	}
	if maxBytes < 0 || maxBytes > MaxChunkSize {
		return ReadSnapshotChunkResponse{}, errors.New("snapshot chunk size is outside the allowed range")
	}

	snapshot, err := manager.lockSnapshot(request.Token)
	if err != nil {
		return ReadSnapshotChunkResponse{}, err
	}
	defer snapshot.mu.Unlock()

	size := snapshot.info.SizeBytes
	if request.Offset > size {
		return ReadSnapshotChunkResponse{}, errors.New("snapshot offset is beyond the end of the file")
	}
	if request.Offset == size {
		return ReadSnapshotChunkResponse{
			Success:    true,
			DatabaseID: snapshot.info.DatabaseID,
			NextOffset: size,
			SizeBytes:  size,
			EOF:        true,
		}, nil
	}

	remaining := size - request.Offset
	if int64(maxBytes) > remaining {
		maxBytes = int(remaining)
	}
	data := make([]byte, maxBytes)
	read, readErr := snapshot.source.file.ReadAt(data, request.Offset)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return ReadSnapshotChunkResponse{}, errors.New("could not read the SQLite snapshot")
	}
	data = data[:read]
	next := request.Offset + int64(read)
	return ReadSnapshotChunkResponse{
		Success:    true,
		DatabaseID: snapshot.info.DatabaseID,
		Data:       data,
		NextOffset: next,
		SizeBytes:  size,
		EOF:        next == size,
	}, nil
}

func (manager *Manager) ReleaseSnapshot(token string) (bool, error) {
	if !validSnapshotToken(token) {
		return false, errors.New("snapshot not found or expired")
	}
	manager.mu.Lock()
	snapshot := manager.snapshots[token]
	if snapshot == nil {
		manager.mu.Unlock()
		return false, nil
	}
	if snapshot.releasing {
		manager.mu.Unlock()
		return false, errors.New("snapshot cleanup is already in progress")
	}
	snapshot.releasing = true
	manager.mu.Unlock()

	snapshot.mu.Lock()
	cleanupErr := cleanupSnapshot(snapshot)
	snapshot.mu.Unlock()
	manager.finishSnapshotCleanup(token, snapshot, cleanupErr)
	if cleanupErr != nil {
		return false, cleanupErr
	}
	return true, nil
}

func (manager *Manager) Optimize(ctx context.Context, databaseID string) (OptimizeResult, error) {
	definition, lock, err := manager.resolve(databaseID)
	if err != nil {
		return OptimizeResult{}, err
	}
	if !definition.Mutable || !definition.Optimizable {
		return OptimizeResult{}, errors.New("this system SQLite database is read-only")
	}
	lock.Lock()
	defer lock.Unlock()

	if err := manager.mutableOperations.Optimize(ctx, definition); err != nil {
		return OptimizeResult{}, publicDatabaseError(err)
	}
	return OptimizeResult{
		DatabaseID:  databaseID,
		OptimizedAt: manager.now().UTC(),
	}, nil
}

func (manager *Manager) Close() error {
	var cleanupErrors []error
	manager.closeOnce.Do(func() {
		close(manager.stop)
		<-manager.done

		manager.mu.Lock()
		manager.closed = true
		type cleanupTask struct {
			token    string
			snapshot *snapshotEntry
		}
		snapshots := make([]cleanupTask, 0, len(manager.snapshots))
		for token, snapshot := range manager.snapshots {
			if snapshot.releasing {
				continue
			}
			snapshot.releasing = true
			snapshots = append(snapshots, cleanupTask{token: token, snapshot: snapshot})
		}
		manager.mu.Unlock()
		for _, task := range snapshots {
			task.snapshot.mu.Lock()
			cleanupErr := cleanupSnapshot(task.snapshot)
			task.snapshot.mu.Unlock()
			manager.finishSnapshotCleanup(task.token, task.snapshot, cleanupErr)
			if cleanupErr != nil {
				cleanupErrors = append(cleanupErrors, cleanupErr)
			}
		}
	})
	return errors.Join(cleanupErrors...)
}

func (manager *Manager) resolve(databaseID string) (Definition, *sync.RWMutex, error) {
	if err := manager.ensureOpen(); err != nil {
		return Definition{}, nil, err
	}
	id := strings.TrimSpace(databaseID)
	definition, exists := manager.definitions[id]
	if !exists || id != databaseID {
		return Definition{}, nil, errors.New("unknown system SQLite database")
	}
	return definition, manager.databaseMu[id], nil
}

func (manager *Manager) ensureOpen() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return errors.New("system SQLite manager is closed")
	}
	return nil
}

func (manager *Manager) claimSnapshotSlot() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return errors.New("system SQLite manager is closed")
	}
	if manager.snapshotSlotHeld {
		return errors.New("another system SQLite snapshot is already active")
	}
	manager.snapshotSlotHeld = true
	return nil
}

func (manager *Manager) releaseSnapshotSlot() {
	manager.mu.Lock()
	manager.snapshotSlotHeld = false
	manager.mu.Unlock()
}

func (manager *Manager) ensureSnapshotCapacity() error {
	available, err := manager.availableBytes(manager.snapshotRoot)
	if err != nil || available < 0 {
		return errors.New("private snapshot storage capacity could not be verified")
	}
	required, ok := snapshotRequiredBytes(1, SnapshotLimits{
		MaxBytes: manager.maxSnapshotBytes, FreeSpaceFloor: manager.freeSpaceFloor,
	})
	if !ok {
		return errors.New("private snapshot storage capacity limits are invalid")
	}
	if available < required {
		return errors.New("private snapshot storage does not have enough free space")
	}
	return nil
}

func (manager *Manager) ensureSnapshotStorage() error {
	manager.snapshotMu.Lock()
	defer manager.snapshotMu.Unlock()
	if err := prepareSnapshotRoot(manager.snapshotRoot); err != nil {
		return err
	}
	if manager.snapshotReady {
		return nil
	}
	if err := cleanupOrphanedSnapshotDirectories(manager.snapshotRoot); err != nil {
		return err
	}
	manager.snapshotReady = true
	return nil
}

func (manager *Manager) registerSnapshot(snapshot *snapshotEntry) (string, error) {
	for attempt := 0; attempt < 128; attempt++ {
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			return "", errors.New("could not generate an opaque snapshot token")
		}
		token := hex.EncodeToString(randomBytes)
		manager.mu.Lock()
		if manager.closed {
			manager.mu.Unlock()
			return "", errors.New("system SQLite manager is closed")
		}
		if _, exists := manager.snapshots[token]; !exists {
			snapshot.info.Token = token
			manager.snapshots[token] = snapshot
			manager.mu.Unlock()
			return token, nil
		}
		manager.mu.Unlock()
	}
	return "", errors.New("could not allocate a unique snapshot token")
}

func (manager *Manager) lockSnapshot(token string) (*snapshotEntry, error) {
	if !validSnapshotToken(token) {
		return nil, errors.New("snapshot not found or expired")
	}
	now := manager.now().UTC()
	manager.mu.Lock()
	snapshot := manager.snapshots[token]
	if snapshot == nil || snapshot.releasing || snapshot.cleanupFailed {
		manager.mu.Unlock()
		return nil, errors.New("snapshot not found or expired")
	}
	if !now.Before(snapshot.info.ExpiresAt) {
		snapshot.releasing = true
		manager.mu.Unlock()
		snapshot.mu.Lock()
		cleanupErr := cleanupSnapshot(snapshot)
		snapshot.mu.Unlock()
		manager.finishSnapshotCleanup(token, snapshot, cleanupErr)
		return nil, errors.New("snapshot not found or expired")
	}
	snapshot.mu.Lock()
	snapshot.info.ExpiresAt = now.Add(manager.snapshotTTL)
	manager.mu.Unlock()
	return snapshot, nil
}

func (manager *Manager) cleanupLoop() {
	ticker := time.NewTicker(manager.cleanupInterval)
	defer ticker.Stop()
	defer close(manager.done)
	for {
		select {
		case <-ticker.C:
			manager.cleanupExpired()
		case <-manager.stop:
			return
		}
	}
}

func (manager *Manager) cleanupExpired() {
	now := manager.now().UTC()
	type cleanupTask struct {
		token    string
		snapshot *snapshotEntry
	}
	manager.mu.Lock()
	expired := make([]cleanupTask, 0)
	for token, snapshot := range manager.snapshots {
		if !snapshot.releasing &&
			(snapshot.cleanupFailed || !now.Before(snapshot.info.ExpiresAt)) {
			snapshot.releasing = true
			expired = append(expired, cleanupTask{token: token, snapshot: snapshot})
		}
	}
	manager.mu.Unlock()
	for _, task := range expired {
		task.snapshot.mu.Lock()
		cleanupErr := cleanupSnapshot(task.snapshot)
		task.snapshot.mu.Unlock()
		manager.finishSnapshotCleanup(task.token, task.snapshot, cleanupErr)
	}
}

func (manager *Manager) finishSnapshotCleanup(
	token string,
	snapshot *snapshotEntry,
	cleanupErr error,
) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.snapshots[token] != snapshot {
		return
	}
	if cleanupErr != nil {
		snapshot.releasing = false
		snapshot.cleanupFailed = true
		return
	}
	delete(manager.snapshots, token)
	manager.snapshotSlotHeld = false
}

func cleanupSnapshot(snapshot *snapshotEntry) error {
	if snapshot == nil {
		return nil
	}
	if snapshot.source != nil {
		snapshot.source.close()
		snapshot.source = nil
	}
	if snapshot.directory == "" {
		return nil
	}
	if err := removePrivateSnapshotDirectory(snapshot.root, snapshot.directory); err != nil {
		return err
	}
	snapshot.directory = ""
	return nil
}

func cleanupOrphanedSnapshotDirectories(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return errors.New("could not inspect private snapshot storage")
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, snapshotDirectoryPrefix) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return errors.New("private snapshot storage contains an unexpected entry")
		}
		if err := removePrivateSnapshotDirectory(root, filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return nil
}

func removePrivateSnapshotDirectory(root, directory string) error {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	cleanDirectory := filepath.Clean(strings.TrimSpace(directory))
	if !filepath.IsAbs(cleanRoot) || !filepath.IsAbs(cleanDirectory) || cleanDirectory == cleanRoot {
		return errors.New("private snapshot storage cleanup was refused")
	}
	relative, err := filepath.Rel(cleanRoot, cleanDirectory)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.Dir(relative) != "." ||
		!strings.HasPrefix(filepath.Base(relative), snapshotDirectoryPrefix) {
		return errors.New("private snapshot storage cleanup was refused")
	}
	info, err := os.Lstat(cleanDirectory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private snapshot storage cleanup was refused")
	}
	if err := os.RemoveAll(cleanDirectory); err != nil {
		return errors.New("could not remove private snapshot storage")
	}
	return nil
}

func inspectDefinition(
	ctx context.Context,
	definition Definition,
	mutableOperations MutableOperations,
) DatabaseInfo {
	result := DatabaseInfo{
		ID:            definition.ID,
		Name:          definition.Name,
		Purpose:       definition.Purpose,
		Kind:          definition.Kind,
		Mutable:       definition.Mutable,
		PathHint:      definition.PathHint,
		Status:        "missing",
		StatusMessage: "Database file is not present on this server.",
		Actions:       []string{},
	}
	source, err := openManagedSource(definition.Path, false)
	if err != nil {
		if os.IsNotExist(err) {
			return result
		}
		result.Status = "unsafe"
		result.StatusMessage = publicDatabaseError(err).Error()
		return result
	}
	defer source.close()
	result.Available = true
	result.Status = "ready"
	result.StatusMessage = "Database is available."
	result.SizeBytes = source.info.Size()
	modifiedAt := source.info.ModTime().UTC()
	result.ModifiedAt = &modifiedAt
	if definition.Mutable {
		inspection, err := mutableOperations.Inspect(ctx, definition)
		if err != nil {
			result.Status = "error"
			result.StatusMessage = publicDatabaseError(err).Error()
			return result
		}
		result.JournalMode = inspection.JournalMode
		result.UserVersion = inspection.UserVersion
		result.Actions = databaseActions(definition)
		return result
	}

	database, err := openSQLite(ctx, source.databasePath(), "ro")
	if err != nil {
		result.Status = "error"
		result.StatusMessage = publicDatabaseError(err).Error()
		return result
	}
	defer database.Close()
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&result.JournalMode); err != nil {
		result.Status = "error"
		result.StatusMessage = publicDatabaseError(err).Error()
		return result
	}
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.UserVersion); err != nil {
		result.Status = "error"
		result.StatusMessage = publicDatabaseError(err).Error()
		return result
	}
	result.Actions = databaseActions(definition)
	return result
}

func databaseActions(definition Definition) []string {
	actions := []string{"check"}
	if definition.SnapshotAllowed {
		actions = append(actions, "snapshot")
	}
	if definition.Optimizable {
		actions = append(actions, "optimize")
	}
	return actions
}

func openSQLite(ctx context.Context, path, mode string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", sqliteURI(path, mode))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func sqliteURI(path, mode string) string {
	uri := &url.URL{Scheme: "file", Opaque: filepath.ToSlash(path)}
	query := uri.Query()
	query.Set("mode", mode)
	query.Add("_pragma", "busy_timeout(5000)")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func validSnapshotToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(token)
	return err == nil && len(decoded) == 32 && token == strings.ToLower(token)
}

func publicDatabaseError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case os.IsNotExist(err):
		return errors.New("managed database is not available")
	case os.IsPermission(err):
		return errors.New("managed database is not accessible")
	case strings.Contains(err.Error(), "symbolic link"):
		return errors.New("managed database path is unsafe")
	case strings.Contains(err.Error(), "hard links"):
		return errors.New("managed database has hard links")
	case strings.Contains(err.Error(), "not a regular file"):
		return errors.New("managed database is not a regular file")
	case strings.Contains(err.Error(), "identity changed"):
		return errors.New("managed database identity changed")
	default:
		return errors.New("managed database operation failed")
	}
}

func boundedMessage(message string) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\x00", ""))
	if len(message) <= maxIntegrityMessage {
		return message
	}
	return message[:maxIntegrityMessage] + "..."
}

func knownDatabaseID(id string) bool {
	switch id {
	case DatabasePanel, DatabasePowerDNS, DatabaseRoundcube, DatabaseComponentCatalog:
		return true
	default:
		return false
	}
}

func databaseOrder(id string) int {
	switch id {
	case DatabasePanel:
		return 0
	case DatabasePowerDNS:
		return 1
	case DatabaseRoundcube:
		return 2
	case DatabaseComponentCatalog:
		return 3
	default:
		return 100
	}
}
