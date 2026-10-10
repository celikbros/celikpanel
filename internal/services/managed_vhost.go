package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// A site file the owner changed is kept and named, never overwritten by a
// render (D-031, 10 Oct 2026; D-022; D-025 invariants 1, 2 and 4).
//
// Every vhost the Panel writes starts with one header line,
//
//	# celikpanel-render v2 sha256=<64 hex: SHA-256 of every byte after this line>
//
// so the file itself says whether it is still the Panel's text; it survives a
// database restore and needs no Panel to read. Before every render the file on
// disk is classified (absent, managed and unchanged, owner-edited, foreign,
// unreadable, unknown origin). Only a managed-and-unchanged file is replaced,
// and an absent one only when the site is created or the owner asks for it to
// be recreated; identical bytes are not rewritten and cause no reload. Any
// other file is kept as it is; the Panel's text is held beside it in
// `<file>.celikpanel-pending`, which nginx never reads. "Take CelikPanel's"
// keeps a dated copy of the owner's file beside it first.
//
// Panel'in yazdığı her vhost tek bir başlık satırıyla başlar; dosya Panel'in
// metni olup olmadığını kendisi söyler. Her üretimden önce diskteki dosya
// sınıflandırılır; yalnız yönetilen ve değişmemiş dosya değiştirilir, yok
// olan dosya yalnız site oluşturulurken ya da sahip isteyince yeniden yazılır.
// Başka her dosya olduğu gibi korunur; Panel'in metni yanına bekleyen dosya
// olarak bırakılır.

const (
	// ManagedRenderFormat names the file format in the ledger.
	ManagedRenderFormat       = "celikpanel-render v2"
	managedRenderHeaderPrefix = "# celikpanel-render v2 sha256="
	// SiteFilePendingSuffix: the Panel's text held beside a kept file. It
	// does not end in .conf, so no include pattern reads it.
	SiteFilePendingSuffix = ".celikpanel-pending"
	// SiteFileBackupMarker: "take CelikPanel's" keeps the owner's file under
	// <file>.celikpanel-backup-<UTC stamp>; never pruned by the Panel.
	SiteFileBackupMarker = ".celikpanel-backup-"
	// ownerIncludeDirName is the directory under the nginx root that holds
	// one owner include directory per site. Nothing in nginx's own
	// configuration includes it; only the site's vhost does.
	ownerIncludeDirName = "celikpanel-sites.d"
	// maxManagedSiteFileBytes bounds what the classifier reads.
	maxManagedSiteFileBytes = 4 << 20
	// maxSiteFileDetail bounds Detail.
	maxSiteFileDetail = 300
)

var errSiteFileChanged = errors.New("the file changed after it was read")

// siteFileNow is the clock of backup names (a test seam).
var siteFileNow = time.Now

// siteFileRename publishes a written file (a test seam: an immutable file,
// `chattr +i`, refuses the rename with EPERM; measured in set8, scenario g).
var siteFileRename = os.Rename

func nginxBase() string {
	if nginxDevMode() {
		return nginxDir
	}
	return "/etc/nginx"
}

// OwnerIncludeDir is the supported place for the owner's additions to one
// site: /etc/nginx/celikpanel-sites.d/<domain>. The vhost includes
// <dir>/*.conf in every server block that serves the site's content.
func OwnerIncludeDir(domain string) string {
	return nginxBase() + "/" + ownerIncludeDirName + "/" + domain
}

