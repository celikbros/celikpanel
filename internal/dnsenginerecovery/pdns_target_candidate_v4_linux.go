//go:build linux

package dnsenginerecovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// CapturePDNSTargetCandidateV4 freezes the identity of a stopped PowerDNS
// candidate before the source is stopped. The caller binds path to the accepted
// journal and holds the host/DNS locks. This byte proof is not a logical proof
// of the rows PowerDNS may change after it starts.
func CapturePDNSTargetCandidateV4(path string) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
	return capturePDNSTargetCandidateV4(path, nil)
}

// VerifyPDNSTargetLiveV4 proves that a candidate was renamed to the installed
// live path without replacement. It must run before PowerDNS starts: the daemon
// can legitimately change SQLite bytes and create WAL/SHM files afterwards.
func VerifyPDNSTargetLiveV4(proof dnsengineartifact.PDNSTargetCandidateProofV4, livePath string) error {
	if !validPDNSTargetPath(proof.Path) || !validPDNSTargetPath(livePath) ||
		proof.Path == livePath ||
		!proof.NoSidecars || proof.Device == 0 || proof.Inode == 0 || proof.Size == 0 ||
		!dnsengineartifact.ValidGeneration(proof.SHA256) {
		return errors.New("PowerDNS target proof is not bound to a safe candidate and live path")
	}
	if _, err := os.Lstat(proof.Path); err == nil {
		return errors.New("PowerDNS candidate still exists after expected rename")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect renamed PowerDNS candidate: %w", err)
	}
	if _, err := observePDNSTargetDirectory(proof.Path, true); err != nil {
		return err
	}
	liveDir, err := observePDNSTargetDirectory(livePath, false)
	if err != nil {
		return err
	}
	if uint64(liveDir.Dev) != proof.Device {
		return errors.New("PowerDNS target rename crosses a filesystem")
	}
	actual, err := probePDNSTargetFileV4(livePath, false, nil)
	if err != nil {
		return err
	}
	actual.Path = proof.Path // Only the journal-authorized rename may change the name.
	if actual != proof {
		return errors.New("PowerDNS live database differs from the frozen candidate")
	}
	return nil
}

func validPDNSTargetPath(name string) bool {
	return filepath.IsAbs(name) && filepath.Clean(name) == name && name != "/" &&
		filepath.Base(name) != "." && filepath.Base(name) != ".."
}

func capturePDNSTargetCandidateV4(name string, afterRead func()) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
	return probePDNSTargetFileV4(name, true, afterRead)
}

func probePDNSTargetFileV4(name string, privateParent bool, afterRead func()) (dnsengineartifact.PDNSTargetCandidateProofV4, error) {
	var empty dnsengineartifact.PDNSTargetCandidateProofV4
	if !validPDNSTargetPath(name) {
		return empty, errors.New("PowerDNS target path is invalid")
	}
	parentBefore, err := observePDNSTargetDirectory(name, privateParent)
	if err != nil {
		return empty, err
	}
	fd, err := openPDNSDatabaseNoSymlinks(name)
	if err != nil {
		return empty, fmt.Errorf("open PowerDNS target without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return empty, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 ||
		before.Size < 16 || before.Size > pdnsLogicalLimit ||
		(before.Mode&0o7777 != 0o640 && before.Mode&0o7777 != 0o600) ||
		before.Uid > 1<<31-1 || before.Gid > 1<<31-1 || before.Dev == 0 || before.Ino == 0 {
		return empty, errors.New("PowerDNS target has unsafe type, links, size or ownership")
	}
	var header [16]byte
	if n, err := unix.Pread(fd, header[:], 0); err != nil || n != len(header) || string(header[:]) != "SQLite format 3\x00" {
		return empty, errors.New("PowerDNS target is not a complete SQLite file")
	}
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > before.Size {
				return empty, errors.New("PowerDNS target grew during proof")
			}
			_, _ = h.Write(buffer[:n])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return empty, fmt.Errorf("read PowerDNS target: %w", readErr)
		}
		if n == 0 {
			return empty, errors.New("PowerDNS target read made no progress")
		}
	}
	if afterRead != nil {
		afterRead()
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return empty, err
	}
	namedFD, err := openPDNSDatabaseNoSymlinks(name)
	if err != nil {
		return empty, fmt.Errorf("reopen PowerDNS target without symlinks: %w", err)
	}
	statErr := unix.Fstat(namedFD, &named)
	closeErr := unix.Close(namedFD)
	if err := errors.Join(statErr, closeErr); err != nil {
		return empty, err
	}
	if total != before.Size || !sameSourceDBStat(before, after) || !sameSourceDBStat(after, named) {
		return empty, errors.New("PowerDNS target bytes or file identity changed during proof")
	}
	parentAfter, err := observePDNSTargetDirectory(name, privateParent)
	if err != nil {
		return empty, err
	}
	if !samePDNSTargetDirectory(parentBefore, parentAfter) || uint64(parentAfter.Dev) != uint64(before.Dev) {
		return empty, errors.New("PowerDNS target directory changed during proof")
	}
	return dnsengineartifact.PDNSTargetCandidateProofV4{
		Path: name, Device: uint64(before.Dev), Inode: before.Ino,
		Mode: before.Mode & 0o7777, UID: before.Uid, GID: before.Gid,
		Size: uint64(total), SHA256: hex.EncodeToString(h.Sum(nil)), NoSidecars: true,
	}, nil
}

