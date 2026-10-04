//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

// Boot registration is separate from the immutable v1 renewal kit. It grants
// no admission: its executable can only consume an existing common reservation.
// A terminal/missing request is a read-only no-op, including after Agent removal.
const mailEnrollmentBootSchema = "celikpanel-mail-enrollment-boot/v1"
const mailEnrollmentBootLimit = 3
const mailEnrollmentBootWants = "multi-user.target.wants"

type mailEnrollmentBootPlan struct {
	Schema      string            `json:"schema"`
	ScopeSHA256 string            `json:"scope_sha256"`
	OwnerID     string            `json:"owner_id"`
	Nonce       string            `json:"nonce"`
	Units       mailCaptureParent `json:"units"`
	Wants       mailCaptureParent `json:"wants"`
	Stage       mailCaptureParent `json:"stage"`
	Unit        promotionIdentity `json:"unit"`
	Link        promotionIdentity `json:"link"`
}
type mailEnrollmentBootReceipt struct {
	Schema     string `json:"schema"`
	PlanSHA256 string `json:"plan_sha256"`
	Attempt    int    `json:"attempt"`
}

func mailEnrollmentBootName(operation string) string {
	return "celikpanel-mail-enrollment-resume-" + operation + ".service"
}
func mailEnrollmentBootUnit(scope mailEnrollmentScope) []byte {
	helper := filepath.Join(mailrenewalkit.InstalledRoot, scope.Target, mailrenewalkit.BinaryName)
	return []byte("[Unit]\nDescription=Continue recorded CelikPanel mail enrollment\nAfter=local-fs.target\n\n[Service]\nType=oneshot\nUser=root\nGroup=root\nUMask=0077\nExecStart=" + helper + " --boot-enrollment " + scope.Operation + "\nTimeoutStartSec=3min\nTimeoutStopSec=15s\nKillMode=control-group\n")
}