// SiteVhostPath is the vhost file of a domain (sites-available).
func SiteVhostPath(domain string) string {
	available, _ := vhostPaths(domain)
	return available
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// SealManagedText puts the render header in front of the Panel's text.
func SealManagedText(body string) string {
	return managedRenderHeaderPrefix + sha256Hex([]byte(body)) + "\n" + body
}

// splitManagedHeader returns the digest the first line declares and the body
// after it. A first line ending in CR (an editor that wrote CRLF) is still read
// as the header, so the edit is "owner-edited", not "foreign".
func splitManagedHeader(content []byte) (string, []byte, bool) {
	newline := bytes.IndexByte(content, '\n')
	if newline < 0 {
		return "", nil, false
	}
	line := bytes.TrimSuffix(content[:newline], []byte("\r"))
	if !bytes.HasPrefix(line, []byte(managedRenderHeaderPrefix)) {
		return "", nil, false
	}
	declared := string(line[len(managedRenderHeaderPrefix):])
	if len(declared) != 64 || strings.Trim(declared, "0123456789abcdef") != "" {
		return "", nil, false
	}
	return declared, content[newline+1:], true
}

// ManagedVhostItem is one site's render and the evidence that decides it.
type ManagedVhostItem struct {
	Domain string
	// Body is the Panel's text without the header.
	Body    string
	Trigger string
	// RecordedSHA256 is the body digest the Panel's ledger last recorded.
	RecordedSHA256 string
	// ExpectedFileSHA256 and ExpectedRenderSHA256 bind "take" to what the
	// owner was shown.
	ExpectedFileSHA256   string
	ExpectedRenderSHA256 string
	// Legacy renders the frozen earlier releases' texts for a headerless
	// file; nil when no earlier text can apply (a new site).
	Legacy func() ([]LegacyVhostRender, error)
}

type siteFileInspection struct {
	state       string
	reason      string
	detail      string
	adoptedFrom string
	exists      bool
	content     []byte
	fileSHA     string
	mode        os.FileMode
	ownership   managedFileOwnership
}

// inspectSiteFile is the classifier. It never follows a symlink, never writes
// and reads at most maxManagedSiteFileBytes.
// inspectSiteFile sınıflandırıcıdır; bağlantı izlemez, yazmaz.
func inspectSiteFile(
	path string,
	legacy func() ([]LegacyVhostRender, error),
	recordedSHA256 string,
) siteFileInspection {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return siteFileInspection{state: transport.SiteFileAbsent}
		}
		return siteFileInspection{
			state: transport.SiteFileUnreadable, reason: unreadableReason(err),
			detail: boundedSiteFileDetail(err.Error()), exists: true,
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return siteFileInspection{state: transport.SiteFileUnreadable, reason: transport.SiteFileReasonSymlink, exists: true}
	}
	if !info.Mode().IsRegular() {
		return siteFileInspection{state: transport.SiteFileUnreadable, reason: transport.SiteFileReasonNotRegular, exists: true}
	}
	if info.Size() > maxManagedSiteFileBytes {
		return siteFileInspection{state: transport.SiteFileUnreadable, reason: transport.SiteFileReasonTooLarge, exists: true}
	}
	content, err := readManagedConfig(path)
	if err != nil {
		return siteFileInspection{
			state: transport.SiteFileUnreadable, reason: unreadableReason(err),
			detail: boundedSiteFileDetail(err.Error()), exists: true,
		}
	}
	if len(content) > maxManagedSiteFileBytes {
		return siteFileInspection{state: transport.SiteFileUnreadable, reason: transport.SiteFileReasonTooLarge, exists: true}
	}
	ownership, err := managedOwnershipFromInfo(info)
	if err != nil {
		return siteFileInspection{
			state: transport.SiteFileUnreadable, reason: transport.SiteFileReasonReadFailed,
			detail: boundedSiteFileDetail(err.Error()), exists: true,
		}
	}
	inspection := siteFileInspection{
		exists: true, content: content, fileSHA: sha256Hex(content),
		mode: info.Mode().Perm(), ownership: ownership,
	}
	if declared, body, ok := splitManagedHeader(content); ok {
		if sha256Hex(body) == declared {
			inspection.state = transport.SiteFileManagedUnchanged
		} else {
			inspection.state = transport.SiteFileOwnerEdited
		}
		return inspection
	}
	if legacy != nil {
		if renders, err := legacy(); err == nil {
			for _, render := range renders {
				if bytes.Equal(content, []byte(render.Text)) {
					inspection.state = transport.SiteFileManagedUnchanged
					inspection.adoptedFrom = render.Release
					return inspection
				}
			}
		}
	}
	if recordedSHA256 != "" {
		inspection.state = transport.SiteFileForeign
	} else {
		inspection.state = transport.SiteFileUnknownOrigin
	}
	return inspection
}

