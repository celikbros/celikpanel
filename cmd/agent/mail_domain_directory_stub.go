//go:build !linux

package main

import "fmt"

func secureEnsureMailRoot(string) error {
	return fmt.Errorf("secure mail roots require Linux openat2")
}

func quarantineMailDomainDirectory(string, int) (func() error, bool, error) {
	return nil, false, fmt.Errorf("secure mail quarantine requires Linux openat2")
}
