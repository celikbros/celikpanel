//go:build linux

package main

import (
	"context"
	"log"
	"time"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

func writeSystemUpdateObservation(state *systemUpdateState, phase, proof, reason string, at time.Time) {
	if state == nil {
		return
	}
	record := recoveryobs.Record{
		RequestID: state.RequestID, TargetCommit: state.TargetCommit,
		Phase: phase, TerminalProof: proof, Reason: reason,
		ObservedAt: at.UTC().Format("2006-01-02T15:04:05Z"), PreviousFailure: "none",
	}
	if recoveryobs.Publish(record) != nil {
		// No native error, path or token is copied into the status surface. This
		// diagnostic must never replace the worker's actual mutation result.
		log.Print("System update recovery observation is unavailable")
	}
}

// A worker can die after private success persistence but before its auxiliary
// observation is durable. Existing status/startup reconciliation can close that
// window only by repeating the current exact target and native final proof.
// Failure to repeat that proof never rewrites the already-terminal private state.
func (backend *linuxSystemUpdateBackend) observePreviouslySucceeded(ctx context.Context, state *systemUpdateState) {
	if backend.observe == nil || state == nil || state.Status != systemUpdateSucceeded || backend.installedBuild == nil || backend.finalProof == nil {
		return
	}
	version, commit, err := backend.installedBuild(ctx)
	if err != nil || version != state.TargetVersion || commit != state.TargetCommit {
		return
	}
	floor, err := backend.ReadFloor()
	if err != nil || floor == nil || floor.Sequence != state.TargetSequence || floor.Version != state.TargetVersion {
		return
	}
	if backend.finalProof(ctx, state) != nil {
		return
	}
	backend.observe(state, "succeeded", "update_verified", "update_verified")
}
