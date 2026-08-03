//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sort"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const (
	maxBackupEntries     = 1_000_000
	maxRestoredFileBytes = int64(20 << 30)
	maxRestoredTotal     = int64(100 << 30)
)

var restoreStagingBaseDir = "/var/www/celikpanel/.restore-staging"

func openBackupBase(base string, create bool) (int, error) {
	if !path.IsAbs(base) || path.Clean(base) != base || base == "/" {
		return -1, errors.New("invalid backup base directory")
	}
	fd, err := openFileManagerRoot(base)
	if err == nil || !create || !errors.Is(err, syscall.ENOENT) {
		return fd, err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return -1, err
	}
	// openFileManagerRoot uses openat2 with NO_SYMLINKS for every component.
	// Even if MkdirAll raced with a symlink swap, the privileged operation
	// fails here instead of following the replacement.
	return openFileManagerRoot(base)
}

func openBackupScope(base, scope string, create bool) (rootFD, scopeFD int, err error) {
	if _, err := hostingpath.ValidateRelativePath(scope); err != nil {
		return -1, -1, err
	}
	rootFD, err = openBackupBase(base, create)
	if err != nil {
		return -1, -1, err
	}
	if create {
		scopeFD, err = secureMkdirAllAt(rootFD, scope)
	} else {
		scopeFD, err = openFileManagerAt(
			rootFD, scope, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
		)
	}
	if err != nil {
		unix.Close(rootFD)
		return -1, -1, err
	}
	return rootFD, scopeFD, nil
}

func secureCreateBackupFile(base, scope, name string) (*os.File, func(), error) {
	if err := hostingpath.ValidateFileName(name); err != nil {
		return nil, nil, err
	}
	rootFD, scopeFD, err := openBackupScope(base, scope, true)
	if err != nil {
		return nil, nil, err
	}
	unix.Close(rootFD)
	fd, err := openFileManagerAt(
		scopeFD, name,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW,
		0o600,
	)
	unix.Close(scopeFD)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, nil, os.ErrInvalid
	}
	cleanup := func() {
		_ = secureDeleteBackupFile(base, scope, name)
	}
	return file, cleanup, nil
}

func secureOpenBackupFile(base, scope, name string) (*os.File, int64, error) {
	if err := hostingpath.ValidateFileName(name); err != nil {
		return nil, 0, err
	}
	rootFD, scopeFD, err := openBackupScope(base, scope, false)
	if err != nil {
		return nil, 0, err
	}
	unix.Close(rootFD)
	defer unix.Close(scopeFD)
	fd, err := openFileManagerAt(
		scopeFD, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0,
	)
	if err != nil {
		return nil, 0, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size < 0 {
		unix.Close(fd)
		return nil, 0, os.ErrPermission
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, 0, os.ErrInvalid
	}
	return file, st.Size, nil
}

func writeTarTree(tarWriter *tar.Writer, rootFD int, relativeDir string, entries *int) error {
	dirFD, err := openFileManagerAt(
		rootFD, relativeDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dirFD), relativeDir)
	if dir == nil {
		unix.Close(dirFD)
		return os.ErrInvalid
	}
	names, readErr := dir.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		dir.Close()
		return readErr
	}
	sort.Strings(names)
	for _, name := range names {
		(*entries)++
		if *entries > maxBackupEntries {
			dir.Close()
			return errors.New("backup contains too many entries")
		}
		relativeName := path.Join(relativeDir, name)
		if _, err := hostingpath.ValidateRelativePath(relativeName); err != nil {
			dir.Close()
			return err
		}
		var before unix.Stat_t
		if err := unix.Fstatat(dirFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			dir.Close()
			return err
		}
		switch before.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			header := &tar.Header{
				Name:     relativeName,
				Typeflag: tar.TypeDir,
				Mode:     int64(before.Mode & 0o777),
				ModTime:  time.Unix(before.Mtim.Sec, before.Mtim.Nsec),
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				dir.Close()
				return err
			}
			if err := writeTarTree(tarWriter, rootFD, relativeName, entries); err != nil {
				dir.Close()
				return err
			}
		case unix.S_IFREG:
			fd, err := openFileManagerAt(
				rootFD, relativeName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0,
			)
			if err != nil {
				dir.Close()
				return err
			}
			var opened unix.Stat_t
			if err := unix.Fstat(fd, &opened); err != nil {
				unix.Close(fd)
				dir.Close()
				return err
			}
			if opened.Mode&unix.S_IFMT != unix.S_IFREG || opened.Nlink != 1 {
				unix.Close(fd)
				dir.Close()
				return os.ErrPermission
			}
			header := &tar.Header{
				Name:     relativeName,
				Typeflag: tar.TypeReg,
				Mode:     int64(opened.Mode & 0o777),
				Size:     opened.Size,
				ModTime:  time.Unix(opened.Mtim.Sec, opened.Mtim.Nsec),
			}
			if err := tarWriter.WriteHeader(header); err != nil {
				unix.Close(fd)
				dir.Close()
				return err
			}
			file := os.NewFile(uintptr(fd), relativeName)
			if file == nil {
				unix.Close(fd)
				dir.Close()
				return os.ErrInvalid
			}
			_, copyErr := io.CopyN(tarWriter, file, opened.Size)
			closeErr := file.Close()
			if copyErr != nil {
				dir.Close()
				return copyErr
			}
			if closeErr != nil {
				dir.Close()
				return closeErr
			}
		default:
			dir.Close()
			return fmt.Errorf("unsupported or unsafe filesystem entry: %s", relativeName)
		}
	}
	return dir.Close()
}

