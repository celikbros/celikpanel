//go:build linux

package bindroot

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// InspectConfigParentFD proves the vendor BIND configuration parent through
// the opened descriptor. Callers must also pin and reprove the path before a
// write; secureconfigwriter does that when this is its ParentValidator.
func InspectConfigParentFD(parentFD int, layout Layout, serviceGID uint32) (unix.Stat_t, error) {
	if layout != APT && layout != Pacman {
		return unix.Stat_t{}, errors.New("unsupported BIND config parent layout")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(parentFD, &stat); err != nil {
		return unix.Stat_t{}, fmt.Errorf("stat BIND config parent: %w", err)
	}
	allowedGID := stat.Gid == 0 || (layout == APT && stat.Gid == serviceGID)
	permissions := stat.Mode & 0o777
	special := stat.Mode & (unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX)
	accessPermissions := uint32(0o005)
	if layout == APT && stat.Gid == serviceGID {
		accessPermissions = 0o050
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || !allowedGID ||
		permissions&0o700 != 0o700 || permissions&0o022 != 0 ||
		permissions&accessPermissions != accessPermissions ||
		(special != 0 && (layout != APT || special != unix.S_ISGID)) {
		return unix.Stat_t{}, errors.New("BIND config parent directory has unsafe ownership or mode")
	}
	if err := RejectACL(parentFD, "BIND config parent directory"); err != nil {
		return unix.Stat_t{}, err
	}
	return stat, nil
}
