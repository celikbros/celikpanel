//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// This file holds the residue half of the PowerDNS-to-BIND rollback end state
// shared by the owner command recover-dns-bind-switch and the Agent's
// in-process V2 rollback: the staged target generation of a standby-class
// journal (BINDSwitchNeverStartedTargetJournal) is removed only when it is
// exactly what the journal staged and nothing selects it; BIND's own runtime
// files are listed, never removed.

// BINDGenerationResidue classifies a journal's staged target generation.
type BINDGenerationResidue uint8

const (
	// BINDGenerationNone: nothing to remove (absent, still selected by the
	// pointer, or a journal outside the standby class).
	BINDGenerationNone BINDGenerationResidue = iota
	// BINDGenerationStaged: exactly the tree the journal staged, or its
	// interrupted removal; RemoveStagedBINDGeneration deletes it.
	BINDGenerationStaged
	// BINDGenerationRetained: present but not provably what the journal
	// staged (an owner changed or added files); left in place.
	BINDGenerationRetained
)

// BINDSwitchExpectedTargetReceipt renders the target generation receipt from
// the frozen journal and requires its identity to be the journal's target.
func BINDSwitchExpectedTargetReceipt(j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout) (binddns.Receipt, error) {
	zones := make([]binddns.ZoneSnapshot, len(j.Zones))
	for i, z := range j.Zones {
		zones[i] = binddns.ZoneSnapshot{
			DesiredGeneration: z.DesiredGeneration, Domain: z.Domain, Delete: z.Delete,
			Qualifier: z.ZoneQualifier, MutationRequestID: j.MutationRequestID,
			MutationOwnerID: j.MutationOwnerID, Records: z.Records,
		}
	}
	generation, err := binddns.RenderManifest(string(layout), binddns.Manifest{EngineEpoch: j.TargetEpoch, Zones: zones})
	if err != nil {
		return binddns.Receipt{}, err
	}
	if generation.ID != j.TargetGeneration {
		return binddns.Receipt{}, errors.New("frozen BIND target differs from journal generation")
	}
	return generation.ReceiptValue, nil
}

// bindGenerationRemovable reports a standby-class journal that names a target
// generation different from its predecessor.
func bindGenerationRemovable(j dnsengineartifact.SwitchJournalV1) bool {
	return BINDSwitchNeverStartedTargetJournal(j) && j.TargetGeneration != "" &&
		!(j.HadPrevious && j.PreviousGeneration == j.TargetGeneration)
}

// BINDGenerationRemovalPath is the reserved in-root name an exact staged
// generation is renamed to before deletion, so an interrupted removal never
// leaves a partial tree under generations/.
func BINDGenerationRemovalPath(layout bindroot.Layout, generation string) string {
	return string(layout) + "/.rollback-remove-" + generation
}

// bindGenerationFS is the filesystem and publisher view of the managed root.
// The installed view is fixed; tests supply a temporary root.
type bindGenerationFS struct {
	root      string
	catalog   func(context.Context) (any, error)
	publisher func() (*binddns.Publisher, error)
}

func installedBINDGenerationFS(layout bindroot.Layout, gid uint32) bindGenerationFS {
	return bindGenerationFS{
		root: string(layout),
		catalog: func(ctx context.Context) (any, error) {
			return bindroot.VerifyInstalledCatalog(ctx, layout, gid)
		},
		publisher: func() (*binddns.Publisher, error) { return binddns.NewOSPublisher(string(layout)) },
	}
}

