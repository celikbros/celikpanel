//go:build !linux

package main

import "io"

func runDNSSwitchStatus(_ []string, _ int, _, _ io.Writer) int { return exitUnavailable }
