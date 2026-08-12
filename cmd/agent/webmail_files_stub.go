//go:build !linux

package main

import (
	"errors"
	"fmt"
	"path/filepath"
)

var errRoundcubeLifecycleUnsupported = errors.New(
	"Roundcube filesystem lifecycle mutations are unsupported on non-Linux hosts",
)

func ensureRoundcubeLifecycleSupported() error {
	return errRoundcubeLifecycleUnsupported
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
	return clean, nil
}

func publishRoundcubeStage(stage, final string) error {
	if _, err := validateRoundcubeTreePath(stage); err != nil {
		return err
	}
	if _, err := validateRoundcubeTreePath(final); err != nil {
		return err
	}
	return errRoundcubeLifecycleUnsupported
}

func retireRoundcubeTree(path string) (roundcubeRetirementResult, error) {
	if _, err := validateRoundcubeTreePath(path); err != nil {
		return roundcubeRetirementResult{}, err
	}
	return roundcubeRetirementResult{}, errRoundcubeLifecycleUnsupported
}

func reconcileRoundcubeArtifacts(path, _ string) error {
	if _, err := validateRoundcubeTreePath(path); err != nil {
		return err
	}
	return errRoundcubeLifecycleUnsupported
}

func roundcubeInstallStagePath(path string) (string, error) {
	clean, err := validateRoundcubeTreePath(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(clean), roundcubeStageName(filepath.Base(clean))), nil
}

func createRoundcubeInstallStage(path string) (string, error) {
	if _, err := roundcubeInstallStagePath(path); err != nil {
		return "", err
	}
	return "", errRoundcubeLifecycleUnsupported
}

func roundcubeStageName(base string) string {
	return "." + base + ".stage"
}
