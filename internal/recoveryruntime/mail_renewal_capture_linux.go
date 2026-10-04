//go:build linux

package recoveryruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"golang.org/x/sys/unix"
)

const mailCaptureSchema = "celikpanel-mail-renewal-before-image/v1"

type mailCapturePaths struct{ hook, units, runtime, journals, transaction string }
type mailCaptureParent struct {
	Dev  uint64 `json:"dev"`
	Ino  uint64 `json:"ino"`
	Mode uint32 `json:"mode"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
}
type mailCaptureRecord struct {
	Schema     string                       `json:"schema"`
	Contract   mailrenewalkit.Transition    `json:"contract"`
	HookParent mailCaptureParent            `json:"hook_parent"`
	UnitParent mailCaptureParent            `json:"unit_parent"`
	Files      map[string]promotionIdentity `json:"files"`
}

func mailParentIdentity(parent *pinnedDirectory) mailCaptureParent {
	s := parent.stat
	return mailCaptureParent{uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid}
}
func validMailParent(p mailCaptureParent) bool {
	return p.Dev != 0 && p.Ino != 0 && p.Mode&unix.S_IFMT == unix.S_IFDIR && p.Mode&07022 == 0 && p.UID == 0
}
func mailFileMode(name string) uint32 {
	if name == mailrenewalkit.HookName {
		return 0755
	}
	return 0644
}
func (r mailCaptureRecord) validate(old, new *flatNativeBundle) error {
	var om []byte
	var of map[string][]byte
	if old != nil {
		om = old.manifest
		of = mailBundleFiles(old)
	}
	if new == nil || r.Files == nil || r.Schema != mailCaptureSchema || !validMailParent(r.HookParent) || !validMailParent(r.UnitParent) || r.Contract.Validate(om, of, new.manifest, mailBundleFiles(new)) != nil || len(r.Files) != len(r.Contract.Before) {
		return fail(ReasonInvalidManifest)
	}
	for name, raw := range r.Contract.Before {
		id, ok := r.Files[name]
		if !ok || id.Dev == 0 || id.Ino == 0 || id.Mode != unix.S_IFREG|mailFileMode(name) || id.UID != 0 || id.GID != 0 || id.Size != int64(len(raw)) || id.SHA256 != Digest(raw) || id.MtimeNsec < 0 || id.MtimeNsec >= 1e9 || id.CtimeNsec < 0 || id.CtimeNsec >= 1e9 {
			return fail(ReasonInvalidManifest)
		}
	}
	return nil
}
func mailBundleFiles(b *flatNativeBundle) map[string][]byte {
	files := make(map[string][]byte, 4)
	for name, raw := range b.payload {
		if name != mailrenewalkit.ManifestName {
			files[name] = raw
		}
	}
	return files
}

// captureMailRenewalBeforeImageAt is a private preparation primitive. It does not
// enroll units, publish a hook, change timer state or start an update. A future
// owner-operated dispatcher must supply accepted authority and actual timer
// observations; no production entry is enabled by this component.
// The inherited exclusive release lock and no pending release markers are
// required even for capture, so it cannot race an application snapshot.
func captureMailRenewalBeforeImageAt(operation, target string, timer mailrenewalkit.TimerState, fd int, paths mailCapturePaths, checkpoint func(string)) ([]byte, error) {
	if os.Geteuid() != 0 || fd != 9 || !validPromotionNonce(operation) || !ValidDigest(target) || filepath.Base(paths.hook) != mailrenewalkit.HookName {
		return nil, fail(ReasonUnsafeMetadata)
	}
	for _, p := range []string{paths.hook, paths.units, paths.runtime, paths.journals, paths.transaction} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return nil, fail(ReasonUnsafeMetadata)
		}
	}
	boundary := func() error { return verifyEnrollmentLock(paths.transaction, fd) }
	if err := boundary(); err != nil {
		return nil, err
	}
	next, err := readMailRenewalBundle(filepath.Join(paths.runtime, target))
	if err != nil {
		return nil, err
	}
	defer next.state.close()
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	hooks, err := state.openPath(filepath.Dir(paths.hook))
	if err != nil {
		return nil, err
	}
	units, err := state.openPath(paths.units)
	if err != nil {
		return nil, err
	}
	before := map[string][]byte{}
	identities := map[string]promotionIdentity{}
	absent := map[string]*pinnedDirectory{}
	for _, name := range []string{mailrenewalkit.HookName, mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		parent := units
		if name == mailrenewalkit.HookName {
			parent = hooks
		}
		file, err := state.openFile(parent, name, mailFileMode(name), 16384)
		if errors.Is(err, unix.ENOENT) {
			absent[name] = parent
			continue
		}
		if err != nil {
			return nil, asReadError(err)
		}
		if err := refusePromotionXattrs(file); err != nil {
			return nil, err
		}
		raw, err := file.readBounded()
		if err != nil {
			return nil, err
		}
		file.digest = Digest(raw)
		before[name] = raw
		identities[name] = identityOf(file)
	}
	var old *flatNativeBundle
	var om []byte
	var of map[string][]byte
	hook := before[mailrenewalkit.HookName]
	if len(hook) != 0 && !bytes.Equal(hook, mailrenewalkit.LegacyHook()) {
		previous, err := mailrenewalkit.HookGeneration(hook)
		if err != nil {
			return nil, fail(ReasonUnsupported)
		}
		old, err = readMailRenewalBundle(filepath.Join(paths.runtime, previous))
		if err != nil {
			return nil, err
		}
		defer old.state.close()
		om = old.manifest
		of = mailBundleFiles(old)
	}
	contract, err := mailrenewalkit.NewTransition(operation, before, timer, om, of, next.manifest, mailBundleFiles(next))
	if err != nil {
		return nil, fail(ReasonUnsupported)
	}
	record := mailCaptureRecord{mailCaptureSchema, contract, mailParentIdentity(hooks), mailParentIdentity(units), identities}
	if err = record.validate(old, next); err != nil {
		return nil, err
	}
	raw, err := promotionJSON(record)
	if err != nil || len(raw) > mailrenewalkit.MaxTransitionSize {
		return nil, fail(ReasonInvalidManifest)
	}
	revalidate := func() error {
		if err := boundary(); err != nil {
			return err
		}
		if err := state.revalidate(); err != nil {
			return err
		}
		if err := next.state.revalidate(); err != nil {
			return err
		}
		if old != nil {
			if err := old.state.revalidate(); err != nil {
				return err
			}
		}
		for name, parent := range absent {
			var st unix.Stat_t
			if err := unix.Fstatat(int(parent.file.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
				return fail(ReasonChanged)
			}
		}
		return nil
	}
	if err = revalidate(); err != nil {
		return nil, err
	}
	// The fixed journal root must already be admitted by its owner operation.
	// Observation never creates or normalizes an owner-selected parent here.
	journal := promotionState()
	defer journal.close()
	parent, err := journal.openPath(paths.journals)
	if err != nil {
		return nil, err
	}
	parent.exactMode = 0700
	if err = journal.verifyDirectory(parent); err != nil {
		return nil, err
	}
	name := operation + ".json"
	existing, err := journal.openFile(parent, name, 0600, mailrenewalkit.MaxTransitionSize)
	if err == nil {
		saved, err := existing.readBounded()
		if err != nil {
			return nil, err
		}
		existing.digest = Digest(saved)
		if !bytes.Equal(saved, raw) {
			return nil, fail(ReasonChanged)
		}
		if err = revalidate(); err != nil {
			return nil, err
		}
		if err = journal.revalidate(); err != nil {
			return nil, err
		}
		if err = unix.Fsync(int(parent.file.Fd())); err != nil {
			return nil, fail(ReasonReadFailed)
		}
		if err = journal.revalidate(); err != nil {
			return nil, err
		}
		if err = revalidate(); err != nil {
			return nil, err
		}
		return saved, nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return nil, asReadError(err)
	}
	nonce, err := promotionNonce()
	if err != nil {
		return nil, err
	}
	stage := ".mail-before-image-" + nonce
	if err = writeMailCaptureStage(parent, stage, raw); err != nil {
		return nil, err
	}
	if checkpoint != nil {
		checkpoint("image_durable")
	}
	staged, err := journal.openFile(parent, stage, 0600, mailrenewalkit.MaxTransitionSize)
	if err != nil {
		return nil, asReadError(err)
	}
	staged.digest = Digest(raw)
	if err = revalidate(); err != nil {
		return nil, err
	}
	if err = journal.revalidate(); err != nil {
		return nil, err
	}
	if err = unix.Renameat2(int(parent.file.Fd()), stage, int(parent.file.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
		return nil, fail(ReasonChanged)
	}
	if checkpoint != nil {
		checkpoint("image_published")
	}
	if err = unix.Fsync(int(parent.file.Fd())); err != nil {
		return nil, fail(ReasonReadFailed)
	}
	if checkpoint != nil {
		checkpoint("image_parent_durable")
	}
	if err = revalidate(); err != nil {
		return nil, err
	}
	// A fresh reader proves the committed name; the staged pin intentionally no
	// longer names it after rename. No evidence or old files are removed.
	proof := promotionState()
	defer proof.close()
	pd, err := proof.openPath(paths.journals)
	if err != nil {
		return nil, err
	}
	pd.exactMode = 0700
	file, err := proof.openFile(pd, name, 0600, mailrenewalkit.MaxTransitionSize)
	if err != nil {
		return nil, asReadError(err)
	}
	file.digest = Digest(raw)
	if mailParentIdentity(pd) != mailParentIdentity(parent) || !matchPromotionIdentity(identityOf(file), identityOf(staged), true) {
		return nil, fail(ReasonChanged)
	}
	for _, directory := range journal.directories {
		if err = journal.verifyDirectory(directory); err != nil {
			return nil, err
		}
	}
	if err = proof.revalidate(); err != nil {
		return nil, err
	}
	return raw, nil
}

// Decode is bounded and exact. Kit binding is repeated by record.validate; a
// JSON parse alone is not a verified before-image or recovery permission.
func decodeMailCapture(raw []byte, old, next *flatNativeBundle) (mailCaptureRecord, error) {
	var r mailCaptureRecord
	if len(raw) == 0 || len(raw) > mailrenewalkit.MaxTransitionSize {
		return r, fail(ReasonInvalidManifest)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, fail(ReasonInvalidManifest)
	}
	canonical, err := promotionJSON(r)
	if err != nil || !bytes.Equal(canonical, raw) {
		return r, fail(ReasonInvalidManifest)
	}
	return r, r.validate(old, next)
}

// Descriptor-relative exclusive creation cannot follow a replaced parent path.
// Failed/incomplete captures are retained; only this new inode gets its mode set.
func writeMailCaptureStage(parent *pinnedDirectory, name string, raw []byte) error {
	fd, err := unix.Openat(int(parent.file.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	f := os.NewFile(uintptr(fd), "mail-before-image")
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return fail(ReasonReadFailed)
	}
	if n, err := f.Write(raw); err != nil || n != len(raw) {
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
