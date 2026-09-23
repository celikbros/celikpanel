//go:build linux

package main

import "github.com/alicelik/celikpanel/internal/processidentity"

func serviceMutationProcessStartIdentity(pid int) (string, error) {
	return processidentity.StartToken(pid)
}

func serviceMutationWorkerMatches(pid int, started string) bool {
	matches, err := processidentity.Matches(pid, started)
	return err == nil && matches
}
