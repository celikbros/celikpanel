//go:build linux

package recoveryruntime

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const LauncherPath = "/usr/libexec/celikpanel/recovery"
const transactionPath = "/var/lib/celikpanel-release-transaction"

// VerifyPreflightBoundary proves that a compatibility check runs before any
// durable transaction has started, under the caller's native release lock.
// It does not create, clear, or repair transaction evidence.
func VerifyPreflightBoundary(fd int) error {
	if os.Geteuid() != 0 || fd != 9 {
		return fail(ReasonUnsafeMetadata)
	}
	return verifyEnrollmentLock(transactionPath, fd)
}

// Enroll installs the first compatible kit before coordinator quiescence. A
// selected predecessor is verified and retained; a later release cannot silently
// replace it. This operation neither starts an update nor creates its markers.
// Enrollment is available only under the already-held native transaction lock.
type enrollmentPaths struct{ runtimeRoot, selection, launcher, transaction string }

func Enroll(source string, fd int) error {
	return enrollAt(source, fd, enrollmentPaths{RuntimeRoot, SelectionPath, LauncherPath, transactionPath})
}
func enrollAt(source string, fd int, paths enrollmentPaths) error {
	return enrollWithCheckpoint(source, fd, paths, nil)
}

// The private checkpoint is available only to deterministic crash tests.
// Production passes nil; no environment or CLI argument can install a hook.
// Ozel kontrol noktasi yalniz belirli kesinti testleri icindir. Uretim nil verir;
// ortam veya CLI argumanlariyla hook eklenemez.
func enrollWithCheckpoint(source string, fd int, paths enrollmentPaths, afterLauncherPublished func()) error {
	resolveSelected := func() (*Runtime, error) {
		return resolveAt(resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0})
	}

	if os.Geteuid() != 0 || fd != 9 {
		return fail(ReasonUnsafeMetadata)
	}
	if err := verifyEnrollmentLock(paths.transaction, fd); err != nil {
		return err
	}
	if _, err := os.Lstat(paths.selection); err == nil {
		selected, err := resolveSelected()
		if err != nil {
			return err
		}
		defer selected.Close()
		if err := verifyLauncherAt(selected, paths.launcher); err != nil {
			return err
		}
		return selected.Revalidate()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(ReasonReadFailed)
	}
	bundle, err := VerifyBundle(source, true)
	if err != nil {
		return err
	}
	defer bundle.Close()
	// Missing ancestors are created individually. Existing paths, including the
	// shared libexec directory, are never chmod'ed or otherwise normalized.
	for _, path := range []string{filepath.Dir(paths.launcher), filepath.Dir(paths.runtimeRoot), paths.runtimeRoot, filepath.Dir(paths.selection)} {
		if err := createTrustedDirectories(path); err != nil {
			return err
		}
	}
	stage, err := os.MkdirTemp(paths.runtimeRoot, ".enroll-")
	if err != nil {
		return fail(ReasonReadFailed)
	}
	// Preserve an interrupted stage as evidence. Only a fully verified, durable
	// directory can become a selected runtime; interrupted stages are never used.
	for _, dir := range []string{"bin", "deploy", "deploy/recovery"} {
		if err := os.Mkdir(filepath.Join(stage, dir), 0700); err != nil {
			return fail(ReasonReadFailed)
		}
	}
	raw, err := bundle.ManifestBytes()
	if err != nil {
		return fail(ReasonReadFailed)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	for _, spec := range ExpectedFiles() {
		limit := int64(MaxScriptSize)
		if strings.HasPrefix(spec.Path, "bin/") {
			limit = MaxBinarySize
		}
		if err := copyVerifiedFile(filepath.Join(bundle.Root, spec.Path), filepath.Join(stage, spec.Path), spec.Mode, manifest.Files[spec.Path], limit); err != nil {
			return err
		}
	}
	if err := writeNewFile(filepath.Join(stage, ManifestName), raw, 0600); err != nil {
		return err
	}
	if err := bundle.Revalidate(); err != nil {
		return err
	}
	for _, dir := range []string{"deploy/recovery", "deploy", "bin", ""} {
		if err := syncDirectory(filepath.Join(stage, dir)); err != nil {
			return err
		}
	}
	staged, err := VerifyBundle(stage, false)
	if err != nil {
		return err
	}
	defer staged.Close()
	if staged.Digest != bundle.Digest {
		return fail(ReasonDigestMismatch)
	}
	if err := staged.Revalidate(); err != nil {
		return err
	}
	final := filepath.Join(paths.runtimeRoot, bundle.Digest)
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return fail(ReasonReadFailed)
		}
		retained, err := VerifyBundle(final, false)
		if err != nil {
			return err
		}
		defer retained.Close()
		if retained.Digest != bundle.Digest {
			return fail(ReasonDigestMismatch)
		}
	}
	if err := syncDirectory(paths.runtimeRoot); err != nil {
		return err
	}
	installed, err := VerifyBundle(final, false)
	if err != nil {
		return err
	}
	defer installed.Close()
	if _, err := os.Lstat(paths.launcher); errors.Is(err, os.ErrNotExist) {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return fail(ReasonReadFailed)
		}
		temporary := filepath.Join(filepath.Dir(paths.launcher), ".recovery-enroll-"+hex.EncodeToString(nonce))
		if _, err := os.Lstat(temporary); err == nil {
			return fail(ReasonUnsafeMetadata)
		}
		if err := copyVerifiedFile(filepath.Join(final, "bin/recovery"), temporary, 0755, manifest.Files["bin/recovery"], MaxBinarySize); err != nil {
			return err
		}
		if err := unix.Renameat2(unix.AT_FDCWD, temporary, unix.AT_FDCWD, paths.launcher, unix.RENAME_NOREPLACE); err != nil {
			return fail(ReasonChanged)
		}
		if err := syncDirectory(filepath.Dir(paths.launcher)); err != nil {
			return err
		}
	} else if err != nil {
		return fail(ReasonReadFailed)
	}
	if err := verifyLauncherAt(installed, paths.launcher); err != nil {
		return err
	}
	// A crash here leaves the durable kit and launcher without a selector.
	// Repeating the same candidate is supported. A different launcher payload
	// is refused; enrollment cannot infer permission to replace that file.
	// Buradaki kesinti kalici kit ve launcher'i secici olmadan birakir. Ayni
	// adayla tekrar desteklenir. Farkli launcher reddedilir; dosyayi degistirme
	// yetkisi enrollment tarafindan varsayilamaz.
	if afterLauncherPublished != nil {
		afterLauncherPublished()
	}
	if err := verifyEnrollmentLock(paths.transaction, fd); err != nil {
		return err
	}
	if err := installed.Revalidate(); err != nil {
		return err
	}
	selection, err := EncodeSelection(bundle.Digest)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(paths.selection), ".runtime-selection-")
	if err != nil {
		return fail(ReasonReadFailed)
	}
	tempName := temp.Name()
	if _, err := temp.Write(selection); err != nil {
		temp.Close()
		return fail(ReasonReadFailed)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fail(ReasonReadFailed)
	}
	if err := temp.Close(); err != nil {
		return fail(ReasonReadFailed)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, tempName, unix.AT_FDCWD, paths.selection, unix.RENAME_NOREPLACE); err != nil {
		return fail(ReasonChanged)
	}
	if err := syncDirectory(filepath.Dir(paths.selection)); err != nil {
		return err
	}
	selected, err := resolveSelected()
	if err != nil {
		return err
	}
	defer selected.Close()
	return selected.Revalidate()
}