// Every database-prefixed sibling is unknown mutable state, including SQLite's
// known WAL/SHM/journal names. An unknown sibling blocks before any inverse.
// The candidate lives in a root-owned 0700 directory. A live database may live
// in the native PowerDNS directory, which must not be group/world writable.
func observePDNSTargetDirectory(name string, private bool) (unix.Stat_t, error) {
	dir := filepath.Dir(name)
	fd, err := unix.Openat2(unix.AT_FDCWD, dir, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return unix.Stat_t{}, fmt.Errorf("open PowerDNS target directory without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), dir)
	defer file.Close()
	var before, after unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return unix.Stat_t{}, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Uid > 1<<31-1 || before.Gid > 1<<31-1 ||
		(private && (before.Uid != 0 || before.Mode&0o7777 != 0o700)) ||
		(!private && before.Mode&0o022 != 0) {
		return unix.Stat_t{}, errors.New("PowerDNS target directory has unsafe ownership or mode")
	}
	if _, err := unix.Fgetxattr(fd, "system.posix_acl_access", nil); err == nil {
		return unix.Stat_t{}, errors.New("PowerDNS target directory has access ACL")
	} else if err != unix.ENODATA && err != unix.ENOTSUP && err != unix.EOPNOTSUPP {
		return unix.Stat_t{}, err
	}
	names, err := file.Readdirnames(-1)
	if err != nil {
		return unix.Stat_t{}, err
	}
	base := filepath.Base(name)
	for _, sibling := range names {
		if strings.HasPrefix(sibling, base+"-") || strings.HasPrefix(sibling, base+".") {
			return unix.Stat_t{}, fmt.Errorf("PowerDNS target has unknown sidecar: %s", sibling)
		}
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return unix.Stat_t{}, err
	}
	if !samePDNSTargetDirectory(before, after) {
		return unix.Stat_t{}, errors.New("PowerDNS target directory changed during sidecar proof")
	}
	return after, nil
}

// RemoveExactStagedPDNSTargetV4 removes only the still-frozen, stopped staged
// candidate in the rollback journal. The caller must hold both recovery locks;
// guard must independently reprove worker exclusion, inactive PowerDNS and
// unchanged source authority. A missing candidate is an idempotent replay only
// while the live path and all sidecars remain absent.
func RemoveExactStagedPDNSTargetV4(
	policy dnsengineartifact.JournalPolicy,
	journal dnsengineartifact.SwitchJournalV1,
	guard func() error,
) error {
	proof, err := stagedPDNSTargetProofV4(policy, journal)
	if err != nil {
		return err
	}
	return removeExactStagedPDNSTargetFileV4(proof, policy.PDNSDatabasePath, guard, nil)
}

func removeExactStagedPDNSTargetFileV4(
	proof dnsengineartifact.PDNSTargetCandidateProofV4,
	livePath string,
	guard func() error,
	afterUnlink func() error,
) error {
	if guard == nil || !validPDNSTargetPath(proof.Path) || !validPDNSTargetPath(livePath) ||
		!proof.NoSidecars || proof.Device == 0 || proof.Inode == 0 || proof.Size == 0 ||
		!dnsengineartifact.ValidGeneration(proof.SHA256) {
		return errors.New("PowerDNS target removal lacks exact proof or native guard")
	}
	if err := verifyPDNSTargetAbsentV4(livePath, false); err != nil {
		return err
	}
	parent := filepath.Dir(proof.Path)
	fd, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("open private PowerDNS candidate parent: %w", err)
	}
	defer unix.Close(fd)
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		return err
	}
	if held.Mode&unix.S_IFMT != unix.S_IFDIR || held.Mode&0o7777 != 0o700 || held.Uid != 0 {
		return errors.New("PowerDNS candidate parent is no longer root-private")
	}
	observed, err := observePDNSTargetDirectory(proof.Path, true)
	if err != nil {
		return err
	}
	if !samePDNSTargetDirectory(held, observed) {
		return errors.New("PowerDNS candidate parent changed before removal")
	}
	leaf := filepath.Base(proof.Path)
	var candidate unix.Stat_t
	err = unix.Fstatat(fd, leaf, &candidate, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		if err := guard(); err != nil {
			return err
		}
		if err := verifyPDNSTargetAbsentV4(proof.Path, true); err != nil {
			return err
		}
		if err := verifyPDNSTargetAbsentV4(livePath, false); err != nil {
			return err
		}
		return unix.Fsync(fd)
	}
	if err != nil {
		return err
	}
	if err := exactPDNSTargetCandidateStatV4(candidate, proof); err != nil {
		return err
	}
	actual, err := CapturePDNSTargetCandidateV4(proof.Path)
	if err != nil {
		return err
	}
	if actual != proof {
		return errors.New("PowerDNS candidate differs from frozen journal before removal")
	}
	if err := guard(); err != nil {
		return err
	}
	if err := verifyPDNSTargetAbsentV4(livePath, false); err != nil {
		return err
	}
	actual, err = CapturePDNSTargetCandidateV4(proof.Path)
	if err != nil {
		return err
	}
	if actual != proof {
		return errors.New("PowerDNS candidate was owner-modified before removal")
	}
	observed, err = observePDNSTargetDirectory(proof.Path, true)
	if err != nil {
		return err
	}
	if !samePDNSTargetDirectory(held, observed) {
		// Directory mtime can only have changed through a concurrent mutation.
		return errors.New("PowerDNS candidate parent changed before unlink")
	}
	if err := unix.Fstatat(fd, leaf, &candidate, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if err := exactPDNSTargetCandidateStatV4(candidate, proof); err != nil {
		return err
	}
	if err := unix.Unlinkat(fd, leaf, 0); err != nil {
		return err
	}
	if afterUnlink != nil {
		if err := afterUnlink(); err != nil {
			return err
		}
	}
	if err := unix.Fsync(fd); err != nil {
		return err
	}
	if err := verifyPDNSTargetAbsentV4(proof.Path, true); err != nil {
		return err
	}
	return verifyPDNSTargetAbsentV4(livePath, false)
}

func exactPDNSTargetCandidateStatV4(stat unix.Stat_t, proof dnsengineartifact.PDNSTargetCandidateProofV4) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 ||
		uint64(stat.Dev) != proof.Device || stat.Ino != proof.Inode ||
		stat.Mode&0o7777 != proof.Mode || stat.Uid != proof.UID ||
		stat.Gid != proof.GID || uint64(stat.Size) != proof.Size {
		return errors.New("PowerDNS candidate inode or metadata differs from frozen journal")
	}
	return nil
}
func samePDNSTargetDirectory(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode &&
		a.Uid == b.Uid && a.Gid == b.Gid && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
