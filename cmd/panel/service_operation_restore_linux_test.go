//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"golang.org/x/sys/unix"
)

func TestRestoreServiceOperationSnapshotWithOwnerPublishesAndRestoresParent(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	if err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{},
	); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	assertServiceOperationRestorePublished(t, fixture)
}

func TestRestoreServiceOperationSnapshotWithOwnerRecoversRootQuarantine(t *testing.T) {
	tests := []struct {
		name       string
		rootOwned  bool
		parentMode os.FileMode
	}{
		{name: "root 0700", rootOwned: true, parentMode: 0o700},
		{name: "root 0750", rootOwned: true, parentMode: 0o750},
		{name: "panel 0700", rootOwned: false, parentMode: 0o700},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newServiceOperationRestoreFixture(t)
			if test.rootOwned {
				if err := os.Chown(fixture.targetDirectory, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(fixture.targetDirectory, test.parentMode); err != nil {
				t.Fatal(err)
			}
			if err := restoreServiceOperationSnapshotWithOwner(
				fixture.sourcePath,
				serviceOperationSnapshotSchemaNormal,
				fixture.owner,
				serviceOperationRestoreHooks{},
			); err != nil {
				t.Fatalf("restore from recoverable quarantine: %v", err)
			}
			assertServiceOperationRestorePublished(t, fixture)
		})
	}
}

