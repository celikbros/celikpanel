//go:build linux

package binddns

import "golang.org/x/sys/unix"

func renameNoReplace(oldName, newName string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldName, unix.AT_FDCWD, newName, unix.RENAME_NOREPLACE)
}
