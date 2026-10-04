package bindrndckey

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

const maxKeySize = 4 << 10

// HashKeyFile returns the SHA-256 of an existing regular key file. A symlink
// or any other file type is refused rather than followed.
func HashKeyFile(path string) (string, error) {
	file, _, err := openExactRegular(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return hashOpen(file)
}

func openExactRegular(path string) (*os.File, fs.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	if before.Size() > maxKeySize {
		return nil, nil, fmt.Errorf("%s is larger than an rndc key", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		file.Close()
		return nil, nil, errors.Join(fmt.Errorf("%s changed while it was opened", path), err)
	}
	return file, opened, nil
}

func hashOpen(file *os.File) (string, error) {
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maxKeySize+1))
	if err != nil {
		return "", err
	}
	if n > maxKeySize {
		return "", errors.New("rndc key grew beyond the expected size")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// RemovalOutcome is what a rollback did with a key this product created.
type RemovalOutcome string

const (
	// RemovalRemoved: the file still had the exact created content; removed.
	RemovalRemoved RemovalOutcome = "removed"
	// RemovalAlreadyAbsent: nothing to remove (an earlier run removed it).
	RemovalAlreadyAbsent RemovalOutcome = "already_absent"
	// RemovalKeptChanged: the content or file type differs; that is an owner
	// change and the file is kept.
	RemovalKeptChanged RemovalOutcome = "kept_changed"
)

// RemoveIfUnchanged removes path only when it is still a regular file whose
// content hashes to wantSHA256. Anything else is kept.
func RemoveIfUnchanged(path, wantSHA256 string) (RemovalOutcome, error) {
	if !KnownKeyPath(path) {
		return "", errors.New("BIND rndc key removal names an unknown path")
	}
	return removeIfUnchangedAt(path, wantSHA256)
}

func removeIfUnchangedAt(path, wantSHA256 string) (RemovalOutcome, error) {
	if !sha256Hex.MatchString(wantSHA256) {
		return "", errors.New("BIND rndc key removal request is invalid")
	}
	before, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return RemovalAlreadyAbsent, nil
	}
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Size() > maxKeySize {
		return RemovalKeptChanged, nil
	}
	file, opened, err := openExactRegular(path)
	if err != nil {
		return "", err
	}
	got, hashErr := hashOpen(file)
	file.Close()
	if hashErr != nil {
		return "", hashErr
	}
	if got != wantSHA256 {
		return RemovalKeptChanged, nil
	}
	again, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, again) {
		return RemovalKeptChanged, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return "", errors.Join(errors.New("rndc key still exists after removal"), err)
	}
	return RemovalRemoved, nil
}
