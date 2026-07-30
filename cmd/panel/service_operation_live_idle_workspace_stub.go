//go:build !linux

package main

import "fmt"

func createSecureLiveIdleTemporaryDirectory() (string, error) {
	return "", fmt.Errorf(
		"WAL-aware service operation proof requires a platform-specific secure temporary workspace",
	)
}
