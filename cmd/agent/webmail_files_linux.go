//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const roundcubeArtifactLimit = 64

type roundcubeArtifactKind uint8

const (
	roundcubeFinalRetirementArtifact roundcubeArtifactKind = iota + 1
	roundcubeStageArtifact
)

type roundcubeArtifact struct {
	name string
	kind roundcubeArtifactKind
	stat unix.Stat_t
}

func ensureRoundcubeLifecycleSupported() error {
	return nil
}

func validateRoundcubeTreePath(path string) (string, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean = filepath.Clean(clean)
	if clean == string(filepath.Separator) || filepath.Dir(clean) == clean {
		return "", fmt.Errorf("unsafe Roundcube tree path: %s", path)
	}
	if err := rejectSymlinkPath(filepath.Dir(clean)); err != nil {
		return "", fmt.Errorf("unsafe Roundcube tree parent: %w", err)
	}
	return clean, nil
}

func publishRoundcubeStage(stage, final string) error {
	stage, err := validateRoundcubeTreePath(stage)
	if err != nil {
		return err
	}
	final, err = validateRoundcubeTreePath(final)
	if err != nil {
		return err
	}
	if filepath.Dir(stage) != filepath.Dir(final) {
		return fmt.Errorf("Roundcube staging and final directories must share a parent")
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		return fmt.Errorf("inspect Roundcube staging tree: %w", err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
		return fmt.Errorf("Roundcube staging path is not a real directory")
	}
	if err := reconcileRoundcubeArtifacts(final, filepath.Base(stage)); err != nil {
		return fmt.Errorf("reconcile retired Roundcube tree before publish: %w", err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("publish Roundcube tree without replacement: %w", err)
	}
	if err := roundcubeSyncParent(filepath.Dir(final)); err != nil {
		if _, rollbackErr := retireRoundcubeTree(final); rollbackErr != nil {
			return fmt.Errorf("%w; Roundcube publish rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

func retireRoundcubeTree(path string) (roundcubeRetirementResult, error) {
	var result roundcubeRetirementResult
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return result, err
	}
	parent := filepath.Dir(clean)
	base := filepath.Base(clean)
	retiredBase := roundcubeRetirementName(base)
	retired := filepath.Join(parent, retiredBase)
	parentFD, err := openRoundcubeArtifactParent(parent)
	if err != nil {
		return result, err
	}
	defer unix.Close(parentFD)

	activeStat, activeExists, err := inspectRoundcubeDirectoryAt(parentFD, base, clean)
	if err != nil {
		return result, err
	}
	reconciled, err := reconcileRoundcubeArtifactsAt(parentFD, parent, base, activeExists, "")
	result.MutationApplied = reconciled
	if err != nil {
		return result, err
	}
	if !activeExists {
		result.Removed = true
		return result, roundcubeSyncParent(parent)
	}

	if err := unix.Renameat2(parentFD, base, parentFD, retiredBase, unix.RENAME_NOREPLACE); err != nil {
		return result, fmt.Errorf("retire Roundcube tree: %w", err)
	}
	result.Removed = true
	result.MutationApplied = true
	if err := roundcubeSyncParent(parent); err != nil {
		return result, err
	}
	if _, err := removeRoundcubeDirectoryAt(parentFD, retiredBase, activeStat); err != nil {
		return result, fmt.Errorf("remove retired Roundcube tree %s: %w", retired, err)
	}
	return result, roundcubeSyncParent(parent)
}

func reconcileRoundcubeArtifacts(path, preservedName string) error {
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(clean)
	base := filepath.Base(clean)
	parentFD, err := openRoundcubeArtifactParent(parent)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	_, activeExists, err := inspectRoundcubeDirectoryAt(parentFD, base, clean)
	if err != nil {
		return err
	}
	applied, err := reconcileRoundcubeArtifactsAt(
		parentFD,
		parent,
		base,
		activeExists,
		preservedName,
	)
	if err != nil {
		return err
	}
	if applied {
		return roundcubeSyncParent(parent)
	}
	return nil
}

func roundcubeInstallStagePath(path string) (string, error) {
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(
		filepath.Dir(clean),
		roundcubeStageName(filepath.Base(clean)),
	), nil
}

func createRoundcubeInstallStage(path string) (string, error) {
	stage, err := roundcubeInstallStagePath(path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(stage)
	name := filepath.Base(stage)
	parentFD, err := openRoundcubeArtifactParent(parent)
	if err != nil {
		return "", err
	}
	defer unix.Close(parentFD)
	if err := unix.Mkdirat(parentFD, name, 0o700); err != nil {
		return "", fmt.Errorf("create Roundcube staging tree: %w", err)
	}
	if _, exists, err := inspectRoundcubeDirectoryAt(parentFD, name, stage); err != nil {
		return "", err
	} else if !exists {
		return "", fmt.Errorf("Roundcube staging tree disappeared after creation")
	}
	if err := roundcubeSyncParent(parent); err != nil {
		return "", err
	}
	return stage, nil
}

func openRoundcubeArtifactParent(parent string) (int, error) {
	parentFD, err := unix.Open(
		parent,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return -1, fmt.Errorf("open Roundcube parent directory: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(parentFD, &stat); err != nil {
		unix.Close(parentFD)
		return -1, fmt.Errorf("inspect Roundcube parent directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o022 != 0 {
		unix.Close(parentFD)
		return -1, fmt.Errorf("unsafe Roundcube artifact parent ownership or permissions: %s", parent)
	}
	return parentFD, nil
}

func reconcileRoundcubeArtifactsAt(
	parentFD int,
	parent string,
	base string,
	activeExists bool,
	preservedName string,
) (bool, error) {
	artifacts, finalRetirement, err := collectRoundcubeArtifactsAt(
		parentFD,
		parent,
		base,
		preservedName,
	)
	if err != nil {
		return false, err
	}
	if activeExists && finalRetirement != "" {
		return false, fmt.Errorf(
			"refuse conflicting Roundcube tree and retirement artifact: %s and %s",
			filepath.Join(parent, base),
			filepath.Join(parent, finalRetirement),
		)
	}

	applied := false
	for _, artifact := range artifacts {
		var removed bool
		switch artifact.kind {
		case roundcubeFinalRetirementArtifact, roundcubeStageArtifact:
			removed, err = removeRoundcubeDirectoryAt(parentFD, artifact.name, artifact.stat)
		default:
			err = fmt.Errorf("unknown Roundcube artifact kind")
		}
		applied = applied || removed
		if err != nil {
			artifactErr := fmt.Errorf(
				"remove Roundcube artifact %s: %w",
				filepath.Join(parent, artifact.name),
				err,
			)
			if !applied {
				return false, artifactErr
			}
			if syncErr := roundcubeSyncParent(parent); syncErr != nil {
				return true, fmt.Errorf("%w; sync partial artifact cleanup: %v", artifactErr, syncErr)
			}
			return true, artifactErr
		}
	}
	return applied, nil
}

func collectRoundcubeArtifactsAt(
	parentFD int,
	parent string,
	base string,
	preservedName string,
) ([]roundcubeArtifact, string, error) {
	duplicateFD, err := unix.Dup(parentFD)
	if err != nil {
		return nil, "", fmt.Errorf("duplicate Roundcube parent descriptor: %w", err)
	}
	directory := os.NewFile(uintptr(duplicateFD), "roundcube-artifact-parent")
	if directory == nil {
		unix.Close(duplicateFD)
		return nil, "", os.ErrInvalid
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, "", fmt.Errorf("scan Roundcube artifact parent: %w", readErr)
	}
	if closeErr != nil {
		return nil, "", fmt.Errorf("close Roundcube artifact scan: %w", closeErr)
	}

	artifacts := make([]roundcubeArtifact, 0)
	finalRetirement := ""
	for _, name := range names {
		if name == base || name == preservedName {
			continue
		}
		kind, owned := classifyRoundcubeArtifact(name, base)
		if !owned {
			continue
		}
		if len(artifacts) == roundcubeArtifactLimit {
			return nil, "", fmt.Errorf("too many Roundcube artifacts under %s", parent)
		}
		var stat unix.Stat_t
		if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return nil, "", fmt.Errorf("inspect Roundcube artifact %s: %w", filepath.Join(parent, name), err)
		}
		if stat.Uid != uint32(os.Geteuid()) {
			return nil, "", fmt.Errorf("refuse foreign-owned Roundcube artifact: %s", filepath.Join(parent, name))
		}
		switch kind {
		case roundcubeFinalRetirementArtifact, roundcubeStageArtifact:
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
				return nil, "", fmt.Errorf("refuse non-directory Roundcube artifact: %s", filepath.Join(parent, name))
			}
		}
		if kind == roundcubeFinalRetirementArtifact && finalRetirement == "" {
			finalRetirement = name
		}
		artifacts = append(artifacts, roundcubeArtifact{name: name, kind: kind, stat: stat})
	}
	return artifacts, finalRetirement, nil
}

func classifyRoundcubeArtifact(name, base string) (roundcubeArtifactKind, bool) {
	retirement := roundcubeRetirementName(base)
	if name == retirement || hasLowerHexSuffix(name, retirement+"-", 24) {
		return roundcubeFinalRetirementArtifact, true
	}
	stage := roundcubeStageName(base)
	if name == stage || name == "."+stage+".retired" ||
		hasLowerHexSuffix(name, "."+stage+".retired-", 24) {
		return roundcubeStageArtifact, true
	}
	return 0, false
}

func roundcubeRetirementName(base string) string {
	return "." + base + ".retired"
}

func roundcubeStageName(base string) string {
	return "." + base + ".stage"
}

func hasLowerHexSuffix(value, prefix string, length int) bool {
	return strings.HasPrefix(value, prefix) && isLowerHex(strings.TrimPrefix(value, prefix), length)
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func inspectRoundcubeDirectoryAt(parentFD int, name, path string) (unix.Stat_t, bool, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return stat, false, nil
		}
		return stat, false, fmt.Errorf("inspect Roundcube path %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return stat, false, fmt.Errorf("refuse to remove non-directory Roundcube path: %s", path)
	}
	return stat, true, nil
}

func removeRoundcubeDirectoryAt(parentFD int, name string, expected unix.Stat_t) (bool, error) {
	directoryFD, err := unix.Openat(
		parentFD,
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return false, err
	}
	var pinned unix.Stat_t
	if err := unix.Fstat(directoryFD, &pinned); err != nil {
		unix.Close(directoryFD)
		return false, err
	}
	if pinned.Mode&unix.S_IFMT != unix.S_IFDIR ||
		pinned.Dev != expected.Dev || pinned.Ino != expected.Ino {
		unix.Close(directoryFD)
		return false, fmt.Errorf("Roundcube directory identity changed")
	}

	applied, removeErr := removeRoundcubeDirectoryContents(directoryFD)
	closeErr := unix.Close(directoryFD)
	if removeErr != nil {
		return applied, removeErr
	}
	if closeErr != nil {
		return applied, closeErr
	}

	var linked unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &linked, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return applied, err
	}
	if linked.Mode&unix.S_IFMT != unix.S_IFDIR ||
		linked.Dev != expected.Dev || linked.Ino != expected.Ino {
		return applied, fmt.Errorf("Roundcube directory link changed")
	}
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil {
		return applied, err
	}
	return true, nil
}

func removeRoundcubeDirectoryContents(directoryFD int) (bool, error) {
	duplicateFD, err := unix.Dup(directoryFD)
	if err != nil {
		return false, err
	}
	directory := os.NewFile(uintptr(duplicateFD), "roundcube-retirement")
	if directory == nil {
		unix.Close(duplicateFD)
		return false, os.ErrInvalid
	}
	names, readErr := directory.Readdirnames(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}

	applied := false
	for _, name := range names {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return applied, fmt.Errorf("unsafe Roundcube directory entry")
		}
		var entry unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return applied, err
		}
		if entry.Mode&unix.S_IFMT == unix.S_IFDIR {
			removed, err := removeRoundcubeDirectoryAt(directoryFD, name, entry)
			applied = applied || removed
			if err != nil {
				return applied, err
			}
			continue
		}
		if err := unix.Unlinkat(directoryFD, name, 0); err != nil {
			return applied, err
		}
		applied = true
	}
	if err := unix.Fsync(directoryFD); err != nil {
		return applied, err
	}
	return applied, nil
}

var roundcubeSyncParent = syncRoundcubeParent

func syncRoundcubeParent(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open Roundcube parent directory: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Fsync(fd); err != nil {
		return fmt.Errorf("sync Roundcube parent directory: %w", err)
	}
	return nil
}
