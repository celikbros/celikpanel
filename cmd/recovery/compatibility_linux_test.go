//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecoveryCompatibilitySubprocessHelper(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "compatibility-env-helper":
		if os.Getenv("CELIKPANEL_DATA_DIR") != "/var/lib/celikpanel" || os.Getenv("CELIKPANEL_MUTATION_LOCK_FD") != "" || os.Getenv("COMPATIBILITY_PRIVATE_POISON") != "" || os.Getenv("HOME") != "/root" {
			os.Exit(13)
		}
		if cwd, err := os.Getwd(); err != nil || cwd != "/" {
			os.Exit(14)
		}
		os.Exit(0)
	case "compatibility-error-helper":
		fmt.Fprintln(os.Stdout, "private checker DB contents")
		fmt.Fprintln(os.Stderr, "private checker path")
		os.Exit(15)
	case "compatibility-hang-helper":
		for {
			time.Sleep(time.Hour)
		}
	}
}

func TestRecoveryCompatibilityNativeSubprocessEnvironmentDeadlineAndRedaction(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPATIBILITY_PRIVATE_POISON", "secret")
	t.Setenv("CELIKPANEL_MUTATION_LOCK_FD", "9")
	args := func(mode string) []string {
		return []string{"-test.run=^TestRecoveryCompatibilitySubprocessHelper$", "--", mode}
	}
	if err := runRecoveryCompatibilityCommand(context.Background(), executable, args("compatibility-env-helper"), recoveryCompatibilityEnvironment()); err != nil {
		t.Fatalf("fixed child environment failed: %v", err)
	}
	err = runRecoveryCompatibilityCommand(context.Background(), executable, args("compatibility-error-helper"), recoveryCompatibilityEnvironment())
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("child error leaked output or was accepted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = runRecoveryCompatibilityCommand(ctx, executable, args("compatibility-hang-helper"), recoveryCompatibilityEnvironment())
	if err == nil || ctx.Err() != context.DeadlineExceeded || time.Since(started) > 3*time.Second {
		t.Fatalf("unbounded checker execution: err=%v context=%v duration=%v", err, ctx.Err(), time.Since(started))
	}
}
