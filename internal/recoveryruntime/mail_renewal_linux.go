//go:build linux

package recoveryruntime

import (
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

// Preparation requires the inherited exclusive native release lock before any
// release transaction starts. It never publishes a hook/unit, enables a timer,
// starts an update, or normalizes an owner path. Outer signed-source admission
// remains the installer's responsibility; digest identity is not authorization.
func PrepareMailRenewalRuntime(source string, fd int) (string, error) {
	return prepareMailRenewalAt(source, fd, mailrenewalkit.InstalledRoot, transactionPath, nil)
}
func prepareMailRenewalAt(source string, fd int, root, transaction string, checkpoint func(string)) (string, error) {
	contract := flatNativeContract{sourceBase: "mail-renewal-runtime", stagePrefix: ".prepare-mail-renewal-", files: []flatNativeFile{
		{mailrenewalkit.BinaryName, 0755}, {mailrenewalkit.ServiceName, 0644}, {mailrenewalkit.TimerName, 0644}, {mailrenewalkit.HookName, 0755}, {mailrenewalkit.ManifestName, 0644},
	}, read: readMailRenewalBundle}
	return prepareFlatNativeRuntimeAt(source, fd, root, transaction, contract, checkpoint)
}
func readMailRenewalBundle(path string) (_ *flatNativeBundle, resultErr error) {
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
	root.inventory = []string{mailrenewalkit.ManifestName, mailrenewalkit.BinaryName, mailrenewalkit.ServiceName, mailrenewalkit.TimerName, mailrenewalkit.HookName}
	if err = state.verifyDirectory(root); err != nil {
		return nil, err
	}
	payload := make(map[string][]byte, 5)
	for _, spec := range []struct {
		name  string
		mode  uint32
		limit int64
	}{
		{mailrenewalkit.ManifestName, 0644, 4096}, {mailrenewalkit.BinaryName, 0755, mailrenewalkit.MaxBinarySize}, {mailrenewalkit.ServiceName, 0644, 16384}, {mailrenewalkit.TimerName, 0644, 16384}, {mailrenewalkit.HookName, 0755, 16384},
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
	files := make(map[string][]byte, 4)
	for name, raw := range payload {
		if name != mailrenewalkit.ManifestName {
			files[name] = raw
		}
	}
	manifest, err := mailrenewalkit.Verify(payload[mailrenewalkit.ManifestName], files)
	if err != nil {
		return nil, fail(ReasonContentMismatch)
	}
	if err = state.revalidate(); err != nil {
		return nil, err
	}
	return &flatNativeBundle{state: state, generation: manifest.Generation, manifest: payload[mailrenewalkit.ManifestName], payload: payload}, nil
}
