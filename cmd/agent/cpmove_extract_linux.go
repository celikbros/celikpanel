//go:build linux

package main

import (
	"archive/tar"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const (
	maxCpmoveSiteFiles = 1_000_000
	maxCpmoveSiteBytes = int64(1 << 40)
)

var cpmoveSiteHome = hostingpath.SiteHome

func cpmovePayloadRelative(member string) (string, bool, error) {
	if member == "" || strings.ContainsRune(member, '\x00') ||
		strings.Contains(member, `\`) {
		return "", false, os.ErrPermission
	}
	raw := strings.TrimPrefix(member, "./")
	parts := strings.Split(raw, "/")
	for _, component := range parts {
		if component == ".." {
			return "", false, os.ErrPermission
		}
	}
	if len(parts) > 1 &&
		(strings.HasPrefix(parts[0], "cpmove-") || strings.HasPrefix(parts[0], "backup-")) {
		parts = parts[1:]
	}
	relative := strings.Join(parts, "/")
	if relative == "homedir/public_html" {
		return "", true, nil
	}
	const prefix = "homedir/public_html/"
	if !strings.HasPrefix(relative, prefix) {
		return "", false, nil
	}
	payload := strings.TrimPrefix(relative, prefix)
	cleaned := path.Clean(payload)
	if cleaned == "." || cleaned == ".." || path.IsAbs(cleaned) ||
		strings.HasPrefix(cleaned, "../") {
		return "", false, os.ErrPermission
	}
	return cleaned, true, nil
}

func openCpmoveSiteHome(subscriptionID, domainID int) (int, error) {
	siteHome, err := cpmoveSiteHome(subscriptionID, domainID)
	if err != nil {
		return -1, os.ErrPermission
	}
	rootFD, err := unix.Open(
		"/",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return -1, err
	}
	defer unix.Close(rootFD)
	fd, err := unix.Openat2(rootFD, strings.TrimPrefix(siteHome, "/"), &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, fmt.Errorf("open immutable site home: %w", err)
	}
	return fd, nil
}

func cpmoveRandomEntryName(prefix string) (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf(".%s-%x", prefix, token[:]), nil
}

func openOrCreateCpmoveDirectory(
	rootFD int,
	relative string,
	uid, gid int,
	mode uint32,
) (int, error) {
	currentFD, err := unix.Dup(rootFD)
	if err != nil {
		return -1, err
	}
	if relative == "" || relative == "." {
		return currentFD, nil
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			unix.Close(currentFD)
			return -1, os.ErrPermission
		}
		nextFD, openErr := unix.Openat(
			currentFD,
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if errors.Is(openErr, unix.ENOENT) {
			if err := unix.Mkdirat(currentFD, component, mode|0o700); err != nil {
				unix.Close(currentFD)
				return -1, err
			}
			nextFD, openErr = unix.Openat(
				currentFD,
				component,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0,
			)
			if openErr == nil {
				if err := unix.Fchown(nextFD, uid, gid); err != nil {
					unix.Close(nextFD)
					unix.Close(currentFD)
					return -1, err
				}
				if err := unix.Fchmod(nextFD, mode|0o700); err != nil {
					unix.Close(nextFD)
					unix.Close(currentFD)
					return -1, err
				}
			}
		}
		if openErr != nil {
			unix.Close(currentFD)
			return -1, openErr
		}
		unix.Close(currentFD)
		currentFD = nextFD
	}
	return currentFD, nil
}

func writeCpmoveRegularFile(
	stageFD int,
	relative string,
	hdr *tar.Header,
	tr *tar.Reader,
	uid, gid int,
) (int64, error) {
	parent := path.Dir(relative)
	if parent == "." {
		parent = ""
	}
	parentFD, err := openOrCreateCpmoveDirectory(stageFD, parent, uid, gid, 0o755)
	if err != nil {
		return 0, err
	}
	defer unix.Close(parentFD)

	base := path.Base(relative)
	if base == "" || base == "." || base == ".." {
		return 0, os.ErrPermission
	}
	mode := uint32(hdr.Mode) & 0o777
	if mode == 0 {
		mode = 0o644
	}
	fd, err := unix.Openat(
		parentFD,
		base,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		mode,
	)
	if err != nil {
		return 0, err
	}
	file := os.NewFile(uintptr(fd), filepath.Base(relative))
	if file == nil {
		unix.Close(fd)
		return 0, os.ErrInvalid
	}
	cleanup := true
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if cleanup {
			_ = unix.Unlinkat(parentFD, base, 0)
		}
	}()
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return 0, err
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return 0, err
	}
	written, err := io.CopyN(file, tr, hdr.Size)
	if err != nil {
		return 0, err
	}
	if written != hdr.Size {
		return 0, io.ErrUnexpectedEOF
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	if err := file.Close(); err != nil {
		file = nil
		return 0, err
	}
	file = nil
	cleanup = false
	return written, nil
}

func publishCpmoveStage(homeFD int, stageName string) error {
	publicFD, err := unix.Openat(
		homeFD,
		"public_html",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open current document root: %w", err)
	}
	unix.Close(publicFD)

	backupName, err := cpmoveRandomEntryName("cpmove-backup")
	if err != nil {
		return err
	}
	if err := unix.Renameat(homeFD, "public_html", homeFD, backupName); err != nil {
		return fmt.Errorf("stage current document root: %w", err)
	}
	rollback := func(cause error) error {
		rollbackErr := unix.Renameat(homeFD, backupName, homeFD, "public_html")
		return errors.Join(cause, rollbackErr)
	}
	if err := unix.Renameat(homeFD, stageName, homeFD, "public_html"); err != nil {
		return rollback(fmt.Errorf("publish imported document root: %w", err))
	}
	if err := unix.Fsync(homeFD); err != nil {
		_ = unix.Renameat(homeFD, "public_html", homeFD, stageName)
		return rollback(fmt.Errorf("sync imported document root: %w", err))
	}
	if err := removeSiteFileEntryAt(homeFD, backupName); err != nil {
		_ = unix.Renameat(homeFD, "public_html", homeFD, stageName)
		return rollback(fmt.Errorf("remove previous document root: %w", err))
	}
	if err := unix.Fsync(homeFD); err != nil {
		return fmt.Errorf("sync site home after import: %w", err)
	}
	return nil
}

func extractCpmoveFilesSecure(
	req *CpmoveExtractRequest,
	resp *CpmoveExtractResponse,
) error {
	if req.SubscriptionID <= 0 || req.DomainID <= 0 {
		return os.ErrPermission
	}
	tr, done, err := openCpmove(req.Path)
	if err != nil {
		return err
	}
	defer done()

	homeFD, err := openCpmoveSiteHome(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(homeFD)

	publicFD, err := unix.Openat(
		homeFD,
		"public_html",
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return fmt.Errorf("open immutable document root: %w", err)
	}
	var publicStat unix.Stat_t
	if err := unix.Fstat(publicFD, &publicStat); err != nil {
		unix.Close(publicFD)
		return err
	}
	unix.Close(publicFD)
	if publicStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return os.ErrPermission
	}

	stageName, err := cpmoveRandomEntryName("cpmove-stage")
	if err != nil {
		return err
	}
	if err := unix.Mkdirat(homeFD, stageName, 0o700); err != nil {
		return fmt.Errorf("create extraction stage: %w", err)
	}
	stageExists := true
	stageFD := -1
	defer func() {
		if stageFD >= 0 {
			_ = unix.Close(stageFD)
		}
		if stageExists {
			_ = removeSiteFileEntryAt(homeFD, stageName)
		}
	}()

	stageFD, err = unix.Openat(
		homeFD,
		stageName,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return err
	}
	if err := unix.Fchown(stageFD, int(publicStat.Uid), int(publicStat.Gid)); err != nil {
		return err
	}
	stageMode := publicStat.Mode & 0o777
	if stageMode == 0 {
		stageMode = 0o755
	}
	if err := unix.Fchmod(stageFD, stageMode|0o700); err != nil {
		return err
	}

	files := 0
	var bytes int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read cpmove archive: %w", err)
		}
		relative, payload, err := cpmovePayloadRelative(hdr.Name)
		if err != nil {
			return fmt.Errorf("unsafe cpmove member path")
		}
		if !payload || relative == "" {
			continue
		}
		if hdr.Size < 0 || hdr.Size > maxCpmoveSiteBytes-bytes {
			return fmt.Errorf("cpmove site payload exceeds the allowed size")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			dirFD, err := openOrCreateCpmoveDirectory(
				stageFD,
				relative,
				int(publicStat.Uid),
				int(publicStat.Gid),
				uint32(hdr.Mode)&0o777,
			)
			if err != nil {
				return fmt.Errorf("create imported directory: %w", err)
			}
			if err := unix.Close(dirFD); err != nil {
				return fmt.Errorf("close imported directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if files >= maxCpmoveSiteFiles {
				return fmt.Errorf("cpmove site payload contains too many files")
			}
			written, err := writeCpmoveRegularFile(
				stageFD,
				relative,
				hdr,
				tr,
				int(publicStat.Uid),
				int(publicStat.Gid),
			)
			if err != nil {
				return fmt.Errorf("write imported file: %w", err)
			}
			files++
			bytes += written
		default:
			return fmt.Errorf("unsupported cpmove site entry type")
		}
	}
	if err := unix.Fsync(stageFD); err != nil {
		return fmt.Errorf("sync extraction stage: %w", err)
	}
	if err := unix.Close(stageFD); err != nil {
		stageFD = -1
		return fmt.Errorf("close extraction stage: %w", err)
	}
	stageFD = -1
	if err := publishCpmoveStage(homeFD, stageName); err != nil {
		return err
	}
	stageExists = false
	resp.Files = files
	resp.Bytes = bytes
	resp.Complete = true
	return nil
}
