//go:build !linux

package main

import "github.com/alicelik/celikpanel/internal/recoveryruntime"

func runDatabaseAction(command, snapshot string) (string, error) {
	return "", recoveryruntime.ErrUnavailable
}

func runDatabaseProbe() error { return recoveryruntime.ErrUnavailable }
