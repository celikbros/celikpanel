//go:build linux

package main

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const maxAdminCredentialsFileBytes int64 = 4096

func readAdminCredentialsFile(file *os.File) (adminCredentials, error) {
	if file == nil {
		return adminCredentials{}, errors.New("admin credentials input is unsafe")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return adminCredentials{}, errors.New("admin credentials input is unsafe")
	}

	var content []byte
	var err error
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		content, err = readAdminCredentialsFileContentForUID(file, 0)
	case unix.S_IFIFO:
		content, err = readBoundedAdminCredentialsStream(file)
	default:
		err = errors.New("admin credentials input is unsafe")
	}
	if err != nil {
		return adminCredentials{}, err
	}
	return parseAdminCredentialsJSON(content)
}

func readBoundedAdminCredentialsStream(file *os.File) ([]byte, error) {
	return readBoundedInheritedStream(file, maxAdminCredentialsFileBytes)
}

// readBoundedInheritedStream is the pipe half of the inherited-stdin discipline.
// It is shared with the control-plane backup key, which needs the same rules
// with a much smaller cap; the admin-credentials behaviour is unchanged.
// readBoundedInheritedStream, devralınan stdin disiplininin boru yarısıdır.
func readBoundedInheritedStream(file *os.File, maxBytes int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(content) < 1 || int64(len(content)) > maxBytes {
		return nil, errors.New("admin credentials input is unsafe")
	}
	return content, nil
}

func readAdminCredentialsFileContentForUID(file *os.File, expectedUID uint32) ([]byte, error) {
	return readInheritedRootOnlyFileContent(file, expectedUID, maxAdminCredentialsFileBytes)
}

// readInheritedRootOnlyFileContent is the regular-file half of the
// inherited-stdin discipline: the descriptor must be a single-link 0600 regular
// file owned by the expected uid, bounded in size, and unchanged across the
// read. It is shared with the control-plane backup key, which needs the same
// rules with a much smaller cap; the admin-credentials behaviour is unchanged.
// readInheritedRootOnlyFileContent, devralınan stdin disiplininin düzenli dosya
// yarısıdır ve kontrol düzlemi yedek anahtarıyla paylaşılır.
func readInheritedRootOnlyFileContent(
	file *os.File,
	expectedUID uint32,
	maxBytes int64,
) ([]byte, error) {
	if file == nil {
		return nil, errors.New("admin credentials file is unsafe")
	}

	var before unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return nil, errors.New("admin credentials file is unsafe")
	}
	if err := validateInheritedRootOnlyFileMetadata(before, expectedUID, maxBytes); err != nil {
		return nil, err
	}

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || int64(len(content)) != before.Size {
		return nil, errors.New("admin credentials file is unsafe")
	}

	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil {
		return nil, errors.New("admin credentials file is unsafe")
	}
	if err := validateInheritedRootOnlyFileMetadata(after, expectedUID, maxBytes); err != nil {
		return nil, err
	}
	if !sameAdminCredentialsFileStat(before, after) {
		return nil, errors.New("admin credentials file changed while being read")
	}
	return content, nil
}

func validateAdminCredentialsFileMetadata(stat unix.Stat_t, expectedUID uint32) error {
	return validateInheritedRootOnlyFileMetadata(stat, expectedUID, maxAdminCredentialsFileBytes)
}

func validateInheritedRootOnlyFileMetadata(
	stat unix.Stat_t,
	expectedUID uint32,
	maxBytes int64,
) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG ||
		stat.Uid != expectedUID ||
		stat.Mode&0o7777 != 0o600 ||
		stat.Nlink != 1 ||
		stat.Size < 1 || stat.Size > maxBytes {
		return errors.New("admin credentials file is unsafe")
	}
	return nil
}

func sameAdminCredentialsFileStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev &&
		left.Ino == right.Ino &&
		left.Mode == right.Mode &&
		left.Nlink == right.Nlink &&
		left.Uid == right.Uid &&
		left.Gid == right.Gid &&
		left.Size == right.Size &&
		left.Mtim == right.Mtim &&
		left.Ctim == right.Ctim
}