func createTrustedDirectories(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fail(ReasonUnsafeMetadata)
	}
	current := "/"
	for _, base := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		current = filepath.Join(current, base)
		if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return fail(ReasonReadFailed)
		} else if err == nil {
			if err := syncDirectory(filepath.Dir(current)); err != nil {
				return err
			}
		}
		var st unix.Stat_t
		if unix.Lstat(current, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07022 != 0 {
			return fail(ReasonUnsafeMetadata)
		}
	}
	return nil
}
func writeNewFile(path string, raw []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return fail(ReasonReadFailed)
	}
	// New files belong to this enrollment. Set the reviewed exact mode through
	// their open descriptor; caller umask must not make the kit unusable.
	if err := file.Chmod(mode); err != nil {
		return fail(ReasonReadFailed)
	}
	if err := file.Sync(); err != nil {
		return fail(ReasonReadFailed)
	}
	if err := file.Close(); err != nil {
		return fail(ReasonReadFailed)
	}
	return nil
}
func copyVerifiedFile(source, dest string, mode os.FileMode, digest string, limit int64) error {
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	file := os.NewFile(uintptr(fd), "runtime-source")
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > limit || before.Mode&07022 != 0 {
		return fail(ReasonUnsafeMetadata)
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return fail(ReasonReadFailed)
	}
	if unix.Fstat(fd, &after) != nil || !sameFile(before, after) || int64(len(raw)) != before.Size {
		return fail(ReasonChanged)
	}
	if Digest(raw) != digest {
		return fail(ReasonContentMismatch)
	}
	return writeNewFile(dest, raw, mode)
}
func syncDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	defer unix.Close(fd)
	if unix.Fsync(fd) != nil {
		return fail(ReasonReadFailed)
	}
	return nil
}
func verifyLauncherAt(runtime *Runtime, launcherPath string) error {
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer state.close()
	parent, err := state.openPath(filepath.Dir(launcherPath))
	if err != nil {
		return err
	}
	launcher, err := state.openFile(parent, filepath.Base(launcherPath), 0755, MaxBinarySize)
	if err != nil {
		return asReadError(err)
	}
	raw, err := runtime.ManifestBytes()
	if err != nil {
		return fail(ReasonReadFailed)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	launcher.digest = manifest.Files["bin/recovery"]
	return launcher.verifyContents()
}
func verifyEnrollmentLock(root string, fd int) error {
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer state.close()
	parent, err := state.openPath(root)
	if err != nil {
		return err
	}
	var held, path unix.Stat_t
	if unix.Fstat(fd, &held) != nil || unix.Fstatat(int(parent.file.Fd()), "transaction.lock", &path, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		held.Mode&unix.S_IFMT != unix.S_IFREG || held.Mode&07777 != 0600 || held.Uid != 0 || held.Gid != 0 || held.Nlink != 1 || held.Size != 0 || !sameFile(held, path) {
		return fail(ReasonUnsafeMetadata)
	}
	info, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if err != nil {
		return fail(ReasonReadFailed)
	}
	hasLock := false
	for _, line := range bytes.Split(info, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) >= 5 && string(fields[0]) == "lock:" && string(fields[2]) == "FLOCK" && string(fields[3]) == "ADVISORY" && string(fields[4]) == "WRITE" {
			hasLock = true
		}
	}
	if !hasLock {
		return fail(ReasonUnsafeMetadata)
	}
	other, err := unix.Openat(int(parent.file.Fd()), "transaction.lock", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	defer unix.Close(other)
	err = unix.Flock(other, unix.LOCK_EX|unix.LOCK_NB)
	if !errors.Is(err, unix.EWOULDBLOCK) {
		if err == nil {
			unix.Flock(other, unix.LOCK_UN)
		}
		return fail(ReasonUnsafeMetadata)
	}
	for _, marker := range []string{"active", "quiesce.pending", "completion.pending", "scheduler-restore.pending"} {
		var stat unix.Stat_t
		err := unix.Fstatat(int(parent.file.Fd()), marker, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if !errors.Is(err, unix.ENOENT) {
			return fail(ReasonChanged)
		}
	}
	return nil
}
