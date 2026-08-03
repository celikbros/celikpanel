//go:build !linux

package main

import (
	"fmt"
	"os"
)

func applyWordPressOwnership(_, _ string) error {
	return fmt.Errorf("WordPress installation is supported only on Linux")
}

func requireWordPressStagingParent(_ string) error {
	return fmt.Errorf("WordPress installation is supported only on Linux")
}

func prepareWordPressPathExchange(_, _ string) (wordpressPathExchange, error) {
	return nil, fmt.Errorf("WordPress installation is supported only on Linux")
}

func readWordPressPlaceholder(_ string, _ int64) ([]byte, os.FileInfo, error) {
	return nil, nil, fmt.Errorf("WordPress installation is supported only on Linux")
}

func syncWordPressTree(_ string) error {
	return fmt.Errorf("WordPress installation is supported only on Linux")
}

func syncWordPressDirectories(_ ...string) error {
	return fmt.Errorf("WordPress installation is supported only on Linux")
}