func unreadableReason(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return transport.SiteFileReasonPermission
	}
	if strings.Contains(err.Error(), "not a regular file") {
		return transport.SiteFileReasonNotRegular
	}
	return transport.SiteFileReasonReadFailed
}

func boundedSiteFileDetail(text string) string {
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(text))
	runes := []rune(text)
	if len(runes) > maxSiteFileDetail {
		return string(runes[:maxSiteFileDetail])
	}
	return text
}

// enabledVhostState reads sites-enabled/<domain>.conf without changing it.
func enabledVhostState(enabledPath, availablePath string) string {
	info, err := os.Lstat(enabledPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "absent"
		}
		return "other"
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "other"
	}
	target, err := os.Readlink(enabledPath)
	if err != nil {
		return "other"
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(enabledPath), target)
	}
	if filepath.Clean(target) == filepath.Clean(availablePath) {
		return "link"
	}
	return "other"
}

// atomicWriteSiteFile replaces path through a temporary file in the same
// directory with the given mode and ownership: the previous file's, or 0644
// and the Agent's own for a new one.
func atomicWriteSiteFile(path string, content []byte, mode os.FileMode, ownership managedFileOwnership) (returnErr error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".celikpanel-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	published := false
	defer func() {
		if temporary != nil {
			_ = temporary.Close()
		}
		if !published {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := applyManagedOwnership(temporary, ownership); err != nil {
		return fmt.Errorf("keep the file's owner: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("keep the file's mode: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporary = nil
	if err := siteFileRename(temporaryName, path); err != nil {
		return err
	}
	published = true
	return syncDirectory(directory)
}

// writeManagedSiteFile writes only if the file still is the one inspected:
// same absence, or same bytes as a regular file. A symlink is refused.
func writeManagedSiteFile(path string, content []byte, inspected siteFileInspection) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !inspected.exists {
			return errSiteFileChanged
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errSiteFileChanged
		}
		current, readErr := readManagedConfig(path)
		if readErr != nil {
			return readErr
		}
		if sha256Hex(current) != inspected.fileSHA {
			return errSiteFileChanged
		}
	case errors.Is(err, os.ErrNotExist):
		if inspected.exists {
			return errSiteFileChanged
		}
	default:
		return err
	}
	mode := os.FileMode(0o644)
	var ownership managedFileOwnership
	if inspected.exists {
		mode = inspected.mode
		ownership = inspected.ownership
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWriteSiteFile(path, content, mode, ownership)
}

// writeSiteFileBackup keeps the owner's bytes, mode and owner beside the file
// under a name no include pattern reads.
func writeSiteFileBackup(path string, inspected siteFileInspection) (string, error) {
	stamp := siteFileNow().UTC().Format("20060102T150405Z")
	for attempt := 0; attempt < 50; attempt++ {
		name := path + SiteFileBackupMarker + stamp
		if attempt > 0 {
			name += "-" + strconv.Itoa(attempt+1)
		}
		file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, inspected.mode)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		writeErr := applyManagedOwnership(file, inspected.ownership)
		if writeErr == nil {
			writeErr = file.Chmod(inspected.mode)
		}
		if writeErr == nil {
			_, writeErr = file.Write(inspected.content)
		}
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(name)
			return "", errors.Join(writeErr, closeErr)
		}
		if err := syncDirectory(filepath.Dir(name)); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", errors.New("no free backup name")
}

// writePendingSiteFile holds the Panel's text beside a kept file. Nothing is
// written when the pending file already holds it; a pending path that is not
// a regular file is left alone.
func writePendingSiteFile(path string, sealed []byte) (string, string, error) {
	pending := path + SiteFilePendingSuffix
	info, err := os.Lstat(pending)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("the pending path is not a regular file")
		}
		current, readErr := readManagedConfig(pending)
		if readErr == nil && bytes.Equal(current, sealed) {
			return pending, sha256Hex(sealed), nil
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", "", err
	}
	if err := atomicWriteSiteFile(pending, sealed, 0o644, managedFileOwnership{}); err != nil {
		return "", "", err
	}
	return pending, sha256Hex(sealed), nil
}

// removeStalePending removes the Panel's own pending file once the file is
// the Panel's text again. Only a regular file is removed.
func removeStalePending(path string) {
	pending := path + SiteFilePendingSuffix
	if info, err := os.Lstat(pending); err == nil && info.Mode().IsRegular() {
		_ = os.Remove(pending)
	}
}

// existingPending names a pending file that is there now.
func existingPending(path string) (string, string) {
	pending := path + SiteFilePendingSuffix
	info, err := os.Lstat(pending)
	if err != nil || !info.Mode().IsRegular() {
		return "", ""
	}
	content, err := readManagedConfig(pending)
	if err != nil {
		return pending, ""
	}
	return pending, sha256Hex(content)
}

// ensureOwnerIncludeDir creates the site's owner include directory when it is
// absent. Nothing inside it is ever written, changed or removed.
func ensureOwnerIncludeDir(domain string) error {
	directory := OwnerIncludeDir(domain)
	if info, err := os.Lstat(directory); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory; it was left alone", directory)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}

func normalizedSiteFileTrigger(trigger string) string {
	switch trigger {
	case transport.SiteFileTriggerStartup, transport.SiteFileTriggerCreate,
		transport.SiteFileTriggerRecreate, transport.SiteFileTriggerTake:
		return trigger
	default:
		return transport.SiteFileTriggerChange
	}
}

type plannedSiteFileWrite struct {
	index      int
	domain     string
	available  string
	enabled    string
	sealed     []byte
	inspected  siteFileInspection
	backup     bool
	createLink bool
	outcome    string
	linkMade   bool
}

// ApplyManagedVhosts classifies every item, writes only what the rule allows,
// validates nginx once and reloads once, and only when something was written.
// One item's write failure is that item's result; the others go on. nginx's
// test covers the whole configuration: when it refuses, every file written in
// this call is put back exactly (bytes, mode, owner), and the error says so.
// Kept, missing, unchanged and refused items are results, never errors.
//
// ApplyManagedVhosts her öğeyi sınıflandırır, yalnız kuralın izin verdiğini
// yazar, nginx'i bir kez doğrular ve yalnız bir şey yazıldıysa bir kez yeniden
// yükler. Bir öğenin yazma hatası yalnız o öğenin sonucudur.
func (ng *NginxGenerator) ApplyManagedVhosts(items []ManagedVhostItem) ([]transport.SiteFileResult, error) {
	results := make([]transport.SiteFileResult, len(items))
	if len(items) == 0 {
		return results, nil
	}

	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()

	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := seen[item.Domain]; exists {
			return nil, fmt.Errorf("duplicate vhost domain %q", item.Domain)
		}
		seen[item.Domain] = struct{}{}
	}

	var writes []plannedSiteFileWrite
	var unchanged []string
	for index, item := range items {
		available, enabled := vhostPaths(item.Domain)
		sealed := []byte(SealManagedText(item.Body))
		trigger := normalizedSiteFileTrigger(item.Trigger)
		inspected := inspectSiteFile(available, item.Legacy, item.RecordedSHA256)
		result := &results[index]
		*result = transport.SiteFileResult{
			Kind:         transport.SiteFileKindNginxVhost,
			Path:         available,
			State:        inspected.state,
			Reason:       inspected.reason,
			Detail:       inspected.detail,
			AdoptedFrom:  inspected.adoptedFrom,
			RenderSHA256: sha256Hex([]byte(item.Body)),
			FileSHA256:   inspected.fileSHA,
			IncludeDir:   OwnerIncludeDir(item.Domain),
			Enabled:      enabledVhostState(enabled, available),
		}
		plan := plannedSiteFileWrite{
			index: index, domain: item.Domain, available: available,
			enabled: enabled, sealed: sealed, inspected: inspected,
		}
		switch inspected.state {
		case transport.SiteFileManagedUnchanged:
			if bytes.Equal(inspected.content, sealed) {
				result.Outcome = transport.SiteFileOutcomeUnchanged
				result.WrittenSHA256 = result.RenderSHA256
				unchanged = append(unchanged, available)
				continue
			}
			plan.outcome = transport.SiteFileOutcomeWritten
			if trigger == transport.SiteFileTriggerTake {
				plan.outcome = transport.SiteFileOutcomeTaken
			}
			writes = append(writes, plan)
		case transport.SiteFileAbsent:
			switch trigger {
			case transport.SiteFileTriggerCreate:
				plan.outcome = transport.SiteFileOutcomeWritten
			case transport.SiteFileTriggerRecreate:
				plan.outcome = transport.SiteFileOutcomeRecreated
			default:
				result.Outcome = transport.SiteFileOutcomeMissing
				continue
			}
			plan.createLink = result.Enabled == "absent"
			writes = append(writes, plan)
		case transport.SiteFileOwnerEdited, transport.SiteFileForeign, transport.SiteFileUnknownOrigin:
			switch trigger {
			case transport.SiteFileTriggerTake:
				if item.ExpectedFileSHA256 == "" || item.ExpectedFileSHA256 != inspected.fileSHA ||
					(item.ExpectedRenderSHA256 != "" && item.ExpectedRenderSHA256 != result.RenderSHA256) {
					result.Outcome = transport.SiteFileOutcomeRefused
					result.Reason = transport.SiteFileReasonChanged
					continue
				}
				plan.outcome = transport.SiteFileOutcomeTaken
				plan.backup = true
				writes = append(writes, plan)
			case transport.SiteFileTriggerCreate:
				result.Outcome = transport.SiteFileOutcomeRefused
				result.Reason = transport.SiteFileReasonNotApplicable
			default:
				result.Outcome = transport.SiteFileOutcomeKept
				pendingPath, pendingSHA, err := writePendingSiteFile(available, sealed)
				if err != nil {
					result.Reason = transport.SiteFileReasonPendingNotSet
					result.Detail = boundedSiteFileDetail(err.Error())
					continue
				}
				result.PendingPath = pendingPath
				result.PendingSHA256 = pendingSHA
			}
		default: // unreadable
			result.Outcome = transport.SiteFileOutcomeRefused
		}
	}

	var touched []plannedSiteFileWrite
	for _, plan := range writes {
		result := &results[plan.index]
		if plan.backup {
			backup, err := writeSiteFileBackup(plan.available, plan.inspected)
			if err != nil {
				result.Outcome = transport.SiteFileOutcomeRefused
				result.Reason = transport.SiteFileReasonWriteRefused
				result.Detail = boundedSiteFileDetail("keep a copy of the file: " + err.Error())
				continue
			}
			result.BackupPath = backup
		}
		if err := ensureOwnerIncludeDir(plan.domain); err != nil {
			// nginx accepts an include pattern that matches nothing; the
			// directory is a convenience, its absence is only reported.
			result.Detail = boundedSiteFileDetail("owner include directory: " + err.Error())
		}
		if err := writeManagedSiteFile(plan.available, plan.sealed, plan.inspected); err != nil {
			// The file is published by one rename; when that (or anything
			// before it) fails, the file on disk is the one that was read and
			// its enabled link was not touched. Only this site is refused;
			// the batch goes on (set8 scenario g: an immutable file).
			result.Outcome = transport.SiteFileOutcomeRefused
			if errors.Is(err, errSiteFileChanged) {
				result.Reason = transport.SiteFileReasonChanged
			} else {
				result.Reason = transport.SiteFileReasonWriteRefused
				result.Detail = boundedSiteFileDetail(err.Error())
			}
			continue
		}
		if plan.createLink {
			err := os.MkdirAll(filepath.Dir(plan.enabled), 0o755)
			if err == nil {
				err = atomicReplaceSymlink(plan.enabled, plan.available)
			}
			if err != nil {
				restoreErr := restorePlannedSiteFile(plan)
				result.Outcome = transport.SiteFileOutcomeFailed
				result.Reason = transport.SiteFileReasonWriteRefused
				result.Detail = boundedSiteFileDetail(errors.Join(fmt.Errorf("enable the vhost: %w", err), restoreErr).Error())
				continue
			}
			plan.linkMade = true
			result.Enabled = "link"
		}
		touched = append(touched, plan)
	}

	for _, path := range unchanged {
		removeStalePending(path)
	}
	if len(touched) == 0 {
		return results, nil
	}

	failTouched := func(reason, detail string) {
		for _, plan := range touched {
			result := &results[plan.index]
			result.Outcome = transport.SiteFileOutcomeFailed
			result.Reason = reason
			result.Detail = boundedSiteFileDetail(detail)
			result.Enabled = enabledVhostState(plan.enabled, plan.available)
			result.FileSHA256 = plan.inspected.fileSHA
		}
	}
	if err := ng.ValidateNginx(); err != nil {
		detail := err.Error()
		var refused *NginxConfigRefusedError
		if errors.As(err, &refused) {
			detail = refused.FirstLine()
		}
		rollbackErr := restorePlannedSiteFiles(touched)
		failTouched(transport.SiteFileReasonNginxRefused, detail)
		return results, ng.finishManagedRollback(fmt.Errorf("nginx validation failed: %w", err), rollbackErr)
	}
	if err := ng.ReloadNginx(); err != nil {
		rollbackErr := restorePlannedSiteFiles(touched)
		failTouched(transport.SiteFileReasonReloadFailed, err.Error())
		return results, ng.finishManagedRollback(fmt.Errorf("nginx reload failed: %w", err), rollbackErr)
	}
	for _, plan := range touched {
		result := &results[plan.index]
		result.Outcome = plan.outcome
		result.WrittenSHA256 = result.RenderSHA256
		result.FileSHA256 = sha256Hex(plan.sealed)
		result.Reloaded = true
		result.PendingPath, result.PendingSHA256 = "", ""
		removeStalePending(plan.available)
	}
	return results, nil
}

func (ng *NginxGenerator) finishManagedRollback(cause, rollbackErr error) error {
	if rollbackErr != nil {
		return fmt.Errorf("%w; rollback incomplete: %v", cause, rollbackErr)
	}
	if err := ng.ValidateNginx(); err != nil {
		return fmt.Errorf("%w; rollback incomplete: rollback validation: %v", cause, err)
	}
	if err := ng.ReloadNginx(); err != nil {
		return fmt.Errorf("%w; rollback incomplete: rollback reload: %v", cause, err)
	}
	return &VhostRestoredError{Cause: cause}
}

func restorePlannedSiteFiles(touched []plannedSiteFileWrite) error {
	var restoreErrors []error
	for index := len(touched) - 1; index >= 0; index-- {
		if err := restorePlannedSiteFile(touched[index]); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore %s: %w", touched[index].domain, err))
		}
	}
	return errors.Join(restoreErrors...)
}

