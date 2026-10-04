//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"path/filepath"
)

const mailParentSchema = "celikpanel-mail-renewal-parent/v1"

// The shared systemd wants directory is a retained infrastructure prerequisite,
// not owned workload configuration. Compensation never deletes it or its other
// users. Creation still has an exact staged inode and durable publication plan.
type mailParentRecord struct {
	Schema        string            `json:"schema"`
	CaptureSHA256 string            `json:"capture_sha256"`
	Existing      bool              `json:"existing"`
	Nonce         string            `json:"nonce"`
	Directory     mailCaptureParent `json:"directory"`
}

func (p mailParentRecord) stageName() string { return ".celikpanel-mail-wants-" + p.Nonce }
func (p mailParentRecord) validate(capture string) error {
	if p.Schema != mailParentSchema || p.CaptureSHA256 != capture || !validMailParent(p.Directory) {
		return fail(ReasonInvalidManifest)
	}
	if p.Existing {
		if p.Nonce != "" {
			return fail(ReasonInvalidManifest)
		}
	} else if !validPromotionNonce(p.Nonce) || p.Directory.Mode != unix.S_IFDIR|0755 || p.Directory.GID != 0 {
		return fail(ReasonInvalidManifest)
	}
	return nil
}

// prepareMailWantsParent creates only an absent fixed native directory. Existing
// directories are observed without mode, owner or inventory normalization. A
// published plan is never rebound to a later replacement, even with equal mode.
func prepareMailWantsParent(ctx context.Context, e *mailEnableContext, operation string, fd int, checkpoint func(string)) error {
	c := e.c
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	parent, err := state.openPath(c.paths.units)
	if err != nil {
		return err
	}
	if mailParentIdentity(parent) != c.capture.UnitParent {
		return fail(ReasonChanged)
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.verify(fd); err != nil {
			return err
		}
		return state.revalidate()
	}
	optional := func(name string) (*pinnedDirectory, bool, error) {
		var st unix.Stat_t
		if err := unix.Fstatat(int(parent.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
			return nil, false, nil
		} else if err != nil {
			return nil, false, fail(ReasonReadFailed)
		}
		d, err := state.openChildDirectory(parent, name, 0)
		return d, true, err
	}
	name := operation + ".timer-parent.json"
	raw, recorded, err := c.read(name)
	if err != nil {
		return err
	}
	var plan mailParentRecord
	if !recorded {
		if _, found, err := c.read(operation + ".timer-parent-ready.json"); err != nil {
			return err
		} else if found {
			return fail(ReasonChanged)
		}
		live, present, err := optional(mailTimerWants)
		if err != nil {
			return err
		}
		plan = mailParentRecord{Schema: mailParentSchema, CaptureSHA256: Digest(c.raw), Existing: present}
		if !present {
			if err = verify(); err != nil {
				return err
			}
			plan.Nonce, err = promotionNonce()
			if err != nil {
				return err
			}
			if unix.Mkdirat(int(parent.file.Fd()), plan.stageName(), 0700) != nil {
				return fail(ReasonChanged)
			}
			live, err = state.openChildDirectory(parent, plan.stageName(), 0)
			if err != nil {
				return err
			}
			// Only this newly created unpublished inode receives final metadata.
			if unix.Fchown(int(live.file.Fd()), 0, 0) != nil || unix.Fchmod(int(live.file.Fd()), 0755) != nil || unix.Fstat(int(live.file.Fd()), &live.stat) != nil {
				return fail(ReasonReadFailed)
			}
			live.exactMode, live.inventory = 0755, []string{}
			if unix.Fsync(int(live.file.Fd())) != nil || unix.Fsync(int(parent.file.Fd())) != nil {
				return fail(ReasonReadFailed)
			}
		}
		plan.Directory = mailParentIdentity(live)
		if err = plan.validate(Digest(c.raw)); err != nil {
			return err
		}
		raw, err = promotionJSON(plan)
		if err != nil {
			return err
		}
		if checkpoint != nil {
			checkpoint("parent_staged")
		}
		if err = publishMailRecord(c, name, raw, verify, checkpoint, "parent_plan"); err != nil {
			return err
		}
		// Reopen after publication instead of treating stale absence as authority.
		return prepareMailWantsParent(ctx, e, operation, fd, checkpoint)
	}
	if err = decodePromotion(raw, &plan); err != nil {
		return err
	}
	if err = plan.validate(Digest(c.raw)); err != nil {
		return err
	}
	ready, _ := promotionJSON(mailEnableReceipt{mailParentSchema, Digest(raw), "retained"})
	terminal, complete, err := c.read(operation + ".timer-parent-ready.json")
	if err != nil {
		return err
	}
	if complete && !bytes.Equal(terminal, ready) {
		return fail(ReasonChanged)
	}
	live, present, err := optional(mailTimerWants)
	if err != nil {
		return err
	}
	if present && mailParentIdentity(live) != plan.Directory || !present && (plan.Existing || complete) {
		return fail(ReasonChanged)
	}
	if !plan.Existing {
		staged, hasStage, err := optional(plan.stageName())
		if err != nil {
			return err
		}
		if present == hasStage || hasStage && mailParentIdentity(staged) != plan.Directory {
			return fail(ReasonChanged)
		}
		if hasStage {
			staged.inventory = []string{}
			if err = verify(); err != nil {
				return err
			}
			if err = unix.Renameat2(int(parent.file.Fd()), plan.stageName(), int(parent.file.Fd()), mailTimerWants, unix.RENAME_NOREPLACE); err != nil {
				return fail(ReasonChanged)
			}
			if checkpoint != nil {
				checkpoint("parent_moved")
			}
			staged.base, staged.path = mailTimerWants, filepath.Join(parent.path, mailTimerWants)
			live = staged
		}
	}
	if err = verify(); err != nil {
		return err
	}
	if unix.Fsync(int(live.file.Fd())) != nil || unix.Fsync(int(parent.file.Fd())) != nil {
		return fail(ReasonReadFailed)
	}
	if checkpoint != nil {
		checkpoint("parent_directory_durable")
	}
	return publishMailRecord(c, operation+".timer-parent-ready.json", ready, verify, checkpoint, "parent_ready")
}
