//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailEnableSchema = "celikpanel-mail-renewal-enable/v1"
const mailTimerWants = "timers.target.wants"
const mailEnableLinkTarget = "/etc/systemd/system/" + mailrenewalkit.TimerName

type mailEnableRecord struct {
	Schema      string            `json:"schema"`
	FilesSHA256 string            `json:"files_sha256"`
	Nonce       string            `json:"nonce"`
	Parent      mailCaptureParent `json:"parent"`
	Stage       mailCaptureParent `json:"stage"`
	Link        promotionIdentity `json:"link"`
}
type mailEnableReceipt struct {
	Schema     string `json:"schema"`
	PlanSHA256 string `json:"plan_sha256"`
	Direction  string `json:"direction"`
}

func (p mailEnableRecord) stageName() string { return ".celikpanel-mail-enable-" + p.Nonce }
func (p mailEnableRecord) validate(filesSHA string) error {
	if p.Schema != mailEnableSchema || p.FilesSHA256 != filesSHA || !validPromotionNonce(p.Nonce) || !validMailParent(p.Parent) || !validMailParent(p.Stage) || p.Stage.Mode != unix.S_IFDIR|0700 || p.Stage.GID != 0 || p.Link.Dev == 0 || p.Link.Ino == 0 || p.Link.Mode != unix.S_IFLNK|0777 || p.Link.UID != 0 || p.Link.GID != 0 || p.Link.Size != int64(len(mailEnableLinkTarget)) || p.Link.SHA256 != Digest([]byte(mailEnableLinkTarget)) || p.Link.MtimeNsec < 0 || p.Link.MtimeNsec >= 1e9 || p.Link.CtimeNsec < 0 || p.Link.CtimeNsec >= 1e9 {
		return fail(ReasonInvalidManifest)
	}
	return nil
}

type mailEnableContext struct {
	c        *mailFilesContext
	files    *mailFilesObservation
	filesSHA string
}

func (e *mailEnableContext) close() {
	if e.files != nil {
		e.files.close()
	}
	if e.c != nil {
		e.c.close()
	}
}
func (e *mailEnableContext) verify(fd int) error {
	if err := verifyEnrollmentLock(e.c.paths.transaction, fd); err != nil {
		return err
	}
	if err := e.files.revalidate(); err != nil {
		return err
	}
	return e.c.revalidate()
}

// Opening this boundary requires exact forward file publication and idle load
// receipts, not merely a generation or a requested enablement boolean.
func openMailEnableContext(operation, captureSHA string, fd int, paths mailCapturePaths) (out *mailEnableContext, resultErr error) {
	c, err := openMailFilesContext(operation, captureSHA, fd, paths)
	if err != nil {
		return nil, err
	}
	e := &mailEnableContext{c: c}
	defer func() {
		if resultErr != nil {
			e.close()
		}
	}()
	if c.capture.Contract.Previous != "" {
		return nil, fail(ReasonUnsupported)
	}
	if _, found, err := c.read(operation + ".files-rollback-intent.json"); err != nil {
		return nil, err
	} else if found {
		return nil, fail(ReasonChanged)
	}
	raw, found, err := c.read(operation + ".files.json")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fail(ReasonChanged)
	}
	var plan mailFilesRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return nil, err
	}
	if err = plan.validate(c); err != nil {
		return nil, err
	}
	e.filesSHA = Digest(raw)
	expected, _ := promotionJSON(mailFilesReceipt{mailFilesSchema, e.filesSHA, "forward"})
	actual, found, err := c.read(operation + ".files-forward.json")
	if err != nil {
		return nil, err
	}
	if !found || !bytes.Equal(actual, expected) {
		return nil, fail(ReasonChanged)
	}
	expected, _ = promotionJSON(mailLoadedRecord{mailBootstrapLoadedSchema, e.filesSHA, "forward", c.capture.Contract.Target, mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}})
	for _, suffix := range []string{".loaded-forward-intent.json", ".loaded-forward.json"} {
		actual, found, err = c.read(operation + suffix)
		if err != nil {
			return nil, err
		}
		if !found || !bytes.Equal(actual, expected) {
			return nil, fail(ReasonChanged)
		}
	}
	e.files, err = observeMailFiles(c, &plan)
	if err != nil {
		return nil, err
	}
	for name := range plan.New {
		if !e.files.after[name] {
			return nil, fail(ReasonChanged)
		}
	}
	if err = e.verify(fd); err != nil {
		return nil, err
	}
	return e, nil
}

