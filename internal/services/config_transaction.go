package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// managedConfigMutationMu protects the complete read/derive/publish/activate
// transaction, not merely the final rename. This prevents concurrent requests
// from deriving changes from a stale snapshot or recreating a just-deleted
// pool.
var managedConfigMutationMu sync.Mutex

type managedConfigSnapshot struct {
	exists  bool
	content []byte
}

func readManagedConfig(path string) (content []byte, returnErr error) {
	expected, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if expected.Mode()&os.ModeSymlink != 0 || !expected.Mode().IsRegular() {
		return nil, fmt.Errorf("managed configuration is not a regular file: %s", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close managed configuration %s: %w", path, err))
		}
	}()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened managed configuration %s: %w", path, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return nil, fmt.Errorf("managed configuration changed while it was being opened: %s", path)
	}
	content, err = io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read managed configuration %s: %w", path, err)
	}
	return content, nil
}

func snapshotManagedConfig(path string) (managedConfigSnapshot, error) {
	content, err := readManagedConfig(path)
	if err == nil {
		return managedConfigSnapshot{exists: true, content: content}, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return managedConfigSnapshot{}, nil
	}
	return managedConfigSnapshot{}, err
}

// atomicWriteManagedConfig publishes complete bytes with fsync+rename and
// preserves the existing file's mode and ownership. It refuses symlink and
// non-regular targets rather than following them as root.
func atomicWriteManagedConfig(path string, content []byte, defaultMode os.FileMode) (returnErr error) {
	directory := filepath.Dir(path)
	base := filepath.Base(path)
	mode := defaultMode.Perm()
	var ownership managedFileOwnership

	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("managed configuration is not a regular file: %s", path)
		}
		mode = info.Mode().Perm()
		ownership, err = managedOwnershipFromInfo(info)
		if err != nil {
			return fmt.Errorf("inspect managed configuration ownership %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("inspect managed configuration %s: %w", path, err)
	}

	temporary, err := os.CreateTemp(directory, "."+base+".celikpanel-")
	if err != nil {
		return fmt.Errorf("create atomic managed configuration %s: %w", path, err)
	}
	temporaryName := temporary.Name()
	published := false
	defer func() {
		if temporary != nil {
			returnErr = errors.Join(returnErr, temporary.Close())
		}
		if !published {
			_ = os.Remove(temporaryName)
		}
	}()

	if err := applyManagedOwnership(temporary, ownership); err != nil {
		return fmt.Errorf("preserve managed configuration ownership %s: %w", path, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("preserve managed configuration mode %s: %w", path, err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write managed configuration %s: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync managed configuration %s: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close managed configuration %s: %w", path, err)
	}
	temporary = nil
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("publish managed configuration %s: %w", path, err)
	}
	published = true
	if err := syncManagedConfigDirectory(directory); err != nil {
		return fmt.Errorf("sync managed configuration directory %s: %w", directory, err)
	}
	return nil
}

func removeManagedConfigFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("managed configuration is not a regular file: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncManagedConfigDirectory(filepath.Dir(path))
}

func restoreManagedConfig(path string, snapshot managedConfigSnapshot, mode os.FileMode) error {
	if snapshot.exists {
		return atomicWriteManagedConfig(path, snapshot.content, mode)
	}
	err := removeManagedConfigFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func rollbackManagedConfig(path string, snapshot managedConfigSnapshot, mode os.FileMode,
	validate, activate func() error, cause error) error {
	if err := restoreManagedConfig(path, snapshot, mode); err != nil {
		return errors.Join(cause, fmt.Errorf("rollback restore failed: %w", err))
	}
	var validationErr, activationErr error
	if validate != nil {
		if err := validate(); err != nil {
			validationErr = fmt.Errorf("previous configuration restored but rollback validation failed: %w", err)
		}
	}
	// Activation is attempted even if validation reports an error. The exact
	// old bytes were the previously active state, and the caller must learn
	// about both failures rather than leaving the failed candidate live.
	if activate != nil {
		if err := activate(); err != nil {
			activationErr = fmt.Errorf("previous configuration restored but rollback activation failed: %w", err)
		}
	}
	if validationErr == nil && activationErr == nil {
		return errors.Join(cause, errors.New("previous configuration restored and activated"))
	}
	return errors.Join(cause, validationErr, activationErr)
}

func applyManagedConfigSnapshotLocked(path string, snapshot managedConfigSnapshot, content []byte,
	mode os.FileMode, validate, activate func() error) error {
	if err := atomicWriteManagedConfig(path, content, mode); err != nil {
		return rollbackManagedConfig(path, snapshot, mode, validate, activate,
			fmt.Errorf("publish managed configuration: %w", err))
	}
	if validate != nil {
		if err := validate(); err != nil {
			return rollbackManagedConfig(path, snapshot, mode, validate, activate,
				fmt.Errorf("configuration validation failed: %w", err))
		}
	}
	if activate != nil {
		if err := activate(); err != nil {
			return rollbackManagedConfig(path, snapshot, mode, validate, activate,
				fmt.Errorf("configuration activation failed: %w", err))
		}
	}
	return nil
}

func applyManagedConfigLocked(path string, content []byte, mode os.FileMode, validate, activate func() error) error {
	snapshot, err := snapshotManagedConfig(path)
	if err != nil {
		return fmt.Errorf("snapshot managed configuration %s: %w", path, err)
	}
	return applyManagedConfigSnapshotLocked(path, snapshot, content, mode, validate, activate)
}

func applyManagedConfig(path string, content []byte, mode os.FileMode, validate, activate func() error) error {
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()
	return applyManagedConfigLocked(path, content, mode, validate, activate)
}

func mutateManagedConfig(path string, mode os.FileMode, derive func([]byte) ([]byte, error),
	validate, activate func() error) error {
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()
	snapshot, err := snapshotManagedConfig(path)
	if err != nil {
		return fmt.Errorf("snapshot managed configuration %s: %w", path, err)
	}
	if !snapshot.exists {
		return fmt.Errorf("managed configuration does not exist: %s", path)
	}
	content, err := derive(append([]byte(nil), snapshot.content...))
	if err != nil {
		return err
	}
	return applyManagedConfigSnapshotLocked(path, snapshot, content, mode, validate, activate)
}

func createManagedConfigLocked(path string, content []byte, mode os.FileMode, validate, activate func() error) error {
	snapshot, err := snapshotManagedConfig(path)
	if err != nil {
		return fmt.Errorf("snapshot managed configuration %s: %w", path, err)
	}
	if snapshot.exists {
		return fmt.Errorf("managed configuration already exists: %s", path)
	}
	return applyManagedConfigSnapshotLocked(path, snapshot, content, mode, validate, activate)
}

func createManagedConfig(path string, content []byte, mode os.FileMode, validate, activate func() error) error {
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()
	return createManagedConfigLocked(path, content, mode, validate, activate)
}

func deleteManagedConfigLocked(path string, mode os.FileMode, validate, activate func() error) error {
	snapshot, err := snapshotManagedConfig(path)
	if err != nil {
		return fmt.Errorf("snapshot managed configuration %s: %w", path, err)
	}
	if !snapshot.exists {
		return os.ErrNotExist
	}
	if err := removeManagedConfigFile(path); err != nil {
		return rollbackManagedConfig(path, snapshot, mode, validate, activate,
			fmt.Errorf("delete managed configuration: %w", err))
	}
	if validate != nil {
		if err := validate(); err != nil {
			return rollbackManagedConfig(path, snapshot, mode, validate, activate,
				fmt.Errorf("configuration validation after deletion failed: %w", err))
		}
	}
	if activate != nil {
		if err := activate(); err != nil {
			return rollbackManagedConfig(path, snapshot, mode, validate, activate,
				fmt.Errorf("configuration activation after deletion failed: %w", err))
		}
	}
	return nil
}

func deleteManagedConfig(path string, mode os.FileMode, validate, activate func() error) error {
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()
	return deleteManagedConfigLocked(path, mode, validate, activate)
}
