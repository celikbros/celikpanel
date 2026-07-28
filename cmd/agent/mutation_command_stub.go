//go:build !linux

package main

import "os/exec"

func configureServiceMutationProcessGroup(*exec.Cmd) {}
