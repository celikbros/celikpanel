//go:build linux

package recoveryruntime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailFilesSchema = "celikpanel-mail-renewal-files/v1"

type mailFilesRecord struct {
	Schema        string                       `json:"schema"`
	CaptureSHA256 string                       `json:"capture_sha256"`
	Nonce         string                       `json:"nonce"`
	HookStage     mailCaptureParent            `json:"hook_stage"`
	UnitStage     mailCaptureParent            `json:"unit_stage"`
	New           map[string]promotionIdentity `json:"new"`
}
type mailFilesReceipt struct {
	Schema     string `json:"schema"`
	PlanSHA256 string `json:"plan_sha256"`
	Direction  string `json:"direction"`
}
type mailFilesContext struct {
	paths     mailCapturePaths
	capture   mailCaptureRecord
	raw       []byte
	journal   *runtimeState
	parent    *pinnedDirectory
	old, next *flatNativeBundle
}

func (c *mailFilesContext) close() {
	if c == nil {
		return
	}
	if c.journal != nil {
		c.journal.close()
	}
	if c.old != nil {
		c.old.state.close()
	}
	if c.next != nil {
		c.next.state.close()
	}
}
func (c *mailFilesContext) revalidate() error {
	if err := c.journal.revalidate(); err != nil {
		return err
	}
	if err := c.next.state.revalidate(); err != nil {
		return err
	}
	if c.old != nil {
		return c.old.state.revalidate()
	}
	return nil
}
func mailNativeNames() []string {
	return []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName, mailrenewalkit.HookName}
}
func (r mailFilesRecord) stageName() string { return ".celikpanel-mail-transition-" + r.Nonce }
func (c *mailFilesContext) read(name string) ([]byte, bool, error) {
	f, err := c.journal.openFile(c.parent, name, 0600, mailrenewalkit.MaxTransitionSize)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, asReadError(err)
	}
	if err = refusePromotionXattrs(f); err != nil {
		return nil, false, err
	}
	raw, err := f.readBounded()
	if err != nil {
		return nil, false, err
	}
	f.digest = Digest(raw)
	return raw, true, nil
}
func openMailFilesContext(operation, captureSHA string, fd int, paths mailCapturePaths) (c *mailFilesContext, err error) {
	if os.Geteuid() != 0 || fd != 9 || !validPromotionNonce(operation) || !ValidDigest(captureSHA) || filepath.Base(paths.hook) != mailrenewalkit.HookName {
		return nil, fail(ReasonUnsafeMetadata)
	}
	for _, p := range []string{paths.hook, paths.units, paths.runtime, paths.journals, paths.transaction} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return nil, fail(ReasonUnsafeMetadata)
		}
	}
	if err = verifyEnrollmentLock(paths.transaction, fd); err != nil {
		return nil, err
	}
	c = &mailFilesContext{paths: paths, journal: promotionState()}
	owned := c
	defer func() {
		if err != nil {
			owned.close()
		}
	}()
	c.parent, err = c.journal.openPath(paths.journals)
	if err != nil {
		return nil, err
	}
	c.parent.exactMode = 0700
	if err = c.journal.verifyDirectory(c.parent); err != nil {
		return nil, err
	}
	raw, ok, err := c.read(operation + ".json")
	if err != nil {
		return nil, err
	}
	if !ok || Digest(raw) != captureSHA {
		return nil, fail(ReasonChanged)
	}
	// Parse identities only to select the bounded immutable readers; canonical
	// schema/kit validation is mandatory before the record is used for action.
	if err = decodePromotion(raw, &c.capture); err != nil {
		return nil, err
	}
	if c.capture.Contract.OperationID != operation || !ValidDigest(c.capture.Contract.Target) {
		return nil, fail(ReasonInvalidManifest)
	}
	c.next, err = readMailRenewalBundle(filepath.Join(paths.runtime, c.capture.Contract.Target))
	if err != nil {
		return nil, err
	}
	if previous := c.capture.Contract.Previous; previous != "" {
		if !ValidDigest(previous) {
			return nil, fail(ReasonInvalidManifest)
		}
		c.old, err = readMailRenewalBundle(filepath.Join(paths.runtime, previous))
		if err != nil {
			return nil, err
		}
	}
	c.capture, err = decodeMailCapture(raw, c.old, c.next)
	if err != nil {
		return nil, err
	}
	c.raw = raw
	if err = c.revalidate(); err != nil {
		return nil, err
	}
	return c, nil
}
func (r mailFilesRecord) validate(c *mailFilesContext) error {
	if r.Schema != mailFilesSchema || r.CaptureSHA256 != Digest(c.raw) || !validPromotionNonce(r.Nonce) || r.New == nil || !validMailParent(r.HookStage) || !validMailParent(r.UnitStage) || r.HookStage.Mode != unix.S_IFDIR|0700 || r.UnitStage.Mode != unix.S_IFDIR|0700 || r.HookStage.GID != 0 || r.UnitStage.GID != 0 {
		return fail(ReasonInvalidManifest)
	}
	count := 0
	for _, name := range mailNativeNames() {
		before, present := c.capture.Contract.Before[name]
		after := c.capture.Contract.After[name]
		id, ok := r.New[name]
		if present && bytes.Equal(before, after) {
			if ok {
				return fail(ReasonInvalidManifest)
			}
			continue
		}
		count++
		if !ok || id.Dev == 0 || id.Ino == 0 || id.Mode != unix.S_IFREG|mailFileMode(name) || id.UID != 0 || id.GID != 0 || id.Size != int64(len(after)) || id.SHA256 != Digest(after) || id.MtimeNsec < 0 || id.MtimeNsec >= 1e9 || id.CtimeNsec < 0 || id.CtimeNsec >= 1e9 {
			return fail(ReasonInvalidManifest)
		}
	}
	if len(r.New) != count {
		return fail(ReasonInvalidManifest)
	}
	return nil
}

