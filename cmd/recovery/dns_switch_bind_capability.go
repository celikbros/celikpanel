package main

import (
	"fmt"
	"io"
	"runtime"
)

const bindSourceInverseCapabilityCommand = "check-bind-source-inverse-v1"
const bindSourceInverseCapabilityMarker = "celikpanel-bind-source-inverse/v1"

// The selected recovery binary advertises this only when its installed BIND
// source inverse is compiled in. The caller independently verifies the binary.
func dispatchBINDSourceInverseCapability(args []string, uid int, out, diagnostic io.Writer) int {
	if len(args) != 1 || args[0] != bindSourceInverseCapabilityCommand {
		fmt.Fprintln(diagnostic, "Usage: recovery check-bind-source-inverse-v1")
		return exitUsage
	}
	if runtime.GOOS != "linux" {
		return exitUnavailable
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, "Root or authorized sudo is required to verify installed BIND recovery capability.")
		return exitNotOwner
	}
	if _, err := fmt.Fprintln(out, bindSourceInverseCapabilityMarker); err != nil {
		return exitOutput
	}
	return exitOK
}

const bindAdoptionInverseCapabilityCommand = "check-bind-adoption-inverse-v1"
const bindAdoptionInverseCapabilityMarker = "celikpanel-bind-adoption-inverse/v1"

// The selected recovery binary advertises this only when its installed BIND
// source inverse is compiled in. The caller independently verifies the binary.
func dispatchBINDAdoptionInverseCapability(args []string, uid int, out, diagnostic io.Writer) int {
	if len(args) != 1 || args[0] != bindAdoptionInverseCapabilityCommand {
		fmt.Fprintln(diagnostic, "Usage: recovery check-bind-adoption-inverse-v1")
		return exitUsage
	}
	if runtime.GOOS != "linux" {
		return exitUnavailable
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, "Root or authorized sudo is required to verify installed BIND recovery capability.")
		return exitNotOwner
	}
	if _, err := fmt.Fprintln(out, bindAdoptionInverseCapabilityMarker); err != nil {
		return exitOutput
	}
	return exitOK
}
