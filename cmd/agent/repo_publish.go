package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	repoManagedFileMode = os.FileMode(0o644)
	repoJournalFileMode = os.FileMode(0o600)
	repoJournalVersion  = 1
)

type repoRecipePaths struct {
	Keyring       string
	Source        string
	StaleKeyrings []string
}

type repoFileSnapshot struct {
	exists bool
	data   []byte
}

type repoJournalEntry struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
}

type repoTransactionJournal struct {
	Version   int                `json:"version"`
	Operation string             `json:"operation"`
	Entries   []repoJournalEntry `json:"entries"`
}

type repoTransactionError struct {
	cause       error
	rollbackErr error
}

func (e *repoTransactionError) Error() string {
	return fmt.Sprintf("%v; repository rollback failed: %v", e.cause, e.rollbackErr)
}

func (e *repoTransactionError) Unwrap() error {
	return e.cause
}

// renameRepoFile is replaceable only by package tests that force the second
// publish step to fail and verify rollback of the first step.
// renameRepoFile yalnız ikinci yayın adımını zorla başarısız kılan ve ilk
// adımın geri alındığını doğrulayan paket testleri tarafından değiştirilebilir.
var renameRepoFile = os.Rename

// repoFileOwnerUID and syncRepoDirectory are replaceable only by focused tests
// that verify Linux ownership and crash-durability decisions without root.
// repoFileOwnerUID ile syncRepoDirectory yalnız root olmadan Linux sahipliği ve
// çökme kalıcılığı kararlarını doğrulayan odaklı testlerce değiştirilebilir.
var (
	repoFileOwnerUID  = platformRepoFileOwnerUID
	syncRepoDirectory = platformSyncRepoDirectory
)

// stageRepoFile writes a world-readable file next to its final path. A rename
// within one directory is atomic, and explicit chmod is required because the
// agent service runs with a restrictive umask while apt reads as _apt.
//
// stageRepoFile, dosyayi nihai yolunun yaninda ve herkesce okunur olarak
// hazirlar. Ayni dizindeki rename atomiktir; agent umask'i kisitli, apt ise _apt
// olarak okudugu icin chmod acikca uygulanir.
func stageRepoFile(finalPath, marker string, data []byte) (string, error) {
	return stageRepoFileMode(finalPath, marker, data, repoManagedFileMode)
}

// stageRepoFileMode writes, chmods and fsyncs one temporary file before it can
// be renamed into a managed pathname.
// stageRepoFileMode, yönetilen bir yola rename edilmeden önce tek bir geçici
// dosyayı yazar, chmod uygular ve fsync eder.
func stageRepoFileMode(finalPath, marker string, data []byte, mode os.FileMode) (string, error) {
	dir := filepath.Dir(finalPath)
	ext := filepath.Ext(finalPath)
	base := filepath.Base(finalPath)
	base = base[:len(base)-len(ext)]
	f, err := os.CreateTemp(dir, "."+base+"-"+marker+"-*"+ext)
	if err != nil {
		return "", err
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

// validateRepoFileMetadata accepts only a regular, non-symlink file with the
// exact expected mode; on Linux the owner must also be root.
// validateRepoFileMetadata, yalnız tam beklenen moda sahip normal ve symlink
// olmayan dosyayı kabul eder; Linux'ta sahibi ayrıca root olmalıdır.
func validateRepoFileMetadata(info os.FileInfo, expectedMode os.FileMode) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repository file must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("repository file is not a regular file")
	}
	uid, unixMetadata := repoFileOwnerUID(info)
	if unixMetadata {
		return validateRepoUnixMetadata(info.Mode().Perm(), uid, expectedMode)
	}
	return validateRepoFileMode(info.Mode().Perm(), expectedMode)
}

// validateRepoUnixMetadata keeps the Linux trust-file policy pure and directly
// testable: root-owned, not group/world-writable, and exactly the expected mode.
// validateRepoUnixMetadata, Linux güven dosyası ilkesini saf ve doğrudan test
// edilebilir tutar: root sahipliği, grup/dünya yazma yasağı ve tam beklenen mod.
func validateRepoUnixMetadata(mode os.FileMode, uid uint32, expectedMode os.FileMode) error {
	if uid != 0 {
		return fmt.Errorf("repository file owner uid is %d, want root", uid)
	}
	return validateRepoFileMode(mode, expectedMode)
}

