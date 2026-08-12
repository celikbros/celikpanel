//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"golang.org/x/sys/unix"
)

const systemUpdateStateMaxSize = 16 * 1024

const systemUpdateTestPathPrefix = "/tmp/celikpanel-system-update-test-"

var systemUpdateStageRE = regexp.MustCompile(`^\.state-[0-9a-f]{32}-[0-9]+$`)

type systemUpdateUnitState uint8

const (
	systemUpdateUnitAmbiguous systemUpdateUnitState = iota
	systemUpdateUnitActive
	systemUpdateUnitInactive
)

type linuxSystemUpdateBackend struct {
	mu             sync.Mutex
	stateRoot      string
	floorPath      string
	checkIdle      func() error
	acquireIdle    func() (systemUpdateIdleLease, error)
	launch         func(context.Context, string) error
	unitState      func(context.Context, string) (systemUpdateUnitState, error)
	installedBuild func(context.Context) (string, string, error)
	runInstaller   func(context.Context, *systemUpdateState, string) error
	now            func() time.Time
	writeFault     func(string) error
}

type systemUpdateIdleLease interface{ Close() error }

type systemUpdateStateLock struct{ file *os.File }

func acquireSystemUpdateStateLock(ctx context.Context, root string) (*systemUpdateStateLock, error) {
	lock, err := acquireSystemUpdateNamedLock(ctx, root, ".lock")
	if err != nil {
		return nil, err
	}
	if err := recoverSystemUpdateStagingFiles(root); err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func acquireSystemUpdateNamedLock(ctx context.Context, root, name string) (*systemUpdateStateLock, error) {
	if name != ".lock" && name != ".worker.lock" {
		return nil, errors.New("invalid system update lock name")
	}
	if err := ensureSystemUpdateRoot(root); err != nil {
		return nil, err
	}
	path := filepath.Join(root, name)
	created := false
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err == nil {
		created = true
	} else if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("open system update lock handle")
	}
	keep := false
	defer func() {
		if !keep {
			file.Close()
		}
	}()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Nlink != 1 || stat.Size != 0 {
		return nil, errors.New("system update lock has unsafe metadata")
	}
	targetUID, targetGID := systemUpdateExpectedOwner(root)
	normalize := created
	if created {
		if stat.Uid != uint32(os.Geteuid()) || stat.Gid != uint32(os.Getegid()) ||
			(!systemUpdateIsTestPath(root) && os.Geteuid() != 0) {
			return nil, errors.New("new system update lock has an unsafe creator")
		}
	} else if stat.Uid != targetUID || stat.Gid != targetGID || stat.Mode&0o7777 != 0o600 {
		// A root process can crash between O_EXCL creation and Fchown(0, 0).
		// The trusted 0700 parent excludes the service group; normalize only
		// that exact creator-owned, empty, 0600 bootstrap artifact.
		if systemUpdateIsTestPath(root) || stat.Uid != 0 || stat.Gid != uint32(os.Getegid()) || stat.Mode&0o7777 != 0o600 || os.Geteuid() != 0 {
			return nil, errors.New("system update lock has unsafe ownership or permissions")
		}
		normalize = true
	}
	if normalize {
		if stat.Uid != targetUID || stat.Gid != targetGID {
			if err := unix.Fchown(fd, int(targetUID), int(targetGID)); err != nil {
				return nil, err
			}
		}
		if err := unix.Fchmod(fd, 0o600); err != nil {
			return nil, err
		}
		if err := file.Sync(); err != nil {
			return nil, err
		}
		if err := syncSystemUpdateDirectory(root); err != nil {
			return nil, err
		}
	}
	for {
		if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err == nil {
			keep = true
			return &systemUpdateStateLock{file: file}, nil
		} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (lock *systemUpdateStateLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	fd := int(lock.file.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func newLinuxSystemUpdateBackend() *linuxSystemUpdateBackend {
	backend := &linuxSystemUpdateBackend{
		stateRoot: systemUpdateStateRoot, floorPath: systemUpdateFloorPath,
		checkIdle:   func() error { return checkServiceMutationIdle("", "") },
		acquireIdle: func() (systemUpdateIdleLease, error) { return acquireSystemUpdateServiceMutationIdleLock() },
		now:         func() time.Time { return time.Now().UTC() },
	}
	backend.launch = backend.launchWorker
	backend.unitState = backend.probeWorkerUnit
	backend.installedBuild = inspectInstalledSystemUpdateBuild
	backend.runInstaller = runSystemUpdateInstaller
	return backend
}

// acquireSystemUpdateServiceMutationIdleLock closes the check-then-launch
// race: it owns the same cross-process flock as every service mutation while
// validating the canonical ledger and package-manager state. The caller keeps
// the returned lock until systemd has accepted the transient worker.
func acquireSystemUpdateServiceMutationIdleLock() (*serviceMutationFileLock, error) {
	stateDir := filepath.Clean(serviceMutationStateDirectory())
	lockPath := filepath.Clean(serviceMutationLockFile())
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return nil, errors.New("service mutation paths are not absolute")
	}
	lock, err := acquireServiceMutationFileLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire global service mutation lock: %w", err)
	}
	fail := func(cause error) (*serviceMutationFileLock, error) {
		return nil, errors.Join(cause, lock.Close())
	}
	info, err := os.Lstat(stateDir)
	if err != nil {
		return fail(fmt.Errorf("inspect service mutation state: %w", err))
	}
	if err := secureServiceMutationStateDirectoryStat(stateDir, info); err != nil {
		return fail(err)
	}
	raw, exists, err := readSecureServiceMutationLedger(filepath.Join(stateDir, serviceMutationLedgerFileName), serviceMutationLedgerMaxSize)
	if err != nil {
		return fail(err)
	}
	if !exists {
		return fail(errors.New("service mutation ledger is not initialized"))
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		return fail(err)
	}
	if ledger.ActiveRequestID != "" {
		return fail(errors.New("a privileged service mutation is active"))
	}
	for requestID, job := range ledger.Jobs {
		if job == nil || job.RequestID != requestID || serviceMutationStatusActive(job.Status) {
			return fail(errors.New("service mutation ledger is not idle"))
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return fail(fmt.Errorf("inspect package manager activity: %w", err))
	}
	if busy {
		return fail(errors.New("the host package manager is active"))
	}
	return lock, nil
}

func newPlatformSystemUpdateService() (*systemUpdateService, error) {
	platformOS, platformArch := runtimeSystemUpdatePlatform()
	backend := newLinuxSystemUpdateBackend()
	service := newSystemUpdateService(newHTTPSystemUpdateManifestFetcher(), backend, platformOS, platformArch)
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		service.unsupportedReason = "host package-manager policy could not be verified"
		return service, nil
	}
	switch profile.PackageManager {
	case hostplatform.PackageManagerAPT, hostplatform.PackageManagerPacman:
	case hostplatform.PackageManagerDNF:
		service.unsupportedReason = "system updates are not enabled for the dnf package-manager family"
	default:
		service.unsupportedReason = "system updates are not enabled for this package-manager family"
	}
	return service, nil
}

func systemUpdateStatePath(root, requestID string) string {
	return filepath.Join(root, requestID+".json")
}

func systemUpdateIsTestPath(path string) bool {
	clean := filepath.Clean(path)
	return strings.HasPrefix(clean, systemUpdateTestPathPrefix)
}

func systemUpdateExpectedOwner(path string) (uint32, uint32) {
	if systemUpdateIsTestPath(path) {
		return uint32(os.Geteuid()), uint32(os.Getegid())
	}
	return 0, 0
}

func recoverSystemUpdateStagingFiles(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	removed := false
	expectedUID, expectedGID := systemUpdateExpectedOwner(root)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".state-") {
			continue
		}
		if !systemUpdateStageRE.MatchString(name) {
			return fmt.Errorf("unexpected system update staging entry %s", name)
		}
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("system update staging entry %s has unsafe metadata", name)
		}
		safeOwner := systemUpdateStagingOwnerAllowed(
			systemUpdateIsTestPath(root),
			uint32(os.Geteuid()), uint32(os.Getegid()),
			expectedUID, expectedGID, stat.Uid, stat.Gid,
		)
		if !info.Mode().IsRegular() || stat.Mode&0o7777 != 0o600 ||
			!safeOwner || stat.Nlink != 1 ||
			stat.Size < 0 || stat.Size > systemUpdateStateMaxSize {
			return fmt.Errorf("system update staging entry %s has unsafe metadata", name)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed = true
	}
	if removed {
		return syncSystemUpdateDirectory(root)
	}
	return nil
}