// ClassifyStagedBINDGeneration is read-only. A generation is removable only
// when the pointer does not select it, its immutable tree verifies against its
// own receipt, and that receipt equals the one rendered from the frozen
// journal. The managed catalog must be identical around the observation.
func ClassifyStagedBINDGeneration(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (BINDGenerationResidue, string, error) {
	return classifyStagedBINDGeneration(ctx, j, layout, installedBINDGenerationFS(layout, gid))
}

func classifyStagedBINDGeneration(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, view bindGenerationFS) (BINDGenerationResidue, string, error) {
	if ctx == nil {
		return BINDGenerationNone, "", errors.New("BIND generation observation requires a context")
	}
	if !bindGenerationRemovable(j) {
		return BINDGenerationNone, "", nil
	}
	before, err := view.catalog(ctx)
	if err != nil {
		return BINDGenerationNone, "", err
	}
	publisher, err := view.publisher()
	if err != nil {
		return BINDGenerationNone, "", err
	}
	current, exists, err := publisher.Current()
	if err != nil {
		return BINDGenerationNone, "", err
	}
	if exists && current == j.TargetGeneration {
		return BINDGenerationNone, "", nil
	}
	state, reason := BINDGenerationNone, ""
	removal := view.root + "/.rollback-remove-" + j.TargetGeneration
	final := view.root + "/generations/" + j.TargetGeneration
	if info, statErr := os.Lstat(removal); statErr == nil {
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			state, reason = BINDGenerationRetained, "an unexpected entry uses the reserved removal name"
		} else {
			state = BINDGenerationStaged
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return BINDGenerationNone, "", statErr
	}
	if state == BINDGenerationNone {
		if _, statErr := os.Lstat(final); errors.Is(statErr, fs.ErrNotExist) {
			return BINDGenerationNone, "", nil
		} else if statErr != nil {
			return BINDGenerationNone, "", statErr
		}
		expected, renderErr := BINDSwitchExpectedTargetReceipt(j, layout)
		if renderErr != nil {
			return BINDGenerationNone, "", renderErr
		}
		tree, loadErr := publisher.LoadGeneration(j.TargetGeneration)
		switch {
		case loadErr != nil:
			state, reason = BINDGenerationRetained, loadErr.Error()
		case !reflect.DeepEqual(tree.CurrentReceipt(), expected):
			state, reason = BINDGenerationRetained, "its receipt differs from the frozen journal"
		default:
			state = BINDGenerationStaged
		}
	}
	after, err := view.catalog(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		return BINDGenerationNone, "", errors.Join(errors.New("managed BIND generation catalog changed during observation"), err)
	}
	return state, reason, ctx.Err()
}

// RemoveStagedBINDGeneration removes an exact staged generation: the tree is
// re-classified, renamed without replacement to its reserved removal name
// inside the managed root, then deleted. A retained tree is left untouched and
// nil is returned. It reports whether a tree was removed.
func RemoveStagedBINDGeneration(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, gid uint32) (bool, error) {
	return removeStagedBINDGeneration(ctx, j, layout, installedBINDGenerationFS(layout, gid))
}

func removeStagedBINDGeneration(ctx context.Context, j dnsengineartifact.SwitchJournalV1, layout bindroot.Layout, view bindGenerationFS) (bool, error) {
	state, _, err := classifyStagedBINDGeneration(ctx, j, layout, view)
	if err != nil || state != BINDGenerationStaged {
		return false, err
	}
	removal := view.root + "/.rollback-remove-" + j.TargetGeneration
	generations := view.root + "/generations"
	final := generations + "/" + j.TargetGeneration
	if _, statErr := os.Lstat(final); statErr == nil {
		if _, removalErr := os.Lstat(removal); removalErr == nil {
			return false, errors.New("both the staged BIND generation and its removal directory exist")
		}
		if err := unix.Renameat2(unix.AT_FDCWD, final, unix.AT_FDCWD, removal, unix.RENAME_NOREPLACE); err != nil {
			return false, fmt.Errorf("detach staged BIND generation: %w", err)
		}
		if err := syncBINDDirectory(generations); err != nil {
			return false, err
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return false, statErr
	}
	if err := removeDetachedBINDTree(removal); err != nil {
		return false, err
	}
	if err := syncBINDDirectory(view.root); err != nil {
		return false, err
	}
	if _, err := view.catalog(ctx); err != nil {
		return false, err
	}
	return true, ctx.Err()
}

// removeDetachedBINDTree deletes a detached generation. Its immutable
// directories are made writable first so removal does not depend on
// privileges that bypass directory permissions. Symlinks are never followed.
func removeDetachedBINDTree(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("detached BIND generation is not a directory")
	}
	if err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			return os.Chmod(name, 0o700)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("prepare detached BIND generation for removal: %w", err)
	}
	return os.RemoveAll(path)
}

func syncBINDDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsync(fd)
}

// BINDWorkingDirectory is the package's BIND working directory (named's
// "directory" option), where named writes its own runtime files. It is
// outside the managed BIND root.
func BINDWorkingDirectory(layout bindroot.Layout) string {
	switch layout {
	case bindroot.APT:
		return "/var/cache/bind"
	case bindroot.Pacman:
		return "/var/named"
	}
	return ""
}

// ListBINDRuntimeFiles lists, read-only, the runtime files named writes in
// its working directory: managed-keys.bind* and zero-length tmp-* regular
// files. Callers record them and never remove them: they are outside the
// managed BIND root, and a V2 journal records no intent time that could bind
// them to one operation.
func ListBINDRuntimeFiles(directory string) ([]string, error) {
	if directory == "" {
		return nil, errors.New("unsupported BIND working directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		managedKeys := strings.HasPrefix(name, "managed-keys.bind")
		temporary := strings.HasPrefix(name, "tmp-")
		if !managedKeys && !temporary {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || (temporary && info.Size() != 0) {
			continue
		}
		owner := ""
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf(", owner %d:%d", stat.Uid, stat.Gid)
		}
		files = append(files, fmt.Sprintf("%s (%d bytes%s)", filepath.Join(directory, name), info.Size(), owner))
	}
	sort.Strings(files)
	return files, nil
}
