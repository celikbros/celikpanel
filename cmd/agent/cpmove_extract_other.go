//go:build !linux

package main

import "fmt"

func extractCpmoveFilesSecure(
	_ *CpmoveExtractRequest,
	_ *CpmoveExtractResponse,
) error {
	return fmt.Errorf("cpmove site extraction requires Linux openat2 support")
}
