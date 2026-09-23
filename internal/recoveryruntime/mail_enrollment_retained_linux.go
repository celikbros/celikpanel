//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"os"
	"path/filepath"
)

// RetainedMailEnrollmentHelper proves only the immutable native consumer. It
// cannot admit work: the existing canonical ledger must separately bind the
// exact scope, owner, direction and this generation. Normal Agent availability
// is deliberately not a dependency of an already accepted native operation.
type RetainedMailEnrollmentHelper struct{ bundle *flatNativeBundle }

func InspectRetainedMailEnrollmentHelper(runtimeRoot, generation string) (*RetainedMailEnrollmentHelper, error) {
	if !ValidDigest(generation) || !filepath.IsAbs(runtimeRoot) || filepath.Clean(runtimeRoot) != runtimeRoot {
		return nil, fail(ReasonUnsupported)
	}
	bundle, err := readMailRenewalBundle(filepath.Join(runtimeRoot, generation))
	if err != nil {
		return nil, err
	}
	if bundle.generation != generation {
		bundle.state.close()
		return nil, fail(ReasonContentMismatch)
	}
	return &RetainedMailEnrollmentHelper{bundle: bundle}, nil
}
func (h *RetainedMailEnrollmentHelper) Generation() string {
	if h == nil || h.bundle == nil {
		return ""
	}
	return h.bundle.generation
}
func (h *RetainedMailEnrollmentHelper) Revalidate() error {
	if h == nil || h.bundle == nil {
		return fail(ReasonChanged)
	}
	return h.bundle.state.revalidate()
}

// VerifyExecutable compares the caller's pinned running image with the pinned
// installed helper, without reopening an attacker-replaced executable path.
func (h *RetainedMailEnrollmentHelper) VerifyExecutable(executable *os.File) error {
	if err := h.Revalidate(); err != nil {
		return err
	}
	if executable == nil {
		return fail(ReasonChanged)
	}
	running, err := executable.Stat()
	if err != nil {
		return err
	}
	for _, file := range h.bundle.state.files {
		if file.base == mailrenewalkit.BinaryName {
			installed, err := file.file.Stat()
			if err != nil || !os.SameFile(running, installed) {
				return fail(ReasonChanged)
			}
			return h.Revalidate()
		}
	}
	return fail(ReasonChanged)
}
func (h *RetainedMailEnrollmentHelper) Close() {
	if h != nil && h.bundle != nil {
		h.bundle.state.close()
		h.bundle = nil
	}
}
