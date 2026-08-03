//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const maxFullBackupManifestBytes = int64(1 << 20)

func writeFullTarTree(
	tarWriter *tar.Writer,
	rootFD int,
	relativeDir, archivePrefix string,
	entries *int,
) error {
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
		archiveName := path.Join(archivePrefix, relativeName)
		if _, err := hostingpath.ValidateRelativePath(archiveName); err != nil {
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
			if err := tarWriter.WriteHeader(&tar.Header{
				Name:     archiveName,
				Typeflag: tar.TypeDir,
				Mode:     int64(before.Mode & 0o777),
				ModTime:  time.Unix(before.Mtim.Sec, before.Mtim.Nsec),
			}); err != nil {
				dir.Close()
				return err
			}
			if err := writeFullTarTree(
				tarWriter, rootFD, relativeName, archivePrefix, entries,
			); err != nil {
				dir.Close()
				return err
			}
		case unix.S_IFREG:
			fd, err := openFileManagerAt(
				rootFD, relativeName,
				unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0,
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
			if err := tarWriter.WriteHeader(&tar.Header{
				Name:     archiveName,
				Typeflag: tar.TypeReg,
				Mode:     int64(opened.Mode & 0o777),
				Size:     opened.Size,
				ModTime:  time.Unix(opened.Mtim.Sec, opened.Mtim.Nsec),
			}); err != nil {
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

func writeFullDatabaseDump(
	ctx context.Context,
	tarWriter *tar.Writer,
	database BackupDatabaseIdentity,
) error {
	dump, err := os.CreateTemp("", "celikpanel-full-database-*.sql")
	if err != nil {
		return err
	}
	dumpName := dump.Name()
	defer os.Remove(dumpName)
	defer dump.Close()

	command, err := databaseDumpCommand(ctx, database.Name, database.Type)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stdout = dump
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf(
			"database %d dump failed: %w: %s",
			database.ID, err, boundedCommandError(&stderr),
		)
	}
	if err := dump.Sync(); err != nil {
		return err
	}
	info, err := dump.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 0 || info.Size() > maxRestoredFileBytes {
		return fmt.Errorf("database %d dump exceeds the archive limit", database.ID)
	}
	if _, err := dump.Seek(0, io.SeekStart); err != nil {
		return err
	}
	entry := fullBackupDatabaseEntry(database.Type, database.ID)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     entry,
		Typeflag: tar.TypeReg,
		Mode:     0o600,
		Size:     info.Size(),
		ModTime:  time.Now().UTC(),
	}); err != nil {
		return err
	}
	_, err = io.CopyN(tarWriter, dump, info.Size())
	return err
}

func secureCreateFullBackup(
	ctx context.Context,
	sourceRoot, backupBase, scope, backupName string,
	databases []BackupDatabaseIdentity,
) (size int64, retErr error) {
	normalized, err := normalizeFullBackupDatabases(databases)
	if err != nil {
		return 0, err
	}
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
	closeWriters := func() {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
	}

	manifestContent, err := json.Marshal(newFullBackupManifest(normalized))
	if err != nil {
		closeWriters()
		return 0, err
	}
	if int64(len(manifestContent)) > maxFullBackupManifestBytes {
		closeWriters()
		return 0, errors.New("full backup manifest is too large")
	}
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     fullBackupManifestEntry,
		Typeflag: tar.TypeReg,
		Mode:     0o600,
		Size:     int64(len(manifestContent)),
		ModTime:  time.Now().UTC(),
	}); err != nil {
		closeWriters()
		return 0, err
	}
	if _, err := tarWriter.Write(manifestContent); err != nil {
		closeWriters()
		return 0, err
	}
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:     fullBackupFilesPrefix,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
		ModTime:  time.Now().UTC(),
	}); err != nil {
		closeWriters()
		return 0, err
	}
	entries := 1
	if err := writeFullTarTree(
		tarWriter, sourceFD, ".", fullBackupFilesPrefix, &entries,
	); err != nil {
		closeWriters()
		return 0, err
	}
	for _, database := range normalized {
		entries++
		if entries > maxBackupEntries {
			closeWriters()
			return 0, errors.New("backup contains too many entries")
		}
		if err := writeFullDatabaseDump(ctx, tarWriter, database); err != nil {
			closeWriters()
			return 0, err
		}
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

func readFullBackupManifest(
	backupBase, scope, backupName string,
) (fullBackupManifest, []BackupDatabaseIdentity, error) {
	archive, _, err := secureOpenBackupFile(backupBase, scope, backupName)
	if err != nil {
		return fullBackupManifest{}, nil, err
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return fullBackupManifest{}, nil, err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for entries := 0; ; entries++ {
		if entries > maxBackupEntries {
			return fullBackupManifest{}, nil, errors.New("backup contains too many entries")
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return fullBackupManifest{}, nil, errors.New("full backup manifest is missing")
		}
		if err != nil {
			return fullBackupManifest{}, nil, err
		}
		if _, err := hostingpath.ValidateRelativePath(header.Name); err != nil {
			return fullBackupManifest{}, nil, fmt.Errorf("invalid archive path: %w", err)
		}
		if header.Name != fullBackupManifestEntry {
			continue
		}
		if header.Typeflag != tar.TypeReg ||
			header.Size < 0 ||
			header.Size > maxFullBackupManifestBytes {
			return fullBackupManifest{}, nil, errors.New("invalid full backup manifest entry")
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(content)) != header.Size {
			return fullBackupManifest{}, nil, errors.New("full backup manifest is truncated")
		}
		var manifest fullBackupManifest
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			return fullBackupManifest{}, nil, fmt.Errorf("decode full backup manifest: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return fullBackupManifest{}, nil, errors.New("full backup manifest has trailing data")
		}
		databases, err := validateFullBackupManifest(manifest)
		return manifest, databases, err
	}
}

func secureReadFullBackupDatabaseIDs(
	backupBase, scope, backupName string,
) ([]int, error) {
	_, databases, err := readFullBackupManifest(backupBase, scope, backupName)
	if err != nil {
		return nil, err
	}
	return fullBackupDatabaseIDs(databases), nil
}

func restoreDatabaseFromFullBackup(
	ctx context.Context,
	backupBase, scope, backupName string,
	database BackupDatabaseIdentity,
) error {
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
	wantEntry := fullBackupDatabaseEntry(database.Type, database.ID)
	for entries := 0; ; entries++ {
		if entries > maxBackupEntries {
			return errors.New("backup contains too many entries")
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("database %d dump is missing from full backup", database.ID)
		}
		if err != nil {
			return err
		}
		if header.Name != wantEntry {
			continue
		}
		if header.Typeflag != tar.TypeReg ||
			header.Size < 0 ||
			header.Size > maxRestoredFileBytes {
			return fmt.Errorf("database %d dump entry is invalid", database.ID)
		}
		command, err := databaseRestoreCommand(ctx, database.Name, database.Type)
		if err != nil {
			return err
		}
		var stderr bytes.Buffer
		command.Stdin = io.LimitReader(tarReader, header.Size)
		command.Stdout = io.Discard
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf(
				"database %d restore failed: %w: %s",
				database.ID, err, boundedCommandError(&stderr),
			)
		}
		return nil
	}
}

func restoreFullFiles(
	targetRoot, backupBase, scope, backupName string,
	manifest fullBackupManifest,
) error {
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

	allowedDatabases := make(map[string]struct{}, len(manifest.Databases))
	for _, database := range manifest.Databases {
		allowedDatabases[database.Entry] = struct{}{}
	}
	seenDatabases := make(map[string]struct{}, len(manifest.Databases))
	manifestSeen := false
	filesRootSeen := false
	var entries int
	var restoredBytes int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			if !manifestSeen || !filesRootSeen ||
				len(seenDatabases) != len(allowedDatabases) {
				return errors.New("full backup archive is incomplete")
			}
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxBackupEntries {
			return errors.New("backup contains too many entries")
		}
		archiveName, err := hostingpath.ValidateRelativePath(header.Name)
		if err != nil {
			return fmt.Errorf("invalid archive path: %w", err)
		}
		switch {
		case archiveName == fullBackupManifestEntry:
			if manifestSeen || header.Typeflag != tar.TypeReg {
				return errors.New("invalid duplicate full backup manifest")
			}
			manifestSeen = true
			continue
		case archiveName == fullBackupFilesPrefix:
			if filesRootSeen || header.Typeflag != tar.TypeDir {
				return errors.New("invalid full backup files root")
			}
			filesRootSeen = true
			continue
		case path.Dir(archiveName) == "databases":
			if header.Typeflag != tar.TypeReg {
				return errors.New("invalid full backup database entry")
			}
			if _, allowed := allowedDatabases[archiveName]; !allowed {
				return fmt.Errorf("unexpected database entry %s", archiveName)
			}
			if _, duplicate := seenDatabases[archiveName]; duplicate {
				return fmt.Errorf("duplicate database entry %s", archiveName)
			}
			seenDatabases[archiveName] = struct{}{}
			continue
		case len(archiveName) > len(fullBackupFilesPrefix)+1 &&
			archiveName[:len(fullBackupFilesPrefix)+1] == fullBackupFilesPrefix+"/":
			relativeName, err := hostingpath.ValidateRelativePath(
				archiveName[len(fullBackupFilesPrefix)+1:],
			)
			if err != nil || relativeName == "." {
				return errors.New("invalid full backup file path")
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
				unix.Close(dirFD)
			case tar.TypeReg, tar.TypeRegA:
				if header.Size < 0 || header.Size > maxRestoredFileBytes {
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
					(restoredStat.Mode&unix.S_IFMT != unix.S_IFREG ||
						restoredStat.Nlink != 1) {
					statErr = os.ErrPermission
				}
				if statErr == nil {
					ownershipErr = unix.Fchown(
						int(target.file.Fd()), target.uid, target.gid,
					)
				}
				var modeErr error
				if statErr == nil && ownershipErr == nil {
					modeErr = unix.Fchmod(int(target.file.Fd()), target.mode)
				}
				syncErr := target.file.Sync()
				closeErr := target.file.Close()
				for _, candidate := range []error{
					copyErr, statErr, ownershipErr, modeErr, syncErr, closeErr,
				} {
					if candidate != nil {
						return candidate
					}
				}
			default:
				return fmt.Errorf(
					"archive contains an unsafe entry type for %s", relativeName,
				)
			}
		default:
			return fmt.Errorf("unexpected full backup archive entry %s", archiveName)
		}
	}
}

func secureRestoreFullBackup(
	ctx context.Context,
	targetRoot, backupBase, scope, backupName string,
	databases []BackupDatabaseIdentity,
) error {
	manifest, manifestDatabases, err := readFullBackupManifest(
		backupBase, scope, backupName,
	)
	if err != nil {
		return err
	}
	normalized, err := normalizeFullBackupDatabases(databases)
	if err != nil {
		return err
	}
	if !sameFullBackupDatabases(manifestDatabases, normalized) {
		return errors.New("full backup database identities do not match the restore targets")
	}
	for _, database := range normalized {
		if err := restoreDatabaseFromFullBackup(
			ctx, backupBase, scope, backupName, database,
		); err != nil {
			return err
		}
	}
	return restoreFullFiles(targetRoot, backupBase, scope, backupName, manifest)
}

var _ = syscall.ENOENT
