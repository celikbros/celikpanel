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
)

func TestSystemUpdateReconcileClosesObservationPublicationCrash(t *testing.T) {
	for _, tc := range []struct {
		name, initial string
		startup       bool
		proofError    error
		want          string
	}{
		{name: "crash before private success", initial: systemUpdateRunning, want: "succeeded:update_verified"},
		{name: "recovered previous failure", initial: systemUpdateFailed, want: "succeeded:update_verified"},
		{name: "crash after private success", initial: systemUpdateSucceeded, want: "succeeded:update_verified"},
		{name: "startup after private success", initial: systemUpdateSucceeded, startup: true, want: "succeeded:update_verified"},
		{name: "success cannot be reproved", initial: systemUpdateSucceeded, proofError: errors.New("native state unavailable")},
		{name: "unfinished proof deadline", initial: systemUpdateRunning, proofError: errors.New("native state unavailable"), want: "recovery_required:recovery_incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := linuxSystemUpdateTestRoot(t)
			state := linuxSystemUpdateTestState(strings.Repeat("8", 32))
			state.Status = tc.initial
			if state.Status == systemUpdateFailed {
				state.Error = "installer stopped"
			}
			if err := writeSystemUpdateState(root, state, nil); err != nil {
				t.Fatal(err)
			}
			backend := newLinuxSystemUpdateBackend()
			backend.stateRoot, backend.floorPath = root, filepath.Join(root, "sequence.floor")
			// Keep the floor outside the operation directory's exact inventory.
			backend.floorPath = filepath.Join(filepath.Dir(root), "observation-sequence.floor")
			if err := os.WriteFile(backend.floorPath, canonicalSystemUpdateFloor(systemUpdateFloor{Sequence: state.TargetSequence, Version: state.TargetVersion}), 0o600); err != nil {
				t.Fatal(err)
			}
			backend.now = func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
			backend.unitState = func(context.Context, string) (systemUpdateUnitState, error) { return systemUpdateUnitInactive, nil }
			backend.installedBuild = func(context.Context) (string, string, error) { return state.TargetVersion, state.TargetCommit, nil }
			proofCalls := 0
			backend.finalProof = func(_ context.Context, actual *systemUpdateState) error {
				proofCalls++
				if !stateIdentityMatches(actual, state) {
					t.Fatal("reconciled another identity")
				}
				return tc.proofError
			}
			var observed string
			backend.observe = func(actual *systemUpdateState, phase, proof, reason string) {
				if actual.RequestID != state.RequestID || actual.TargetCommit != state.TargetCommit {
					t.Fatal("wrong observation identity")
				}
				if proof != "none" && (proofCalls == 0 || tc.proofError != nil) {
					t.Fatal("terminal observation without repeated proof")
				}
				observed = phase + ":" + reason
			}
			if tc.startup {
				if err := backend.Reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else if _, err := backend.Status(context.Background(), state.RequestID); err != nil {
				t.Fatal(err)
			}
			if proofCalls != 1 || observed != tc.want {
				t.Fatalf("proofs=%d observation=%q want=%q", proofCalls, observed, tc.want)
			}
			if tc.initial == systemUpdateSucceeded {
				loaded, err := readSystemUpdateState(root, state.RequestID)
				if err != nil || loaded.Status != systemUpdateSucceeded || loaded.Error != "" {
					t.Fatalf("observer rewrote private terminal state: %#v %v", loaded, err)
				}
			}
		})
	}
}
