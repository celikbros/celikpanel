package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// checkWALAwareServiceOperationsIdle proves the durable queue state from a
// private copy of the pinned database and its WAL. It works for running
// databases and stopped databases that still have a WAL. It never asks SQLite
// to open the source, so the proof cannot create or modify the source SHM file.
func checkWALAwareServiceOperationsIdle(databasePath string) error {
	return checkWALAwareServiceOperationsIdleWith(
		databasePath,
		checkServiceOperationsIdle,
	)
}

// checkWALAwarePreLedgerServiceOperationsIdle applies the exact pre-ledger
// contract to the same WAL-aware private-copy proof.
func checkWALAwarePreLedgerServiceOperationsIdle(databasePath string) error {
	return checkWALAwareServiceOperationsIdleWith(
		databasePath,
		checkPreLedgerServiceOperationsIdle,
	)
}

func checkWALAwareServiceOperationsIdleWith(
	databasePath string,
	immutableCheck func(string) error,
) (returnErr error) {
	databasePath = filepath.Clean(databasePath)
	if !filepath.IsAbs(databasePath) {
		absolutePath, err := filepath.Abs(databasePath)
		if err != nil {
			return fmt.Errorf(
				"%w: resolve live panel database path: %v",
				errServiceOperationsNotIdle,
				err,
			)
		}
		databasePath = absolutePath
	}

	pinned, err := pinWALAwarePanelDatabase(databasePath)
	if err != nil {
		return err
	}
	defer pinned.close()

	temporaryDirectory, err := createSecureLiveIdleTemporaryDirectory()
	if err != nil {
		return fmt.Errorf(
			"%w: create private live SQLite snapshot directory: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(temporaryDirectory); cleanupErr != nil {
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf(
					"%w: remove private live SQLite snapshot: %v",
					errServiceOperationsNotIdle,
					cleanupErr,
				),
			)
		}
	}()
	if err := os.Chmod(temporaryDirectory, 0o700); err != nil {
		return fmt.Errorf(
			"%w: protect private live SQLite snapshot directory: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}

	if err := pinned.verifyPath(); err != nil {
		return err
	}
	snapshotPath := filepath.Join(temporaryDirectory, filepath.Base(databasePath))
	if err := copyPinnedLiveSQLiteFile(pinned.file, pinned.info, snapshotPath); err != nil {
		return fmt.Errorf(
			"%w: copy pinned panel database: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	if wal := pinned.sidecars["-wal"]; wal != nil && wal.info.Size() != 0 {
		walSnapshotSize, err := recoverPinnedLiveSQLiteWAL(wal)
		if err != nil {
			return fmt.Errorf(
				"%w: recover pinned SQLite WAL: %v",
				errServiceOperationsNotIdle,
				err,
			)
		}
		if walSnapshotSize != 0 {
			if err := copyPinnedLiveSQLiteFilePrefix(
				wal.file,
				wal.info,
				walSnapshotSize,
				snapshotPath+"-wal",
			); err != nil {
				return fmt.Errorf(
					"%w: copy recovered SQLite WAL prefix: %v",
					errServiceOperationsNotIdle,
					err,
				)
			}
		}
	}
	if err := syncLiveIdleSnapshotDirectory(temporaryDirectory); err != nil {
		return fmt.Errorf(
			"%w: sync private live SQLite snapshot directory: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	if err := pinned.verifyPath(); err != nil {
		return err
	}

	if err := normalizeStandaloneSQLiteSnapshot(snapshotPath); err != nil {
		return fmt.Errorf(
			"%w: normalize private live SQLite snapshot: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	if err := syncLiveIdleSnapshotFile(snapshotPath); err != nil {
		return fmt.Errorf(
			"%w: sync normalized live SQLite snapshot: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	if err := syncLiveIdleSnapshotDirectory(temporaryDirectory); err != nil {
		return fmt.Errorf(
			"%w: sync normalized live SQLite snapshot directory: %v",
			errServiceOperationsNotIdle,
			err,
		)
	}
	if err := pinned.verifyPath(); err != nil {
		return err
	}
	if err := immutableCheck(snapshotPath); err != nil {
		return fmt.Errorf("validate private live SQLite snapshot: %w", err)
	}
	return pinned.verifyPath()
}

// recoverPinnedLiveSQLiteWAL reproduces SQLite's WAL recovery scan without
// opening the source database. It returns the byte length through the last
// valid commit frame. Frames after that commit are intentionally excluded from
// the private snapshot. A salt change marks an old tail left behind after WAL
// reuse; a checksum failure inside the current salt generation fails closed.
func recoverPinnedLiveSQLiteWAL(wal *pinnedSQLiteSidecar) (int64, error) {
	const (
		headerSize       = int64(32)
		frameHeaderSize  = int64(24)
		walFormatVersion = uint32(3007000)
	)
	if wal == nil || wal.file == nil || wal.info == nil || !wal.info.Mode().IsRegular() {
		return 0, fmt.Errorf("pinned SQLite WAL is not a regular file")
	}
	if wal.info.Size() < headerSize {
		return 0, fmt.Errorf("SQLite WAL is shorter than its 32-byte header")
	}
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(io.NewSectionReader(wal.file, 0, headerSize), header); err != nil {
		return 0, fmt.Errorf("read SQLite WAL header: %w", err)
	}
	magic := binary.BigEndian.Uint32(header[0:4])
	if magic != 0x377f0682 && magic != 0x377f0683 {
		return 0, fmt.Errorf("SQLite WAL has invalid magic %#x", magic)
	}
	if version := binary.BigEndian.Uint32(header[4:8]); version != walFormatVersion {
		return 0, fmt.Errorf("SQLite WAL format is %d, expected %d", version, walFormatVersion)
	}
	pageSize := binary.BigEndian.Uint32(header[8:12])
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize > 65536 || pageSize&(pageSize-1) != 0 {
		return 0, fmt.Errorf("SQLite WAL page size %d is invalid", pageSize)
	}
	var checksumOrder binary.ByteOrder = binary.LittleEndian
	if magic&1 != 0 {
		checksumOrder = binary.BigEndian
	}
	checksum := sqliteWALChecksum(checksumOrder, header[:24], [2]uint32{})
	storedHeaderChecksum := [2]uint32{
		binary.BigEndian.Uint32(header[24:28]),
		binary.BigEndian.Uint32(header[28:32]),
	}
	if checksum != storedHeaderChecksum {
		return 0, fmt.Errorf(
			"SQLite WAL header checksum is %08x/%08x, expected %08x/%08x",
			storedHeaderChecksum[0],
			storedHeaderChecksum[1],
			checksum[0],
			checksum[1],
		)
	}

	frameSize := frameHeaderSize + int64(pageSize)
	completeFrameCount := (wal.info.Size() - headerSize) / frameSize
	trailingBytes := (wal.info.Size() - headerSize) % frameSize
	frame := make([]byte, frameSize)
	walSalt := header[16:24]
	var lastCommitEnd int64
	stoppedAtOldSalt := false
	for frameIndex := int64(0); frameIndex < completeFrameCount; frameIndex++ {
		offset := headerSize + frameIndex*frameSize
		if _, err := io.ReadFull(
			io.NewSectionReader(wal.file, offset, frameSize),
			frame,
		); err != nil {
			return 0, fmt.Errorf("read SQLite WAL frame %d: %w", frameIndex+1, err)
		}
		if !bytes.Equal(frame[8:16], walSalt) {
			stoppedAtOldSalt = true
			break
		}
		if pageNumber := binary.BigEndian.Uint32(frame[0:4]); pageNumber == 0 {
			return 0, fmt.Errorf("SQLite WAL frame %d has page number zero", frameIndex+1)
		}
		calculated := sqliteWALChecksum(checksumOrder, frame[:8], checksum)
		calculated = sqliteWALChecksum(checksumOrder, frame[24:], calculated)
		stored := [2]uint32{
			binary.BigEndian.Uint32(frame[16:20]),
			binary.BigEndian.Uint32(frame[20:24]),
		}
		if calculated != stored {
			return 0, fmt.Errorf(
				"SQLite WAL frame %d checksum is %08x/%08x, expected %08x/%08x",
				frameIndex+1,
				stored[0],
				stored[1],
				calculated[0],
				calculated[1],
			)
		}
		checksum = calculated
		if binary.BigEndian.Uint32(frame[4:8]) != 0 {
			lastCommitEnd = offset + frameSize
		}
	}
	if trailingBytes != 0 && !stoppedAtOldSalt {
		return 0, fmt.Errorf(
			"SQLite WAL has an incomplete %d-byte tail",
			trailingBytes,
		)
	}
	return lastCommitEnd, nil
}

func sqliteWALChecksum(order binary.ByteOrder, data []byte, checksum [2]uint32) [2]uint32 {
	for offset := 0; offset < len(data); offset += 8 {
		checksum[0] += order.Uint32(data[offset:offset+4]) + checksum[1]
		checksum[1] += order.Uint32(data[offset+4:offset+8]) + checksum[0]
	}
	return checksum
}

func copyPinnedLiveSQLiteFile(source *os.File, sourceInfo os.FileInfo, destinationPath string) (returnErr error) {
	if sourceInfo == nil {
		return fmt.Errorf("pinned SQLite source metadata is unavailable")
	}
	return copyPinnedLiveSQLiteFilePrefix(source, sourceInfo, sourceInfo.Size(), destinationPath)
}

func copyPinnedLiveSQLiteFilePrefix(
	source *os.File,
	sourceInfo os.FileInfo,
	copySize int64,
	destinationPath string,
) (returnErr error) {
	if source == nil || sourceInfo == nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() < 0 {
		return fmt.Errorf("pinned SQLite source is not a regular file")
	}
	if copySize < 0 || copySize > sourceInfo.Size() {
		return fmt.Errorf("copy size %d is outside pinned source size %d", copySize, sourceInfo.Size())
	}
	destination, err := os.OpenFile(
		destinationPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, destination.Close())
	}()
	if err := destination.Chmod(0o600); err != nil {
		return err
	}
	written, err := io.Copy(destination, io.NewSectionReader(source, 0, copySize))
	if err != nil {
		return err
	}
	if written != copySize {
		return fmt.Errorf(
			"copied %d bytes, expected exactly %d",
			written,
			copySize,
		)
	}
	return destination.Sync()
}

func syncLiveIdleSnapshotFile(path string) (returnErr error) {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, file.Close())
	}()
	return file.Sync()
}

func syncLiveIdleSnapshotDirectory(path string) (returnErr error) {
	// Windows does not expose directory fsync through os.File.Sync. The deployed
	// Unix path is synced; Windows unit tests still exercise all copy semantics.
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, directory.Close())
	}()
	return directory.Sync()
}
