//go:build !linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func validateRoundcubeTreePath(path string) (string, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean = filepath.Clean(clean)
	if clean == string(filepath.Separator) || filepath.Dir(clean) == clean {
		return "", fmt.Errorf("unsafe Roundcube tree path: %s", path)
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
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("Roundcube destination already exists")
		}
		return err
	}
	return os.Rename(stage, final)
}

func retireRoundcubeTree(path string) error {
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refuse to remove non-directory Roundcube path: %s", clean)
	}
	return os.RemoveAll(clean)
}
