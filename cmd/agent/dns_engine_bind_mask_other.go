//go:build !linux

package main

import "errors"

func verifyBINDMaskParentMetadata() error {
	return errors.New("exact BIND mask parent proof requires Linux")
}

func verifyBINDPersistentMaskFiles() error {
	return errors.New("exact persistent BIND mask proof requires Linux")
}

func verifyExactPersistentServiceMask(string) error {
	return errors.New("exact persistent service mask proof requires Linux")
}
