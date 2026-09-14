package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

// Internal material commands accept no token or arbitrary output location.
// Only an actually absent material returns 3; invalid evidence never falls back.
func dispatchMaterial(args []string, uid int, execute func(string, recoverypublication.Request) (string, error), out io.Writer, report func(string)) int {
	if uid != 0 {
		report("Owner authentication is required. Use your root or authorized sudo session.")
		return exitNotOwner
	}
	var request recoverypublication.Request
	switch {
	case len(args) == 1 && args[0] == "verify-material-support":
	case len(args) == 3 && args[0] == "material-root" && args[1] == "--snapshot" && recoverypublication.ValidSnapshot(args[2]):
		request.Snapshot = args[2]
	case len(args) == 9 && args[0] == "prepare-recovery-material" && args[1] == "--snapshot" && args[3] == "--snapshot-manifest" && args[5] == "--candidate-root" && args[7] == "--candidate-manifest":
		request = recoverypublication.Request{Snapshot: args[2], SnapshotManifest: args[4], CandidateRoot: args[6], CandidateManifest: args[8]}
		if !recoverypublication.ValidSnapshot(request.Snapshot) || !recoverypublication.ValidManifest(request.SnapshotManifest) || !recoverypublication.ValidManifest(request.CandidateManifest) || !filepath.IsAbs(request.CandidateRoot) || filepath.Clean(request.CandidateRoot) != request.CandidateRoot {
			return exitUsage
		}
	default:
		return exitUsage
	}
	root, err := execute(args[0], request)
	if err != nil {
		if args[0] == "material-root" && errors.Is(err, recoverypublication.ErrMaterialAbsent) {
			return exitUnavailable
		}
		report("Recovery material could not be verified. Preserve this operation and its evidence; no alternative data was adopted.")
		return exitOutput
	}
	if args[0] == "material-root" {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.ContainsAny(root, "\r\n\x00") {
			return exitOutput
		}
		if _, err = fmt.Fprintln(out, root); err != nil {
			return exitOutput
		}
	}
	return exitOK
}
func runMaterial(command string, request recoverypublication.Request) (string, error) {
	selected, err := recoveryruntime.Resolve()
	if err != nil {
		return "", err
	}
	defer selected.Close()
	if err = selected.VerifyExecutingBinary(); err != nil {
		return "", err
	}
	var root string
	switch command {
	case "verify-material-support":
	case "prepare-recovery-material":
		err = recoverypublication.PrepareRecoveryMaterial(request)
	case "material-root":
		root, err = recoverypublication.VerifyRecoveryMaterial(request.Snapshot)
	default:
		return "", recoveryruntime.ErrUnavailable
	}
	// Recheck the selected program even for the distinct absent result.
	if proof := selected.Revalidate(); proof != nil {
		return "", proof
	}
	return root, err
}
