//go:build !linux

package main

import "github.com/alicelik/celikpanel/internal/recoveryruntime"

func prepareRuntime(string, string) error { return recoveryruntime.ErrUnavailable }
func dispatchLauncher([]string) error     { return recoveryruntime.ErrUnavailable }
