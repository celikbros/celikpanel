package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxManifestBytes = 1 << 20
const maxPackageEntries = 10000
const maxPackagePayloadBytes int64 = 1 << 40

func publishBackupPackage(finalPath, workDir string, manifest backupManifest) (returnErr error) {
	tmp, err := os.CreateTemp(filepath.Dir(finalPath), ".partial-*.cpbak")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if returnErr != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(manifestBytes) > maxManifestBytes {
		return errors.New("manifest too large")
	}
	if err := writeTarBytes(tw, backupManifestName, manifestBytes); err != nil {
		return err
	}
	for _, payload := range sortedPayloads(manifest) {
		filePath := filepath.Join(workDir, filepath.FromSlash(payload.Name))
		if err := writeTarPayload(tw, filePath, payload); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := atomicPublishFile(tmpPath, finalPath); err != nil {
		return err
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		_ = os.Remove(finalPath)
		return err
	}
	return syncDirectory(filepath.Dir(finalPath))
}

func writeTarBytes(writer *tar.Writer, name string, data []byte) error {
	header := &tar.Header{
		Name: name, Mode: 0o600, Size: int64(len(data)),
		Typeflag: tar.TypeReg, ModTime: backupNow().UTC(),
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(data)
	return err
}

func writeTarPayload(writer *tar.Writer, filePath string, expected manifestPayload) error {
	file, info, err := secureOpenRegular(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Size() != expected.Size {
		return errors.New("payload size changed")
	}
	header := &tar.Header{
		Name: expected.Name, Mode: 0o600, Size: expected.Size,
		Typeflag: tar.TypeReg, ModTime: backupNow().UTC(),
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hash), file)
	if err != nil {
		return err
	}
	if written != expected.Size {
		return errors.New("payload size changed")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return errors.New("payload hash changed")
	}
	return nil
}

func readBackupManifest(backupPath string) (backupManifest, error) {
	file, _, err := secureOpenRegular(backupPath)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return backupManifest{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		return backupManifest{}, err
	}
	if header.Name != backupManifestName {
		return backupManifest{}, errors.New("manifest must be first")
	}
	if header.Typeflag != tar.TypeReg {
		return backupManifest{}, errors.New("manifest is not a regular file")
	}
	if header.Size < 0 || header.Size > maxManifestBytes {
		return backupManifest{}, errors.New("manifest size is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(tr, header.Size+1))
	if err != nil {
		return backupManifest{}, err
	}
	if int64(len(data)) != header.Size {
		return backupManifest{}, errors.New("manifest is truncated")
	}
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return backupManifest{}, err
	}
	return manifest, nil
}

func unpackBackupPackage(backupPath, destination string, scope backupScope) (backupManifest, error) {
	manifest, err := readBackupManifest(backupPath)
	if err != nil {
		return backupManifest{}, err
	}
	if err := validateManifest(manifest, scope); err != nil {
		return backupManifest{}, err
	}
	if err := mkdirPrivate(destination); err != nil {
		return backupManifest{}, err
	}
	expected := make(map[string]manifestPayload)
	for _, payload := range sortedPayloads(manifest) {
		expected[payload.Name] = payload
	}

	file, _, err := secureOpenRegular(backupPath)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return backupManifest{}, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seenManifest := false
	seen := make(map[string]bool)
	entryCount := 0
	var totalSize int64
	for {
		header, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return backupManifest{}, nextErr
		}
		entryCount++
		if entryCount > maxPackageEntries {
			return backupManifest{}, errors.New("package entry limit exceeded")
		}
		if header.Typeflag != tar.TypeReg || !validPackageEntryName(header.Name) {
			return backupManifest{}, errors.New("unsafe package entry")
		}
		if header.Name == backupManifestName {
			if entryCount != 1 || seenManifest {
				return backupManifest{}, errors.New("duplicate or misplaced manifest")
			}
			seenManifest = true
			if _, err := io.Copy(io.Discard, tr); err != nil {
				return backupManifest{}, err
			}
			continue
		}
		payload, ok := expected[header.Name]
		if !ok || seen[header.Name] || header.Size != payload.Size {
			return backupManifest{}, errors.New("unexpected package payload")
		}
		if header.Size < 0 || totalSize > maxPackagePayloadBytes-header.Size {
			return backupManifest{}, errors.New("package size limit exceeded")
		}
		totalSize += header.Size
		seen[header.Name] = true
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if err := mkdirPrivate(filepath.Dir(target)); err != nil {
			return backupManifest{}, err
		}
		out, err := openPrivateExclusive(target)
		if err != nil {
			return backupManifest{}, err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(out, hash), tr)
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil {
			return backupManifest{}, copyErr
		}
		if syncErr != nil {
			return backupManifest{}, syncErr
		}
		if closeErr != nil {
			return backupManifest{}, closeErr
		}
		if written != payload.Size {
			return backupManifest{}, errors.New("payload size mismatch")
		}
		if hex.EncodeToString(hash.Sum(nil)) != payload.SHA256 {
			return backupManifest{}, errors.New("payload hash mismatch")
		}
	}
	if !seenManifest || len(seen) != len(expected) {
		return backupManifest{}, errors.New("incomplete package")
	}
	return manifest, nil
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
