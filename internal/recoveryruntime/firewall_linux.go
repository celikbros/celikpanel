//go:build linux

package recoveryruntime

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/firewallruntime"
)

const firewallUnitName = "celikpanel-firewall-restore.service"

type firewallBundle struct {
	state    *runtimeState
	manifest firewallruntime.Manifest
	payload  map[string][]byte
}

// readFirewallBundle shares the pinned directory/file reader with recovery kits.
// It grants no authority to execute the binary or publish the native service.
func readFirewallBundle(path string) (_ *firewallBundle, resultErr error) {
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer func() {
		if resultErr != nil {
			state.close()
		}
	}()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fail(ReasonUnsafeMetadata)
	}
	root, err := state.openPath(path)
	if err != nil {
		return nil, err
	}
	root.exactMode = 0755
	root.inventory = []string{firewallruntime.ManifestName, "restore", firewallUnitName}
	if err = state.verifyDirectory(root); err != nil {
		return nil, err
	}
	payload := make(map[string][]byte, 3)
	for _, spec := range []struct {
		name  string
		mode  uint32
		limit int64
	}{
		{firewallruntime.ManifestName, 0644, 4096}, {"restore", 0755, firewallruntime.MaxBinarySize}, {firewallUnitName, 0644, 16384},
	} {
		file, err := state.openFile(root, spec.name, spec.mode, spec.limit)
		if err != nil {
			return nil, asReadError(err)
		}
		raw, err := file.readBounded()
		if err != nil {
			return nil, err
		}
		file.digest = Digest(raw)
		payload[spec.name] = raw
	}
	manifest, err := firewallruntime.Verify(payload[firewallruntime.ManifestName], payload["restore"], payload[firewallUnitName])
	if err != nil {
		return nil, fail(ReasonContentMismatch)
	}
	if err = state.revalidate(); err != nil {
		return nil, err
	}
	return &firewallBundle{state: state, manifest: manifest, payload: payload}, nil
}

// PrepareFirewallRuntime only prepares immutable helper bytes before downtime,
// under the caller's inherited exclusive release lock and empty-marker boundary.
// It neither starts an update nor edits/enables/starts the installed firewall
// unit. The caller must admit the source through the outer signed release first.
// All predecessors and incomplete stages are retained; no garbage collection or
// ownership normalization is part of this operation.
func PrepareFirewallRuntime(source string, fd int) (string, error) {
	return prepareFirewallAt(source, fd, firewallruntime.InstalledRoot, transactionPath, nil)
}

// checkpoint is private to native interruption tests; production always nil.
func prepareFirewallAt(source string, fd int, root, transaction string, checkpoint func(string)) (string, error) {
	contract := flatNativeContract{
		sourceBase: "firewall-runtime", stagePrefix: ".prepare-firewall-",
		files: []flatNativeFile{{"restore", 0755}, {firewallUnitName, 0644}, {firewallruntime.ManifestName, 0644}},
		read: func(path string) (*flatNativeBundle, error) {
			bundle, err := readFirewallBundle(path)
			if err != nil {
				return nil, err
			}
			return &flatNativeBundle{state: bundle.state, generation: bundle.manifest.Generation, manifest: bundle.payload[firewallruntime.ManifestName], payload: bundle.payload}, nil
		},
	}
	return prepareFlatNativeRuntimeAt(source, fd, root, transaction, contract, checkpoint)
}

// VerifyFirewallUnit is read-only. It proves the exact native unit and retained
// helper it references without starting a service or requiring a live Agent.
// The caller still owns authorization to publish/restore that unit.
func VerifyFirewallUnit(path string) error {
	return verifyFirewallUnitAt(path, firewallruntime.InstalledRoot)
}
func verifyFirewallUnitAt(path, runtimeRoot string) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != firewallUnitName {
		return fail(ReasonUnsafeMetadata)
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer state.close()
	parent, err := state.openPath(filepath.Dir(path))
	if err != nil {
		return err
	}
	file, err := state.openFile(parent, firewallUnitName, 0644, 16384)
	if err != nil {
		return asReadError(err)
	}
	raw, err := file.readBounded()
	if err != nil {
		return err
	}
	file.digest = Digest(raw)
	generation, err := firewallruntime.UnitGeneration(raw)
	if err != nil {
		return fail(ReasonUnsupported)
	}
	bundle, err := readFirewallBundle(filepath.Join(runtimeRoot, generation))
	if err != nil {
		return err
	}
	defer bundle.state.close()
	if bundle.manifest.Generation != generation || !bytes.Equal(raw, bundle.payload[firewallUnitName]) {
		return fail(ReasonContentMismatch)
	}
	if err = bundle.state.revalidate(); err != nil {
		return err
	}
	return state.revalidate()
}
