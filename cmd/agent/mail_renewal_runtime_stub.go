//go:build !linux

package main

func prepareIndependentMailRuntime() error { return mailHostLinuxOnly() }
