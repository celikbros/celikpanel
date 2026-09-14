//go:build !linux

package main

import "github.com/alicelik/celikpanel/internal/recoveryruntime"

func runSelectedRuntime([]string) error { return recoveryruntime.ErrUnavailable }
func enrollRuntime(string) error        { return recoveryruntime.ErrUnavailable }
