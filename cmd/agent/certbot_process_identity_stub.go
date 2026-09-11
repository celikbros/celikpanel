//go:build !linux

package main

import "os/exec"

func configureCertbotProcessIdentity(*exec.Cmd) error { return nil }