// A symlink is inspected without following it through a pinned parent. Its
// exact inode, target, protected metadata and absence of xattrs are required.
// Only a rename-induced ctime change is permitted after the recorded move.
func observeMailEnableLink(parent *pinnedDirectory) (promotionIdentity, bool, error) {
	return observeMailEnableLinkAt(parent, mailrenewalkit.TimerName, mailEnableLinkTarget)
}

func observeMailEnableLinkAt(parent *pinnedDirectory, name, target string) (promotionIdentity, bool, error) {
	var st, after unix.Stat_t
	err := unix.Fstatat(int(parent.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return promotionIdentity{}, false, nil
	}
	if err != nil {
		return promotionIdentity{}, false, fail(ReasonReadFailed)
	}
	if st.Mode != unix.S_IFLNK|0777 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || st.Size != int64(len(target)) {
		return promotionIdentity{}, false, fail(ReasonChanged)
	}
	buf := make([]byte, len(target)+1)
	n, err := unix.Readlinkat(int(parent.file.Fd()), name, buf)
	if err != nil || string(buf[:n]) != target {
		return promotionIdentity{}, false, fail(ReasonChanged)
	}
	count, err := unix.Llistxattr(fmt.Sprintf("/proc/self/fd/%d/%s", parent.file.Fd(), name), nil)
	if err != nil || count != 0 {
		return promotionIdentity{}, false, fail(ReasonUnsafeMetadata)
	}
	if unix.Fstatat(int(parent.file.Fd()), name, &after, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameFile(st, after) {
		return promotionIdentity{}, false, fail(ReasonChanged)
	}
	return promotionIdentity{Dev: uint64(st.Dev), Ino: st.Ino, Mode: st.Mode, UID: st.Uid, GID: st.Gid, Size: st.Size, MtimeSec: st.Mtim.Sec, MtimeNsec: st.Mtim.Nsec, CtimeSec: st.Ctim.Sec, CtimeNsec: st.Ctim.Nsec, SHA256: Digest(buf[:n])}, true, nil
}

type mailEnableObservation struct {
	state         *runtimeState
	parent, stage *pinnedDirectory
	after         bool
	identity      promotionIdentity
}

func (o *mailEnableObservation) close() {
	if o != nil && o.state != nil {
		o.state.close()
	}
}
func (o *mailEnableObservation) verify() error {
	if err := o.state.revalidate(); err != nil {
		return err
	}
	for _, side := range []struct {
		parent  *pinnedDirectory
		present bool
	}{{o.parent, o.after}, {o.stage, !o.after}} {
		id, found, err := observeMailEnableLink(side.parent)
		if err != nil {
			return err
		}
		if found != side.present || found && id != o.identity {
			return fail(ReasonChanged)
		}
	}
	return o.state.revalidate()
}
func observeMailEnable(paths mailCapturePaths, p mailEnableRecord) (out *mailEnableObservation, resultErr error) {
	o := &mailEnableObservation{state: &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}}
	defer func() {
		if resultErr != nil {
			o.close()
		}
	}()
	var err error
	o.parent, err = o.state.openPath(filepath.Join(paths.units, mailTimerWants))
	if err != nil {
		return nil, err
	}
	o.stage, err = o.state.openPath(filepath.Join(o.parent.path, p.stageName()))
	if err != nil {
		return nil, err
	}
	o.stage.exactMode = 0700
	if mailParentIdentity(o.parent) != p.Parent || mailParentIdentity(o.stage) != p.Stage {
		return nil, fail(ReasonChanged)
	}
	native, hasNative, err := observeMailEnableLink(o.parent)
	if err != nil {
		return nil, err
	}
	staged, hasStage, err := observeMailEnableLink(o.stage)
	if err != nil {
		return nil, err
	}
	if hasNative == hasStage {
		return nil, fail(ReasonChanged)
	}
	o.after = hasNative
	o.identity = staged
	o.stage.inventory = []string{mailrenewalkit.TimerName}
	if hasNative {
		o.identity = native
		o.stage.inventory = []string{}
	}
	if !matchPromotionIdentity(o.identity, p.Link, true) {
		return nil, fail(ReasonChanged)
	}
	if err = o.verify(); err != nil {
		return nil, err
	}
	return o, nil
}
func observeMailEnableSchedule(ctx context.Context, commands mailLoadedCommands, enabled, allowPrevious bool) error {
	wanted := "disabled"
	if enabled {
		wanted = "enabled"
	}
	err := commands.observePair(ctx, mailrenewalkit.TimerState{Enablement: wanted, Activity: "inactive"}, true)
	if err != nil && allowPrevious {
		alternate := "enabled"
		if enabled {
			alternate = "disabled"
		}
		return commands.observePair(ctx, mailrenewalkit.TimerState{Enablement: alternate, Activity: "inactive"}, true)
	}
	return err
}

// prepareMailEnableAt stages the exact native enablement link and durably binds
// its inode before publication. The pre-existing protected wants parent is
// required; missing/foreign parents are not normalized or adopted. No unit starts.
func prepareMailEnableAt(ctx context.Context, operation, captureSHA string, fd int, paths mailCapturePaths, commands mailLoadedCommands, checkpoint func(string)) ([]byte, error) {
	if ctx == nil || commands.observe == nil {
		return nil, fail(ReasonUnsupported)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, err := openMailEnableContext(operation, captureSHA, fd, paths)
	if err != nil {
		return nil, err
	}
	defer e.close()
	name := operation + ".timer-enable.json"
	if raw, ok, err := e.c.read(name); err != nil {
		return nil, err
	} else if ok {
		var plan mailEnableRecord
		if err = decodePromotion(raw, &plan); err != nil {
			return nil, err
		}
		if err = plan.validate(e.filesSHA); err != nil {
			return nil, err
		}
		o, err := observeMailEnable(paths, plan)
		if err != nil {
			return nil, err
		}
		defer o.close()
		if err = e.verify(fd); err != nil {
			return nil, err
		}
		if err = unix.Fsync(int(e.c.parent.file.Fd())); err != nil {
			return nil, fail(ReasonReadFailed)
		}
		if err = e.verify(fd); err != nil {
			return nil, err
		}
		return raw, o.verify()
	}
	if err = observeMailEnableSchedule(ctx, commands, false, false); err != nil {
		return nil, err
	}
	if err = prepareMailWantsParent(ctx, e, operation, fd, checkpoint); err != nil {
		return nil, err
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	parent, err := state.openPath(filepath.Join(paths.units, mailTimerWants))
	if err != nil {
		return nil, err
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.verify(fd); err != nil {
			return err
		}
		if err := state.revalidate(); err != nil {
			return err
		}
		if _, found, err := observeMailEnableLink(parent); err != nil {
			return err
		} else if found {
			return fail(ReasonChanged)
		}
		return observeMailEnableSchedule(ctx, commands, false, false)
	}
	if err = verify(); err != nil {
		return nil, err
	}
	nonce, err := promotionNonce()
	if err != nil {
		return nil, err
	}
	plan := mailEnableRecord{Schema: mailEnableSchema, FilesSHA256: e.filesSHA, Nonce: nonce, Parent: mailParentIdentity(parent)}
	if err = unix.Mkdirat(int(parent.file.Fd()), plan.stageName(), 0700); err != nil {
		return nil, fail(ReasonChanged)
	}
	stage, err := state.openPath(filepath.Join(parent.path, plan.stageName()))
	if err != nil {
		return nil, err
	}
	stage.exactMode = 0700
	if err = state.verifyDirectory(stage); err != nil {
		return nil, err
	}
	if err = unix.Symlinkat(mailEnableLinkTarget, int(stage.file.Fd()), mailrenewalkit.TimerName); err != nil {
		return nil, fail(ReasonReadFailed)
	}
	if err = unix.Fsync(int(stage.file.Fd())); err != nil {
		return nil, fail(ReasonReadFailed)
	}
	if err = unix.Fsync(int(parent.file.Fd())); err != nil {
		return nil, fail(ReasonReadFailed)
	}
	plan.Stage = mailParentIdentity(stage)
	var found bool
	plan.Link, found, err = observeMailEnableLink(stage)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fail(ReasonChanged)
	}
	stage.inventory = []string{mailrenewalkit.TimerName}
	if err = plan.validate(e.filesSHA); err != nil {
		return nil, err
	}
	raw, err := promotionJSON(plan)
	if err != nil {
		return nil, err
	}
	if checkpoint != nil {
		checkpoint("enable_link_staged")
	}
	verifyStaged := func() error {
		if err := verify(); err != nil {
			return err
		}
		id, exists, err := observeMailEnableLink(stage)
		if err != nil {
			return err
		}
		if !exists || id != plan.Link {
			return fail(ReasonChanged)
		}
		return state.revalidate()
	}
	if err = publishMailRecord(e.c, name, raw, verifyStaged, checkpoint, "enable_plan"); err != nil {
		return nil, err
	}
	return raw, nil
}

// applyMailEnableAt publishes or inversely moves only the recorded enablement
// symlink. daemon-reload observes the final native enablement; it never starts or
// stops a timer. Accepted intent and host/renewal exclusion remain caller duties.
func applyMailEnableAt(ctx context.Context, operation, captureSHA, direction string, fd int, paths mailCapturePaths, commands mailLoadedCommands, checkpoint func(string)) error {
	if ctx == nil || commands.observe == nil || commands.reload == nil || (direction != "forward" && direction != "rollback") {
		return fail(ReasonUnsupported)
	}
	e, err := openMailEnableContext(operation, captureSHA, fd, paths)
	if err != nil {
		return err
	}
	defer e.close()
	raw, ok, err := e.c.read(operation + ".timer-enable.json")
	if err != nil {
		return err
	}
	if !ok {
		return fail(ReasonChanged)
	}
	var plan mailEnableRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return err
	}
	if err = plan.validate(e.filesSHA); err != nil {
		return err
	}
	if direction == "rollback" {
		if err = mailActivityRollbackBarrier(e.c, operation, Digest(raw)); err != nil {
			return err
		}
	}
	expected, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, Digest(raw), direction})
	rollbackRaw, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, Digest(raw), "rollback"})
	rollback, present, err := e.c.read(operation + ".timer-enable-rollback-intent.json")
	if err != nil {
		return err
	}
	if present && (!bytes.Equal(rollback, rollbackRaw) || direction != "rollback") {
		return fail(ReasonChanged)
	}
	inverseRaw, inverseComplete, err := e.c.read(operation + ".timer-enable-rollback.json")
	if err != nil {
		return err
	}
	if inverseComplete && (!present || !bytes.Equal(inverseRaw, rollbackRaw) || direction != "rollback") {
		return fail(ReasonChanged)
	}
	forwardRaw, forwardComplete, err := e.c.read(operation + ".timer-enable-forward.json")
	if err != nil {
		return err
	}
	forwardExpected, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, Digest(raw), "forward"})
	if forwardComplete && !bytes.Equal(forwardRaw, forwardExpected) {
		return fail(ReasonChanged)
	}
	receipt, complete, err := e.c.read(operation + ".timer-enable-" + direction + ".json")
	if err != nil {
		return err
	}
	if complete && (!bytes.Equal(receipt, expected) || direction == "rollback" && !present) {
		return fail(ReasonChanged)
	}
	o, err := observeMailEnable(paths, plan)
	if err != nil {
		return err
	}
	defer func() { o.close() }()
	if forwardComplete && !present && !o.after {
		return fail(ReasonChanged)
	}
	desired := direction == "forward"
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.verify(fd); err != nil {
			return err
		}
		return o.verify()
	}
	if complete {
		if o.after != desired {
			return fail(ReasonChanged)
		}
		if err = commands.observePair(ctx, mailrenewalkit.TimerState{Enablement: map[bool]string{true: "enabled", false: "disabled"}[desired], Activity: "inactive"}, false); err != nil {
			return err
		}
		if err = unix.Fsync(int(e.c.parent.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
		return verify()
	}
	if err = observeMailEnableSchedule(ctx, commands, o.after, o.after || present); err != nil {
		return err
	}
	if direction == "rollback" && !present {
		if err = publishMailRecord(e.c, operation+".timer-enable-rollback-intent.json", rollbackRaw, verify, checkpoint, "enable_rollback_intent"); err != nil {
			return err
		}
	}
	if err = verify(); err != nil {
		return err
	}
	if o.after != desired {
		if err = observeMailEnableSchedule(ctx, commands, o.after, o.after || present); err != nil {
			return err
		}
		if err = verify(); err != nil {
			return err
		}
		from, to := o.stage, o.parent
		if !desired {
			from, to = to, from
		}
		if err = unix.Renameat2(int(from.file.Fd()), mailrenewalkit.TimerName, int(to.file.Fd()), mailrenewalkit.TimerName, unix.RENAME_NOREPLACE); err != nil {
			return fail(ReasonChanged)
		}
		if checkpoint != nil {
			checkpoint("enable_" + direction + "_moved")
		}
		o.close()
		o, err = observeMailEnable(paths, plan)
		if err != nil {
			return err
		}
	}
	for _, dir := range []*pinnedDirectory{o.parent, o.stage} {
		if err = unix.Fsync(int(dir.file.Fd())); err != nil {
			return fail(ReasonReadFailed)
		}
	}
	if checkpoint != nil {
		checkpoint("enable_" + direction + "_parent_durable")
	}
	if err = verify(); err != nil {
		return err
	}
	if err = observeMailEnableSchedule(ctx, commands, desired, true); err != nil {
		return err
	}
	if err = commands.reload(ctx); err != nil {
		var budget *mailEnrollmentReloadBudget
		if errors.As(err, &budget) {
			return budget
		}
		return mailrenewalkit.ErrScheduleObservation
	}
	if checkpoint != nil {
		checkpoint("enable_" + direction + "_reloaded")
	}
	verifyFinal := func() error {
		if err := verify(); err != nil {
			return err
		}
		if err := commands.observePair(ctx, mailrenewalkit.TimerState{Enablement: map[bool]string{true: "enabled", false: "disabled"}[desired], Activity: "inactive"}, false); err != nil {
			return err
		}
		return verify()
	}
	return publishMailRecord(e.c, operation+".timer-enable-"+direction+".json", expected, verifyFinal, checkpoint, "enable_"+direction+"_receipt")
}

// File compensation cannot outrun the exact enablement inverse. A completed
// historical receipt is insufficient when the recorded link has reappeared or
// an owner substituted a path. The caller still observes native timer activity
// and holds host/renewal exclusion before starting its composite rollback.
func mailEnableRollbackBarrier(c *mailFilesContext, operation, filesSHA string) (func() error, func(), error) {
	raw, found, err := c.read(operation + ".timer-enable.json")
	if err != nil {
		return nil, nil, err
	}
	if !found {
		for _, suffix := range []string{"forward.json", "rollback-intent.json", "rollback.json"} {
			if _, present, err := c.read(operation + ".timer-enable-" + suffix); err != nil {
				return nil, nil, err
			} else if present {
				return nil, nil, fail(ReasonChanged)
			}
		}
		return func() error { return nil }, func() {}, nil
	}
	var plan mailEnableRecord
	if err = decodePromotion(raw, &plan); err != nil {
		return nil, nil, err
	}
	if err = plan.validate(filesSHA); err != nil {
		return nil, nil, err
	}
	expected, _ := promotionJSON(mailEnableReceipt{mailEnableSchema, Digest(raw), "rollback"})
	for _, suffix := range []string{"rollback-intent.json", "rollback.json"} {
		actual, present, err := c.read(operation + ".timer-enable-" + suffix)
		if err != nil {
			return nil, nil, err
		}
		if !present || !bytes.Equal(actual, expected) {
			return nil, nil, fail(ReasonChanged)
		}
	}
	o, err := observeMailEnable(c.paths, plan)
	if err != nil {
		return nil, nil, err
	}
	if o.after {
		o.close()
		return nil, nil, fail(ReasonChanged)
	}
	return o.verify, o.close, nil
}
