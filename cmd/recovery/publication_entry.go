package main

import (
	"github.com/alicelik/celikpanel/internal/recoverypublication"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"path/filepath"
)

func runPublication(apply bool, request recoverypublication.Request) error {
	selected, err := recoveryruntime.Resolve()
	if err != nil {
		return err
	}
	defer selected.Close()
	if err := selected.VerifyExecutingBinary(); err != nil {
		return err
	}
	if apply {
		err = recoverypublication.ApplyExisting(request)
	} else {
		err = recoverypublication.RestoreExisting(request)
	}
	if err != nil {
		return err
	}
	return selected.Revalidate()
}

func dispatchPublication(args []string, uid int, execute func(bool, recoverypublication.Request) error, report func(string)) int {
	if uid != 0 {
		report("Owner authentication is required. Use your root or authorized sudo session.")
		return exitNotOwner
	}
	if len(args) != 11 || (args[0] != "restore-resource" && args[0] != "publish-resource") || args[1] != "--resource" || args[3] != "--snapshot" || args[5] != "--snapshot-manifest" || args[7] != "--candidate-root" || args[9] != "--candidate-manifest" {
		return exitUsage
	}
	request := recoverypublication.Request{Resource: args[2], Snapshot: args[4], SnapshotManifest: args[6], CandidateRoot: args[8], CandidateManifest: args[10]}
	if !recoverypublication.ValidResource(request.Resource) || !recoverypublication.ValidSnapshot(request.Snapshot) || !recoverypublication.ValidManifest(request.SnapshotManifest) || !recoverypublication.ValidManifest(request.CandidateManifest) || !filepath.IsAbs(request.CandidateRoot) || filepath.Clean(request.CandidateRoot) != request.CandidateRoot {
		return exitUsage
	}
	if err := execute(args[0] == "publish-resource", request); err != nil {
		report("The existing operation could not verify its program files. Preserve the operation and resource evidence. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}
