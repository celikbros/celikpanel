//go:build linux

package dnsenginerecovery

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// FreshPrimaryPartialBuildV3 reports which of an unsealed V3 journal's own
// temporary build files exist. The journal must be the exact V3 fresh-primary
// shape with no sealed candidate; the names come from the journal's candidate
// path (dnsengineartifact.PDNSFreshCandidateBuildPathsV3).
func FreshPrimaryPartialBuildV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) ([]string, error) {
	names, err := freshPrimaryPartialNamesV3(policy, journal)
	if err != nil {
		return nil, err
	}
	var present []string
	for _, name := range names {
		var stat unix.Stat_t
		err := unix.Lstat(name, &stat)
		switch {
		case errors.Is(err, unix.ENOENT):
		case err != nil:
			return nil, fmt.Errorf("inspect PowerDNS partial candidate %s: %w", filepath.Base(name), err)
		default:
			present = append(present, name)
		}
	}
	return present, nil
}

func freshPrimaryPartialNamesV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1) ([]string, error) {
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 ||
		journal.PDNSFreshPlan == nil || journal.PDNSFreshPlan.Candidate != nil ||
		journal.PDNSFreshPlan.Native != nil {
		return nil, errors.New("v3 partial candidate removal requires an unsealed fresh-primary journal")
	}
	build, sidecars, err := dnsengineartifact.PDNSFreshCandidateBuildPathsV3(journal.PDNSCandidatePath)
	if err != nil {
		return nil, err
	}
	// Sidecars first: SQLite never needs them without the main file.
	return append(sidecars, build), nil
}

// RemoveFreshPrimaryPartialV3 removes only this operation's own temporary
// build files after its rollback decision is durable. Each name is derived
// from the request's candidate path inside the root-private state directory,
// where only root can create entries and whose absence was proved before the
// intent was written; each must be a regular, singly linked file. The guard
// re-proves the journal and the never-started target immediately before each
// unlink. Nothing else is removed, and absence is an idempotent replay.
//
// Yalnız bu işlemin kendi geçici kurulum dosyalarını, geri alma kararı kalıcı
// olduktan sonra kaldırır. Her ad isteğin aday yolundan türetilir; başka hiçbir
// şey silinmez ve yokluk idempotent bir tekrardır.
func RemoveFreshPrimaryPartialV3(policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, guard func() error) error {
	if guard == nil {
		return errors.New("v3 partial candidate removal requires a native guard")
	}
	if journal.Phase != dnsengineartifact.SwitchPhaseRollingBack {
		return errors.New("v3 partial candidate removal requires a durable rollback decision")
	}
	names, err := freshPrimaryPartialNamesV3(policy, journal)
	if err != nil {
		return err
	}
	parent := filepath.Dir(names[len(names)-1])
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
	removed := false
	for _, name := range names {
		leaf := filepath.Base(name)
		var stat unix.Stat_t
		err := unix.Fstatat(fd, leaf, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
			return fmt.Errorf("PowerDNS partial candidate %s is not a regular, singly linked file", leaf)
		}
		if err := guard(); err != nil {
			return err
		}
		var again unix.Stat_t
		if err := unix.Fstatat(fd, leaf, &again, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
			again.Dev != stat.Dev || again.Ino != stat.Ino {
			return errors.Join(fmt.Errorf("PowerDNS partial candidate %s changed before removal", leaf), err)
		}
		if err := unix.Unlinkat(fd, leaf, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("remove PowerDNS partial candidate %s: %w", leaf, err)
		}
		removed = true
	}
	if removed {
		if err := unix.Fsync(fd); err != nil {
			return fmt.Errorf("sync PowerDNS candidate parent after partial removal: %w", err)
		}
	}
	present, err := FreshPrimaryPartialBuildV3(policy, journal)
	if err != nil {
		return err
	}
	if len(present) != 0 {
		return errors.New("PowerDNS partial candidate files remain after removal")
	}
	return nil
}