// ArmBoot is an explicit owner-admission effect, before workload publication.
// Missing runtime directories are not created here. The durable plan binds both
// pre-staged inodes; a partial arm resumes only those inodes. A completed arm
// never re-enables a subsequently removed link or replaces an edited unit.
func (e *PreparedMailEnrollment) ArmBoot(ctx context.Context) error {
	return e.armBoot(ctx, nil)
}
func (e *PreparedMailEnrollment) armBoot(ctx context.Context, checkpoint func(string)) error {
	c, err := e.bootContext(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	verify := func() error {
		if err := e.verifyBoundary(ctx); err != nil {
			return err
		}
		return c.revalidate()
	}
	raw, found, err := c.read(e.scope.Operation + ".boot-plan.json")
	if err != nil {
		return err
	}
	if !found {
		raw, err = e.prepareBootPlan(c, verify)
		if err != nil {
			return err
		}
		if err = publishMailRecord(c, e.scope.Operation+".boot-plan.json", raw, verify, checkpoint, "boot_plan"); err != nil {
			return err
		}
	}
	plan, err := e.decodeBootPlan(raw)
	if err != nil {
		return err
	}
	armed, err := e.readBootReceipt(c, raw, 0)
	if err != nil {
		return err
	}
	for _, part := range []string{"unit", "enable"} {
		state, from, to, staged, err := e.observeBootPart(plan, part)
		if err != nil {
			return err
		}
		if armed && staged {
			state.close()
			return fail(ReasonChanged)
		}
		if err = verify(); err == nil {
			err = state.revalidate()
		}
		if err == nil && staged {
			err = unix.Renameat2(int(from.file.Fd()), part, int(to.file.Fd()), mailEnrollmentBootName(e.scope.Operation), unix.RENAME_NOREPLACE)
			if err == nil && checkpoint != nil {
				checkpoint("boot_" + part + "_moved")
			}
		}
		// Sync on retry too: a previously interrupted rename may already be visible.
		if err == nil {
			err = unix.Fsync(int(from.file.Fd()))
		}
		if err == nil {
			err = unix.Fsync(int(to.file.Fd()))
		}
		state.close()
		if err != nil {
			return err
		}
	}
	if err = e.verifyBootParts(plan); err != nil {
		return err
	}
	receipt, _ := promotionJSON(mailEnrollmentBootReceipt{mailEnrollmentBootSchema, Digest(raw), 0})
	return publishMailRecord(c, e.scope.Operation+".boot-armed.json", receipt, verify, checkpoint, "boot_armed")
}

// ClaimBootAttempt never arms/repairs registration or resets an attempt budget.
// It is called with the existing release/host locks and exact recorded scope,
// before automatic continuation. A killed attempt remains spent after reboot.
func (e *PreparedMailEnrollment) ClaimBootAttempt(ctx context.Context) error {
	c, err := e.bootContext(ctx)
	if err != nil {
		return err
	}
	defer c.close()
	raw, found, err := c.read(e.scope.Operation + ".boot-plan.json")
	if err != nil {
		return err
	}
	if !found {
		return fail(ReasonMissing)
	}
	plan, err := e.decodeBootPlan(raw)
	if err != nil {
		return err
	}
	verify := func() error {
		if err := e.verifyBoundary(ctx); err != nil {
			return err
		}
		if err := c.revalidate(); err != nil {
			return err
		}
		return e.verifyBootParts(plan)
	}
	if err = verify(); err != nil {
		return err
	}
	armed, err := e.readBootReceipt(c, raw, 0)
	if err != nil {
		return err
	}
	if !armed {
		return fail(ReasonMissing)
	}
	attempts := 0
	for n := 1; n <= mailEnrollmentBootLimit; n++ {
		found, err := e.readBootReceipt(c, raw, n)
		if err != nil {
			return err
		}
		if found {
			if n != attempts+1 {
				return fail(ReasonChanged)
			}
			attempts = n
		}
	}
	if attempts == mailEnrollmentBootLimit {
		return &MailEnrollmentBootBudget{e.scope.Operation}
	}
	next := attempts + 1
	receipt, _ := promotionJSON(mailEnrollmentBootReceipt{mailEnrollmentBootSchema, Digest(raw), next})
	return publishMailRecord(c, fmt.Sprintf("%s.boot-attempt-%d.json", e.scope.Operation, next), receipt, verify, nil, "")
}

type MailEnrollmentBootBudget struct{ Operation string }

func (e *MailEnrollmentBootBudget) Error() string {
	return "Automatic mail enrollment continuation reached its three-attempt limit. The server owner must inspect celikpanel-mail-enrollment-resume-" + e.Operation + ".service and native mail status, resolve the reported prerequisite, then use --continue-enrollment with this same request. Evidence and completed work are retained; no new enrollment was started."
}

func (e *PreparedMailEnrollment) bootContext(ctx context.Context) (*mailFilesContext, error) {
	if !servicemutationledger.ValidIdentity(e.binding.OwnerID) {
		return nil, fail(ReasonUnsupported)
	}
	if err := e.verifyBoundary(ctx); err != nil {
		return nil, err
	}
	c, err := openMailFilesContext(e.scope.Operation, e.scope.CaptureSHA256, 9, e.paths)
	if err != nil {
		return nil, err
	}
	if err = verifyMailEnrollmentInventory(c, e.scope.Operation); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}
func (e *PreparedMailEnrollment) decodeBootPlan(raw []byte) (mailEnrollmentBootPlan, error) {
	var p mailEnrollmentBootPlan
	scopeRaw, _ := promotionJSON(e.scope)
	if decodePromotion(raw, &p) != nil || p.Schema != mailEnrollmentBootSchema || p.ScopeSHA256 != Digest(scopeRaw) || p.OwnerID != e.binding.OwnerID || !servicemutationledger.ValidIdentity(p.OwnerID) || !validPromotionNonce(p.Nonce) || p.Unit.SHA256 != Digest(mailEnrollmentBootUnit(e.scope)) || p.Link.SHA256 != Digest([]byte("../"+mailEnrollmentBootName(e.scope.Operation))) {
		return p, fail(ReasonInvalidManifest)
	}
	return p, nil
}
func (e *PreparedMailEnrollment) readBootReceipt(c *mailFilesContext, plan []byte, n int) (bool, error) {
	name := e.scope.Operation + ".boot-armed.json"
	if n > 0 {
		name = fmt.Sprintf("%s.boot-attempt-%d.json", e.scope.Operation, n)
	}
	raw, found, err := c.read(name)
	if err != nil || !found {
		return found, err
	}
	var r mailEnrollmentBootReceipt
	if decodePromotion(raw, &r) != nil || r != (mailEnrollmentBootReceipt{mailEnrollmentBootSchema, Digest(plan), n}) {
		return false, fail(ReasonChanged)
	}
	return true, nil
}
func (e *PreparedMailEnrollment) prepareBootPlan(c *mailFilesContext, verify func() error) ([]byte, error) {
	state := promotionState()
	defer state.close()
	units, err := state.openPath(e.paths.units)
	if err != nil {
		return nil, err
	}
	wants, err := state.openChildDirectory(units, mailEnrollmentBootWants, 0)
	if err != nil {
		return nil, asReadError(err)
	}
	name := mailEnrollmentBootName(e.scope.Operation)
	for _, parent := range []*pinnedDirectory{units, wants} {
		if err = requireMailPathAbsent(parent, name); err != nil {
			return nil, err
		}
	}
	// Residue without its immutable plan never becomes a new authority.
	for _, suffix := range []string{".boot-armed.json", ".boot-attempt-1.json", ".boot-attempt-2.json", ".boot-attempt-3.json"} {
		if _, found, err := c.read(e.scope.Operation + suffix); err != nil {
			return nil, err
		} else if found {
			return nil, fail(ReasonChanged)
		}
	}
	nonce, err := promotionNonce()
	if err != nil {
		return nil, err
	}
	if err = verify(); err != nil {
		return nil, err
	}
	if err = state.revalidate(); err != nil {
		return nil, err
	}
	stageName := ".celikpanel-mail-boot-" + nonce
	if err = unix.Mkdirat(int(units.file.Fd()), stageName, 0700); err != nil {
		return nil, err
	}
	stage, err := state.openChildDirectory(units, stageName, 0700)
	if err != nil {
		return nil, err
	}
	raw := mailEnrollmentBootUnit(e.scope)
	if err = writeMailNativeStage(stage, "unit", raw, 0644); err != nil {
		return nil, err
	}
	f, err := state.openFile(stage, "unit", 0644, 4096)
	if err != nil {
		return nil, err
	}
	f.digest = Digest(raw)
	if err = refusePromotionXattrs(f); err != nil {
		return nil, err
	}
	if err = unix.Symlinkat("../"+name, int(stage.file.Fd()), "enable"); err != nil {
		return nil, err
	}
	link, found, err := observeMailEnableLinkAt(stage, "enable", "../"+name)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fail(ReasonChanged)
	}
	if err = unix.Fsync(int(stage.file.Fd())); err != nil {
		return nil, err
	}
	if err = unix.Fsync(int(units.file.Fd())); err != nil {
		return nil, err
	}
	if err = verify(); err != nil {
		return nil, err
	}
	if err = state.revalidate(); err != nil {
		return nil, err
	}
	scope, _ := promotionJSON(e.scope)
	return promotionJSON(mailEnrollmentBootPlan{mailEnrollmentBootSchema, Digest(scope), e.binding.OwnerID, nonce, mailParentIdentity(units), mailParentIdentity(wants), mailParentIdentity(stage), identityOf(f), link})
}