type mailFilesObservation struct {
	state                              *runtimeState
	hooks, units, hookStage, unitStage *pinnedDirectory
	after                              map[string]bool
	absent                             map[string]*pinnedDirectory
}

func (o *mailFilesObservation) close() { o.state.close() }
func (o *mailFilesObservation) revalidate() error {
	if err := o.state.revalidate(); err != nil {
		return err
	}
	for name, parent := range o.absent {
		var st unix.Stat_t
		if err := unix.Fstatat(int(parent.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
			return fail(ReasonChanged)
		}
	}
	return nil
}
func mailObserveOptional(s *runtimeState, p *pinnedDirectory, name string, mode uint32) (*pinnedFile, bool, error) {
	f, err := s.openFile(p, name, mode, 16384)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, asReadError(err)
	}
	if err = refusePromotionXattrs(f); err != nil {
		return nil, false, err
	}
	return f, true, nil
}
func mailMatchFile(f *pinnedFile, want promotionIdentity, moved bool) error {
	f.digest = want.SHA256
	if !matchPromotionIdentity(identityOf(f), want, moved) {
		return fail(ReasonChanged)
	}
	return f.verifyContents()
}
func observeMailFiles(c *mailFilesContext, plan *mailFilesRecord) (o *mailFilesObservation, err error) {
	o = &mailFilesObservation{state: &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}, after: map[string]bool{}, absent: map[string]*pinnedDirectory{}}
	owned := o
	defer func() {
		if err != nil {
			owned.close()
		}
	}()
	o.hooks, err = o.state.openPath(filepath.Dir(c.paths.hook))
	if err != nil {
		return nil, err
	}
	o.units, err = o.state.openPath(c.paths.units)
	if err != nil {
		return nil, err
	}
	if mailParentIdentity(o.hooks) != c.capture.HookParent || mailParentIdentity(o.units) != c.capture.UnitParent {
		return nil, fail(ReasonChanged)
	}
	if plan != nil {
		if err = plan.validate(c); err != nil {
			return nil, err
		}
		o.hookStage, err = o.state.openPath(filepath.Join(filepath.Dir(c.paths.hook), plan.stageName()))
		if err != nil {
			return nil, err
		}
		o.unitStage, err = o.state.openPath(filepath.Join(c.paths.units, plan.stageName()))
		if err != nil {
			return nil, err
		}
		o.hookStage.inventory = []string{}
		o.unitStage.inventory = []string{}
		if mailParentIdentity(o.hookStage) != plan.HookStage || mailParentIdentity(o.unitStage) != plan.UnitStage {
			return nil, fail(ReasonChanged)
		}
	}
	for _, name := range mailNativeNames() {
		parent, stage := o.units, o.unitStage
		if name == mailrenewalkit.HookName {
			parent, stage = o.hooks, o.hookStage
		}
		current, present, e := mailObserveOptional(o.state, parent, name, mailFileMode(name))
		if e != nil {
			return nil, e
		}
		old, hadOld := c.capture.Files[name]
		var next promotionIdentity
		changed := false
		if plan != nil {
			next, changed = plan.New[name]
		}
		if !changed {
			if present != hadOld {
				return nil, fail(ReasonChanged)
			}
			if present {
				if e = mailMatchFile(current, old, false); e != nil {
					return nil, e
				}
			} else {
				o.absent[name] = parent
			}
			// With no plan this is the capture before-state; unchanged plan files are
			// already equal to the target and must retain their original inode.
			o.after[name] = plan != nil
			continue
		}
		staged, hasStage, e := mailObserveOptional(o.state, stage, name, mailFileMode(name))
		if hasStage {
			stage.inventory = append(stage.inventory, name)
		}
		if e != nil {
			return nil, e
		}
		if hadOld {
			if !present || !hasStage {
				return nil, fail(ReasonChanged)
			}
			after := current.stat.Ino == next.Ino && uint64(current.stat.Dev) == next.Dev
			liveWant, stageWant := old, next
			if after {
				liveWant, stageWant = next, old
			}
			// Only the two recorded exchanged inodes may have rename-induced ctime.
			// Bytes, owner, mode, inode, size and mtime remain exact on both sides.
			if e = mailMatchFile(current, liveWant, true); e != nil {
				return nil, e
			}
			if e = mailMatchFile(staged, stageWant, true); e != nil {
				return nil, e
			}
			o.after[name] = after
		} else {
			if present == hasStage {
				return nil, fail(ReasonChanged)
			}
			if present {
				if e = mailMatchFile(current, next, true); e != nil {
					return nil, e
				}
				o.absent[name] = stage
				o.after[name] = true
			} else {
				if e = mailMatchFile(staged, next, true); e != nil {
					return nil, e
				}
				o.absent[name] = parent
			}
		}
	}
	if err = o.revalidate(); err != nil {
		return nil, err
	}
	if err = c.revalidate(); err != nil {
		return nil, err
	}
	return o, nil
}

