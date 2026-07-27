//go:build !linux

package main

import "errors"

var errSSLConfinedFSUnsupported = errors.New("confined SSL filesystem access requires Linux openat2")

func secureDeleteManagedCertificateSnapshot(string, string) error {
	return errSSLConfinedFSUnsupported
}
