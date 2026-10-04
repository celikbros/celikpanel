//go:build linux

package bindroot

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Identity pins a directory across two no-symlink walks.
type Identity struct {
	Device uint64
	Inode  uint64
}

// ValidateInheritedAnchor accepts only a root-owned, traversable system parent
// that cannot be replaced by an unprivileged user.
func ValidateInheritedAnchor(fd int, label string) (Identity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return Identity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return Identity{}, fmt.Errorf("%s is not a directory", label)
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return Identity{}, fmt.Errorf("%s has uid:gid %d:%d, want 0:0", label, stat.Uid, stat.Gid)
	}
	permissions := stat.Mode & 0o7777
	if permissions&0o022 != 0 {
		return Identity{}, fmt.Errorf("%s has mode %04o and is group- or world-writable", label, permissions)
	}
	if permissions&0o001 == 0 {
		return Identity{}, fmt.Errorf("%s has mode %04o and is not world-traversable", label, permissions)
	}
	if stat.Mode&uint32(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return Identity{}, fmt.Errorf("%s carries setuid, setgid or sticky bits", label)
	}
	if err := RejectACL(fd, label); err != nil {
		return Identity{}, err
	}
	return Identity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func OpenInheritedAnchorAt(parentFD int, name, label string) (int, Identity, error) {
	fd, err := OpenDirectoryAt(parentFD, name, label)
	if err != nil {
		return -1, Identity{}, err
	}
	identity, err := ValidateInheritedAnchor(fd, label)
	if err != nil {
		unix.Close(fd)
		return -1, Identity{}, err
	}
	return fd, identity, nil
}

func OpenExactDirectoryAt(parentFD int, name string, uid, gid, mode uint32, label string) (int, Identity, error) {
	fd, err := OpenDirectoryAt(parentFD, name, label)
	if err != nil {
		return -1, Identity{}, err
	}
	identity, err := ValidateExactDirectory(fd, uid, gid, mode, label)
	if err != nil {
		unix.Close(fd)
		return -1, Identity{}, err
	}
	return fd, identity, nil
}

func OpenDirectoryAt(parentFD int, name, label string) (int, error) {
	if name == "" || name == "." || name == ".." {
		return -1, fmt.Errorf("%s has an invalid path component", label)
	}
	fd, err := unix.Openat2(parentFD, name, &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		return -1, fmt.Errorf("%s requires Linux openat2: %w", label, err)
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
		return -1, fmt.Errorf("%s refused a symbolic link or path escape: %w", label, err)
	}
	if err != nil {
		return -1, fmt.Errorf("open %s: %w", label, err)
	}
	return fd, nil
}

func ValidateExactDirectory(fd int, uid, gid, mode uint32, label string) (Identity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return Identity{}, fmt.Errorf("stat %s: %w", label, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return Identity{}, fmt.Errorf("%s is not a directory", label)
	}
	if stat.Uid != uid || stat.Gid != gid {
		return Identity{}, fmt.Errorf("%s has uid:gid %d:%d, want %d:%d", label, stat.Uid, stat.Gid, uid, gid)
	}
	if stat.Mode&0o7777 != mode {
		return Identity{}, fmt.Errorf("%s has mode %04o, want %04o", label, stat.Mode&0o7777, mode)
	}
	if err := RejectACL(fd, label); err != nil {
		return Identity{}, err
	}
	return Identity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func RejectACL(fd int, label string) error {
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		size, err := unix.Fgetxattr(fd, name, nil)
		if err == nil && size > 0 {
			return fmt.Errorf("%s has an unsupported POSIX ACL", label)
		}
		if err != nil && !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
			return fmt.Errorf("inspect %s ACL: %w", label, err)
		}
	}
	return nil
}
