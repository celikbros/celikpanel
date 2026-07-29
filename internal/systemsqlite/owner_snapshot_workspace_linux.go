//go:build linux

package systemsqlite

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const ownerSnapshotWorkspacePrefix = ".celikpanel-sqlite-owner-"

const (
	ownerSnapshotStageName         = "stage"
	ownerSnapshotCleanupMaxDepth   = 16
	ownerSnapshotCleanupMaxEntries = 10000
)

type ownerSnapshotWorkspace struct {
	root        *os.File
	outer       *os.File
	directory   *os.File
	outerName   string
	outerDevice uint64
	outerInode  uint64
	device      uint64
	inode       uint64
}

func createOwnerSnapshotWorkspace(
	root string,
	uid uint32,
	gid uint32,
) (*ownerSnapshotWorkspace, error) {
	clean := filepath.Clean(strings.TrimSpace(root))
	if clean == "." || !filepath.IsAbs(clean) || filepath.Dir(clean) == clean {
		return nil, errors.New("isolated SQLite workspace root is unsafe")
	}
	if err := rejectSymlinkComponents(clean); err != nil {
		return nil, errors.New("isolated SQLite workspace root is unsafe")
	}
	rootFD, err := unix.Open(
		clean,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, errors.New("could not open isolated SQLite workspace root")
	}
	rootFile := os.NewFile(uintptr(rootFD), "system-sqlite-workspace-root")
	if rootFile == nil {
		_ = unix.Close(rootFD)
		return nil, errors.New("could not pin isolated SQLite workspace root")
	}
	if err := validateOwnerWorkspaceRoot(rootFD); err != nil {
		_ = rootFile.Close()
		return nil, errors.New("isolated SQLite workspace root is unsafe")
	}

	for attempt := 0; attempt < 128; attempt++ {
		randomBytes := make([]byte, 16)
		if _, err := rand.Read(randomBytes); err != nil {
			_ = rootFile.Close()
			return nil, errors.New("could not name isolated SQLite workspace")
		}
		outerName := ownerSnapshotWorkspacePrefix + hex.EncodeToString(randomBytes)
		if err := unix.Mkdirat(rootFD, outerName, 0o700); err != nil {
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			_ = rootFile.Close()
			return nil, errors.New("could not create isolated SQLite workspace")
		}
		outerFD, err := unix.Openat(
			rootFD,
			outerName,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not pin isolated SQLite workspace")
		}
		outerFile := os.NewFile(uintptr(outerFD), "system-sqlite-workspace-outer")
		if outerFile == nil {
			_ = unix.Close(outerFD)
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not pin isolated SQLite workspace")
		}
		var initialOuter unix.Stat_t
		if err := unix.Fstat(outerFD, &initialOuter); err != nil ||
			initialOuter.Mode&unix.S_IFMT != unix.S_IFDIR || initialOuter.Uid != 0 ||
			initialOuter.Mode&0o777 != 0o700 {
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("isolated SQLite workspace identity is unsafe")
		}
		if err := unix.Fchown(outerFD, 0, int(gid)); err != nil || unix.Fchmod(outerFD, 0o710) != nil {
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not assign isolated SQLite workspace")
		}
		var assignedOuter unix.Stat_t
		if err := unix.Fstat(outerFD, &assignedOuter); err != nil ||
			assignedOuter.Mode&unix.S_IFMT != unix.S_IFDIR || assignedOuter.Uid != 0 ||
			assignedOuter.Gid != gid || assignedOuter.Mode&0o777 != 0o710 {
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not verify isolated SQLite workspace")
		}
		if err := unix.Mkdirat(outerFD, ownerSnapshotStageName, 0o700); err != nil {
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not create isolated SQLite workspace")
		}
		directoryFD, err := unix.Openat(
			outerFD,
			ownerSnapshotStageName,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			_ = unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR)
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not pin isolated SQLite workspace")
		}
		directoryFile := os.NewFile(uintptr(directoryFD), "system-sqlite-workspace")
		if directoryFile == nil {
			_ = unix.Close(directoryFD)
			_ = unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR)
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not pin isolated SQLite workspace")
		}
		var initial unix.Stat_t
		if err := unix.Fstat(directoryFD, &initial); err != nil ||
			initial.Mode&unix.S_IFMT != unix.S_IFDIR || initial.Uid != 0 ||
			initial.Mode&0o777 != 0o700 || initial.Dev != assignedOuter.Dev {
			_ = directoryFile.Close()
			_ = unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR)
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("isolated SQLite workspace identity is unsafe")
		}
		if err := unix.Fchown(directoryFD, int(uid), int(gid)); err != nil ||
			unix.Fchmod(directoryFD, 0o700) != nil {
			_ = directoryFile.Close()
			_ = unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR)
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not assign isolated SQLite workspace")
		}
		var assigned unix.Stat_t
		if err := unix.Fstat(directoryFD, &assigned); err != nil ||
			assigned.Mode&unix.S_IFMT != unix.S_IFDIR || assigned.Uid != uid ||
			assigned.Gid != gid || assigned.Mode&0o777 != 0o700 ||
			assigned.Dev != assignedOuter.Dev {
			_ = directoryFile.Close()
			_ = unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR)
			_ = outerFile.Close()
			_ = unix.Unlinkat(rootFD, outerName, unix.AT_REMOVEDIR)
			_ = rootFile.Close()
			return nil, errors.New("could not verify isolated SQLite workspace")
		}
		return &ownerSnapshotWorkspace{
			root: rootFile, outer: outerFile, directory: directoryFile, outerName: outerName,
			outerDevice: uint64(assignedOuter.Dev), outerInode: assignedOuter.Ino,
			device: uint64(assigned.Dev), inode: assigned.Ino,
		}, nil
	}
	_ = rootFile.Close()
	return nil, errors.New("could not allocate isolated SQLite workspace")
}