func secureCreateFilesBackup(sourceRoot, backupBase, scope, backupName string) (size int64, retErr error) {
	sourceFD, err := openFileManagerRoot(sourceRoot)
	if err != nil {
		return 0, err
	}
	defer unix.Close(sourceFD)

	file, cleanup, err := secureCreateBackupFile(backupBase, scope, backupName)
	if err != nil {
		return 0, err
	}
	keep := false
	defer func() {
		if closeErr := file.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
		if !keep {
			cleanup()
		}
	}()
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := 0
	if err := writeTarTree(tarWriter, sourceFD, ".", &entries); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return 0, err
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return 0, err
	}
	if err := gzipWriter.Close(); err != nil {
		return 0, err
	}
	if err := file.Sync(); err != nil {
		return 0, err
	}
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	keep = true
	return info.Size(), nil
}

func secureListBackupFiles(base, scope string) ([]backupFileRecord, error) {
	rootFD, scopeFD, err := openBackupScope(base, scope, false)
	if errors.Is(err, syscall.ENOENT) {
		return []backupFileRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	unix.Close(rootFD)
	dir := os.NewFile(uintptr(scopeFD), scope)
	if dir == nil {
		unix.Close(scopeFD)
		return nil, os.ErrInvalid
	}
	names, readErr := dir.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		dir.Close()
		return nil, readErr
	}
	records := make([]backupFileRecord, 0, len(names))
	for _, name := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(scopeFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			dir.Close()
			return nil, err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Size < 0 {
			dir.Close()
			return nil, fmt.Errorf("unsafe backup entry: %s", name)
		}
		records = append(records, backupFileRecord{
			Name:      name,
			Size:      st.Size,
			CreatedAt: time.Unix(st.Mtim.Sec, st.Mtim.Nsec).UTC(),
		})
	}
	if err := dir.Close(); err != nil {
		return nil, err
	}
	return records, nil
}

type restoreTarget struct {
	file *os.File
	uid  int
	gid  int
	mode uint32
}

func openRestoreTarget(rootFD int, relativeName string, mode uint32) (*restoreTarget, error) {
	parentFD, leaf, err := openParent(rootFD, relativeName, true)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFD)

	var parentStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return nil, err
	}

	// Never truncate an existing inode in place. A tenant could otherwise add
	// a hard link after the link-count check and make the privileged restore
	// overwrite a file outside the site tree. Validate and unlink the old
	// leaf, then create a fresh root-owned inode with O_EXCL.
	var oldStat unix.Stat_t
	switch err := unix.Fstatat(parentFD, leaf, &oldStat, unix.AT_SYMLINK_NOFOLLOW); {
	case err == nil:
		if oldStat.Mode&unix.S_IFMT != unix.S_IFREG || oldStat.Nlink != 1 {
			return nil, os.ErrPermission
		}
		if err := unix.Unlinkat(parentFD, leaf, 0); err != nil {
			return nil, err
		}
	case errors.Is(err, syscall.ENOENT):
		// Nothing to replace.
	default:
		return nil, err
	}

	flags := unix.O_WRONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK |
		unix.O_CREAT | unix.O_EXCL
	fd, err := openFileManagerAt(parentFD, leaf, flags, 0o600)
	if err != nil {
		return nil, err
	}

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		unix.Close(fd)
		return nil, os.ErrPermission
	}
	file := os.NewFile(uintptr(fd), relativeName)
	if file == nil {
		unix.Close(fd)
		return nil, os.ErrInvalid
	}
	return &restoreTarget{
		file: file,
		uid:  int(parentStat.Uid),
		gid:  int(parentStat.Gid),
		mode: mode & 0o777,
	}, nil
}

