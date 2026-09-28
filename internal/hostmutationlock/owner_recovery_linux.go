//go:build linux

package hostmutationlock

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const ownerRecoveryLockPath = "/run/celikpanel/service-mutation.lock"

// AcquireOrCreateOwnerRecovery obtains the historical host flock after an
// independent recovery caller has obtained the release lock and established
// that the Agent and Panel cannot concurrently mutate the host. Only root may
// use it, and the supplied group must be the installed celikpanel group. It
// creates a missing volatile /run directory or lock without replacing an
// existing owner object. The caller owns and must close the returned lease.
func AcquireOrCreateOwnerRecovery(owner Owner) (*os.File, error) {
	if err := verifyOwnerRecoveryAdmissionAt(owner, "/etc/group"); err != nil {
		return nil, err
	}
	return acquireOrCreateOwnerRecoveryAt(ownerRecoveryLockPath, owner)
}

func verifyOwnerRecoveryAdmissionAt(owner Owner, groupPath string) error {
	if err := verifyOwnerRecoveryNumericIdentity(owner); err != nil {
		return err
	}
	gid, err := localCelikpanelGroupIDAt(groupPath)
	if err != nil {
		return err
	}
	if gid != owner.GID {
		return errors.New("owner recovery host lock group does not match local celikpanel")
	}
	return nil
}

// VerifyHeldOwnerRecovery re-proves the canonical parent/file identity and the
// held exclusive flock before each native recovery effect or lease retirement.
// It does not create anything or acquire an absent lease.
func VerifyHeldOwnerRecovery(file *os.File, owner Owner) error {
	if file == nil {
		return errors.New("owner recovery host lock lease is missing")
	}
	if err := verifyOwnerRecoveryNumericIdentity(owner); err != nil {
		return err
	}
	return VerifyInherited(ownerRecoveryLockPath, int(file.Fd()), owner)
}

func verifyOwnerRecoveryNumericIdentity(owner Owner) error {
	if os.Geteuid() != 0 || owner.UID != 0 || owner.GID == 0 || owner.GID > uint32(1<<31-1) {
		return errors.New("owner recovery host lock requires root and an established nonzero group")
	}
	return nil
}

