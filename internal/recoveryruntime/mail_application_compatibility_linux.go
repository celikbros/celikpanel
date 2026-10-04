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

// CheckMailApplicationCompatibility is a read-only preflight. Its caller must
// hold release/host exclusion and supply trusted candidate/snapshot provenance.
// A native hook is not changed, disabled or downgraded to accommodate an older
// Agent. This check never executes the inspected application.
func CheckMailApplicationCompatibility(binaryDirectory string) error {
	return checkMailApplicationCompatibilityAt(binaryDirectory, MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot)
}
func checkMailApplicationCompatibilityAt(bin, hook, units, runtime string) error {
	if os.Geteuid() != 0 {
		return fail(ReasonUnsafeMetadata)
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	// Preserve proof of the first absent ancestor, not just a past ENOENT result.
	parent, absence, err := optionalMailHookParent(state, filepath.Dir(hook))
	if err != nil {
		return err
	}
	absentHook := parent == nil
	var legacy bool
	if parent != nil {
		file, e := state.openFile(parent, mailrenewalkit.HookName, 0755, 16384)
		if errors.Is(e, unix.ENOENT) {
			absentHook = true
			absence = func() error { return requireMailPathAbsent(parent, mailrenewalkit.HookName) }
		} else if e != nil {
			return asReadError(e)
		} else {
			if err = refusePromotionXattrs(file); err != nil {
				return err
			}
			raw, e := file.readBounded()
			if e != nil {
				return e
			}
			file.digest = Digest(raw)
			legacy = bytes.Equal(raw, mailrenewalkit.LegacyHook())
		}
	}
	if absentHook || legacy {
		// Partially enrolled or owner-defined fixed native units cannot be silently
		// reclassified as legacy compatibility merely because the hook is absent.
		unitParent, e := state.openPath(units)
		if e != nil {
			return e
		}
		wantsParent, wantsAbsence, e := optionalMailHookParent(state, filepath.Join(units, "timers.target.wants"))
		if e != nil {
			return e
		}
		verify := func() error {
			if e := state.revalidate(); e != nil {
				return e
			}
			if absence != nil {
				if e := absence(); e != nil {
					return e
				}
			}
			for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
				if e := requireMailPathAbsent(unitParent, name); e != nil {
					return e
				}
			}
			if wantsParent != nil {
				if e := requireMailPathAbsent(wantsParent, mailrenewalkit.TimerName); e != nil {
					return e
				}
			}
			if wantsAbsence != nil {
				if e := wantsAbsence(); e != nil {
					return e
				}
			}
			return state.revalidate()
		}
		return verify()
	}
	installed, err := inspectMailRenewalHookAt(hook, units, runtime)
	if err != nil {
		return err
	}
	defer installed.Close()
	if installed.Mode != MailRenewalHookIndependent {
		return fail(ReasonChanged)
	}
	candidate, err := InspectCompatibleMailAgent(bin)
	if err != nil {
		return err
	}
	defer candidate.Close()
	if err = candidate.Revalidate(); err != nil {
		return err
	}
	if err = installed.Revalidate(); err != nil {
		return err
	}
	return state.revalidate()
}
func requireMailPathAbsent(parent *pinnedDirectory, name string) error {
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return fail(ReasonChanged)
	}
	return nil
}
func optionalMailHookParent(state *runtimeState, path string) (*pinnedDirectory, func() error, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, fail(ReasonUnsafeMetadata)
	}
	current, err := state.openPath("/")
	if err != nil {
		return nil, nil, err
	}
	if path == "/" {
		return current, nil, nil
	}
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, e := state.openChildDirectory(current, name, 0)
		if errors.Is(e, unix.ENOENT) {
			held := current
			missing := name
			return nil, func() error { return requireMailPathAbsent(held, missing) }, nil
		}
		if e != nil {
			return nil, nil, asReadError(e)
		}
		current = next
	}
	return current, nil, nil
}
