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

// ProbePDNSDatabasePreimage compares a native database file with the frozen
// adoption journal's byte count and digest. It never opens SQLite, changes
// ownership, or grants authority to restore the source. The caller must bind
// path to the installed host policy and hold its recovery observation locks.
func ProbePDNSDatabasePreimage(ctx context.Context, path string, size int64, digest string) error {
	return probePDNSDatabasePreimage(ctx, path, size, digest, nil)
}

func probePDNSDatabasePreimage(ctx context.Context, path string, size int64, digest string, afterRead func()) error {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		size <= 0 || !dnsengineartifact.ValidGeneration(digest) {
		return errors.New("PowerDNS preimage request is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("open PowerDNS preimage without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after, named unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return fmt.Errorf("inspect PowerDNS preimage: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Size != size {
		return errors.New("PowerDNS preimage file type, link count or size differs from the journal")
	}
	hash := sha256.New()
	buffer := make([]byte, 64<<10)
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			read += int64(count)
			if read > size {
				return errors.New("PowerDNS preimage grew while it was read")
			}
			_, _ = hash.Write(buffer[:count])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("read PowerDNS preimage: %w", readErr)
			}
			break
		}
		if count == 0 {
			return errors.New("PowerDNS preimage read made no progress")
		}
	}
	if afterRead != nil {
		afterRead()
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return fmt.Errorf("reinspect PowerDNS preimage: %w", err)
	}
	reopenedFD, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return fmt.Errorf("reopen PowerDNS preimage without symlinks: %w", err)
	}
	defer unix.Close(reopenedFD)
	if err := unix.Fstat(reopenedFD, &named); err != nil {
		return fmt.Errorf("reinspect PowerDNS preimage path: %w", err)
	}
	if read != size || before.Dev != after.Dev || before.Ino != after.Ino ||
		before.Size != after.Size || before.Mode != after.Mode || before.Uid != after.Uid ||
		before.Gid != after.Gid || before.Nlink != after.Nlink ||
		before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		after.Dev != named.Dev || after.Ino != named.Ino ||
		after.Size != named.Size || after.Mode != named.Mode ||
		after.Uid != named.Uid || after.Gid != named.Gid ||
		after.Mtim != named.Mtim || after.Ctim != named.Ctim || named.Nlink != 1 ||
		hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("PowerDNS preimage bytes or file identity changed")
	}
	return ctx.Err()
}