// restorePlannedSiteFile puts back exactly what was there: the previous bytes
// with their mode and owner, or no file; a link this call made is removed.
func restorePlannedSiteFile(plan plannedSiteFileWrite) error {
	var restoreErrors []error
	if plan.linkMade {
		if info, err := os.Lstat(plan.enabled); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(plan.enabled); err != nil {
				restoreErrors = append(restoreErrors, err)
			}
		}
	}
	if plan.inspected.exists {
		if err := atomicWriteSiteFile(plan.available, plan.inspected.content, plan.inspected.mode, plan.inspected.ownership); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	} else if info, err := os.Lstat(plan.available); err == nil && info.Mode().IsRegular() {
		if err := os.Remove(plan.available); err != nil {
			restoreErrors = append(restoreErrors, err)
		}
	}
	return errors.Join(restoreErrors...)
}

// ApplyManagedVhost is ApplyManagedVhosts for one site.
func (ng *NginxGenerator) ApplyManagedVhost(item ManagedVhostItem) (transport.SiteFileResult, error) {
	results, err := ng.ApplyManagedVhosts([]ManagedVhostItem{item})
	if len(results) != 1 {
		return transport.SiteFileResult{}, err
	}
	return results[0], err
}

// SiteFileDiffLimits bound the diff the domain screen shows.
const (
	maxSiteFileDiffBytes = 64 << 10
	maxSiteFileDiffLines = 4000
)