// prepareMailFilesAt is private and has no installed-panel dispatcher. It only
// stages verified native file after-images and a durable inode-bound plan. The
// caller must separately establish accepted owner authority and live schedule.
// Stages are directories so Certbot cannot execute an unfinished hook as a
// top-level deploy hook. Abandoned stages are retained, never adopted or cleaned.
func prepareMailFilesAt(operation, captureSHA string, fd int, paths mailCapturePaths, checkpoint func(string)) ([]byte, error) {
	c, err := openMailFilesContext(operation, captureSHA, fd, paths)
	if err != nil {
		return nil, err
	}
	defer c.close()
	planName := operation + ".files.json"
	if raw, ok, e := c.read(planName); e != nil {
		return nil, e
	} else if ok {
		var r mailFilesRecord
		if e = decodePromotion(raw, &r); e != nil {
			return nil, e
		}
		o, e := observeMailFiles(c, &r)
		if e != nil {
			return nil, e
		}
		defer o.close()
		if e = verifyEnrollmentLock(paths.transaction, fd); e != nil {
			return nil, e
		}
		if e = unix.Fsync(int(c.parent.file.Fd())); e != nil {
			return nil, fail(ReasonReadFailed)
		}
		if e = c.revalidate(); e != nil {
			return nil, e
		}
		if e = o.revalidate(); e != nil {
			return nil, e
		}
		return raw, nil
	}
	before, err := observeMailFiles(c, nil)
	if err != nil {
		return nil, err
	}
	defer before.close()
	nonce, err := promotionNonce()
	if err != nil {
		return nil, err
	}
	r := mailFilesRecord{Schema: mailFilesSchema, CaptureSHA256: captureSHA, Nonce: nonce, New: map[string]promotionIdentity{}}
	staged := promotionState()
	defer staged.close()
	makeStage := func(parent *pinnedDirectory) (*pinnedDirectory, error) {
		if err := before.revalidate(); err != nil {
			return nil, err
		}
		if err := verifyEnrollmentLock(paths.transaction, fd); err != nil {
			return nil, err
		}
		if err := unix.Mkdirat(int(parent.file.Fd()), r.stageName(), 0700); err != nil {
			return nil, fail(ReasonChanged)
		}
		dir, err := staged.openPath(filepath.Join(parent.path, r.stageName()))
		if err != nil {
			return nil, err
		}
		dir.exactMode = 0700
		if err = staged.verifyDirectory(dir); err != nil {
			return nil, err
		}
		if err = unix.Fsync(int(parent.file.Fd())); err != nil {
			return nil, fail(ReasonReadFailed)
		}
		return dir, nil
	}
	// Native ancestors may retain a protected legacy group; new stage ownership
	// itself remains exact root:root and the existing parent is never normalized.
	staged.config.protectedDirectoryGroups = true
	hs, err := makeStage(before.hooks)
	if err != nil {
		return nil, err
	}
	us, err := makeStage(before.units)
	if err != nil {
		return nil, err
	}
	r.HookStage, r.UnitStage = mailParentIdentity(hs), mailParentIdentity(us)
	for _, name := range mailNativeNames() {
		raw := c.capture.Contract.After[name]
		if old, ok := c.capture.Contract.Before[name]; ok && bytes.Equal(old, raw) {
			continue
		}
		parent := us
		if name == mailrenewalkit.HookName {
			parent = hs
		}
		if err = writeMailNativeStage(parent, name, raw, mailFileMode(name)); err != nil {
			return nil, err
		}
		file, err := staged.openFile(parent, name, mailFileMode(name), 16384)
		if err != nil {
			return nil, asReadError(err)
		}
		file.digest = Digest(raw)
		r.New[name] = identityOf(file)
		if err = unix.Fsync(int(parent.file.Fd())); err != nil {
			return nil, fail(ReasonReadFailed)
		}
		if checkpoint != nil {
			checkpoint("stage_" + name)
		}
	}
	hs.inventory = []string{}
	us.inventory = []string{}
	for name := range r.New {
		if name == mailrenewalkit.HookName {
			hs.inventory = append(hs.inventory, name)
		} else {
			us.inventory = append(us.inventory, name)
		}
	}
	if err = r.validate(c); err != nil {
		return nil, err
	}
	raw, err := promotionJSON(r)
	if err != nil {
		return nil, err
	}
	verify := func() error {
		if e := verifyEnrollmentLock(paths.transaction, fd); e != nil {
			return e
		}
		if e := before.revalidate(); e != nil {
			return e
		}
		if e := staged.revalidate(); e != nil {
			return e
		}
		return c.revalidate()
	}
	if err = publishMailRecord(c, planName, raw, verify, checkpoint, "plan"); err != nil {
		return nil, err
	}
	return raw, nil
}
func writeMailNativeStage(parent *pinnedDirectory, name string, raw []byte, mode uint32) error {
	fd, err := unix.Openat(int(parent.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	f := os.NewFile(uintptr(fd), "mail-native-stage")
	defer f.Close()
	if err = f.Chmod(os.FileMode(mode)); err != nil {
		return fail(ReasonReadFailed)
	}
	if n, e := f.Write(raw); e != nil || n != len(raw) {
		return fail(ReasonReadFailed)
	}
	if err = f.Sync(); err != nil {
		return fail(ReasonReadFailed)
	}
	if err = f.Close(); err != nil {
		return fail(ReasonReadFailed)
	}
	return nil
}
func publishMailRecord(c *mailFilesContext, name string, raw []byte, verify func() error, checkpoint func(string), label string) error {
	if err := verify(); err != nil {
		return err
	}
	existing, ok, err := c.read(name)
	if err != nil {
		return err
	}
	if ok {
		if !bytes.Equal(existing, raw) {
			return fail(ReasonChanged)
		}
		if err = unix.Fsync(int(c.parent.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
		return verify()
	}
	nonce, err := promotionNonce()
	if err != nil {
		return err
	}
	stage := ".mail-record-" + nonce
	if err = writeMailCaptureStage(c.parent, stage, raw); err != nil {
		return err
	}
	proof := promotionState()
	defer proof.close()
	parent, err := proof.openPath(c.paths.journals)
	if err != nil {
		return err
	}
	if mailParentIdentity(parent) != mailParentIdentity(c.parent) {
		return fail(ReasonChanged)
	}
	f, err := proof.openFile(parent, stage, 0600, mailrenewalkit.MaxTransitionSize)
	if err != nil {
		return asReadError(err)
	}
	f.digest = Digest(raw)
	if checkpoint != nil {
		checkpoint(label + "_durable")
	}
	if err = verify(); err != nil {
		return err
	}
	if err = proof.revalidate(); err != nil {
		return err
	}
	if err = unix.Renameat2(int(parent.file.Fd()), stage, int(parent.file.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return fail(ReasonChanged)
	}
	if checkpoint != nil {
		checkpoint(label + "_published")
	}
	if err = unix.Fsync(int(parent.file.Fd())); err != nil {
		return fail(ReasonReadFailed)
	}
	if checkpoint != nil {
		checkpoint(label + "_parent_durable")
	}
	// Re-open only the new final name; the source stage pin has moved.
	final, e := c.journal.openFile(c.parent, name, 0600, mailrenewalkit.MaxTransitionSize)
	if e != nil {
		return asReadError(e)
	}
	final.digest = Digest(raw)
	if !matchPromotionIdentity(identityOf(final), identityOf(f), true) {
		return fail(ReasonChanged)
	}
	return verify()
}

// applyMailFilesAt publishes or inversely exchanges exactly the recorded file
// inodes. It does not reload systemd or operate timers/services. A durable
// rollback intent makes direction monotonic, including interruption during
// compensation. Native configuration is never synthesized from a status flag.
func applyMailFilesAt(operation, captureSHA, direction string, fd int, paths mailCapturePaths, checkpoint func(string)) error {
	if direction != "forward" && direction != "rollback" {
		return fail(ReasonUnsupported)
	}
	c, err := openMailFilesContext(operation, captureSHA, fd, paths)
	if err != nil {
		return err
	}
	defer c.close()
	raw, ok, err := c.read(operation + ".files.json")
	if err != nil {
		return err
	}
	if !ok {
		return fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return err
	}
	if err = plan.validate(c); err != nil {
		return err
	}
	verifyEnable := func() error { return nil }
	if direction == "rollback" {
		var closeEnable func()
		verifyEnable, closeEnable, err = mailEnableRollbackBarrier(c, operation, Digest(raw))
		if err != nil {
			return err
		}
		defer closeEnable()
	}
	barrier := func() error {
		if e := verifyEnrollmentLock(paths.transaction, fd); e != nil {
			return e
		}
		return verifyEnable()
	}
	receipt := mailFilesReceipt{mailFilesSchema, Digest(raw), direction}
	receiptRaw, err := promotionJSON(receipt)
	if err != nil {
		return err
	}
	intentName := operation + ".files-rollback-intent.json"
	intent, present, err := c.read(intentName)
	if err != nil {
		return err
	}
	if present {
		expected, _ := promotionJSON(mailFilesReceipt{mailFilesSchema, Digest(raw), "rollback"})
		if !bytes.Equal(intent, expected) || direction != "rollback" {
			return fail(ReasonChanged)
		}
	}
	completed := map[string]bool{}
	for _, side := range []string{"forward", "rollback"} {
		saved, exists, e := c.read(operation + ".files-" + side + ".json")
		if e != nil {
			return e
		}
		if exists {
			expected, _ := promotionJSON(mailFilesReceipt{mailFilesSchema, Digest(raw), side})
			if !bytes.Equal(saved, expected) {
				return fail(ReasonChanged)
			}
			completed[side] = true
		}
	}
	if completed["rollback"] && !present {
		return fail(ReasonChanged)
	}
	observation, err := observeMailFiles(c, &plan)
	if err != nil {
		return err
	}
	for name := range plan.New {
		if completed["rollback"] && observation.after[name] || completed["forward"] && !present && !observation.after[name] {
			observation.close()
			return fail(ReasonChanged)
		}
	}
	if err = unix.Fsync(int(c.parent.file.Fd())); err != nil {
		observation.close()
		return fail(ReasonReadFailed)
	}
	if direction == "rollback" && !present {
		verify := func() error {
			if e := barrier(); e != nil {
				return e
			}
			if e := observation.revalidate(); e != nil {
				return e
			}
			return c.revalidate()
		}
		err = publishMailRecord(c, intentName, receiptRaw, verify, checkpoint, "rollback_intent")
	}
	observation.close()
	if err != nil {
		return err
	}
	names := mailNativeNames()
	if direction == "rollback" {
		names = []string{mailrenewalkit.HookName, mailrenewalkit.TimerName, mailrenewalkit.ServiceName}
	}
	for _, name := range names {
		if _, changed := plan.New[name]; !changed {
			continue
		}
		o, e := observeMailFiles(c, &plan)
		if e != nil {
			return e
		}
		desired := direction == "forward"
		if o.after[name] == desired {
			o.close()
			continue
		}
		e = func() error {
			defer o.close()
			if e := barrier(); e != nil {
				return e
			}
			if e := c.revalidate(); e != nil {
				return e
			}
			if e := o.revalidate(); e != nil {
				return e
			}
			parent, stage := o.units, o.unitStage
			if name == mailrenewalkit.HookName {
				parent, stage = o.hooks, o.hookStage
			}
			source, dest := stage, parent
			flags := uint(unix.RENAME_EXCHANGE)
			if _, hadOld := c.capture.Files[name]; !hadOld {
				flags = unix.RENAME_NOREPLACE
				if !desired {
					source, dest = parent, stage
				}
			}
			if e := unix.Renameat2(int(source.file.Fd()), name, int(dest.file.Fd()), name, flags); e != nil {
				return fail(ReasonChanged)
			}
			if checkpoint != nil {
				checkpoint(direction + "_" + name)
			}
			if e := unix.Fsync(int(stage.file.Fd())); e != nil {
				return fail(ReasonReadFailed)
			}
			if e := unix.Fsync(int(parent.file.Fd())); e != nil {
				return fail(ReasonReadFailed)
			}
			if checkpoint != nil {
				checkpoint(direction + "_synced_" + name)
			}
			return nil
		}()
		if e != nil {
			return e
		}
	}
	final, err := observeMailFiles(c, &plan)
	if err != nil {
		return err
	}
	defer final.close()
	for name := range plan.New {
		if final.after[name] != (direction == "forward") {
			return fail(ReasonChanged)
		}
	}
	// Re-fsync both sides even when every observed rename was already done by a
	// killed predecessor. A receipt cannot outrun durability of skipped moves.
	for _, parent := range []*pinnedDirectory{final.hooks, final.units, final.hookStage, final.unitStage} {
		if err = unix.Fsync(int(parent.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
	}
	verify := func() error {
		if e := barrier(); e != nil {
			return e
		}
		if e := final.revalidate(); e != nil {
			return e
		}
		return c.revalidate()
	}
	return publishMailRecord(c, operation+".files-"+direction+".json", receiptRaw, verify, checkpoint, direction+"_receipt")
}