// Exactly one side must contain each recorded inode. A same-content replacement
// is an owner edit; a missing enabled link is never permission to recreate it.
func (e *PreparedMailEnrollment) observeBootPart(p mailEnrollmentBootPlan, part string) (state *runtimeState, from, to *pinnedDirectory, staged bool, result error) {
	state = promotionState()
	defer func() {
		if result != nil {
			state.close()
		}
	}()
	units, err := state.openPath(e.paths.units)
	if err != nil {
		result = err
		return
	}
	wants, err := state.openChildDirectory(units, mailEnrollmentBootWants, 0)
	if err != nil {
		result = asReadError(err)
		return
	}
	from, err = state.openChildDirectory(units, ".celikpanel-mail-boot-"+p.Nonce, 0700)
	if err != nil {
		result = asReadError(err)
		return
	}
	if mailParentIdentity(units) != p.Units || mailParentIdentity(wants) != p.Wants || mailParentIdentity(from) != p.Stage {
		result = fail(ReasonChanged)
		return
	}
	to = units
	expected := p.Unit
	if part == "enable" {
		to = wants
		expected = p.Link
	} else if part != "unit" {
		result = fail(ReasonUnsupported)
		return
	}
	count := 0
	for _, side := range []struct {
		parent *pinnedDirectory
		name   string
		stage  bool
	}{{from, part, true}, {to, mailEnrollmentBootName(e.scope.Operation), false}} {
		var id promotionIdentity
		var found bool
		if part == "enable" {
			id, found, err = observeMailEnableLinkAt(side.parent, side.name, "../"+mailEnrollmentBootName(e.scope.Operation))
		} else {
			var f *pinnedFile
			f, err = state.openFile(side.parent, side.name, 0644, 4096)
			if errors.Is(err, unix.ENOENT) {
				err = nil
			} else if err == nil {
				found = true
				f.digest = p.Unit.SHA256
				err = refusePromotionXattrs(f)
				if err == nil {
					err = f.verifyContents()
				}
				id = identityOf(f)
			}
		}
		if err != nil {
			result = err
			return
		}
		if found {
			count++
			if !matchPromotionIdentity(id, expected, !side.stage) {
				result = fail(ReasonChanged)
				return
			}
			staged = side.stage
		}
	}
	if count != 1 {
		result = fail(ReasonChanged)
		return
	}
	result = state.revalidate()
	return
}
func (e *PreparedMailEnrollment) verifyBootParts(plan mailEnrollmentBootPlan) error {
	for _, part := range []string{"unit", "enable"} {
		state, _, _, staged, err := e.observeBootPart(plan, part)
		if err != nil {
			return err
		}
		state.close()
		if staged {
			return fail(ReasonChanged)
		}
	}
	return nil
}
