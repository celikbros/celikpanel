//go:build linux

package recoveryruntime

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/firewallruntime"
	"golang.org/x/sys/unix"
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
	if os.Geteuid() != 0 || fd != 9 || !filepath.IsAbs(source) || filepath.Clean(source) != source || filepath.Base(source) != "firewall-runtime" {
		return "", fail(ReasonUnsafeMetadata)
	}
	boundary := func() error { return verifyEnrollmentLock(transaction, fd) }
	if err := boundary(); err != nil {
		return "", err
	}
	bundle, err := readFirewallBundle(source)
	if err != nil {
		return "", err
	}
	defer bundle.state.close()
	if err = createTrustedDirectories(root); err != nil {
		return "", err
	}
	destination := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
	defer destination.close()
	parent, err := destination.openPath(root)
	if err != nil {
		return "", err
	}
	generation := bundle.manifest.Generation
	final := filepath.Join(root, generation)
	verifyFinal := func() error {
		retained, err := readFirewallBundle(final)
		if err != nil {
			return err
		}
		defer retained.state.close()
		if retained.manifest != bundle.manifest {
			return fail(ReasonContentMismatch)
		}
		if err = bundle.state.revalidate(); err != nil {
			return err
		}
		if err = destination.revalidate(); err != nil {
			return err
		}
		if err = boundary(); err != nil {
			return err
		}
		return retained.state.revalidate()
	}
	var stat unix.Stat_t
	err = unix.Fstatat(int(parent.file.Fd()), generation, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		if err = verifyFinal(); err != nil {
			return "", err
		}
		// Repeat the durability barrier after a crash between rename and parent sync.
		if err = unix.Fsync(int(parent.file.Fd())); err != nil {
			return "", fail(ReasonReadFailed)
		}
		return generation, verifyFinal()
	}
	if !errors.Is(err, unix.ENOENT) {
		return "", asReadError(err)
	}
	if checkpoint != nil {
		checkpoint("before_stage")
	}
	if err = bundle.state.revalidate(); err != nil {
		return "", err
	}
	if err = destination.revalidate(); err != nil {
		return "", err
	}
	if err = boundary(); err != nil {
		return "", err
	}
	// Root-only stage is never used by a native unit. A killed writer leaves it
	// for diagnosis; a subsequent invocation prepares a fresh complete stage.
	stage, err := os.MkdirTemp(root, ".prepare-firewall-")
	if err != nil {
		return "", fail(ReasonReadFailed)
	}
	for _, name := range []string{"restore", firewallUnitName, firewallruntime.ManifestName} {
		mode := os.FileMode(0644)
		if name == "restore" {
			mode = 0755
		}
		if err = writeNewFile(filepath.Join(stage, name), bundle.payload[name], mode); err != nil {
			return "", err
		}
		if checkpoint != nil {
			checkpoint("file_" + name)
		}
	}
	if err = os.Chmod(stage, 0755); err != nil {
		return "", fail(ReasonReadFailed)
	}
	if err = syncDirectory(stage); err != nil {
		return "", err
	}
	staged, err := readFirewallBundle(stage)
	if err != nil {
		return "", err
	}
	defer staged.state.close()
	if staged.manifest != bundle.manifest {
		return "", fail(ReasonContentMismatch)
	}
	if checkpoint != nil {
		checkpoint("stage_durable")
	}
	if err = bundle.state.revalidate(); err != nil {
		return "", err
	}
	if err = staged.state.revalidate(); err != nil {
		return "", err
	}
	if err = destination.revalidate(); err != nil {
		return "", err
	}
	if err = boundary(); err != nil {
		return "", err
	}
	if err = unix.Renameat2(int(parent.file.Fd()), filepath.Base(stage), int(parent.file.Fd()), generation, unix.RENAME_NOREPLACE); err != nil && !errors.Is(err, unix.EEXIST) {
		return "", fail(ReasonReadFailed)
	}
	if checkpoint != nil {
		checkpoint("published")
	}
	if err = unix.Fsync(int(parent.file.Fd())); err != nil {
		return "", fail(ReasonReadFailed)
	}
	if checkpoint != nil {
		checkpoint("parent_durable")
	}
	if err = verifyFinal(); err != nil {
		return "", err
	}
	return generation, nil
}
