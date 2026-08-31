//go:build !linux

package main

import "os"

func openReadOnlyNoAtime(path string) (*os.File, error) {
	return os.Open(path)
}
