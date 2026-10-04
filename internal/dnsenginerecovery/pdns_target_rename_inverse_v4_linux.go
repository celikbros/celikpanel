//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// RestoreRenamedPDNSTargetV4 reverses only the candidate-to-live rename made
// under a pre-start journal checkpoint. The caller holds both installed DNS and
// release locks. guard independently proves worker exclusion, an inactive
// PowerDNS unit with no process, and the original BIND authority and receipt.
// The candidate metadata must have been finalized before the forward rename:
// changing ownership or mode after rename makes this exact inverse refuse.
func RestoreRenamedPDNSTargetV4(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, guard func() error) error {
	proof, err := stagedPDNSTargetProofV4(policy, journal)
	if err != nil {
		return err
	}
	if journal.PDNSTargetPlan.StagedDigest == "" {
		return errors.New("PowerDNS rename inverse requires the staged checkpoint")
	}
	return restoreRenamedPDNSTargetFileV4(proof, policy.PDNSDatabasePath, guard, nil)
}

func restoreRenamedPDNSTargetFileV4(proof dnsengineartifact.PDNSTargetCandidateProofV4, livePath string, guard func() error, afterRename func() error) error {
	if guard == nil || !validPDNSTargetPath(proof.Path) || !validPDNSTargetPath(livePath) ||
		filepath.Dir(proof.Path) == filepath.Dir(livePath) || !proof.NoSidecars ||
		proof.Device == 0 || proof.Inode == 0 || proof.Size == 0 ||
		!dnsengineartifact.ValidGeneration(proof.SHA256) {
		return errors.New("PowerDNS rename inverse lacks exact proof or native guard")
	}
	privateFD, privateStat, err := openHeldPDNSTargetDirV4(proof.Path, true)
	if err != nil {
		return err
	}
	defer unix.Close(privateFD)
	liveFD, liveStat, err := openHeldPDNSTargetDirV4(livePath, false)
	if err != nil {
		return err
	}
	defer unix.Close(liveFD)
	if privateStat.Dev != liveStat.Dev || uint64(liveStat.Dev) != proof.Device {
		return errors.New("PowerDNS rename inverse crosses a filesystem")
	}
	privateLeaf, liveLeaf := filepath.Base(proof.Path), filepath.Base(livePath)
	// A crash after rename may have left the candidate back at its original
	// name. Accept only that exact file while the live path is absent.
	if err := verifyPDNSTargetAbsentV4(livePath, false); err == nil {
		if actual, captureErr := CapturePDNSTargetCandidateV4(proof.Path); captureErr == nil && actual == proof {
			if err := guard(); err != nil {
				return err
			}
			if err := verifyHeldPDNSTargetDirV4(privateFD, privateStat, proof.Path, true); err != nil {
				return err
			}
			if err := verifyHeldPDNSTargetDirV4(liveFD, liveStat, livePath, false); err != nil {
				return err
			}
			if err := VerifyPDNSTargetCandidateV4Exact(proof); err != nil {
				return err
			}
			if err := verifyPDNSTargetAbsentV4(livePath, false); err != nil {
				return err
			}
			return errors.Join(unix.Fsync(privateFD), unix.Fsync(liveFD))
		}
	}
	if err := verifyPDNSTargetAbsentV4(proof.Path, true); err != nil {
		return err
	}
	if err := VerifyPDNSTargetLiveV4(proof, livePath); err != nil {
		return err
	}
	if err := guard(); err != nil {
		return err
	}
	if err := verifyHeldPDNSTargetDirV4(privateFD, privateStat, proof.Path, true); err != nil {
		return err
	}
	if err := verifyHeldPDNSTargetDirV4(liveFD, liveStat, livePath, false); err != nil {
		return err
	}
	if err := verifyPDNSTargetAbsentV4(proof.Path, true); err != nil {
		return err
	}
	if err := VerifyPDNSTargetLiveV4(proof, livePath); err != nil {
		return err
	}
	var liveStatNow unix.Stat_t
	if err := unix.Fstatat(liveFD, liveLeaf, &liveStatNow, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if err := exactPDNSTargetCandidateStatV4(liveStatNow, proof); err != nil {
		return err
	}
	if err := unix.Renameat2(liveFD, liveLeaf, privateFD, privateLeaf, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("return exact PowerDNS database to private candidate path: %w", err)
	}
	if afterRename != nil {
		if err := afterRename(); err != nil {
			return err
		}
	}
	if err := unix.Fsync(liveFD); err != nil {
		return err
	}
	if err := unix.Fsync(privateFD); err != nil {
		return err
	}
	if err := verifyHeldPDNSTargetDirIdentityV4(privateFD, privateStat, proof.Path, true); err != nil {
		return err
	}
	if err := verifyHeldPDNSTargetDirIdentityV4(liveFD, liveStat, livePath, false); err != nil {
		return err
	}
	if err := VerifyPDNSTargetCandidateV4Exact(proof); err != nil {
		return err
	}
	return verifyPDNSTargetAbsentV4(livePath, false)
}

// VerifyPDNSTargetCandidateV4Exact is the frozen-byte postcondition after a
// rename and intentionally cannot repair a different file at the path.
func VerifyPDNSTargetCandidateV4Exact(proof dnsengineartifact.PDNSTargetCandidateProofV4) error {
	actual, err := CapturePDNSTargetCandidateV4(proof.Path)
	if err != nil {
		return err
	}
	if actual != proof {
		return errors.New("PowerDNS candidate differs from frozen journal")
	}
	return nil
}

func openHeldPDNSTargetDirV4(path string, private bool) (int, unix.Stat_t, error) {
	observed, err := observePDNSTargetDirectory(path, private)
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, unix.Stat_t{}, err
	}
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		unix.Close(fd)
		return -1, unix.Stat_t{}, err
	}
	if !samePDNSTargetDirectory(held, observed) {
		unix.Close(fd)
		return -1, unix.Stat_t{}, errors.New("PowerDNS target directory changed before pinning")
	}
	return fd, held, nil
}

func verifyHeldPDNSTargetDirV4(fd int, original unix.Stat_t, path string, private bool) error {
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		return err
	}
	observed, err := observePDNSTargetDirectory(path, private)
	if err != nil {
		return err
	}
	if !samePDNSTargetDirectory(original, held) || !samePDNSTargetDirectory(held, observed) {
		return errors.New("PowerDNS target directory changed during guarded rename")
	}
	return nil
}

func verifyHeldPDNSTargetDirIdentityV4(fd int, original unix.Stat_t, path string, private bool) error {
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		return err
	}
	observed, err := observePDNSTargetDirectory(path, private)
	if err != nil {
		return err
	}
	if held.Dev != original.Dev || held.Ino != original.Ino || held.Mode != original.Mode ||
		held.Uid != original.Uid || held.Gid != original.Gid || !samePDNSTargetDirectory(held, observed) {
		return errors.New("PowerDNS target directory identity changed after guarded rename")
	}
	return nil
}
