//go:build linux

package main

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

// The candidate CLI verifies the retained kit against the current fixed host
// state before quiescence. Resolving also checks this CLI's supported protocol
// and snapshot format. Acceptance does not prove a future restore succeeded.
// Aday CLI, servisler durdurulmadan once korunan kiti mevcut sabit sunucu
// durumuyla dogrular. Resolve bu CLI'nin destekledigi protokolu ve snapshot
// bicimini de denetler. Kabul, gelecekteki geri yuklemenin basardigi anlamina gelmez.
func checkRecoveryCompatibility(mode string) error {
	if err := verifyRecoveryCompatibility(mode, compatibilityDependencies{
		euid:     os.Geteuid,
		boundary: recoveryruntime.VerifyPreflightBoundary,
		resolve: func() (string, compatibilityRuntime, error) {
			selected, err := recoveryruntime.Resolve()
			if err != nil {
				return "", nil, err
			}
			return selected.Root, selected, nil
		},
		run: runRecoveryCompatibilityCommand,
	}); err != nil {
		return err
	}
	// Unsupported owner metadata must be found while management is available,
	// before quiescence. This probe cannot create publication intents or stages.
	if err := recoveryruntime.VerifyPreflightBoundary(9); err != nil {
		return err
	}
	if err := recoverypublication.ProbeCurrentResources(); err != nil {
		return err
	}
	return recoveryruntime.VerifyPreflightBoundary(9)
}

func runRecoveryCompatibilityCommand(ctx context.Context, path string, args, environment []string) error {
	command := exec.CommandContext(ctx, path, args...)
	command.Env = environment
	command.Dir = "/"
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	// FD 9 proves the parent's release boundary; it is not a service-mutation
	// lock. The read-only agent checker independently probes that latter lock.
	// FD 9 ust islemin release sinirini kanitlar; service-mutation kilidi degildir.
	// Salt-okur agent kontrolu o ikinci kilidi bagimsiz olarak yoklar.
	return command.Run()
}