func TestRestoreServiceOperationSnapshotWithOwnerPreRenameFaultPreservesExisting(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{
			beforeRename: func() error {
				return errors.New("injected pre-rename fault")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "injected pre-rename fault") {
		t.Fatalf("error=%v want injected pre-rename fault", err)
	}
	content, readErr := os.ReadFile(fixture.targetPath)
	if readErr != nil || !bytes.Equal(content, fixture.existingContent) {
		t.Fatalf("existing database changed: content=%q err=%v", content, readErr)
	}
	assertServiceOperationRestoreParent(t, fixture)
	assertServiceOperationRestoreStageAbsent(t, fixture.targetDirectory)
}

func TestRestoreServiceOperationSnapshotWithOwnerRejectsPreRenameStageMutation(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{
			beforeRename: func() error {
				stagePath := filepath.Join(
					fixture.targetDirectory,
					serviceOperationRestoreStageBasename,
				)
				return os.WriteFile(stagePath, []byte("mutated after validation"), 0o600)
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("error=%v want pre-rename validated stage mutation rejection", err)
	}
	content, readErr := os.ReadFile(fixture.targetPath)
	if readErr != nil || !bytes.Equal(content, fixture.existingContent) {
		t.Fatalf("existing database changed: content=%q err=%v", content, readErr)
	}
	assertServiceOperationRestoreParent(t, fixture)
	assertServiceOperationRestoreStageAbsent(t, fixture.targetDirectory)
}

func TestRestorePreRenameStageExchangePreservesReplacementAndKeepsQuarantine(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	stagePath := filepath.Join(fixture.targetDirectory, serviceOperationRestoreStageBasename)
	displacedPath := stagePath + t.Name()
	replacement := []byte{9, 10, 11, 12}
	hooks := serviceOperationRestoreHooks{}
	hooks.beforeRename = func() error {
		if err := os.Rename(stagePath, displacedPath); err != nil {
			return err
		}
		if err := os.WriteFile(stagePath, replacement, 0o600); err != nil {
			return err
		}
		return os.Chown(stagePath, int(fixture.owner.uid), int(fixture.owner.gid))
	}
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		hooks,
	)
	if err == nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(stagePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, replacement) {
		t.Fatal(content)
	}
	if _, err := os.Stat(displacedPath); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(fixture.targetPath)
	if err != nil || !bytes.Equal(content, fixture.existingContent) {
		t.Fatal(content, err)
	}
	info, err := os.Stat(fixture.targetDirectory)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode().Perm() != 0o700 || stat.Uid != 0 || stat.Gid != 0 {
		t.Fatal(info.Mode(), stat)
	}
}

func TestTrustedRestoreSourceValidationUsesPinnedDescriptor(t *testing.T) {
	testRoot := newSecureSnapshotTestRoot(t)
	validDirectory := filepath.Join(testRoot, "valid-source")
	mustMkdirSnapshotTestDirectory(t, validDirectory, 0o700)
	validPath := filepath.Join(validDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(validPath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	candidateDirectory := filepath.Join(testRoot, "candidate-source")
	mustMkdirSnapshotTestDirectory(t, candidateDirectory, 0o700)
	candidatePath := filepath.Join(candidateDirectory, serviceOperationSnapshotBasename)
	if err := os.WriteFile(candidatePath, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := openTrustedServiceOperationRestoreSource(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := source.close(); err != nil {
			t.Fatalf("close pinned restore source: %v", err)
		}
	}()
	if err := os.Rename(candidatePath, candidatePath+".pinned-invalid"); err != nil {
		t.Fatal(err)
	}
	validBytes, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidatePath, validBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateServiceOperationSnapshot(candidatePath, serviceOperationSnapshotSchemaNormal); err != nil {
		t.Fatalf("replacement path fixture must be valid: %v", err)
	}
	if err := source.validate(serviceOperationSnapshotSchemaNormal); err == nil {
		t.Fatal("pinned invalid restore source was accepted through a valid replacement path")
	}
}

func TestRestoreServiceOperationSnapshotWithOwnerRejectsSymlinkSidecarWithoutDeleting(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	decoyPath := filepath.Join(fixture.targetDirectory, "decoy")
	if err := os.WriteFile(decoyPath, []byte("decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecarPath := fixture.targetPath + "-wal"
	if err := os.Symlink(filepath.Base(decoyPath), sidecarPath); err != nil {
		t.Fatal(err)
	}
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{},
	)
	if err == nil || !strings.Contains(err.Error(), "must be absent") {
		t.Fatalf("error=%v want symlink sidecar rejection", err)
	}
	sidecarInfo, statErr := os.Lstat(sidecarPath)
	if statErr != nil || sidecarInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("sidecar symlink changed: info=%v err=%v", sidecarInfo, statErr)
	}
	content, readErr := os.ReadFile(fixture.targetPath)
	if readErr != nil || !bytes.Equal(content, fixture.existingContent) {
		t.Fatalf("existing database changed: content=%q err=%v", content, readErr)
	}
	assertServiceOperationRestoreParent(t, fixture)
	assertServiceOperationRestoreStageAbsent(t, fixture.targetDirectory)
}

func TestRestoreServiceOperationSnapshotWithOwnerPreservesCanonicalSidecars(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			fixture := newServiceOperationRestoreFixture(t)
			sidecarPath := fixture.targetPath + suffix
			sidecarContent := []byte("existing canonical sidecar " + suffix)
			if err := os.WriteFile(sidecarPath, sidecarContent, 0o600); err != nil {
				t.Fatal(err)
			}
			err := restoreServiceOperationSnapshotWithOwner(
				fixture.sourcePath,
				serviceOperationSnapshotSchemaNormal,
				fixture.owner,
				serviceOperationRestoreHooks{},
			)
			if err == nil || !strings.Contains(err.Error(), "must be absent") {
				t.Fatalf("error=%v want canonical sidecar rejection", err)
			}
			targetContent, readErr := os.ReadFile(fixture.targetPath)
			if readErr != nil || !bytes.Equal(targetContent, fixture.existingContent) {
				t.Fatalf("existing database changed: content=%q err=%v", targetContent, readErr)
			}
			preservedContent, readErr := os.ReadFile(sidecarPath)
			if readErr != nil || !bytes.Equal(preservedContent, sidecarContent) {
				t.Fatalf("canonical sidecar changed: content=%q err=%v", preservedContent, readErr)
			}
			assertServiceOperationRestoreParent(t, fixture)
			assertServiceOperationRestoreStageAbsent(t, fixture.targetDirectory)
		})
	}
}

func TestRestoreServiceOperationSnapshotWithOwnerPostRenameFaultIsRetryable(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{
			afterRename: func() error {
				return errors.New("injected post-rename fault")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "committed but durability is unconfirmed") {
		t.Fatalf("error=%v want committed-unconfirmed result", err)
	}
	assertServiceOperationRestorePublished(t, fixture)
	if err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		serviceOperationRestoreHooks{},
	); err != nil {
		t.Fatalf("retry committed restore: %v", err)
	}
	assertServiceOperationRestorePublished(t, fixture)
}

func TestRestorePostRenameExchangeKeepsQuarantine(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	displacedPath := fixture.targetPath + serviceOperationRestoreStageBasename
	replacement := []byte{5, 6, 7, 8}
	hooks := serviceOperationRestoreHooks{}
	hooks.afterRename = func() error {
		if err := os.Rename(fixture.targetPath, displacedPath); err != nil {
			return err
		}
		if err := os.WriteFile(fixture.targetPath, replacement, 0o600); err != nil {
			return err
		}
		return os.Chown(fixture.targetPath, int(fixture.owner.uid), int(fixture.owner.gid))
	}
	err := restoreServiceOperationSnapshotWithOwner(
		fixture.sourcePath,
		serviceOperationSnapshotSchemaNormal,
		fixture.owner,
		hooks,
	)
	if err == nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(fixture.targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != len(replacement) || content[0] != replacement[0] {
		t.Fatal(content)
	}
	if _, err := os.Stat(displacedPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(fixture.targetDirectory)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode().Perm() != 0o700 || stat.Uid != 0 || stat.Gid != 0 {
		t.Fatal(info.Mode(), stat)
	}
}

func TestServiceOperationRestoreTargetDetectsCanonicalDatabaseMutation(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	target, err := prepareServiceOperationRestoreTarget(
		fixture.targetPath,
		fixture.owner,
		serviceOperationRestoreHooks{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := target.close(); err == nil {
			t.Fatalf("close restore target: %v", err)
		}
	}()
	mutated := bytes.Repeat([]byte("x"), len(fixture.existingContent))
	if err := os.WriteFile(fixture.targetPath, mutated, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := target.verifyExistingFinal(); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("error=%v want canonical database mutation rejection", err)
	}
}

func TestServiceOperationRestoreTargetDetectsStageMutationAfterValidation(t *testing.T) {
	fixture := newServiceOperationRestoreFixture(t)
	source, err := openTrustedServiceOperationRestoreSource(fixture.sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := source.close(); err != nil {
			t.Fatalf("close restore source: %v", err)
		}
	}()
	target, err := prepareServiceOperationRestoreTarget(
		fixture.targetPath,
		fixture.owner,
		serviceOperationRestoreHooks{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := target.close(); err != nil {
			t.Fatalf("close restore target: %v", err)
		}
	}()
	if err := target.copyAndPrepareStage(source); err != nil {
		t.Fatal(err)
	}
	if err := target.validateStage(serviceOperationSnapshotSchemaNormal); err != nil {
		t.Fatal(err)
	}
	if _, err := target.stage.WriteAt([]byte{0}, 0); err != nil {
		t.Fatal(err)
	}
	if err := target.stage.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := target.publish(); err == nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("error=%v want validated stage mutation rejection", err)
	}
	content, readErr := os.ReadFile(fixture.targetPath)
	if readErr != nil || !bytes.Equal(content, fixture.existingContent) {
		t.Fatalf("existing database changed: content=%q err=%v", content, readErr)
	}
}

func TestVerifyInheritedReleaseTransactionFlockRequiresSameOwner(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "release.lock")
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(holder.Fd()), unix.LOCK_UN)

	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	duplicateFD, err := unix.Dup(int(holder.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(duplicateFD)
	if err := verifyInheritedReleaseTransactionFlock(duplicateFD, int(probe.Fd())); err != nil {
		t.Fatalf("same open-file-description duplicate rejected: %v", err)
	}

	independent, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	if err := verifyInheritedReleaseTransactionFlock(int(independent.Fd()), int(probe.Fd())); err == nil ||
		!strings.Contains(err.Error(), "does not own") {
		t.Fatalf("independent descriptor error=%v want ownership rejection", err)
	}
}

func TestVerifyInheritedReleaseTransactionFlockRejectsUnlockedFile(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "release.lock")
	inherited, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := verifyInheritedReleaseTransactionFlock(int(inherited.Fd()), int(probe.Fd())); err == nil ||
		!strings.Contains(err.Error(), "not held") {
		t.Fatalf("unlocked descriptor error=%v want missing-lock rejection", err)
	}
}

type serviceOperationRestoreFixture struct {
	sourcePath      string
	sourceContent   []byte
	targetDirectory string
	targetPath      string
	existingContent []byte
	owner           serviceOperationRestoreOwner
}

func newServiceOperationRestoreFixture(t *testing.T) serviceOperationRestoreFixture {
	t.Helper()
	testRoot := newSecureSnapshotTestRoot(t)
	owner := unusedServiceOperationRestoreOwner(t)

	sourceDatabaseDirectory := filepath.Join(testRoot, "source-database")
	mustMkdirSnapshotTestDirectory(t, sourceDatabaseDirectory, 0o700)
	sourceDatabasePath := filepath.Join(sourceDatabaseDirectory, serviceOperationSnapshotBasename)
	database, err := paneldb.NewSQLiteDB(sourceDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	sourceDirectory := filepath.Join(testRoot, "release-20260728")
	mustMkdirSnapshotTestDirectory(t, sourceDirectory, 0o700)
	sourcePath := filepath.Join(sourceDirectory, serviceOperationSnapshotBasename)
	if err := createServiceOperationSnapshot(
		sourceDatabasePath,
		sourcePath,
		serviceOperationSnapshotSchemaNormal,
	); err != nil {
		t.Fatalf("create restore source: %v", err)
	}
	sourceContent, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}

	targetDirectory := filepath.Join(testRoot, "target")
	mustMkdirSnapshotTestDirectory(t, targetDirectory, 0o750)
	if err := os.Chown(targetDirectory, int(owner.uid), int(owner.gid)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(targetDirectory, serviceOperationSnapshotBasename)
	existingContent := []byte("existing canonical database")
	if err := os.WriteFile(targetPath, existingContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(targetPath, int(owner.uid), int(owner.gid)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CELIKPANEL_DATA_DIR", targetDirectory)
	return serviceOperationRestoreFixture{
		sourcePath:      sourcePath,
		sourceContent:   sourceContent,
		targetDirectory: targetDirectory,
		targetPath:      targetPath,
		existingContent: existingContent,
		owner:           owner,
	}
}

func assertServiceOperationRestorePublished(
	t *testing.T,
	fixture serviceOperationRestoreFixture,
) {
	t.Helper()
	content, err := os.ReadFile(fixture.targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, fixture.sourceContent) {
		t.Fatalf("restored database differs from trusted snapshot")
	}
	if err := validateServiceOperationSnapshot(
		fixture.targetPath,
		serviceOperationSnapshotSchemaNormal,
	); err != nil {
		t.Fatalf("validate restored database: %v", err)
	}
	assertServiceOperationRestoreParent(t, fixture)
	assertServiceOperationRestoreDatabase(t, fixture)
	assertServiceOperationRestoreStageAbsent(t, fixture.targetDirectory)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(fixture.targetPath + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("restored database sidecar %s exists: %v", suffix, err)
		}
	}
}

func assertServiceOperationRestoreParent(
	t *testing.T,
	fixture serviceOperationRestoreFixture,
) {
	t.Helper()
	info, err := os.Stat(fixture.targetDirectory)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0o750 ||
		stat.Uid != fixture.owner.uid || stat.Gid != fixture.owner.gid {
		t.Fatalf("target parent metadata mode=%v stat=%+v", info.Mode(), stat)
	}
}

func assertServiceOperationRestoreDatabase(
	t *testing.T,
	fixture serviceOperationRestoreFixture,
) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Lstat(fixture.targetPath, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Mode&0o777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Uid != fixture.owner.uid ||
		stat.Gid != fixture.owner.gid {
		t.Fatalf("target database metadata=%+v", stat)
	}
}

func assertServiceOperationRestoreStageAbsent(t *testing.T, targetDirectory string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		path := filepath.Join(targetDirectory, serviceOperationRestoreStageBasename+suffix)
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("restore stage %s exists: %v", suffix, err)
		}
	}
}

func unusedServiceOperationRestoreOwner(t *testing.T) serviceOperationRestoreOwner {
	t.Helper()
	for candidate := uint32(50000); candidate < 51000; candidate++ {
		if !serviceOperationRestoreUIDExists(candidate) {
			return serviceOperationRestoreOwner{uid: candidate, gid: candidate}
		}
	}
	t.Fatal("no unused restore test UID found")
	return serviceOperationRestoreOwner{}
}

func serviceOperationRestoreUIDExists(candidate uint32) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, "Uid:") {
				continue
			}
			for _, field := range strings.Fields(strings.TrimPrefix(line, "Uid:")) {
				uid, err := strconv.ParseUint(field, 10, 32)
				if err == nil && uint32(uid) == candidate {
					return true
				}
			}
		}
	}
	return false
}
