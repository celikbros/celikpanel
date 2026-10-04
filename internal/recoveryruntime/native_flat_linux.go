//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Contracts are fixed internal adapters, never caller-provided CLI policy.
// The shared writer only prepares an immutable retained directory. Publication
// of installed native units, hooks or selection is a separate owner operation.
type flatNativeFile struct {
	name string
	mode os.FileMode
}
type flatNativeBundle struct {
	state      *runtimeState
	generation string
	manifest   []byte
	payload    map[string][]byte
}
type flatNativeContract struct {
	sourceBase, stagePrefix string
	files                   []flatNativeFile
	read                    func(string) (*flatNativeBundle, error)
}

func prepareFlatNativeRuntimeAt(source string, fd int, root, transaction string, contract flatNativeContract, checkpoint func(string)) (string, error) {
	if os.Geteuid() != 0 || fd != 9 || !filepath.IsAbs(source) || filepath.Clean(source) != source || filepath.Base(source) != contract.sourceBase {
		return "", fail(ReasonUnsafeMetadata)
	}
	boundary := func() error { return verifyEnrollmentLock(transaction, fd) }
	if err := boundary(); err != nil {
		return "", err
	}
	bundle, err := contract.read(source)
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
	generation := bundle.generation
	final := filepath.Join(root, generation)
	verifyFinal := func() error {
		retained, err := contract.read(final)
		if err != nil {
			return err
		}
		defer retained.state.close()
		if !bytes.Equal(retained.manifest, bundle.manifest) {
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
	stage, err := os.MkdirTemp(root, contract.stagePrefix)
	if err != nil {
		return "", fail(ReasonReadFailed)
	}
	for _, spec := range contract.files {
		name := spec.name
		if err = writeNewFile(filepath.Join(stage, name), bundle.payload[name], spec.mode); err != nil {
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
	staged, err := contract.read(stage)
	if err != nil {
		return "", err
	}
	defer staged.state.close()
	if !bytes.Equal(staged.manifest, bundle.manifest) {
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