func restoreFilesBackupIntoEmptyRoot(targetRoot, backupBase, scope, backupName string) error {
	archive, _, err := secureOpenBackupFile(backupBase, scope, backupName)
	if err != nil {
		return err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	rootFD, err := openFileManagerRoot(targetRoot)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	var entries int
	var restoredBytes int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxBackupEntries {
			return errors.New("backup contains too many entries")
		}
		relativeName, err := hostingpath.ValidateRelativePath(header.Name)
		if err != nil {
			return fmt.Errorf("invalid archive path: %w", err)
		}
		mode := uint32(header.Mode) & 0o777
		switch header.Typeflag {
		case tar.TypeDir:
			if relativeName == "." {
				continue
			}
			dirFD, err := secureMkdirAllAt(rootFD, relativeName)
			if err != nil {
				return err
			}
			// Keep owner access while child entries are still being restored.
			if err := unix.Fchmod(dirFD, mode|0o700); err != nil {
				unix.Close(dirFD)
				return err
			}
			unix.Close(dirFD)
		case tar.TypeReg, tar.TypeRegA:
			if relativeName == "." || header.Size < 0 || header.Size > maxRestoredFileBytes {
				return errors.New("invalid archive file entry")
			}
			restoredBytes += header.Size
			if restoredBytes < 0 || restoredBytes > maxRestoredTotal {
				return errors.New("backup expands beyond restore limit")
			}
			target, err := openRestoreTarget(rootFD, relativeName, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(target.file, tarReader, header.Size)
			var restoredStat unix.Stat_t
			statErr := unix.Fstat(int(target.file.Fd()), &restoredStat)
			var ownershipErr error
			if statErr == nil &&
				(restoredStat.Mode&unix.S_IFMT != unix.S_IFREG || restoredStat.Nlink != 1) {
				statErr = os.ErrPermission
			}
			if statErr == nil {
				ownershipErr = unix.Fchown(int(target.file.Fd()), target.uid, target.gid)
			}
			var modeErr error
			if statErr == nil && ownershipErr == nil {
				modeErr = unix.Fchmod(int(target.file.Fd()), target.mode)
			}
			syncErr := target.file.Sync()
			closeErr := target.file.Close()
			if copyErr != nil {
				return copyErr
			}
			if statErr != nil {
				return statErr
			}
			if ownershipErr != nil {
				return ownershipErr
			}
			if modeErr != nil {
				return modeErr
			}
			if syncErr != nil {
				return syncErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("archive contains an unsafe entry type for %s", relativeName)
		}
	}
}

func createRestoreStage(base string, targetStat *unix.Stat_t) (baseFD int, name, stagePath string, err error) {
	baseFD, err = openBackupBase(base, true)
	if err != nil {
		return -1, "", "", err
	}
	fail := func(cause error) (int, string, string, error) {
		unix.Close(baseFD)
		return -1, "", "", cause
	}
	var baseStat unix.Stat_t
	if err := unix.Fstat(baseFD, &baseStat); err != nil {
		return fail(err)
	}
	if int(baseStat.Uid) != os.Geteuid() || baseStat.Mode&0o022 != 0 {
		return fail(errors.New("restore staging base is not private to the agent"))
	}

	for attempts := 0; attempts < 32; attempts++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return fail(err)
		}
		name = "restore-" + hex.EncodeToString(random[:])
		err = unix.Mkdirat(baseFD, name, 0o700)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		break
	}
	if err != nil {
		return fail(err)
	}
	stageFD, err := openFileManagerAt(
		baseFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		_ = unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR)
		return fail(err)
	}
	defer unix.Close(stageFD)
	if err := unix.Fchown(stageFD, int(targetStat.Uid), int(targetStat.Gid)); err != nil {
		_ = unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR)
		return fail(err)
	}
	if err := unix.Fchmod(stageFD, targetStat.Mode&0o777); err != nil {
		_ = unix.Unlinkat(baseFD, name, unix.AT_REMOVEDIR)
		return fail(err)
	}
	return baseFD, name, path.Join(base, name), nil
}

