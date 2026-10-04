//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

// InspectMailRenewalHook verifies only installed files. Loaded native unit state,
// timer activity, accepted host intent and certificate health are separate checks.
// Unknown/unsafe content is never treated as absence or legacy compatibility.
func InspectMailRenewalHook() (*MailRenewalHook, error) {
	return inspectMailRenewalHookAt(MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot)
}
func inspectMailRenewalHookAt(path, units, runtimeRoot string) (result *MailRenewalHook, resultErr error) {
	if os.Geteuid() != 0 || filepath.Base(path) != mailrenewalkit.HookName {
		return nil, fail(ReasonUnsafeMetadata)
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	var bundle *flatNativeBundle
	cleanup := func() {
		state.close()
		if bundle != nil {
			bundle.state.close()
		}
	}
	defer func() {
		if resultErr != nil {
			cleanup()
		}
	}()
	parent, missing, err := mailOptionalDirectory(state, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if missing != nil {
		h := &MailRenewalHook{Mode: MailRenewalHookAbsent, close: cleanup, revalidate: func() error {
			if err := state.revalidate(); err != nil {
				return err
			}
			return missing.revalidate()
		}}
		return h, h.Revalidate()
	}
	var absent bool
	proof := func() error {
		if err := state.revalidate(); err != nil {
			return err
		}
		if bundle != nil {
			if err := bundle.state.revalidate(); err != nil {
				return err
			}
		}
		if absent {
			var st unix.Stat_t
			if err := unix.Fstatat(int(parent.file.Fd()), mailrenewalkit.HookName, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
				return fail(ReasonChanged)
			}
		}
		return nil
	}
	h := &MailRenewalHook{revalidate: proof, close: cleanup}
	file, err := state.openFile(parent, mailrenewalkit.HookName, 0755, 16384)
	if errors.Is(err, unix.ENOENT) {
		absent = true
		h.Mode = MailRenewalHookAbsent
		return h, proof()
	}
	if err != nil {
		return nil, asReadError(err)
	}
	raw, err := file.readBounded()
	if err != nil {
		return nil, err
	}
	file.digest = Digest(raw)
	if bytes.Equal(raw, mailrenewalkit.LegacyHook()) {
		h.Mode = MailRenewalHookLegacy
		return h, proof()
	}
	generation, err := mailrenewalkit.HookGeneration(raw)
	if err != nil {
		return nil, fail(ReasonUnsupported)
	}
	bundle, err = readMailRenewalBundle(filepath.Join(runtimeRoot, generation))
	if err != nil {
		return nil, err
	}
	if bundle.generation != generation || !bytes.Equal(raw, bundle.payload[mailrenewalkit.HookName]) {
		return nil, fail(ReasonContentMismatch)
	}
	unitRoot, err := state.openPath(units)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		unit, err := state.openFile(unitRoot, name, 0644, 16384)
		if err != nil {
			return nil, asReadError(err)
		}
		content, err := unit.readBounded()
		if err != nil {
			return nil, err
		}
		unit.digest = Digest(content)
		if !bytes.Equal(content, bundle.payload[name]) {
			return nil, fail(ReasonContentMismatch)
		}
	}
	h.Mode = MailRenewalHookIndependent
	h.Generation = generation
	return h, proof()
}

// Pin the first missing component beneath protected, no-follow ancestors. This
// permits read-only fresh-install review before Certbot creates its directories;
// it never creates them and a later creation invalidates the observation.
type mailPathAbsence struct {
	parent *pinnedDirectory
	name   string
}

func (a *mailPathAbsence) revalidate() error {
	var st unix.Stat_t
	if err := unix.Fstatat(int(a.parent.file.Fd()), a.name, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return fail(ReasonChanged)
	}
	return nil
}
func mailOptionalDirectory(state *runtimeState, path string) (*pinnedDirectory, *mailPathAbsence, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, fail(ReasonUnsafeMetadata)
	}
	relative, err := filepath.Rel(state.config.anchor, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil, nil, fail(ReasonUnsafeMetadata)
	}
	current, err := state.openPath(state.config.anchor)
	if err != nil {
		return nil, nil, err
	}
	if relative == "." {
		return current, nil, nil
	}
	for _, base := range strings.Split(relative, "/") {
		child, err := state.openChildDirectory(current, base, 0)
		if errors.Is(err, unix.ENOENT) {
			a := &mailPathAbsence{current, base}
			return nil, a, a.revalidate()
		}
		if err != nil {
			return nil, nil, asReadError(err)
		}
		current = child
	}
	return current, nil, nil
}