func systemUpdateStagingOwnerAllowed(testPath bool, effectiveUID, effectiveGID, expectedUID, expectedGID, actualUID, actualGID uint32) bool {
	if actualUID == expectedUID && actualGID == expectedGID {
		return true
	}
	// Production os.CreateTemp runs as root with the service's effective group.
	// A crash before Chown(0, 0) can leave exactly that creator-owned artifact
	// inside the already trusted root:root 0700 state directory.
	return !testPath && effectiveUID == 0 && actualUID == 0 && actualGID == effectiveGID
}

func ensureSystemUpdateRoot(root string) error {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) || (clean != systemUpdateStateRoot && !systemUpdateIsTestPath(clean)) {
		return errors.New("system update state root is outside the trusted boundary")
	}
	parent := filepath.Dir(clean)
	if clean == systemUpdateStateRoot {
		exists, err := validateOrRecoverSystemUpdateReleaseRoot()
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("trusted release state root is unavailable")
		}
	}
	info, err := os.Lstat(clean)
	if os.IsNotExist(err) {
		if clean == systemUpdateStateRoot {
			if err := os.Mkdir(clean, 0o700); err != nil {
				return err
			}
			if err := normalizeCreatedSystemUpdateDirectory(clean); err != nil {
				return err
			}
			if err := syncSystemUpdateDirectory(parent); err != nil {
				return err
			}
			info, err = os.Lstat(clean)
		} else {
			if err := os.MkdirAll(clean, 0o700); err != nil {
				return err
			}
			info, err = os.Lstat(clean)
		}
	}
	if err != nil {
		return err
	}
	if clean == systemUpdateStateRoot {
		info, err = recoverSystemUpdateDirectoryCreatorArtifact(
			clean, info, uint32(os.Geteuid()), uint32(os.Getegid()),
		)
		if err != nil {
			return err
		}
	}
	return validateRootOwnedStateDirectory(clean, info)
}