// secureRestoreFilesBackup builds a complete replacement tree in an
// agent-private staging directory on the same hosting filesystem. Only after
// every archive entry and fsync succeeds is the live document-root directory
// exchanged atomically. Any pre-swap error leaves the site untouched; any
// post-swap durability error exchanges the old tree back before returning.
func secureRestoreFilesBackup(targetRoot, backupBase, scope, backupName string) error {
	targetFD, err := openFileManagerRoot(targetRoot)
	if err != nil {
		return err
	}
	var targetStat unix.Stat_t
	if err := unix.Fstat(targetFD, &targetStat); err != nil {
		unix.Close(targetFD)
		return err
	}
	if targetStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(targetFD)
		return os.ErrPermission
	}
	unix.Close(targetFD)

	stagingFD, stageName, stagePath, err := createRestoreStage(restoreStagingBaseDir, &targetStat)
	if err != nil {
		return err
	}
	stageExists := true
	defer func() {
		if stageExists {
			_ = secureDeleteAt(stagingFD, stageName, false)
		}
		unix.Close(stagingFD)
	}()

	if err := restoreFilesBackupIntoEmptyRoot(stagePath, backupBase, scope, backupName); err != nil {
		return err
	}
	stageFD, err := openFileManagerAt(
		stagingFD, stageName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0,
	)
	if err != nil {
		return err
	}
	if err := unix.Fsync(stageFD); err != nil {
		unix.Close(stageFD)
		return err
	}
	unix.Close(stageFD)

	targetParentPath, targetLeaf := path.Split(targetRoot)
	targetParentPath = path.Clean(targetParentPath)
	if err := hostingpath.ValidateFileName(targetLeaf); err != nil {
		return err
	}
	targetParentFD, err := openFileManagerRoot(targetParentPath)
	if err != nil {
		return err
	}
	defer unix.Close(targetParentFD)
	var current unix.Stat_t
	if err := unix.Fstatat(targetParentFD, targetLeaf, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if current.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(current.Dev) != uint64(targetStat.Dev) || current.Ino != targetStat.Ino {
		return syscall.EBUSY
	}

	if err := unix.Renameat2(
		stagingFD, stageName, targetParentFD, targetLeaf, unix.RENAME_EXCHANGE,
	); err != nil {
		return fmt.Errorf("atomic document-root exchange failed: %w", err)
	}
	rollback := func(cause error) error {
		if rollbackErr := unix.Renameat2(
			stagingFD, stageName, targetParentFD, targetLeaf, unix.RENAME_EXCHANGE,
		); rollbackErr != nil {
			return fmt.Errorf("%v; restore rollback failed: %w", cause, rollbackErr)
		}
		_ = unix.Fsync(targetParentFD)
		_ = unix.Fsync(stagingFD)
		return cause
	}
	if err := unix.Fsync(targetParentFD); err != nil {
		return rollback(fmt.Errorf("fsync live document-root parent: %w", err))
	}
	if err := unix.Fsync(stagingFD); err != nil {
		return rollback(fmt.Errorf("fsync restore staging parent: %w", err))
	}

	if err := secureDeleteAt(stagingFD, stageName, false); err != nil {
		log.Printf("restore committed for %s but old tree cleanup failed: %v", targetRoot, err)
		return nil
	}
	stageExists = false
	return nil
}

func secureDeleteBackupFile(base, scope, name string) error {
	if err := hostingpath.ValidateFileName(name); err != nil {
		return err
	}
	rootFD, scopeFD, err := openBackupScope(base, scope, false)
	if err != nil {
		return err
	}
	unix.Close(rootFD)
	defer unix.Close(scopeFD)
	var st unix.Stat_t
	if err := unix.Fstatat(scopeFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 {
		return os.ErrPermission
	}
	return unix.Unlinkat(scopeFD, name, 0)
}

func secureReadBackupFile(base, scope, name string, maxBytes int64) ([]byte, int64, error) {
	file, size, err := secureOpenBackupFile(base, scope, name)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	if size > maxBytes {
		return nil, 0, errors.New("backup is too large to download through the panel")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(content)) != size || int64(len(content)) > maxBytes {
		return nil, 0, errors.New("backup changed while it was being read")
	}
	return content, size, nil
}
