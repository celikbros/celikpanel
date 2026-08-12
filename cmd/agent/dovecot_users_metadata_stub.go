//go:build !linux

package main

import "fmt"

func readDovecotUsersFileForMutation(path string, _ bool) ([]byte, bool, error) {
	return nil, false, fmt.Errorf("dovecot passwd-file metadata validation is unavailable on this operating system: %s", path)
}

func validateDovecotUsersFileMetadata(path string, _ bool) error {
	return fmt.Errorf("dovecot passwd-file metadata validation is unavailable on this operating system: %s", path)
}