// localCelikpanelGroupIDAt reads a fixed local group database through secure
// descriptors. It never calls NSS, DNS, LDAP, or a panel service.
func localCelikpanelGroupIDAt(path string) (uint32, error) {
	dirFD, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return 0, fmt.Errorf("open local group directory: %w", err)
	}
	defer unix.Close(dirFD)
	var dirStat unix.Stat_t
	if err := unix.Fstat(dirFD, &dirStat); err != nil || !trustedDirectory(dirStat, Owner{}) {
		return 0, errors.New("local group directory is untrusted")
	}
	fd, err := unix.Openat2(dirFD, filepath.Base(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return 0, fmt.Errorf("open local group file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG ||
		before.Uid != 0 || before.Gid != 0 || before.Mode&0o022 != 0 || before.Nlink != 1 ||
		before.Size < 0 || before.Size > 1<<20 {
		return 0, errors.New("local group file is untrusted")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return 0, errors.New("local group file exceeds its trusted read limit")
	}
	if err := unix.Fstat(fd, &after); err != nil ||
		unix.Fstatat(dirFD, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		!sameIdentity(before, after) || !sameIdentity(after, named) ||
		before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		named.Size != after.Size || named.Mtim != after.Mtim || named.Ctim != after.Ctim || int64(len(raw)) != after.Size {
		return 0, errors.New("local group file changed during admission")
	}
	var found bool
	var gid uint32
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 0 || fields[0] != "celikpanel" {
			continue
		}
		if len(fields) != 4 || found {
			return 0, errors.New("local celikpanel group record is ambiguous")
		}
		number, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || number == 0 || number > 1<<31-1 || strconv.FormatUint(number, 10) != fields[2] {
			return 0, errors.New("local celikpanel group number is invalid")
		}
		gid, found = uint32(number), true
	}
	if !found {
		return 0, errors.New("local celikpanel group is absent")
	}
	return gid, nil
}

// acquireOrCreateOwnerRecoveryAt exists for disposable native fixtures. Its
// caller must establish the same authority as the fixed-path public entry.
func acquireOrCreateOwnerRecoveryAt(path string, owner Owner) (*os.File, error) {
	if err := ensureOwnerRecoveryDirectory(filepath.Dir(path), owner); err != nil {
		return nil, err
	}
	loc, err := openLocation(path, owner)
	if err != nil {
		return nil, err
	}
	defer unix.Close(loc.parent)
	var named unix.Stat_t
	if err := unix.Fstatat(loc.parent, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return AcquireExisting(path, owner)
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, fmt.Errorf("inspect owner recovery host lock: %w", err)
	}
	// An anonymous inode is fully owned, permissioned and locked before its
	// canonical name becomes visible. Filesystems without O_TMPFILE fail closed.
	fd, err := unix.Openat(loc.parent, ".", unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("stage owner recovery host lock: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			unix.Close(fd)
		}
	}()
	if err := unix.Fchown(fd, int(owner.UID), int(owner.GID)); err != nil {
		return nil, fmt.Errorf("own staged host lock: %w", err)
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return nil, fmt.Errorf("permission staged host lock: %w", err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("flock staged host lock: %w", err)
	}
	if err := unix.Fsync(fd); err != nil {
		return nil, fmt.Errorf("sync staged host lock: %w", err)
	}
	var staged unix.Stat_t
	if err := unix.Fstat(fd, &staged); err != nil || staged.Mode&unix.S_IFMT != unix.S_IFREG || staged.Mode&0o7777 != 0o600 || staged.Uid != owner.UID || staged.Gid != owner.GID || staged.Nlink != 0 || staged.Size != 0 {
		return nil, errors.New("staged host lock metadata is unsafe")
	}
	if err := loc.verifyDirectory(); err != nil {
		return nil, err
	}
	if err := unix.Linkat(fd, "", loc.parent, filepath.Base(path), unix.AT_EMPTY_PATH); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return AcquireExisting(path, owner)
		}
		return nil, fmt.Errorf("publish owner recovery host lock without replacement: %w", err)
	}
	if err := unix.Fsync(loc.parent); err != nil {
		return nil, fmt.Errorf("sync published host lock directory: %w", err)
	}
	published, err := loc.verifyFile(fd, nil)
	if err != nil {
		return nil, err
	}
	if published.Dev != staged.Dev || published.Ino != staged.Ino {
		return nil, errors.New("published host lock is not the held staged inode")
	}
	keep = true
	return os.NewFile(uintptr(fd), path), nil
}

func ensureOwnerRecoveryDirectory(path string, owner Owner) error {
	runPath := filepath.Dir(path)
	runFD, err := unix.Openat2(unix.AT_FDCWD, runPath, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("open owner recovery runtime parent: %w", err)
	}
	defer unix.Close(runFD)
	var runStat unix.Stat_t
	if err := unix.Fstat(runFD, &runStat); err != nil || !trustedDirectory(runStat, Owner{UID: 0, GID: 0}) {
		return errors.New("owner recovery runtime parent has unsafe ownership or permissions")
	}
	base := filepath.Base(path)
	var existing unix.Stat_t
	if err := unix.Fstatat(runFD, base, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return nil // openLocation must still prove the existing directory.
	} else if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("inspect owner recovery runtime directory: %w", err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	private := ".celikpanel-recovery-" + hex.EncodeToString(random[:])
	if err := unix.Mkdirat(runFD, private, 0o700); err != nil {
		return fmt.Errorf("stage owner recovery runtime directory: %w", err)
	}
	var created unix.Stat_t
	if err := unix.Fstatat(runFD, private, &created, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect staged owner recovery directory: %w", err)
	}
	privateFD, err := unix.Openat(runFD, private, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		var current unix.Stat_t
		if unix.Fstatat(runFD, private, &current, unix.AT_SYMLINK_NOFOLLOW) == nil && current.Dev == created.Dev && current.Ino == created.Ino && current.Mode&unix.S_IFMT == unix.S_IFDIR {
			unix.Unlinkat(runFD, private, unix.AT_REMOVEDIR)
		}
		return fmt.Errorf("open staged owner recovery directory: %w", err)
	}
	defer unix.Close(privateFD)
	defer func() {
		var current, named unix.Stat_t
		if unix.Fstat(privateFD, &current) == nil && unix.Fstatat(runFD, private, &named, unix.AT_SYMLINK_NOFOLLOW) == nil &&
			current.Dev == named.Dev && current.Ino == named.Ino && named.Mode&unix.S_IFMT == unix.S_IFDIR {
			unix.Unlinkat(runFD, private, unix.AT_REMOVEDIR)
		}
	}()
	if err := unix.Fchown(privateFD, int(owner.UID), int(owner.GID)); err != nil {
		return fmt.Errorf("own staged owner recovery directory: %w", err)
	}
	if err := unix.Fchmod(privateFD, 0o750); err != nil {
		return fmt.Errorf("permission staged owner recovery directory: %w", err)
	}
	if err := unix.Fsync(privateFD); err != nil {
		return fmt.Errorf("sync staged owner recovery directory: %w", err)
	}
	var staged unix.Stat_t
	if err := unix.Fstat(privateFD, &staged); err != nil || !trustedDirectory(staged, owner) {
		return errors.New("staged owner recovery directory metadata is unsafe")
	}
	var current unix.Stat_t
	if err := unix.Fstat(runFD, &current); err != nil || current.Dev != runStat.Dev || current.Ino != runStat.Ino || current.Mode != runStat.Mode || current.Uid != runStat.Uid || current.Gid != runStat.Gid {
		return errors.New("owner recovery runtime parent changed")
	}
	if err := unix.Renameat2(runFD, private, runFD, base, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return nil // openLocation must prove the winner before admission.
		}
		return fmt.Errorf("publish owner recovery directory without replacement: %w", err)
	}
	if err := unix.Fsync(runFD); err != nil {
		return fmt.Errorf("sync published owner recovery directory: %w", err)
	}
	return nil
}
