//go:build !linux

package main

func checkRecoveryCompatibility(string) error { return errRecoveryCompatibility }