func validateRepoFileMode(mode os.FileMode, expectedMode os.FileMode) error {
	if mode&0o022 != 0 {
		return fmt.Errorf("repository file is group/world-writable: mode %04o", mode)
	}
	if mode != expectedMode {
		return fmt.Errorf("repository file mode is %04o, want %04o", mode, expectedMode)
	}
	return nil
}

// openRepoRegularFile rejects symlinks before opening and proves that the
// opened descriptor is still the same regular file observed by lstat.
// openRepoRegularFile, açmadan önce symlinkleri reddeder ve açılan descriptor'ın
// lstat ile görülen aynı normal dosya olduğunu kanıtlar.
func openRepoRegularFile(path string) (*os.File, os.FileInfo, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("repository file must not be a symbolic link")
	}
	if !linkInfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("repository file is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !os.SameFile(linkInfo, info) {
		file.Close()
		return nil, nil, fmt.Errorf("repository file changed while it was opened")
	}
	afterOpen, err := os.Lstat(path)
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if afterOpen.Mode()&os.ModeSymlink != 0 || !afterOpen.Mode().IsRegular() || !os.SameFile(info, afterOpen) {
		file.Close()
		return nil, nil, fmt.Errorf("repository path changed to a symlink or non-regular file while it was opened")
	}
	return file, info, nil
}

// readSecureRepoFile validates metadata on the opened descriptor before
// reading, closing the lstat/open race used by a swapped symlink.
// readSecureRepoFile, okumadan önce açılmış descriptor üzerindeki metaveriyi
// doğrular ve değiştirilmiş symlink ile oluşan lstat/open yarışını kapatır.
func readSecureRepoFile(path string, expectedMode os.FileMode) ([]byte, error) {
	file, info, err := openRepoRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := validateRepoFileMetadata(info, expectedMode); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) || after.Mode() != info.Mode() {
		return nil, fmt.Errorf("repository file changed while it was read")
	}
	if err := validateRepoFileMetadata(after, expectedMode); err != nil {
		return nil, fmt.Errorf("repository file metadata changed while it was read: %w", err)
	}
	afterPath, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if afterPath.Mode()&os.ModeSymlink != 0 || !afterPath.Mode().IsRegular() || !os.SameFile(after, afterPath) {
		return nil, fmt.Errorf("repository path changed to a symlink or non-regular file while it was read")
	}
	return data, nil
}

func snapshotRepoFile(path string) (repoFileSnapshot, error) {
	file, _, err := openRepoRegularFile(path)
	if os.IsNotExist(err) {
		return repoFileSnapshot{}, nil
	}
	if err != nil {
		return repoFileSnapshot{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return repoFileSnapshot{}, err
	}
	return repoFileSnapshot{exists: true, data: data}, nil
}

func restoreRepoFile(path string, snapshot repoFileSnapshot) error {
	if !snapshot.exists {
		return removeRepoFileVerified(path)
	}
	staged, err := stageRepoFile(path, "restore", snapshot.data)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(staged) }()
	if err := renameRepoFile(staged, path); err != nil {
		return err
	}
	return syncRepoDirectory(filepath.Dir(path))
}