// InspectManagedVhost classifies the file and computes the difference between
// it and the Panel's text, writing nothing.
// InspectManagedVhost dosyayı sınıflandırır ve farkı hesaplar; hiçbir şey
// yazmaz.
func (ng *NginxGenerator) InspectManagedVhost(item ManagedVhostItem) (transport.SiteFileResult, string, bool) {
	nginxMutationMu.Lock()
	defer nginxMutationMu.Unlock()

	available, enabled := vhostPaths(item.Domain)
	sealed := SealManagedText(item.Body)
	inspected := inspectSiteFile(available, item.Legacy, item.RecordedSHA256)
	result := transport.SiteFileResult{
		Kind:         transport.SiteFileKindNginxVhost,
		Path:         available,
		State:        inspected.state,
		Outcome:      transport.SiteFileOutcomeInspected,
		Reason:       inspected.reason,
		Detail:       inspected.detail,
		AdoptedFrom:  inspected.adoptedFrom,
		RenderSHA256: sha256Hex([]byte(item.Body)),
		FileSHA256:   inspected.fileSHA,
		IncludeDir:   OwnerIncludeDir(item.Domain),
		Enabled:      enabledVhostState(enabled, available),
	}
	if inspected.state == transport.SiteFileManagedUnchanged && inspected.adoptedFrom == "" {
		if declared, _, ok := splitManagedHeader(inspected.content); ok {
			result.WrittenSHA256 = declared
		}
	}
	result.PendingPath, result.PendingSHA256 = existingPending(available)
	if !inspected.exists || inspected.state == transport.SiteFileUnreadable || bytes.Equal(inspected.content, []byte(sealed)) {
		return result, "", false
	}
	diff, truncated := UnifiedSiteFileDiff(
		available+" (on this server)", "CelikPanel's text",
		stripManagedHeader(inspected.content), stripManagedHeader([]byte(sealed)),
	)
	return result, diff, truncated
}

