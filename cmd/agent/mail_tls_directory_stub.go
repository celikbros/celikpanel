//go:build !linux

package main

import "fmt"

func prepareDefaultMailTLSDirectory(_, _ string) (mailTLSDirectoryOwner, error) {
	return mailTLSDirectoryOwner{}, fmt.Errorf("default mail TLS directory preparation requires Linux openat2")
}
