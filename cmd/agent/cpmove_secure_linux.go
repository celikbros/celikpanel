//go:build linux

package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const maxCpmoveEntries = 1_000_000

// secureExtractCpmoveFiles performs every lookup relative to directory file
// descriptors protected by openat2. The privileged agent never accepts a
// caller-selected target path, follows an existing link, or truncates a live
// inode in place.
func secureExtractCpmoveFiles(
	tarReader *tar.Reader,
	targetRoot string,
	resp *CpmoveExtractResponse,
) error {
	if !path.IsAbs(targetRoot) || path.Clean(targetRoot) != targetRoot {
		return errors.New("invalid cpmove target root")
	}
	slashFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	rootFD, err := secureMkdirAllAt(slashFD, strings.TrimPrefix(targetRoot, "/"))
	unix.Close(slashFD)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	const prefix = "homedir/public_html/"
	var extractedBytes int64
	for entries := 0; ; entries++ {
		if entries >= maxCpmoveEntries {
			return errors.New("cpmove archive contains too many entries")
		}
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("archive read failed: %w", err)
		}
		relativeArchiveName := cpmoveRel(header.Name)
		if !strings.HasPrefix(relativeArchiveName, prefix) {
			continue
		}
		relativeName, err := hostingpath.NormalizeRelativePath(
			strings.TrimPrefix(relativeArchiveName, prefix),
		)
		if err != nil || relativeName == "." {
			return errors.New("cpmove archive contains an unsafe site path")
		}
		mode := uint32(header.Mode) & 0o777
		switch header.Typeflag {
		case tar.TypeDir:
			dirFD, err := secureMkdirAllAt(rootFD, relativeName)
			if err != nil {
				return err
			}
			if err := unix.Fchmod(dirFD, mode|0o700); err != nil {
				unix.Close(dirFD)
				return err
			}
			if err := unix.Close(dirFD); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxRestoredFileBytes {
				return errors.New("cpmove archive file exceeds extraction limit")
			}
			extractedBytes += header.Size
			if extractedBytes < 0 || extractedBytes > maxRestoredTotal {
				return errors.New("cpmove archive expands beyond extraction limit")
			}
			parentFD, leaf, err := openParent(rootFD, relativeName, true)
			if err != nil {
				return err
			}
			err = secureAtomicWriteAt(parentFD, leaf, mode, func(file *os.File) error {
				written, copyErr := io.CopyN(file, tarReader, header.Size)
				if copyErr != nil {
					return copyErr
				}
				if written != header.Size {
					return io.ErrUnexpectedEOF
				}
				return nil
			})
			unix.Close(parentFD)
			if err != nil {
				return err
			}
			resp.Files++
			resp.Bytes += header.Size
		default:
			// Link, device and special entries are intentionally ignored.
		}
	}
	return nil
}