// removeRepoFileVerified removes one managed repository file and then proves
// that the pathname is absent. ENOENT is idempotent success; every other
// removal or verification error must reach the caller.
//
// removeRepoFileVerified, yonetilen bir depo dosyasini kaldirir ve ardindan
// yolun artik bulunmadigini kanitlar. ENOENT idempotent basaridir; diger tum
// kaldirma veya dogrulama hatalari cagirani ulasmalidir.
func removeRepoFileVerified(path string) error {
	if path == "" {
		return fmt.Errorf("repository file path is empty")
	}
	removed := false
	if err := os.Remove(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		removed = true
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("repository file still exists after removal")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("verify repository file removal: %w", err)
	}
	if removed {
		if err := syncRepoDirectory(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

// repoRecipeManagedPaths returns the deduplicated path allowlist that one
// transaction journal may restore.
// repoRecipeManagedPaths, tek bir işlem günlüğünün geri yükleyebileceği
// yinelenmeyen yol izin listesini döndürür.
func repoRecipeManagedPaths(paths repoRecipePaths) []string {
	return dedupeRepoPaths(append([]string{paths.Source, paths.Keyring}, paths.StaleKeyrings...))
}

func dedupeRepoPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result
}

func repoJournalPath(source string) string {
	return source + ".transaction.json"
}

// snapshotRepoEntries captures every pathname before the first mutation. A
// symlink or non-regular file fails closed before anything is removed.
// snapshotRepoEntries, ilk değişiklikten önce her yolu yakalar. Symlink ya da
// normal olmayan dosya, hiçbir şey kaldırılmadan önce işlemi kapalı başarısız eder.
func snapshotRepoEntries(paths []string) ([]repoJournalEntry, error) {
	entries := make([]repoJournalEntry, 0, len(paths))
	for _, path := range dedupeRepoPaths(paths) {
		snapshot, err := snapshotRepoFile(path)
		if err != nil {
			return nil, fmt.Errorf("snapshot repository file %s: %w", path, err)
		}
		entries = append(entries, repoJournalEntry{
			Path: path, Exists: snapshot.exists, Data: snapshot.data,
		})
	}
	return entries, nil
}

// writeRepoTransactionJournal persists old file contents before the first
// rename/removal and fsyncs the journal's parent directory.
// writeRepoTransactionJournal, ilk rename/kaldırmadan önce eski dosya
// içeriklerini kalıcılaştırır ve günlüğün üst dizinini fsync eder.
func writeRepoTransactionJournal(source, operation string, entries []repoJournalEntry) error {
	journalPath := repoJournalPath(source)
	if _, err := os.Lstat(journalPath); err == nil {
		return fmt.Errorf("repository transaction journal already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect repository transaction journal: %w", err)
	}
	payload, err := json.Marshal(repoTransactionJournal{
		Version: repoJournalVersion, Operation: operation, Entries: entries,
	})
	if err != nil {
		return fmt.Errorf("encode repository transaction journal: %w", err)
	}
	staged, err := stageRepoFileMode(journalPath, "journal", payload, repoJournalFileMode)
	if err != nil {
		return fmt.Errorf("stage repository transaction journal: %w", err)
	}
	defer func() { _ = os.Remove(staged) }()
	if err := renameRepoFile(staged, journalPath); err != nil {
		return fmt.Errorf("publish repository transaction journal: %w", err)
	}
	if err := syncRepoDirectory(filepath.Dir(journalPath)); err != nil {
		_ = os.Remove(journalPath)
		return fmt.Errorf("persist repository transaction journal: %w", err)
	}
	return nil
}

func readRepoTransactionJournal(source string) (*repoTransactionJournal, error) {
	data, err := readSecureRepoFile(repoJournalPath(source), repoJournalFileMode)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repository transaction journal: %w", err)
	}
	var journal repoTransactionJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("decode repository transaction journal: %w", err)
	}
	return &journal, nil
}