func stripManagedHeader(content []byte) []byte {
	if bytes.HasPrefix(content, []byte(managedRenderHeaderPrefix)) {
		if newline := bytes.IndexByte(content, '\n'); newline >= 0 {
			return content[newline+1:]
		}
	}
	return content
}

// RemoveSiteVhost removes a deleted site's vhost the way RemoveVhost always
// did, after keeping a copy of a file that is not the Panel's unchanged text
// beside it. The owner include directory and everything in it are left; the
// Panel's pending file is removed.
// RemoveSiteVhost, silinen sitenin vhost'unu kaldırır; Panel'in değişmemiş
// metni olmayan dosyanın bir kopyasını önce yanında bırakır.
func (ng *NginxGenerator) RemoveSiteVhost(domain string) (string, error) {
	available, _ := vhostPaths(domain)
	backup := ""
	nginxMutationMu.Lock()
	inspected := inspectSiteFile(available, nil, "")
	if inspected.exists && inspected.state != transport.SiteFileUnreadable &&
		inspected.state != transport.SiteFileManagedUnchanged {
		name, err := writeSiteFileBackup(available, inspected)
		if err != nil {
			nginxMutationMu.Unlock()
			return "", fmt.Errorf("keep a copy of the edited vhost before removing it: %w", err)
		}
		backup = name
	}
	nginxMutationMu.Unlock()
	if err := ng.RemoveVhost(domain); err != nil {
		return backup, err
	}
	removeStalePending(available)
	return backup, nil
}
