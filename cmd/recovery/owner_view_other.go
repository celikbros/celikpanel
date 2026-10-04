//go:build !linux

package main

func runOwnerView([]string) int { return exitUnavailable }
