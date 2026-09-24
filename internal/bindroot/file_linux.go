//go:build linux

package bindroot

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

const exactRootFileMaxSize = 64 << 10

// FileIdentity binds an exact root-owned file to its observed bytes.
type FileIdentity struct {
	Device uint64
	Inode  uint64
	Size   int64
	GID    uint32
	Digest [32]byte
}

// ReadExactRootOwnedFileAt refuses symlink traversal, unsafe ancestors,
// unexpected ownership or mode, ACLs, hard links and changes during reading.
func ReadExactRootOwnedFileAt(
	rootFD int,
	absolutePath string,
	label string,
) ([]byte, FileIdentity, error) {
	return readExactFileAt(rootFD, absolutePath, label, 0o644, []uint32{0})
}

// ReadExactBINDConfigAt uses the same no-follow descriptor reader for the
// certified native config paths and package owner modes. It does not parse
// or alter BIND configuration.
func ReadExactBINDConfigAt(rootFD int, layout Layout, serviceGID uint32, absolutePath string) ([]byte, FileIdentity, error) {
	if serviceGID == 0 {
		return nil, FileIdentity{}, errors.New("BIND config service group is unknown")
	}
	switch layout {
	case APT:
		if absolutePath != "/etc/bind/named.conf" && absolutePath != "/etc/bind/named.conf.options" && absolutePath != "/etc/bind/named.conf.local" {
			return nil, FileIdentity{}, errors.New("unsupported APT BIND config path")
		}
		return readExactFileAt(rootFD, absolutePath, "APT BIND config", 0o644, []uint32{0, serviceGID})
	case Pacman:
		if absolutePath != "/etc/named.conf" {
			return nil, FileIdentity{}, errors.New("unsupported pacman BIND config path")
		}
		return readExactFileAt(rootFD, absolutePath, "pacman BIND config", 0o640, []uint32{serviceGID})
	default:
		return nil, FileIdentity{}, errors.New("unsupported BIND config layout")
	}
}

func readExactFileAt(
	rootFD int,
	absolutePath string,
	label string,
	mode uint32,
	allowedGIDs []uint32,
) ([]byte, FileIdentity, error) {
	if !path.IsAbs(absolutePath) || path.Clean(absolutePath) != absolutePath ||
		absolutePath == "/" {
		return nil, FileIdentity{}, fmt.Errorf("%s path is not canonical", label)
	}
	if _, err := ValidateInheritedAnchor(
		rootFD, "BIND vendor filesystem root",
	); err != nil {
		return nil, FileIdentity{}, err
	}
	components := strings.Split(strings.TrimPrefix(absolutePath, "/"), "/")
	if len(components) < 2 {
		return nil, FileIdentity{}, fmt.Errorf("%s path is incomplete", label)
	}
	currentFD, err := unix.FcntlInt(uintptr(rootFD), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, FileIdentity{}, fmt.Errorf("duplicate BIND vendor root: %w", err)
	}
	defer unix.Close(currentFD)
	for _, component := range components[:len(components)-1] {
		// Vendor unit directories (/lib, /usr/lib, /etc/systemd/...) are
		// distribution-owned ancestors, not directories this product created.
		nextFD, _, openErr := OpenInheritedAnchorAt(
			currentFD, component,
			path.Join("/", strings.Join(components[:len(components)-1], "/")),
		)
		if openErr != nil {
			return nil, FileIdentity{}, openErr
		}
		unix.Close(currentFD)
		currentFD = nextFD
	}
	leaf := components[len(components)-1]
	fd, err := unix.Openat2(currentFD, leaf, &unix.OpenHow{
		Flags: uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		return nil, FileIdentity{}, fmt.Errorf("%s requires Linux openat2: %w", label, err)
	}
	if err != nil {
		return nil, FileIdentity{}, fmt.Errorf("open %s: %w", label, err)
	}
	file := os.NewFile(uintptr(fd), absolutePath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, FileIdentity{}, fmt.Errorf("open %s handle", label)
	}
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, FileIdentity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	allowedGID := false
	for _, gid := range allowedGIDs {
		if before.Gid == gid {
			allowedGID = true
		}
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || !allowedGID ||
		before.Mode&0o7777 != mode || before.Nlink != 1 ||
		before.Size < 1 || before.Size > exactRootFileMaxSize {
		return nil, FileIdentity{},
			fmt.Errorf("%s is not an exact root-owned single-link regular file with the certified group and mode", label)
	}
	if err := RejectACL(fd, label); err != nil {
		return nil, FileIdentity{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, exactRootFileMaxSize+1))
	if err != nil {
		return nil, FileIdentity{}, fmt.Errorf("read %s: %w", label, err)
	}
	if len(data) == 0 || len(data) > exactRootFileMaxSize || int64(len(data)) != before.Size {
		return nil, FileIdentity{}, fmt.Errorf("%s size changed while reading", label)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, FileIdentity{}, fmt.Errorf("restat %s: %w", label, err)
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size ||
		before.Uid != after.Uid || before.Gid != after.Gid || before.Mode != after.Mode ||
		before.Nlink != after.Nlink || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, FileIdentity{}, fmt.Errorf("%s changed while reading", label)
	}
	return data, FileIdentity{
		Device: uint64(after.Dev), Inode: after.Ino, Size: after.Size, GID: after.Gid,
		Digest: sha256.Sum256(data),
	}, nil
}
