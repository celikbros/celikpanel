//go:build !linux

package main

import (
	"fmt"
	"io"
)

func runOwnerPDNSAdoptionInverse(_ []string, _ int, _, diagnostic io.Writer) int {
	fmt.Fprintln(diagnostic, "The PowerDNS adoption recovery command requires a supported Linux installation.")
	return exitUnavailable
}

func runOwnerBINDSwitchInverse(_ []string, _ int, _, diagnostic io.Writer) int {
	fmt.Fprintln(diagnostic, "The BIND switch recovery command requires a supported Linux installation.")
	return exitUnavailable
}

func runOwnerBINDAdoptionInverse(_ []string, _ int, _, diagnostic io.Writer) int {
	fmt.Fprintln(diagnostic, "The running BIND adoption recovery command requires a supported Linux installation.")
	return exitUnavailable
}
