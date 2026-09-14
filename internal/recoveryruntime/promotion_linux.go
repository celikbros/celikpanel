//go:build linux

package recoveryruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const promotionSchema = "celikpanel/recovery-promotion/v1"
const promotionReceiptSchema = "celikpanel/recovery-promotion-committed/v1"
const maxPromotionRecord = 16384

type promotionPaths struct {
	enrollmentPaths
	promotions string
}

func defaultPromotionPaths() promotionPaths {
	return promotionPaths{enrollmentPaths{RuntimeRoot, SelectionPath, LauncherPath, transactionPath}, PromotionRoot}
}

type promotionIdentity struct {
	Dev       uint64 `json:"dev"`
	Ino       uint64 `json:"ino"`
	Mode      uint32 `json:"mode"`
	UID       uint32 `json:"uid"`
	GID       uint32 `json:"gid"`
	Size      int64  `json:"size"`
	MtimeSec  int64  `json:"mtime_sec"`
	MtimeNsec int64  `json:"mtime_nsec"`
	CtimeSec  int64  `json:"ctime_sec"`
	CtimeNsec int64  `json:"ctime_nsec"`
	SHA256    string `json:"sha256"`
}
type promotionRecord struct {
	Schema       string            `json:"schema"`
	Nonce        string            `json:"nonce"`
	Previous     string            `json:"previous"`
	Target       string            `json:"target"`
	Mode         string            `json:"mode"`
	OldLauncher  promotionIdentity `json:"old_launcher"`
	NewLauncher  promotionIdentity `json:"new_launcher"`
	OldSelection promotionIdentity `json:"old_selection"`
	NewSelection promotionIdentity `json:"new_selection"`
}
type promotionReceipt struct {
	Schema       string `json:"schema"`
	IntentSHA256 string `json:"intent_sha256"`
}
type promotionProof struct {
	record    promotionRecord
	raw       []byte
	state     *runtimeState
	directory *pinnedDirectory
	committed bool
}

func (proof *promotionProof) close() {
	if proof != nil && proof.state != nil {
		proof.state.close()
	}
}
func promotionState() *runtimeState {
	return &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0}}
}
func promotionJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fail(ReasonInvalidManifest)
	}
	return append(raw, '\n'), nil
}
func decodePromotion(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fail(ReasonInvalidManifest)
	}
	canonical, err := promotionJSON(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return fail(ReasonInvalidManifest)
	}
	return nil
}
func promotionNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fail(ReasonReadFailed)
	}
	return hex.EncodeToString(raw), nil
}
func validPromotionNonce(value string) bool { return len(value) == 32 && ValidDigest(value+value) }
func validPromotionMode(mode string) bool {
	return mode == "--normal" || mode == "--bootstrap-pre-ledger" || mode == "--bootstrap-schema17"
}
func (record promotionRecord) launcherStage(paths promotionPaths) string {
	return paths.launcher + ".promotion-" + record.Nonce
}
func (record promotionRecord) selectionStage(paths promotionPaths) string {
	return paths.selection + ".promotion-" + record.Nonce
}
func (record promotionRecord) validate() error {
	if record.Schema != promotionSchema || !validPromotionNonce(record.Nonce) || !ValidDigest(record.Previous) || !ValidDigest(record.Target) || record.Previous == record.Target || !validPromotionMode(record.Mode) {
		return fail(ReasonInvalidManifest)
	}
	for index, identity := range []promotionIdentity{record.OldLauncher, record.NewLauncher, record.OldSelection, record.NewSelection} {
		mode, limit := uint32(0755), int64(MaxBinarySize)
		if index >= 2 {
			mode, limit = 0600, MaxSelectionSize
		}
		if identity.Dev == 0 || identity.Ino == 0 || identity.Mode != unix.S_IFREG|mode || identity.UID != 0 || identity.GID != 0 || identity.Size <= 0 || identity.Size > limit || !ValidDigest(identity.SHA256) || identity.MtimeNsec < 0 || identity.MtimeNsec >= 1e9 || identity.CtimeNsec < 0 || identity.CtimeNsec >= 1e9 {
			return fail(ReasonUnsafeMetadata)
		}
	}
	oldSelection, _ := EncodeSelection(record.Previous)
	newSelection, _ := EncodeSelection(record.Target)
	if record.OldSelection.SHA256 != Digest(oldSelection) || record.NewSelection.SHA256 != Digest(newSelection) || record.OldSelection.Size != int64(len(oldSelection)) || record.NewSelection.Size != int64(len(newSelection)) {
		return fail(ReasonInvalidSelection)
	}
	if record.OldLauncher.Dev == record.NewLauncher.Dev && record.OldLauncher.Ino == record.NewLauncher.Ino || record.OldSelection.Dev == record.NewSelection.Dev && record.OldSelection.Ino == record.NewSelection.Ino {
		return fail(ReasonUnsafeMetadata)
	}
	return nil
}