func normalizeCreatedSystemUpdateDirectory(path string) error {
	return normalizeCreatedSystemUpdateDirectoryForOwner(path, uint32(os.Geteuid()), uint32(os.Getegid()))
}

func normalizeCreatedSystemUpdateDirectoryForOwner(path string, creatorUID, creatorGID uint32) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != creatorUID ||
		(stat.Gid != 0 && stat.Gid != creatorGID) || stat.Mode&0o7777 != 0o700 ||
		creatorUID != 0 || os.Geteuid() != 0 {
		return errors.New("new system update directory has an unsafe creator")
	}
	if stat.Gid != 0 {
		if err := unix.Fchown(fd, 0, 0); err != nil {
			return err
		}
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		return err
	}
	if err := unix.Fsync(fd); err != nil {
		return err
	}
	return nil
}

func recoverSystemUpdateDirectoryCreatorArtifact(path string, info os.FileInfo, creatorUID, creatorGID uint32) (os.FileInfo, error) {
	if info == nil {
		return nil, errors.New("system update directory metadata is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info, nil
	}
	exactMode := info.IsDir() && info.Mode().Perm() == 0o700 &&
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0
	if exactMode && creatorUID == 0 && os.Geteuid() == 0 && stat.Uid == 0 &&
		stat.Gid == creatorGID && stat.Gid != 0 {
		if err := normalizeCreatedSystemUpdateDirectoryForOwner(path, creatorUID, creatorGID); err != nil {
			return nil, err
		}
		return os.Lstat(path)
	}
	return info, nil
}

func validateOrRecoverSystemUpdateReleaseRoot() (bool, error) {
	if err := validateRootOwnedDirectoryChain(filepath.Dir(systemUpdateReleaseRoot)); err != nil {
		return false, err
	}
	info, err := os.Lstat(systemUpdateReleaseRoot)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err = recoverSystemUpdateDirectoryCreatorArtifact(
		systemUpdateReleaseRoot, info, uint32(os.Geteuid()), uint32(os.Getegid()),
	)
	if err != nil {
		return false, err
	}
	if err := validateRootOwnedDirectoryChain(systemUpdateReleaseRoot); err != nil {
		return false, err
	}
	if err := validateRootOwnedStateDirectory(systemUpdateReleaseRoot, info); err != nil {
		return false, err
	}
	return true, nil
}

func validateRootOwnedDirectoryChain(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return errors.New("trusted directory path is not absolute")
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("trusted directory path is invalid")
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		var stat unix.Stat_t
		if err := unix.Fstat(next, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o022 != 0 {
			unix.Close(next)
			if err != nil {
				return err
			}
			return fmt.Errorf("trusted directory %s has unsafe metadata", part)
		}
		unix.Close(fd)
		fd = next
	}
	return nil
}

func validateRootOwnedStateDirectory(path string, info os.FileInfo) error {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("%s must be a real 0700 directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	expectedUID, expectedGID := systemUpdateExpectedOwner(path)
	if !ok || stat.Uid != expectedUID || stat.Gid != expectedGID {
		return fmt.Errorf("%s must be root-owned", path)
	}
	return nil
}

func syncSystemUpdateDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(fd), path)
	if directory == nil {
		unix.Close(fd)
		return errors.New("open system update directory")
	}
	defer directory.Close()
	return directory.Sync()
}

