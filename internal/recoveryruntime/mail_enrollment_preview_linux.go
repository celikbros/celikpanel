//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

// MailEnrollmentPreview describes native before-state, never owner consent or
// certificate health. Existing independent schedules must be preserved, even
// when the owner disabled them. Only initial/legacy configuration is eligible
// for the separate initial enrollment executor.
type MailEnrollmentPreview struct {
	Mode       MailRenewalHookMode
	Generation string
	Timer      mailrenewalkit.TimerState
	hook       *MailRenewalHook
	state      *runtimeState
	absent     []*mailPathAbsence
}

func (p *MailEnrollmentPreview) Close() {
	if p != nil {
		if p.hook != nil {
			p.hook.Close()
			p.hook = nil
		}
		if p.state != nil {
			p.state.close()
			p.state = nil
		}
	}
}
func (p *MailEnrollmentPreview) Revalidate() error {
	if p == nil || p.hook == nil || p.state == nil {
		return fail(ReasonReadFailed)
	}
	if err := p.hook.Revalidate(); err != nil {
		return err
	}
	if err := p.state.revalidate(); err != nil {
		return err
	}
	for _, a := range p.absent {
		if err := a.revalidate(); err != nil {
			return err
		}
	}
	return nil
}
func InspectMailEnrollmentPreview(ctx context.Context, observe func(context.Context, string) ([]byte, error)) (*MailEnrollmentPreview, error) {
	return inspectMailEnrollmentPreviewAt(ctx, MailRenewalHookPath, "/etc/systemd/system", mailrenewalkit.InstalledRoot, observe)
}
func inspectMailEnrollmentPreviewAt(ctx context.Context, hookPath, units, runtimeRoot string, observe func(context.Context, string) ([]byte, error)) (result *MailEnrollmentPreview, resultErr error) {
	if ctx == nil || observe == nil {
		return nil, fail(ReasonUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hook, err := inspectMailRenewalHookAt(hookPath, units, runtimeRoot)
	if err != nil {
		return nil, err
	}
	p := &MailEnrollmentPreview{Mode: hook.Mode, Generation: hook.Generation, hook: hook, state: &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}}
	defer func() {
		if resultErr != nil {
			p.Close()
		}
	}()
	if hook.Mode != MailRenewalHookIndependent {
		for _, path := range []string{filepath.Join(units, mailrenewalkit.ServiceName), filepath.Join(units, mailrenewalkit.TimerName), filepath.Join(units, "timers.target.wants", mailrenewalkit.TimerName)} {
			parent, absent, err := mailOptionalDirectory(p.state, filepath.Dir(path))
			if err != nil {
				return nil, err
			}
			if absent == nil {
				var st unix.Stat_t
				if err := unix.Fstatat(int(parent.file.Fd()), filepath.Base(path), &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
					return nil, fail(ReasonChanged)
				}
				absent = &mailPathAbsence{parent, filepath.Base(path)}
			}
			p.absent = append(p.absent, absent)
		}
	}
	seen := make([]mailrenewalkit.UnitObservation, 0, 2)
	for _, unit := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := observe(ctx, unit)
		if err != nil {
			return nil, mailrenewalkit.ErrScheduleObservation
		}
		one, err := mailrenewalkit.ParseUnitObservation(unit, raw)
		if err != nil {
			return nil, err
		}
		seen = append(seen, one)
	}
	p.Timer, err = mailrenewalkit.TransitionTimer(hook.Mode == MailRenewalHookIndependent, seen[0], seen[1])
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := p.Revalidate(); err != nil {
		return nil, err
	}
	return p, nil
}