func validateRepoTransactionJournal(journal *repoTransactionJournal, source string, allowedPaths []string) error {
	if journal == nil || journal.Version != repoJournalVersion {
		return fmt.Errorf("unsupported repository transaction journal version")
	}
	if journal.Operation != "publish" && journal.Operation != "disable" {
		return fmt.Errorf("invalid repository transaction operation")
	}
	if len(journal.Entries) == 0 {
		return fmt.Errorf("repository transaction journal has no entries")
	}
	allowed := make(map[string]struct{}, len(allowedPaths))
	for _, path := range dedupeRepoPaths(allowedPaths) {
		allowed[path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(journal.Entries))
	hasSource := false
	for _, entry := range journal.Entries {
		if _, ok := allowed[entry.Path]; !ok {
			return fmt.Errorf("repository transaction journal contains an unmanaged path")
		}
		if _, duplicate := seen[entry.Path]; duplicate {
			return fmt.Errorf("repository transaction journal contains a duplicate path")
		}
		seen[entry.Path] = struct{}{}
		hasSource = hasSource || entry.Path == source
	}
	if !hasSource {
		return fmt.Errorf("repository transaction journal is missing its source path")
	}
	return nil
}

// recoverRepoTransaction always rolls an interrupted two-file operation back
// to its journaled pre-operation state; recovery is idempotent until journal
// removal and its parent-directory fsync both succeed.
// recoverRepoTransaction, yarım kalmış iki dosyalı işlemi her zaman günlükteki
// işlem öncesi duruma döndürür; günlük kaldırma ve üst dizin fsync başarılı olana
// dek kurtarma idempotent kalır.
func recoverRepoTransaction(source string, allowedPaths []string) error {
	journal, err := readRepoTransactionJournal(source)
	if err != nil || journal == nil {
		return err
	}
	if err := validateRepoTransactionJournal(journal, source, allowedPaths); err != nil {
		return err
	}
	// Restore keys before the source so apt never observes a live source whose
	// signed-by target is absent.
	// apt hiçbir zaman signed-by hedefi eksik canlı kaynak görmesin diye
	// anahtarları kaynaktan önce geri yükle.
	for _, sourcePass := range []bool{false, true} {
		for _, entry := range journal.Entries {
			if (entry.Path == source) != sourcePass {
				continue
			}
			if err := restoreRepoFile(entry.Path, repoFileSnapshot{
				exists: entry.Exists, data: entry.Data,
			}); err != nil {
				return fmt.Errorf("restore repository file %s: %w", entry.Path, err)
			}
		}
	}
	if err := removeRepoFileVerified(repoJournalPath(source)); err != nil {
		return fmt.Errorf("commit repository transaction recovery: %w", err)
	}
	return nil
}

func beginRepoTransaction(source, operation string, allowedPaths []string) ([]repoJournalEntry, error) {
	if err := recoverRepoTransaction(source, allowedPaths); err != nil {
		return nil, fmt.Errorf("recover previous repository transaction: %w", err)
	}
	entries, err := snapshotRepoEntries(allowedPaths)
	if err != nil {
		return nil, err
	}
	if err := writeRepoTransactionJournal(source, operation, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func rollbackRepoTransaction(source, operation string, allowedPaths []string, entries []repoJournalEntry, cause error) error {
	if _, err := os.Lstat(repoJournalPath(source)); errors.Is(err, os.ErrNotExist) {
		if journalErr := writeRepoTransactionJournal(source, operation, entries); journalErr != nil {
			return &repoTransactionError{cause: cause, rollbackErr: journalErr}
		}
	} else if err != nil {
		return &repoTransactionError{cause: cause, rollbackErr: err}
	}
	if err := recoverRepoTransaction(source, allowedPaths); err != nil {
		return &repoTransactionError{cause: cause, rollbackErr: err}
	}
	return cause
}

func commitRepoTransaction(source string) error {
	return removeRepoFileVerified(repoJournalPath(source))
}

func repoMutationApplied(err error) bool {
	var transactionErr *repoTransactionError
	return errors.As(err, &transactionErr) && transactionErr.rollbackErr != nil
}

// disableRepoRecipe journals every old file, removes the source first, and
// commits only after apt refresh succeeds. Any failure restores every source
// and keyring byte with a secure 0644 mode.
// disableRepoRecipe, her eski dosyayı günlüğe alır, önce kaynağı kaldırır ve
// yalnız apt yenilemesi başarılı olunca commit eder. Her hata tüm kaynak ve
// keyring baytlarını güvenli 0644 moduyla geri yükler.
func disableRepoRecipe(
	source string,
	keyrings []string,
	refresh func() ([]byte, error),
) error {
	managedPaths := dedupeRepoPaths(append([]string{source}, keyrings...))
	entries, err := beginRepoTransaction(source, "disable", managedPaths)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		return rollbackRepoTransaction(source, "disable", managedPaths, entries, cause)
	}
	if err := removeRepoFileVerified(source); err != nil {
		return fail(fmt.Errorf("remove repository source: %w", err))
	}

	for _, keyring := range dedupeRepoPaths(keyrings) {
		if err := removeRepoFileVerified(keyring); err != nil {
			return fail(fmt.Errorf("remove repository keyring %s: %w", keyring, err))
		}
	}
	if refresh == nil {
		return fail(fmt.Errorf("repository refresh function is missing"))
	}
	if output, err := refresh(); err != nil {
		return fail(fmt.Errorf(
			"apt update after disabling repository failed: %s",
			cleanCommandError(output, err),
		))
	}
	if err := commitRepoTransaction(source); err != nil {
		return fail(fmt.Errorf("commit repository disable transaction: %w", err))
	}
	return nil
}

// publishRepoRecipe journals all old paths, renames the key first and source
// last, and fsyncs each parent directory. The journal is the commit marker:
// until its durable removal, recovery deterministically restores the old pair.
// publishRepoRecipe, tüm eski yolları günlüğe alır, önce anahtarı sonra kaynağı
// rename eder ve her üst dizini fsync eder. Günlük commit işaretidir: kalıcı
// kaldırılana dek kurtarma eski çifti deterministik olarak geri yükler.
func publishRepoRecipe(paths repoRecipePaths, stagedKey, stagedSource string) error {
	managedPaths := repoRecipeManagedPaths(paths)
	entries, err := beginRepoTransaction(paths.Source, "publish", managedPaths)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		return rollbackRepoTransaction(paths.Source, "publish", managedPaths, entries, cause)
	}
	if err := renameRepoFile(stagedKey, paths.Keyring); err != nil {
		return fail(fmt.Errorf("publish keyring: %w", err))
	}
	if err := syncRepoDirectory(filepath.Dir(paths.Keyring)); err != nil {
		return fail(fmt.Errorf("persist published keyring: %w", err))
	}
	if err := renameRepoFile(stagedSource, paths.Source); err != nil {
		return fail(fmt.Errorf("publish source: %w", err))
	}
	if err := syncRepoDirectory(filepath.Dir(paths.Source)); err != nil {
		return fail(fmt.Errorf("persist published source: %w", err))
	}
	for _, stale := range paths.StaleKeyrings {
		if stale != "" && stale != paths.Keyring {
			if err := removeRepoFileVerified(stale); err != nil {
				return fail(fmt.Errorf("remove stale repository keyring %s: %w", stale, err))
			}
		}
	}
	if err := commitRepoTransaction(paths.Source); err != nil {
		return fail(fmt.Errorf("commit repository publish transaction: %w", err))
	}
	return nil
}

// prepareAndPublishRepoRecipe validates a staged source before changing either
// live file. A failed download, write or apt refresh therefore leaves the
// previous working recipe byte-for-byte intact.
//
// prepareAndPublishRepoRecipe, canli iki dosyadan birini degistirmeden once
// staged kaynagi dogrular. Basarisiz indirme, yazma veya apt yenileme boylece
// onceki calisan tarifi bayt bayt korur.
func prepareAndPublishRepoRecipe(
	paths repoRecipePaths,
	key []byte,
	source string,
	validate func(stagedSource string) ([]byte, error),
) error {
	stagedKey, err := stageRepoFile(paths.Keyring, "validate", key)
	if err != nil {
		return fmt.Errorf("stage keyring: %w", err)
	}
	defer func() { _ = os.Remove(stagedKey) }()

	validationSource := signedRepoSource(source, stagedKey) + "\n"
	stagedValidationSource, err := stageRepoFile(paths.Source, "validate", []byte(validationSource))
	if err != nil {
		return fmt.Errorf("stage source: %w", err)
	}
	defer func() { _ = os.Remove(stagedValidationSource) }()
	if out, err := validate(stagedValidationSource); err != nil {
		return fmt.Errorf("apt update for staged repository failed: %s", cleanCommandError(out, err))
	}

	finalSource := signedRepoSource(source, paths.Keyring) + "\n"
	stagedFinalSource, err := stageRepoFile(paths.Source, "publish", []byte(finalSource))
	if err != nil {
		return fmt.Errorf("stage final source: %w", err)
	}
	defer func() { _ = os.Remove(stagedFinalSource) }()
	if err := publishRepoRecipe(paths, stagedKey, stagedFinalSource); err != nil {
		return err
	}
	return nil
}

func cleanCommandError(output []byte, err error) string {
	detail := string(output)
	if detail == "" && err != nil {
		detail = err.Error()
	}
	return detail
}