// An absent directory is recognized only after every existing ancestor has been
// opened without following links and checked for owner-controlled metadata.
func openOptionalPromotionRoot(state *runtimeState, path string) (*pinnedDirectory, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, fail(ReasonUnsafeMetadata)
	}
	current, err := state.openPath("/")
	if err != nil {
		return nil, false, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		mode := uint32(0)
		if index == len(parts)-1 {
			mode = 0700
		}
		current, err = state.openChildDirectory(current, part, mode)
		if errors.Is(err, unix.ENOENT) {
			if err := state.revalidate(); err != nil {
				return nil, false, err
			}
			return nil, true, nil
		}
		if err != nil {
			return nil, false, asReadError(err)
		}
	}
	return current, false, nil
}
func readPromotion(paths promotionPaths) (_ *promotionProof, absent bool, returnErr error) {
	proof := &promotionProof{state: promotionState()}
	defer func() {
		if returnErr != nil || absent {
			proof.close()
		}
	}()
	parent, missing, err := openOptionalPromotionRoot(proof.state, paths.promotions)
	if err != nil || missing {
		return nil, missing, err
	}
	dir, err := proof.state.openChildDirectory(parent, "current", 0700)
	if errors.Is(err, unix.ENOENT) {
		if err := proof.state.revalidate(); err != nil {
			return nil, false, err
		}
		return nil, true, nil
	}
	if err != nil {
		return nil, false, asReadError(err)
	}
	proof.directory = dir
	intent, err := proof.state.openFile(dir, "intent.json", 0600, maxPromotionRecord)
	if err != nil {
		return nil, false, asReadError(err)
	}
	proof.raw, err = intent.readBounded()
	if err != nil {
		return nil, false, err
	}
	intent.digest = Digest(proof.raw)
	if err = decodePromotion(proof.raw, &proof.record); err != nil {
		return nil, false, err
	}
	if err = proof.record.validate(); err != nil {
		return nil, false, err
	}
	receipt, err := proof.state.openFile(dir, "committed.json", 0600, maxPromotionRecord)
	if err == nil {
		raw, readErr := receipt.readBounded()
		if readErr != nil {
			return nil, false, readErr
		}
		var value promotionReceipt
		if decodePromotion(raw, &value) != nil || value.Schema != promotionReceiptSchema || value.IntentSHA256 != Digest(proof.raw) {
			return nil, false, fail(ReasonInvalidManifest)
		}
		receipt.digest = Digest(raw)
		proof.committed = true
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, false, asReadError(err)
	}
	// Unpublished receipt temporaries are inert evidence, never inferred to be a
	// commit. Unknown names and unsafe objects are refused, even on read-only entry.
	fd, err := unix.Openat(int(dir.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, fail(ReasonReadFailed)
	}
	listing := os.NewFile(uintptr(fd), "promotion-inventory")
	names, readErr := listing.Readdirnames(65)
	listing.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(names) > 64 {
		return nil, false, fail(ReasonInventoryMismatch)
	}
	for _, name := range names {
		if name == "intent.json" || name == "committed.json" {
			continue
		}
		if !strings.HasPrefix(name, ".committed-") || !validPromotionNonce(strings.TrimPrefix(name, ".committed-")) {
			return nil, false, fail(ReasonInventoryMismatch)
		}
		var stat unix.Stat_t
		if unix.Fstatat(int(dir.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode != unix.S_IFREG|0600 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Size < 0 || stat.Size > maxPromotionRecord {
			return nil, false, fail(ReasonUnsafeMetadata)
		}
	}
	dir.inventory = names
	if err := proof.state.revalidate(); err != nil {
		return nil, false, err
	}
	return proof, false, nil
}
func identityOf(file *pinnedFile) promotionIdentity {
	s := file.stat
	return promotionIdentity{uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid, s.Size, s.Mtim.Sec, s.Mtim.Nsec, s.Ctim.Sec, s.Ctim.Nsec, file.digest}
}
func openPromotionFile(state *runtimeState, path string, mode uint32, limit int64, digest string) (*pinnedFile, error) {
	parent, err := state.openPath(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	file, err := state.openFile(parent, filepath.Base(path), mode, limit)
	if err != nil {
		return nil, asReadError(err)
	}
	if err := refusePromotionXattrs(file); err != nil {
		return nil, err
	}
	file.digest = digest
	if err = file.verifyContents(); err != nil {
		return nil, err
	}
	return file, nil
}
func matchPromotionIdentity(actual, expected promotionIdentity, moved bool) bool {
	if moved {
		actual.CtimeSec, actual.CtimeNsec = expected.CtimeSec, expected.CtimeNsec
	}
	return actual == expected
}
func provePromotionPair(state *runtimeState, current, stage string, old, new promotionIdentity, mode uint32, limit int64) (bool, error) {
	// Choose by exact inode identity, then verify bytes and every relevant metadata
	// field. A changed ctime is allowed only for the two recorded exchanged inodes.
	parent, err := state.openPath(filepath.Dir(current))
	if err != nil {
		return false, err
	}
	file, err := state.openFile(parent, filepath.Base(current), mode, limit)
	if err != nil {
		return false, asReadError(err)
	}
	if err := refusePromotionXattrs(file); err != nil {
		return false, err
	}
	swapped := uint64(file.stat.Dev) == new.Dev && file.stat.Ino == new.Ino
	expected, other := old, new
	if swapped {
		expected, other = new, old
	}
	file.digest = expected.SHA256
	if err = file.verifyContents(); err != nil {
		return false, err
	}
	if !matchPromotionIdentity(identityOf(file), expected, swapped) {
		return false, fail(ReasonChanged)
	}
	staged, err := openPromotionFile(state, stage, mode, limit, other.SHA256)
	if err != nil {
		return false, err
	}
	if !matchPromotionIdentity(identityOf(staged), other, swapped) {
		return false, fail(ReasonChanged)
	}
	return swapped, nil
}
func verifyPromotionState(paths promotionPaths, proof *promotionProof) (launcherNew, selectionNew bool, returnErr error) {
	old, err := VerifyBundle(filepath.Join(paths.runtimeRoot, proof.record.Previous), false)
	if err != nil {
		return false, false, err
	}
	defer old.Close()
	target, err := VerifyBundle(filepath.Join(paths.runtimeRoot, proof.record.Target), false)
	if err != nil {
		return false, false, err
	}
	defer target.Close()
	if old.Digest != proof.record.Previous || target.Digest != proof.record.Target {
		return false, false, fail(ReasonDigestMismatch)
	}
	oldRaw, _ := old.ManifestBytes()
	newRaw, _ := target.ManifestBytes()
	oldManifest, err := ParseManifest(oldRaw)
	if err != nil {
		return false, false, err
	}
	newManifest, err := ParseManifest(newRaw)
	if err != nil {
		return false, false, err
	}
	if oldManifest.Files["bin/recovery"] != proof.record.OldLauncher.SHA256 || newManifest.Files["bin/recovery"] != proof.record.NewLauncher.SHA256 {
		return false, false, fail(ReasonDigestMismatch)
	}
	state := promotionState()
	defer state.close()
	launcherNew, err = provePromotionPair(state, paths.launcher, proof.record.launcherStage(paths), proof.record.OldLauncher, proof.record.NewLauncher, 0755, MaxBinarySize)
	if err != nil {
		return false, false, err
	}
	selectionNew, err = provePromotionPair(state, paths.selection, proof.record.selectionStage(paths), proof.record.OldSelection, proof.record.NewSelection, 0600, MaxSelectionSize)
	if err != nil {
		return false, false, err
	}
	if selectionNew && !launcherNew || proof.committed && (!launcherNew || !selectionNew) {
		return false, false, fail(ReasonChanged)
	}
	if err = old.Revalidate(); err != nil {
		return false, false, err
	}
	if err = target.Revalidate(); err != nil {
		return false, false, err
	}
	if err = state.revalidate(); err != nil {
		return false, false, err
	}
	if err = proof.state.revalidate(); err != nil {
		return false, false, err
	}
	return launcherNew, selectionNew, nil
}

// PromotionPending is strictly read-only. A malformed or unsafe selected record
// cannot be represented as absence; unrelated archived records are never scanned.
func PromotionPending() (bool, error) { return promotionPendingAt(defaultPromotionPaths()) }
func promotionPendingAt(paths promotionPaths) (bool, error) {
	proof, absent, err := readPromotion(paths)
	if err != nil || absent {
		return false, err
	}
	defer proof.close()
	if proof.committed {
		return false, verifyCommittedPromotion(paths, proof)
	}
	if _, _, err := verifyPromotionState(paths, proof); err != nil {
		return false, err
	}
	return true, nil
}
func requireNoPromotion(paths promotionPaths) error {
	pending, err := promotionPendingAt(paths)
	if err != nil {
		return err
	}
	if pending {
		return fail(ReasonChanged)
	}
	return nil
}

// Promote publishes only a recovery-runtime selection. Both complete immutable
// kits and both original entry objects remain available after a successful swap.
func Promote(request PromotionRequest, fd int) error {
	return promoteAt(request, fd, defaultPromotionPaths(), nil, nil)
}

// Hooks and checker injection are private deterministic fixture seams only.
func promoteAt(request PromotionRequest, fd int, paths promotionPaths, checkpoint func(string), check func(string, string) error) error {
	if os.Geteuid() != 0 || fd != 9 || !validPromotionMode(request.Mode) {
		return fail(ReasonUnsafeMetadata)
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	source, err := VerifyBundle(request.Source, true)
	if err != nil {
		return err
	}
	defer source.Close()
	proof, absent, err := readPromotion(paths)
	if err != nil {
		return err
	}
	if !absent {
		if _, _, err := verifyPromotionState(paths, proof); err != nil {
			proof.close()
			return err
		}
		if !proof.committed {
			same := proof.record.Target == source.Digest && proof.record.Mode == request.Mode
			proof.close()
			if !same {
				return fail(ReasonChanged)
			}
			return resumePromotionAt(fd, paths, checkpoint, check)
		}
		archive := filepath.Join(paths.promotions, "completed-"+proof.record.Nonce)
		if err := verifyPromotionBoundary(paths, fd); err != nil {
			proof.close()
			return err
		}
		if err := proof.state.revalidate(); err != nil {
			proof.close()
			return err
		}
		if err := unix.Renameat2(unix.AT_FDCWD, filepath.Join(paths.promotions, "current"), unix.AT_FDCWD, archive, unix.RENAME_NOREPLACE); err != nil {
			proof.close()
			return fail(ReasonChanged)
		}
		proof.close()
		if err := syncDirectory(paths.promotions); err != nil {
			return err
		}
	}
	selected, err := resolveAt(resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0})
	if err != nil {
		return err
	}
	defer selected.Close()
	if err := verifyLauncherAt(selected, paths.launcher); err != nil {
		return err
	}
	if selected.Digest == source.Digest {
		return selected.Revalidate()
	}
	target, err := installPromotionBundle(source, paths)
	if err != nil {
		return err
	}
	defer target.Close()
	if err := runPromotionCompatibility(paths, target, request.Mode, fd, check); err != nil {
		return err
	}
	if err := createTrustedDirectories(paths.promotions); err != nil {
		return err
	}
	nonce, err := promotionNonce()
	if err != nil {
		return err
	}
	record := promotionRecord{Schema: promotionSchema, Nonce: nonce, Previous: selected.Digest, Target: target.Digest, Mode: request.Mode}
	oldRaw, err := selected.ManifestBytes()
	if err != nil {
		return err
	}
	newRaw, err := target.ManifestBytes()
	if err != nil {
		return err
	}
	oldManifest, _ := ParseManifest(oldRaw)
	newManifest, _ := ParseManifest(newRaw)
	if err := copyVerifiedFile(filepath.Join(target.Root, "bin/recovery"), record.launcherStage(paths), 0755, newManifest.Files["bin/recovery"], MaxBinarySize); err != nil {
		return err
	}
	selectionRaw, _ := EncodeSelection(target.Digest)
	if err := writeNewFile(record.selectionStage(paths), selectionRaw, 0600); err != nil {
		return err
	}
	for _, directory := range []string{filepath.Dir(paths.launcher), filepath.Dir(paths.selection)} {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	state := promotionState()
	defer state.close()
	oldSelectionRaw, _ := EncodeSelection(selected.Digest)
	specs := []struct {
		path   string
		mode   uint32
		limit  int64
		digest string
		dest   *promotionIdentity
	}{
		{paths.launcher, 0755, MaxBinarySize, oldManifest.Files["bin/recovery"], &record.OldLauncher},
		{record.launcherStage(paths), 0755, MaxBinarySize, newManifest.Files["bin/recovery"], &record.NewLauncher},
		{paths.selection, 0600, MaxSelectionSize, Digest(oldSelectionRaw), &record.OldSelection},
		{record.selectionStage(paths), 0600, MaxSelectionSize, Digest(selectionRaw), &record.NewSelection},
	}
	for _, spec := range specs {
		file, err := openPromotionFile(state, spec.path, spec.mode, spec.limit, spec.digest)
		if err != nil {
			return err
		}
		*spec.dest = identityOf(file)
	}
	if err := record.validate(); err != nil {
		return err
	}
	raw, err := promotionJSON(record)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(paths.promotions, ".prepare-")
	if err != nil {
		return fail(ReasonReadFailed)
	}
	if err := writeNewFile(filepath.Join(stage, "intent.json"), raw, 0600); err != nil {
		return err
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	for _, runtime := range []*Runtime{source, selected, target} {
		if err := runtime.Revalidate(); err != nil {
			return err
		}
	}
	if err := state.revalidate(); err != nil {
		return err
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, filepath.Join(paths.promotions, "current"), unix.RENAME_NOREPLACE); err != nil {
		return fail(ReasonChanged)
	}
	if err := syncDirectory(paths.promotions); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("intent_published")
	}
	return resumePromotionAt(fd, paths, checkpoint, check)
}
func installPromotionBundle(source *Runtime, paths promotionPaths) (*Runtime, error) {
	if err := createTrustedDirectories(paths.runtimeRoot); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(paths.runtimeRoot, ".promote-")
	if err != nil {
		return nil, fail(ReasonReadFailed)
	}
	for _, dir := range []string{"bin", "deploy", "deploy/recovery"} {
		if err := os.Mkdir(filepath.Join(stage, dir), 0700); err != nil {
			return nil, fail(ReasonReadFailed)
		}
	}
	raw, err := source.ManifestBytes()
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	for _, spec := range ExpectedFiles() {
		limit := int64(MaxScriptSize)
		if strings.HasPrefix(spec.Path, "bin/") {
			limit = MaxBinarySize
		}
		if err := copyVerifiedFile(filepath.Join(source.Root, spec.Path), filepath.Join(stage, spec.Path), spec.Mode, manifest.Files[spec.Path], limit); err != nil {
			return nil, err
		}
	}
	if err := writeNewFile(filepath.Join(stage, ManifestName), raw, 0600); err != nil {
		return nil, err
	}
	if err := source.Revalidate(); err != nil {
		return nil, err
	}
	for _, dir := range []string{"deploy/recovery", "deploy", "bin", ""} {
		if err := syncDirectory(filepath.Join(stage, dir)); err != nil {
			return nil, err
		}
	}
	staged, err := VerifyBundle(stage, false)
	if err != nil {
		return nil, err
	}
	defer staged.Close()
	if staged.Digest != source.Digest {
		return nil, fail(ReasonDigestMismatch)
	}
	if err := staged.Revalidate(); err != nil {
		return nil, err
	}
	final := filepath.Join(paths.runtimeRoot, source.Digest)
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE); err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, fail(ReasonReadFailed)
	}
	if err := syncDirectory(paths.runtimeRoot); err != nil {
		return nil, err
	}
	installed, err := VerifyBundle(final, false)
	if err != nil {
		return nil, err
	}
	if installed.Digest != source.Digest {
		installed.Close()
		return nil, fail(ReasonDigestMismatch)
	}
	return installed, nil
}

func ResumePromotion(fd int) error {
	if fd != 9 {
		return fail(ReasonUnsafeMetadata)
	}
	return resumePromotionAt(fd, defaultPromotionPaths(), nil, nil)
}
func resumePromotionAt(fd int, paths promotionPaths, checkpoint func(string), check func(string, string) error) error {
	if os.Geteuid() != 0 || fd < 3 {
		return fail(ReasonUnsafeMetadata)
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	proof, absent, err := readPromotion(paths)
	if err != nil {
		return err
	}
	if absent {
		return nil
	}
	defer proof.close()
	if proof.committed {
		return verifyCommittedPromotion(paths, proof)
	}
	launcherNew, selectionNew, err := verifyPromotionState(paths, proof)
	if err != nil {
		return err
	}
	target, err := VerifyBundle(filepath.Join(paths.runtimeRoot, proof.record.Target), false)
	if err != nil {
		return err
	}
	defer target.Close()
	if err := runPromotionCompatibility(paths, target, proof.record.Mode, fd, check); err != nil {
		return err
	}
	if !launcherNew {
		if err := exchangePromotion(paths, proof, fd, true); err != nil {
			return err
		}
		if checkpoint != nil {
			checkpoint("launcher_exchanged")
		}
	}
	if !selectionNew {
		if err := exchangePromotion(paths, proof, fd, false); err != nil {
			return err
		}
		if checkpoint != nil {
			checkpoint("selector_exchanged")
		}
	}
	if _, _, err := verifyPromotionState(paths, proof); err != nil {
		return err
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	receiptRaw, _ := promotionJSON(promotionReceipt{promotionReceiptSchema, Digest(proof.raw)})
	nonce, err := promotionNonce()
	if err != nil {
		return err
	}
	temp := filepath.Join(paths.promotions, "current", ".committed-"+nonce)
	if err := writeNewFile(temp, receiptRaw, 0600); err != nil {
		return err
	}
	// Creating our inert temporary changes inventory, not any authority object.
	proof.directory.inventory = append(proof.directory.inventory, filepath.Base(temp))
	if _, _, err := verifyPromotionState(paths, proof); err != nil {
		return err
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, filepath.Join(paths.promotions, "current", "committed.json"), unix.RENAME_NOREPLACE); err != nil {
		return fail(ReasonChanged)
	}
	if err := syncDirectory(filepath.Join(paths.promotions, "current")); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("committed")
	}
	final, missing, err := readPromotion(paths)
	if err != nil {
		return err
	}
	if missing {
		return fail(ReasonChanged)
	}
	defer final.close()
	if !final.committed {
		return fail(ReasonChanged)
	}
	_, _, err = verifyPromotionState(paths, final)
	return err
}
func exchangePromotion(paths promotionPaths, proof *promotionProof, fd int, launcher bool) error {
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	launcherNew, selectionNew, err := verifyPromotionState(paths, proof)
	if err != nil {
		return err
	}
	current, stage := paths.selection, proof.record.selectionStage(paths)
	if launcher {
		if launcherNew || selectionNew {
			return fail(ReasonChanged)
		}
		current, stage = paths.launcher, proof.record.launcherStage(paths)
	} else if !launcherNew || selectionNew {
		return fail(ReasonChanged)
	}
	// Anchor the actual exchange to the verified directory descriptor. Replacing
	// its pathname concurrently cannot redirect us into an owner's new directory.
	state := promotionState()
	defer state.close()
	parent, err := state.openPath(filepath.Dir(current))
	if err != nil {
		return err
	}
	oldIdentity, newIdentity, mode, limit := proof.record.OldSelection, proof.record.NewSelection, uint32(0600), int64(MaxSelectionSize)
	if launcher {
		oldIdentity, newIdentity, mode, limit = proof.record.OldLauncher, proof.record.NewLauncher, 0755, MaxBinarySize
	}
	if alreadyNew, err := provePromotionPair(state, current, stage, oldIdentity, newIdentity, mode, limit); err != nil {
		return err
	} else if alreadyNew {
		return fail(ReasonChanged)
	}
	if err := state.revalidate(); err != nil {
		return err
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	if err := state.revalidate(); err != nil {
		return err
	}
	if err := unix.Renameat2(int(parent.file.Fd()), filepath.Base(stage), int(parent.file.Fd()), filepath.Base(current), unix.RENAME_EXCHANGE); err != nil {
		return fail(ReasonChanged)
	}
	if err := unix.Fsync(int(parent.file.Fd())); err != nil {
		return fail(ReasonReadFailed)
	}
	return state.verifyDirectory(parent)
}
func runPromotionCompatibility(paths promotionPaths, target *Runtime, mode string, fd int, fixture func(string, string) error) error {
	if !validPromotionMode(mode) {
		return fail(ReasonUnsupported)
	}
	if err := verifyPromotionBoundary(paths, fd); err != nil {
		return err
	}
	if err := target.Revalidate(); err != nil {
		return err
	}
	if fixture != nil {
		if err := fixture(target.Root, mode); err != nil {
			return err
		}
	} else {
		panel, args := "panel-checker", []string{"--check-service-operations-idle-wal-aware"}
		agent := "--check-service-mutation-idle"
		if mode == "--bootstrap-pre-ledger" {
			args = []string{"--check-pre-ledger-service-operations-idle-wal-aware"}
			agent = "--check-pre-ledger-service-mutation-idle"
		}
		if mode == "--bootstrap-schema17" {
			panel = "schema17-bridge"
			args = []string{"check", "--db", "/var/lib/celikpanel/celikpanel.db"}
			agent = "--check-pre-ledger-service-mutation-idle"
		}
		commands := []struct {
			binary string
			args   []string
		}{{panel, args}, {"agent-checker", []string{agent}}}
		for _, command := range commands {
			if err := target.Revalidate(); err != nil {
				return err
			}
			if err := verifyPromotionBoundary(paths, fd); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			cmd := exec.CommandContext(ctx, filepath.Join(target.Root, "bin", command.binary), command.args...)
			cmd.Env = promotionCheckerEnvironment()
			cmd.Dir = "/"
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			err := cmd.Run()
			deadline := ctx.Err()
			cancel()
			if err != nil || deadline != nil {
				return fail(ReasonUnsupported)
			}
			if err := target.Revalidate(); err != nil {
				return err
			}
			if err := verifyPromotionBoundary(paths, fd); err != nil {
				return err
			}
		}
	}
	if err := target.Revalidate(); err != nil {
		return err
	}
	return verifyPromotionBoundary(paths, fd)
}
func promotionCheckerEnvironment() []string {
	return append(dispatchEnvironment(), "CELIKPANEL_DATA_DIR=/var/lib/celikpanel", "CELIKPANEL_AGENT_STATE_DIR="+hostingpath.ServiceMutationStateRoot(), "CELIKPANEL_MUTATION_LOCK=/run/celikpanel/service-mutation.lock")
}
func dispatchEnvironment() []string {
	return []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "USER=root", "LOGNAME=root", "SHELL=/bin/bash", "LANG=C", "LC_ALL=C"}
}

// ResumePromotionForOwner acquires only the existing native lock. The public
// admitted-update API retains its inherited FD 9 contract, but an owner process
// may already use FD 9 for a runtime poller or unrelated inherited object. Such
// descriptors grant no authority and are neither replaced nor closed here.
func ResumePromotionForOwner() error { return resumePromotionForOwnerAt(defaultPromotionPaths()) }
func resumePromotionForOwnerAt(paths promotionPaths) error {
	if os.Geteuid() != 0 {
		return fail(ReasonUnsafeMetadata)
	}
	var occupied unix.Stat_t
	if err := unix.Fstat(9, &occupied); err == nil {
		// Reuse an inherited lock only after its exact canonical identity and
		// exclusive ownership are proved. Otherwise acquire our own descriptor.
		if err := verifyEnrollmentLockState(paths.transaction, 9, false); err == nil {
			return resumePromotionOwnerLocked(paths, 9)
		}
	} else if !errors.Is(err, unix.EBADF) {
		return fail(ReasonReadFailed)
	}
	state := promotionState()
	defer state.close()
	parent, err := state.openPath(paths.transaction)
	if err != nil {
		return err
	}
	fd, err := unix.Openat(int(parent.file.Fd()), "transaction.lock", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return asReadError(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fail(ReasonChanged)
	}
	if err := state.revalidate(); err != nil {
		return err
	}
	// Carry the actual owned descriptor through every proof and exchange. No
	// descriptor-number reservation or replacement is needed for owner recovery.
	return resumePromotionOwnerLocked(paths, fd)
}

// IsLauncherEntry is false for candidate/direct-kit execution and for a not-yet
// installed launcher. Existing unsafe launcher metadata is never treated absent.
func IsLauncherEntry() (bool, error) { return isLauncherEntryAt(LauncherPath) }
func isLauncherEntryAt(path string) (bool, error) {
	state := promotionState()
	defer state.close()
	parent, missing, err := openOptionalPromotionRootLoose(state, filepath.Dir(path))
	if err != nil || missing {
		return false, err
	}
	fixed, err := state.openFile(parent, filepath.Base(path), 0755, MaxBinarySize)
	if errors.Is(err, unix.ENOENT) {
		if err := state.revalidate(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, asReadError(err)
	}
	proc, err := os.Open("/proc/self/exe")
	if err != nil {
		return false, fail(ReasonReadFailed)
	}
	defer proc.Close()
	var stat unix.Stat_t
	if unix.Fstat(int(proc.Fd()), &stat) != nil {
		return false, fail(ReasonReadFailed)
	}
	if err := fixed.verifyMetadata(); err != nil {
		return false, err
	}
	return stat.Dev == fixed.stat.Dev && stat.Ino == fixed.stat.Ino, nil
}
func openOptionalPromotionRootLoose(state *runtimeState, path string) (*pinnedDirectory, bool, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, false, fail(ReasonUnsafeMetadata)
	}
	current, err := state.openPath("/")
	if err != nil {
		return nil, false, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		current, err = state.openChildDirectory(current, part, 0)
		if errors.Is(err, unix.ENOENT) {
			if err := state.revalidate(); err != nil {
				return nil, false, err
			}
			return nil, true, nil
		}
		if err != nil {
			return nil, false, asReadError(err)
		}
	}
	return current, false, nil
}
func VerifiedLauncherRuntime() (*Runtime, error) {
	return verifiedLauncherRuntimeAt(defaultPromotionPaths())
}
func verifiedLauncherRuntimeAt(paths promotionPaths) (_ *Runtime, returnErr error) {
	isEntry, err := isLauncherEntryAt(paths.launcher)
	if err != nil {
		return nil, err
	}
	if !isEntry {
		return nil, fail(ReasonChanged)
	}
	runtime, err := resolveAt(resolveConfig{runtimeRoot: paths.runtimeRoot, selectionPath: paths.selection, anchor: "/", uid: 0, gid: 0})
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			runtime.Close()
		}
	}()
	raw, err := runtime.ManifestBytes()
	if err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return nil, err
	}
	// A directly selected fixed entry does not depend on retired kit/stage/history.
	// Only a mixed launcher/selector needs the exact unfinished promotion intent.
	expected := manifest.Files["bin/recovery"]
	if err := verifyLauncherProcess(paths.launcher, expected); err == nil {
		if err := runtime.Revalidate(); err != nil {
			return nil, err
		}
		return runtime, nil
	} else if ReasonCode(err) != string(ReasonContentMismatch) {
		return nil, err
	}
	proof, absent, err := readPromotion(paths)
	if err != nil {
		return nil, err
	}
	if absent {
		return nil, fail(ReasonContentMismatch)
	}
	defer proof.close()
	launcherNew, selectionNew, err := verifyPromotionState(paths, proof)
	if err != nil {
		return nil, err
	}
	selected := proof.record.Previous
	if selectionNew {
		selected = proof.record.Target
	}
	if runtime.Digest != selected {
		return nil, fail(ReasonChanged)
	}
	if launcherNew {
		expected = proof.record.NewLauncher.SHA256
	} else {
		expected = proof.record.OldLauncher.SHA256
	}
	if err := verifyLauncherProcess(paths.launcher, expected); err != nil {
		return nil, err
	}
	if err := proof.state.revalidate(); err != nil {
		return nil, err
	}
	if err := runtime.Revalidate(); err != nil {
		return nil, err
	}
	return runtime, nil
}
func verifyLauncherProcess(path, expected string) error {
	state := promotionState()
	defer state.close()
	fixed, err := openPromotionFile(state, path, 0755, MaxBinarySize, expected)
	if err != nil {
		return err
	}
	proc, err := os.Open("/proc/self/exe")
	if err != nil {
		return fail(ReasonReadFailed)
	}
	defer proc.Close()
	var stat unix.Stat_t
	if unix.Fstat(int(proc.Fd()), &stat) != nil || stat.Dev != fixed.stat.Dev || stat.Ino != fixed.stat.Ino {
		return fail(ReasonChanged)
	}
	if err := verifyExecutableDescriptor(proc, expected); err != nil {
		return err
	}
	return state.revalidate()
}

// ExecSelected uses an already-pinned executable, never an unverified candidate
// pathname. Existing inherited FD 9 is preserved across exec without changing
// the caller's lock ownership or accepting a caller-provided environment.
func (runtime *Runtime) ExecSelected(args []string) error {
	return runtime.execSelectedAt(args, transactionPath)
}
func (runtime *Runtime) execSelectedAt(args []string, transaction string) error {
	if err := runtime.Revalidate(); err != nil {
		return err
	}
	var executable *pinnedFile
	for _, file := range runtime.state.files {
		if file.parent.path == filepath.Join(runtime.Root, "bin") && file.base == "recovery" {
			executable = file
			break
		}
	}
	if executable == nil {
		return fail(ReasonMissing)
	}
	if err := executable.verifyContents(); err != nil {
		return err
	}
	// Resolve may itself own FD 9 when the caller did not supply it. Preserve
	// CLOEXEC for those descriptors. An unrelated inherited descriptor is also
	// left unchanged; only the proven native release lock gets CLOEXEC cleared.
	ownedNine := false
	for _, file := range runtime.state.files {
		if file.file.Fd() == 9 {
			ownedNine = true
		}
	}
	for _, directory := range runtime.state.directories {
		if directory.file.Fd() == 9 {
			ownedNine = true
		}
	}
	if !ownedNine {
		if flags, err := unix.FcntlInt(9, unix.F_GETFD, 0); err == nil {
			if flags&unix.FD_CLOEXEC != 0 && verifyEnrollmentLockState(transaction, 9, false) == nil {
				if _, err := unix.FcntlInt(9, unix.F_SETFD, flags & ^unix.FD_CLOEXEC); err != nil {
					return fail(ReasonReadFailed)
				}
			}
		} else if !errors.Is(err, unix.EBADF) {
			return fail(ReasonReadFailed)
		}
	}
	if err := runtime.Revalidate(); err != nil {
		return err
	}
	argv := append([]string{filepath.Join(runtime.Root, "bin/recovery")}, args...)
	if err := unix.Exec(fmt.Sprintf("/proc/self/fd/%d", executable.file.Fd()), argv, dispatchEnvironment()); err != nil {
		return fail(ReasonReadFailed)
	}
	return nil
}

// Once an ordinary release has started, an incomplete promotion receipt must
// not delay the selected release recovery. This only admits read-only handoff;
// the selected runner still verifies its complete snapshot/operation contract.
func resumePromotionOwnerLocked(paths promotionPaths, fd int) error {
	if err := verifyEnrollmentLockState(paths.transaction, fd, false); err != nil {
		return err
	}
	existing, err := promotionReleaseWork(paths)
	if err != nil {
		return err
	}
	if !existing {
		return resumePromotionAt(fd, paths, nil, nil)
	}
	proof, absent, err := readPromotion(paths)
	if err != nil {
		return err
	}
	if absent {
		return nil
	}
	defer proof.close()
	if _, _, err := verifyPromotionState(paths, proof); err != nil {
		return err
	}
	return verifyEnrollmentLockState(paths.transaction, fd, false)
}

var promotionMarkerPattern = regexp.MustCompile(`\Aversion=1\ntoken=([0-9a-f]{64})\noperation=(update|rollback)\nsnapshot=([A-Za-z0-9][A-Za-z0-9._-]{0,127})\n\z`)

func promotionReleaseWork(paths promotionPaths) (bool, error) {
	state := promotionState()
	defer state.close()
	parent, err := state.openPath(paths.transaction)
	if err != nil {
		return false, err
	}
	fd, err := unix.Openat(int(parent.file.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, fail(ReasonReadFailed)
	}
	listing := os.NewFile(uintptr(fd), "promotion-release-boundary")
	names, readErr := listing.Readdirnames(7)
	listing.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(names) > 5 {
		return false, fail(ReasonInventoryMismatch)
	}
	markers := map[string][]byte{}
	for _, name := range names {
		if name == "transaction.lock" {
			continue
		}
		switch name {
		case "active", "quiesce.pending", "completion.pending", "scheduler-restore.pending":
		default:
			return false, fail(ReasonInventoryMismatch)
		}
		file, err := state.openFile(parent, name, 0600, 512)
		if err != nil {
			return false, asReadError(err)
		}
		raw, err := file.readBounded()
		if err != nil {
			return false, err
		}
		match := promotionMarkerPattern.FindSubmatch(raw)
		if match == nil || name == "quiesce.pending" && string(match[2]) != "update" {
			return false, fail(ReasonInvalidManifest)
		}
		file.digest = Digest(raw)
		markers[name] = raw
	}
	if len(markers) > 1 {
		completion, c := markers["completion.pending"]
		scheduler, s := markers["scheduler-restore.pending"]
		if len(markers) != 2 || !c || !s || !bytes.Equal(completion, scheduler) {
			return false, fail(ReasonChanged)
		}
	}
	parent.inventory = names
	if err := state.revalidate(); err != nil {
		return false, err
	}
	return len(markers) > 0, nil
}

func refusePromotionXattrs(file *pinnedFile) error {
	if err := file.verifyMetadata(); err != nil {
		return err
	}
	count, err := unix.Flistxattr(int(file.file.Fd()), nil)
	if err != nil {
		return fail(ReasonReadFailed)
	}
	if count != 0 {
		return fail(ReasonUnsafeMetadata)
	}
	return file.verifyMetadata()
}

func InspectPromotion() (PromotionStatus, error) { return inspectPromotionAt(defaultPromotionPaths()) }
func inspectPromotionAt(paths promotionPaths) (PromotionStatus, error) {
	proof, absent, err := readPromotion(paths)
	if err != nil {
		return PromotionStatus{}, err
	}
	if absent {
		return PromotionStatus{Phase: "none"}, nil
	}
	defer proof.close()
	if proof.committed {
		if err := verifyCommittedPromotion(paths, proof); err != nil {
			return PromotionStatus{}, err
		}
		return PromotionStatus{Phase: "committed", Previous: proof.record.Previous, Target: proof.record.Target}, nil
	}
	launcherNew, selectionNew, err := verifyPromotionState(paths, proof)
	if err != nil {
		return PromotionStatus{}, err
	}
	phase := "prepared"
	if launcherNew {
		phase = "launcher_published"
	}
	if selectionNew {
		phase = "selection_published"
	}
	if proof.committed {
		phase = "committed"
	}
	return PromotionStatus{Phase: phase, Previous: proof.record.Previous, Target: proof.record.Target}, nil
}

// A completed promotion no longer makes current recovery depend on its retired
// predecessor. Retired objects are checked only to admit a later promotion.
func verifyCommittedPromotion(paths promotionPaths, proof *promotionProof) error {
	if !proof.committed {
		return fail(ReasonChanged)
	}
	target, err := VerifyBundle(filepath.Join(paths.runtimeRoot, proof.record.Target), false)
	if err != nil {
		return err
	}
	defer target.Close()
	if target.Digest != proof.record.Target {
		return fail(ReasonDigestMismatch)
	}
	raw, err := target.ManifestBytes()
	if err != nil {
		return err
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		return err
	}
	if manifest.Files["bin/recovery"] != proof.record.NewLauncher.SHA256 {
		return fail(ReasonDigestMismatch)
	}
	state := promotionState()
	defer state.close()
	for _, item := range []struct {
		path     string
		expected promotionIdentity
		mode     uint32
		limit    int64
	}{
		{paths.launcher, proof.record.NewLauncher, 0755, MaxBinarySize}, {paths.selection, proof.record.NewSelection, 0600, MaxSelectionSize},
	} {
		file, err := openPromotionFile(state, item.path, item.mode, item.limit, item.expected.SHA256)
		if err != nil {
			return err
		}
		if !matchPromotionIdentity(identityOf(file), item.expected, true) {
			return fail(ReasonChanged)
		}
	}
	if err := state.revalidate(); err != nil {
		return err
	}
	if err := target.Revalidate(); err != nil {
		return err
	}
	return proof.state.revalidate()
}
func verifyPromotionBoundary(paths promotionPaths, fd int) error {
	if err := verifyEnrollmentLock(paths.transaction, fd); err != nil {
		return err
	}
	existing, err := promotionReleaseWork(paths)
	if err != nil {
		return err
	}
	if existing {
		return fail(ReasonChanged)
	}
	return nil
}
