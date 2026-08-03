//go:build !linux

package main

import (
	"archive/tar"
	"errors"
)

func secureExtractCpmoveFiles(
	_ *tar.Reader,
	_ string,
	_ *CpmoveExtractResponse,
) error {
	return errors.New("secure cpmove extraction is supported only on Linux")
}
