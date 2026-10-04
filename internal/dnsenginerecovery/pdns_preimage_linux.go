//go:build linux

package dnsenginerecovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

// InspectPDNSDatabaseFile is the shared Agent/recovery byte reader for a native
// PowerDNS database. Presence and bytes alone never authorize a mutation.
func InspectPDNSDatabaseFile(ctx context.Context, path string, allowAbsent bool) (bool, int64, string, error) {
	return inspectPDNSDatabaseFile(ctx, path, allowAbsent, 0, nil)
}

// ProbePDNSDatabasePreimage compares a native database file with the frozen
// adoption journal's byte count and digest. It never opens SQLite, changes
// ownership, or grants authority to restore the source. The caller must bind
// path to the installed host policy and hold its recovery observation locks.
func ProbePDNSDatabasePreimage(ctx context.Context, path string, size int64, digest string) error {
	return probePDNSDatabasePreimage(ctx, path, size, digest, nil)
}

func probePDNSDatabasePreimage(ctx context.Context, path string, size int64, digest string, afterRead func()) error {
	if size <= 0 || !dnsengineartifact.ValidGeneration(digest) {
		return errors.New("PowerDNS preimage request is invalid")
	}
	exists, observedSize, observedDigest, err := inspectPDNSDatabaseFile(ctx, path, false, size, afterRead)
	if err != nil {
		return err
	}
	if !exists || observedSize != size || observedDigest != digest {
		return errors.New("PowerDNS preimage bytes differ from the frozen journal")
	}
	return nil
}

func openPDNSDatabaseNoSymlinks(path string) (int, error) {
	return unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
}

func inspectPDNSDatabaseFile(ctx context.Context, path string, allowAbsent bool, expectedSize int64, afterRead func()) (bool, int64, string, error) {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || expectedSize < 0 {
		return false, 0, "", errors.New("PowerDNS database inspection request is invalid")
	}
	if err := ctx.Err(); err != nil {
		return false, 0, "", err
	}
	fd, err := openPDNSDatabaseNoSymlinks(path)
	if err != nil {
		if allowAbsent && errors.Is(err, unix.ENOENT) {
			return false, 0, "", nil
		}
		return false, 0, "", fmt.Errorf("open PowerDNS database without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return false, 0, "", fmt.Errorf("inspect PowerDNS database: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Size <= 0 ||
		(expectedSize > 0 && before.Size != expectedSize) {
		return false, 0, "", errors.New("PowerDNS database file type, link count or size is unsafe")
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return false, 0, "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			read += int64(count)
			if read > before.Size {
				return false, 0, "", errors.New("PowerDNS database grew while it was read")
			}
			_, _ = hash.Write(buffer[:count])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return false, 0, "", fmt.Errorf("read PowerDNS database: %w", readErr)
			}
			break
		}
		if count == 0 {
			return false, 0, "", errors.New("PowerDNS database read made no progress")
		}
	}
	if afterRead != nil {
		afterRead()
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return false, 0, "", fmt.Errorf("reinspect PowerDNS database: %w", err)
	}
	reopenedFD, err := openPDNSDatabaseNoSymlinks(path)
	if err != nil {
		return false, 0, "", fmt.Errorf("reopen PowerDNS database without symlinks: %w", err)
	}
	defer unix.Close(reopenedFD)
	if err := unix.Fstat(reopenedFD, &named); err != nil {
		return false, 0, "", fmt.Errorf("reinspect PowerDNS database path: %w", err)
	}
	if read != before.Size || before.Dev != after.Dev || before.Ino != after.Ino ||
		before.Size != after.Size || before.Mode != after.Mode || before.Uid != after.Uid ||
		before.Gid != after.Gid || before.Nlink != after.Nlink ||
		before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		after.Dev != named.Dev || after.Ino != named.Ino ||
		after.Size != named.Size || after.Mode != named.Mode ||
		after.Uid != named.Uid || after.Gid != named.Gid ||
		after.Mtim != named.Mtim || after.Ctim != named.Ctim || named.Nlink != 1 {
		return false, 0, "", errors.New("PowerDNS database bytes or file identity changed")
	}
	if err := ctx.Err(); err != nil {
		return false, 0, "", err
	}
	return true, read, hex.EncodeToString(hash.Sum(nil)), nil
}
