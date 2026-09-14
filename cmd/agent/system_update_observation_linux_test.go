//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

func TestSystemUpdateWorkerObservationRequiresActualFinalProof(t *testing.T) {
	for _, test := range []struct {
		name                       string
		installerError, proofError error
		want                       string
	}{
		{name: "verified", want: "succeeded:update_verified:update_verified"},
		{name: "installer failure", installerError: errors.New("private-path/token must not enter observation"), want: "failed:none:update_failed"},
		{name: "proof unavailable", proofError: errors.New("native proof missing"), want: "failed:none:update_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := linuxSystemUpdateTestRoot(t)
			state := linuxSystemUpdateTestState(strings.Repeat("a", 32))
			if err := writeSystemUpdateState(root, state, nil); err != nil {
				t.Fatal(err)
			}
			manifest := systemUpdateManifest{Sequence: state.TargetSequence, Version: state.TargetVersion, Commit: state.TargetCommit, PublishedAt: "2026-08-12T12:00:00Z", OS: state.TargetOS, Arch: state.TargetArch, Archive: "celikpanel-" + state.TargetVersion + "-linux-amd64.tar.gz", ArchiveSHA256: state.TargetArchiveSHA256, ArchiveSize: state.TargetArchiveSize}
			backend := newLinuxSystemUpdateBackend()
			backend.stateRoot, backend.floorPath = root, filepath.Join(root, "sequence.floor")
			backend.now = func() time.Time { return time.Date(2026, 8, 12, 12, 2, 0, 0, time.UTC) }
			if err := os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: "41", Version: "v1.2.2"}), 0o600); err != nil {
				t.Fatal(err)
			}
			backend.runInstaller = func(context.Context, *systemUpdateState, string) error {
				if test.installerError != nil {
					return test.installerError
				}
				return os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: state.TargetSequence, Version: state.TargetVersion}), 0o600)
			}
			backend.installedBuild = func(context.Context) (string, string, error) { return state.TargetVersion, state.TargetCommit, nil }
			verified := false
			backend.finalProof = func(context.Context, *systemUpdateState) error {
				verified = test.proofError == nil
				return test.proofError
			}
			var observations []string
			backend.observe = func(observed *systemUpdateState, phase, proof, reason string) {
				// A rejected observer publication is deliberately ignored by the
				// callback, exactly as production does. Invalid identity is checked
				// before any production path can be opened by Publish.
				if err := recoveryobs.Publish(recoveryobs.Record{RequestID: "invalid"}); err == nil {
					t.Fatal("invalid observer publication unexpectedly succeeded")
				}
				if observed.RequestID != state.RequestID || observed.TargetCommit != state.TargetCommit {
					t.Fatal("observer received another operation identity")
				}
				if proof != "none" && !verified {
					t.Fatal("success observation preceded actual final proof")
				}
				observations = append(observations, phase+":"+proof+":"+reason)
			}
			err := backend.RunWorker(context.Background(), state.RequestID, &fakeSystemUpdateFetcher{manifest: manifest})
			if (err != nil) != (test.installerError != nil || test.proofError != nil) {
				t.Fatalf("observer changed worker result: %v", err)
			}
			if len(observations) < 2 || observations[0] != "running:none:update_running" || observations[len(observations)-1] != test.want {
				t.Fatalf("observations = %v", observations)
			}
		})
	}
}