func canonicalSystemUpdateState(state *systemUpdateState) ([]byte, error) {
	if err := validateSystemUpdateState(state); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeCanonicalSystemUpdateState(raw []byte) (*systemUpdateState, error) {
	var state systemUpdateState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("system update state has trailing data")
	}
	canonical, err := canonicalSystemUpdateState(&state)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(raw, canonical) {
		return nil, errors.New("system update state is not canonical")
	}
	return &state, nil
}

func readSystemUpdateState(root, requestID string) (*systemUpdateState, error) {
	if !systemUpdateRequestRE.MatchString(requestID) {
		return nil, errors.New("invalid system update request identity")
	}
	if err := ensureSystemUpdateRoot(root); err != nil {
		return nil, err
	}
	path := systemUpdateStatePath(root, requestID)
	raw, err := readRootOwnedSystemUpdateFile(path, systemUpdateStateMaxSize, false)
	if os.IsNotExist(err) {
		return nil, errSystemUpdateNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeCanonicalSystemUpdateState(raw)
}

func writeSystemUpdateState(root string, state *systemUpdateState, fault func(string) error) error {
	if err := ensureSystemUpdateRoot(root); err != nil {
		return err
	}
	raw, err := canonicalSystemUpdateState(state)
	if err != nil {
		return err
	}
	finalPath := systemUpdateStatePath(root, state.RequestID)
	stage, err := os.CreateTemp(root, ".state-"+state.RequestID+"-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer func() { stage.Close(); os.Remove(stagePath) }()
	expectedUID, expectedGID := systemUpdateExpectedOwner(root)
	if !systemUpdateIsTestPath(root) {
		if err := stage.Chown(int(expectedUID), int(expectedGID)); err != nil {
			return err
		}
	}
	if err := stage.Chmod(0o600); err != nil {
		return err
	}
	if _, err := stage.Write(raw); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if fault != nil {
		if err := fault("before_rename"); err != nil {
			return err
		}
	}
	if info, err := os.Lstat(finalPath); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Mode&0o7777 != 0o600 ||
			stat.Uid != expectedUID || stat.Gid != expectedGID || stat.Nlink != 1 {
			return errors.New("existing system update state has unsafe metadata")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		return err
	}
	if fault != nil {
		if err := fault("after_rename"); err != nil {
			return err
		}
	}
	if err := syncSystemUpdateDirectory(root); err != nil {
		return err
	}
	return nil
}

func (backend *linuxSystemUpdateBackend) ReadFloor() (*systemUpdateFloor, error) {
	if filepath.Clean(backend.floorPath) == systemUpdateFloorPath {
		exists, err := validateOrRecoverSystemUpdateReleaseRoot()
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, nil
		}
	}
	raw, err := readRootOwnedSystemUpdateFile(backend.floorPath, 1024, false)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	floor, err := parseCanonicalSystemUpdateFloor(raw)
	if err != nil {
		return nil, err
	}
	return &floor, nil
}

func stateIdentityMatches(left, right *systemUpdateState) bool {
	return left != nil && right != nil && left.RequestID == right.RequestID && left.TargetVersion == right.TargetVersion &&
		left.TargetCommit == right.TargetCommit && left.TargetSequence == right.TargetSequence && left.TargetOS == right.TargetOS &&
		left.TargetArch == right.TargetArch && left.TargetArchiveSHA256 == right.TargetArchiveSHA256 &&
		left.TargetArchiveSize == right.TargetArchiveSize && left.ExpectedCurrentVersion == right.ExpectedCurrentVersion &&
		left.ExpectedCurrentCommit == right.ExpectedCurrentCommit
}

func (backend *linuxSystemUpdateBackend) QueueAndLaunch(ctx context.Context, state *systemUpdateState) (*systemUpdateState, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	stateLock, err := acquireSystemUpdateStateLock(ctx, backend.stateRoot)
	if err != nil {
		return nil, err
	}
	defer stateLock.Close()
	if err := validateSystemUpdateState(state); err != nil {
		return nil, err
	}
	if err := ensureSystemUpdateRoot(backend.stateRoot); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(backend.stateRoot)
	if err != nil {
		return nil, err
	}
	var existingForRequest *systemUpdateState
	var active []*systemUpdateState
	for _, entry := range entries {
		name := entry.Name()
		if name == ".lock" || name == ".worker.lock" {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("unexpected system update state entry %s", name)
		}
		requestID := strings.TrimSuffix(name, ".json")
		existing, err := readSystemUpdateState(backend.stateRoot, requestID)
		if err != nil {
			return nil, err
		}
		if requestID == state.RequestID {
			if !stateIdentityMatches(existing, state) {
				return nil, errors.New("request_id belongs to another system update")
			}
			existingForRequest = existing
		}
		if existing.active() {
			active = append(active, existing)
		}
	}
	if len(active) > 1 {
		return nil, errors.New("multiple active system update requests are ambiguous")
	}
	if len(active) == 1 {
		reconciled, err := backend.reconcileStateLocked(ctx, active[0])
		if err != nil {
			return nil, err
		}
		if existingForRequest != nil && existingForRequest.RequestID == reconciled.RequestID {
			existingForRequest = reconciled
		}
		if reconciled.active() {
			if reconciled.RequestID == state.RequestID {
				return reconciled, nil
			}
			return reconciled, errors.New("another system update request is active")
		}
	}
	if existingForRequest != nil {
		return existingForRequest, errors.New("system update request_id was already consumed")
	}
	// The strict idle proof runs immediately before durable queue publication.
	// systemd-run starts only after that publication and while this process-local
	// gate excludes another updater RPC.
	var idleLock systemUpdateIdleLease
	if backend.acquireIdle != nil {
		idleLock, err = backend.acquireIdle()
		if err != nil {
			return nil, fmt.Errorf("global service mutation state is not idle: %w", err)
		}
		defer idleLock.Close()
	} else if backend.checkIdle == nil || backend.checkIdle() != nil {
		return nil, errors.New("global service mutation state is not idle")
	}
	if err := writeSystemUpdateState(backend.stateRoot, state, backend.writeFault); err != nil {
		return nil, err
	}
	if backend.launch == nil {
		return nil, backend.failStateLocked(state, errors.New("system-update worker launcher is unavailable"))
	}
	if err := backend.launch(ctx, state.RequestID); err != nil {
		return nil, backend.failStateLocked(state, fmt.Errorf("launch system-update worker: %w", err))
	}
	return state, nil
}

func (backend *linuxSystemUpdateBackend) failStateLocked(state *systemUpdateState, cause error) error {
	failed := *state
	failed.Status = systemUpdateFailed
	failed.Error = sanitizedSystemUpdateError(cause)
	failed.UpdatedAt = backend.now().UTC().Format(time.RFC3339Nano)
	if err := writeSystemUpdateState(backend.stateRoot, &failed, backend.writeFault); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (backend *linuxSystemUpdateBackend) Status(ctx context.Context, requestID string) (*systemUpdateState, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	stateLock, err := acquireSystemUpdateStateLock(ctx, backend.stateRoot)
	if err != nil {
		return nil, err
	}
	defer stateLock.Close()
	entries, err := os.ReadDir(backend.stateRoot)
	if err != nil {
		return nil, err
	}
	active := 0
	for _, entry := range entries {
		if entry.Name() == ".lock" || entry.Name() == ".worker.lock" {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unexpected system update state entry %s", entry.Name())
		}
		candidate, err := readSystemUpdateState(backend.stateRoot, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if candidate.active() {
			active++
		}
	}
	if active > 1 {
		return nil, errors.New("multiple active system update requests are ambiguous")
	}
	state, err := readSystemUpdateState(backend.stateRoot, requestID)
	if err != nil {
		return nil, err
	}
	if state.active() {
		state, err = backend.reconcileStateLocked(ctx, state)
	}
	return state, err
}

func (backend *linuxSystemUpdateBackend) Reconcile(ctx context.Context) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	exists, err := systemUpdateRootExistsForReconcile(backend.stateRoot)
	if err != nil || !exists {
		return err
	}
	stateLock, err := acquireSystemUpdateStateLock(ctx, backend.stateRoot)
	if err != nil {
		return err
	}
	defer stateLock.Close()
	if err := ensureSystemUpdateRoot(backend.stateRoot); err != nil {
		return err
	}
	entries, err := os.ReadDir(backend.stateRoot)
	if err != nil {
		return err
	}
	var active []*systemUpdateState
	for _, entry := range entries {
		if entry.Name() == ".lock" || entry.Name() == ".worker.lock" {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected system update state entry %s", entry.Name())
		}
		state, err := readSystemUpdateState(backend.stateRoot, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return err
		}
		if state.active() {
			active = append(active, state)
		}
	}
	if len(active) > 1 {
		return errors.New("multiple active system update requests are ambiguous")
	}
	if len(active) == 1 {
		_, err := backend.reconcileStateLocked(ctx, active[0])
		return err
	}
	return nil
}

// Startup reconciliation must not provision update trust/state on a host that
// has never enrolled in signed self-updates. A truly absent canonical parent
// or child is therefore an empty state, while any existing alias or unsafe
// metadata remains fatal.
func systemUpdateRootExistsForReconcile(root string) (bool, error) {
	clean := filepath.Clean(root)
	if clean != systemUpdateStateRoot {
		if !systemUpdateIsTestPath(clean) {
			return false, errors.New("system update state root is outside the trusted boundary")
		}
		info, err := os.Lstat(clean)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, validateRootOwnedStateDirectory(clean, info)
	}
	parentExists, err := validateOrRecoverSystemUpdateReleaseRoot()
	if err != nil || !parentExists {
		return false, err
	}
	info, err := os.Lstat(clean)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	info, err = recoverSystemUpdateDirectoryCreatorArtifact(
		clean, info, uint32(os.Geteuid()), uint32(os.Getegid()),
	)
	if err != nil {
		return false, err
	}
	return true, validateRootOwnedStateDirectory(clean, info)
}

func (backend *linuxSystemUpdateBackend) reconcileStateLocked(ctx context.Context, state *systemUpdateState) (*systemUpdateState, error) {
	if backend.unitState == nil {
		return state, nil
	}
	unit, err := backend.unitState(ctx, state.RequestID)
	if err != nil || unit == systemUpdateUnitAmbiguous {
		return state, nil
	}
	if unit == systemUpdateUnitActive {
		return state, nil
	}
	created, _ := time.Parse(time.RFC3339Nano, state.CreatedAt)
	if state.Status == systemUpdateQueued && backend.now().UTC().Sub(created) < systemUpdateLaunchGrace {
		return state, nil
	}
	version, commit, identityErr := backend.installedBuild(ctx)
	floor, floorErr := backend.ReadFloor()
	if identityErr == nil && floorErr == nil && version == state.TargetVersion && commit == state.TargetCommit && floor != nil && floor.Sequence == state.TargetSequence && floor.Version == state.TargetVersion {
		reconciled := *state
		reconciled.Status = systemUpdateSucceeded
		reconciled.Error = ""
		reconciled.UpdatedAt = backend.now().UTC().Format(time.RFC3339Nano)
		if err := writeSystemUpdateState(backend.stateRoot, &reconciled, backend.writeFault); err != nil {
			return nil, err
		}
		return &reconciled, nil
	}
	failed := *state
	failed.Status = systemUpdateFailed
	failed.Error = "system update worker stopped before proving the target build"
	failed.UpdatedAt = backend.now().UTC().Format(time.RFC3339Nano)
	if err := writeSystemUpdateState(backend.stateRoot, &failed, backend.writeFault); err != nil {
		return nil, err
	}
	return &failed, nil
}