func validateOwnerWorkspaceRoot(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 ||
		stat.Mode&0o7777 != uint32(unix.S_ISVTX)|0o777 {
		return errors.New("isolated SQLite workspace root must be root-owned mode 01777")
	}
	return nil
}

func (workspace *ownerSnapshotWorkspace) remove() error {
	if workspace == nil || workspace.root == nil || workspace.outer == nil ||
		workspace.directory == nil {
		return errors.New("isolated SQLite workspace is not pinned")
	}
	defer workspace.root.Close()
	defer workspace.outer.Close()
	defer workspace.directory.Close()
	rootFD := int(workspace.root.Fd())
	outerFD := int(workspace.outer.Fd())
	directoryFD := int(workspace.directory.Fd())

	var outerPinned unix.Stat_t
	if err := unix.Fstat(outerFD, &outerPinned); err != nil ||
		outerPinned.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(outerPinned.Dev) != workspace.outerDevice || outerPinned.Ino != workspace.outerInode {
		return errors.New("isolated SQLite workspace outer identity changed")
	}
	if err := unix.Fchown(outerFD, 0, 0); err != nil || unix.Fchmod(outerFD, 0o700) != nil {
		return errors.New("could not reclaim isolated SQLite workspace outer directory")
	}

	var pinned unix.Stat_t
	if err := unix.Fstat(directoryFD, &pinned); err != nil ||
		pinned.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(pinned.Dev) != workspace.device ||
		pinned.Ino != workspace.inode {
		return errors.New("isolated SQLite workspace identity changed")
	}
	if err := unix.Fchown(directoryFD, 0, 0); err != nil || unix.Fchmod(directoryFD, 0o700) != nil {
		return errors.New("could not reclaim isolated SQLite workspace")
	}
	entryCount := 0
	if err := removeOwnerWorkspaceContents(directoryFD, 0, &entryCount); err != nil {
		return err
	}

	var linkedStage unix.Stat_t
	if err := unix.Fstatat(outerFD, ownerSnapshotStageName, &linkedStage, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		linkedStage.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(linkedStage.Dev) != workspace.device || linkedStage.Ino != workspace.inode {
		return errors.New("isolated SQLite workspace stage link changed")
	}
	if err := unix.Unlinkat(outerFD, ownerSnapshotStageName, unix.AT_REMOVEDIR); err != nil {
		return errors.New("could not remove isolated SQLite workspace stage")
	}

	var linkedOuter unix.Stat_t
	if err := unix.Fstatat(rootFD, workspace.outerName, &linkedOuter, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		linkedOuter.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(linkedOuter.Dev) != workspace.outerDevice || linkedOuter.Ino != workspace.outerInode {
		return errors.New("isolated SQLite workspace outer link changed")
	}
	if err := unix.Unlinkat(rootFD, workspace.outerName, unix.AT_REMOVEDIR); err != nil {
		return errors.New("could not remove isolated SQLite workspace outer directory")
	}
	return nil
}

func removeOwnerWorkspaceContents(directoryFD int, depth int, entryCount *int) error {
	if depth > ownerSnapshotCleanupMaxDepth || entryCount == nil {
		return errors.New("isolated SQLite workspace cleanup limit exceeded")
	}
	duplicateFD, err := unix.Dup(directoryFD)
	if err != nil {
		return errors.New("could not inspect isolated SQLite workspace")
	}
	directory := os.NewFile(uintptr(duplicateFD), "system-sqlite-workspace-cleanup")
	if directory == nil {
		_ = unix.Close(duplicateFD)
		return errors.New("could not inspect isolated SQLite workspace")
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("could not inspect isolated SQLite workspace")
	}
	for _, name := range names {
		(*entryCount)++
		if *entryCount > ownerSnapshotCleanupMaxEntries {
			return errors.New("isolated SQLite workspace cleanup limit exceeded")
		}
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return errors.New("isolated SQLite workspace contains an unsafe entry")
		}
		var entry unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return errors.New("could not verify isolated SQLite workspace entry")
		}
		if entry.Mode&unix.S_IFMT != unix.S_IFDIR {
			if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
				return errors.New("could not remove isolated SQLite workspace entry")
			}
			continue
		}
		childFD, err := unix.Openat(
			directoryFD,
			name,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			return errors.New("could not pin isolated SQLite workspace directory")
		}
		var childPinned unix.Stat_t
		if err := unix.Fstat(childFD, &childPinned); err != nil ||
			childPinned.Mode&unix.S_IFMT != unix.S_IFDIR || childPinned.Dev != entry.Dev ||
			childPinned.Ino != entry.Ino {
			_ = unix.Close(childFD)
			return errors.New("isolated SQLite workspace directory identity changed")
		}
		childErr := removeOwnerWorkspaceContents(childFD, depth+1, entryCount)
		closeErr := unix.Close(childFD)
		if childErr != nil {
			return childErr
		}
		if closeErr != nil {
			return errors.New("could not close isolated SQLite workspace directory")
		}
		var linkedChild unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &linkedChild, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
			linkedChild.Mode&unix.S_IFMT != unix.S_IFDIR || linkedChild.Dev != entry.Dev ||
			linkedChild.Ino != entry.Ino {
			return errors.New("isolated SQLite workspace directory link changed")
		}
		if err := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR); err != nil {
			return errors.New("could not remove isolated SQLite workspace directory")
		}
	}
	return nil
}

func prepareOwnerWorkerWorkspace(workspace *os.File) error {
	if workspace == nil {
		return errors.New("isolated SQLite workspace descriptor is missing")
	}
	var pinned unix.Stat_t
	if err := unix.Fstat(int(workspace.Fd()), &pinned); err != nil ||
		pinned.Mode&unix.S_IFMT != unix.S_IFDIR || pinned.Uid != uint32(os.Geteuid()) ||
		pinned.Gid != uint32(os.Getegid()) || pinned.Mode&0o777 != 0o700 {
		return errors.New("isolated SQLite workspace descriptor is unsafe")
	}
	if err := unix.Fchdir(int(workspace.Fd())); err != nil {
		return errors.New("could not enter isolated SQLite workspace")
	}
	currentFD, err := unix.Open(".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("could not verify isolated SQLite workspace")
	}
	defer unix.Close(currentFD)
	var current unix.Stat_t
	if err := unix.Fstat(currentFD, &current); err != nil || current.Dev != pinned.Dev ||
		current.Ino != pinned.Ino {
		return errors.New("isolated SQLite workspace identity changed")
	}
	return nil
}
