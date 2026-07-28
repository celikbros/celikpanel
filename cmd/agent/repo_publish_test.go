package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func disableRepoOwnershipCheckForTest(t *testing.T) {
	t.Helper()
	originalOwner := repoFileOwnerUID
	// Tests run without root (and may run on Windows), so ownership enforcement
	// is covered by the pure metadata tests below instead of host credentials.
	// Testler root olmadan (ve Windows'ta) calisabilir; bu nedenle sahiplik
	// zorlamasi ana makine kimligi yerine asagidaki saf metaveri testleriyle sinanir.
	repoFileOwnerUID = func(os.FileInfo) (uint32, bool) { return 0, false }
	t.Cleanup(func() { repoFileOwnerUID = originalOwner })
}

func testRepoRecipePaths(t *testing.T) repoRecipePaths {
	t.Helper()
	disableRepoOwnershipCheckForTest(t)
	dir := t.TempDir()
	return repoRecipePaths{
		Keyring:       filepath.Join(dir, "celikpanel-test.gpg"),
		Source:        filepath.Join(dir, "celikpanel-test.list"),
		StaleKeyrings: []string{filepath.Join(dir, "celikpanel-test.asc")},
	}
}

func mustWriteRepoTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadRepoTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertRepoTestPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository path still exists: %s (error=%v)", path, err)
	}
}

func TestDisableRepoRecipeRemovesAndVerifiesBeforeRefresh(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	missingKeyring := filepath.Join(filepath.Dir(paths.Keyring), "missing-key.asc")

	refreshes := 0
	err := disableRepoRecipe(
		paths.Source,
		[]string{paths.Keyring, missingKeyring, paths.Keyring},
		func() ([]byte, error) {
			refreshes++
			assertRepoTestPathAbsent(t, paths.Source)
			assertRepoTestPathAbsent(t, paths.Keyring)
			assertRepoTestPathAbsent(t, missingKeyring)
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 {
		t.Fatalf("apt refreshes = %d, want 1", refreshes)
	}
	assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
}

func TestDisableRepoRecipeStopsWhenSourceRemovalFails(t *testing.T) {
	paths := testRepoRecipePaths(t)
	if err := os.Mkdir(paths.Source, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWriteRepoTestFile(t, filepath.Join(paths.Source, "keep"), "occupied")
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	refreshed := false

	err := disableRepoRecipe(paths.Source, []string{paths.Keyring}, func() ([]byte, error) {
		refreshed = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot repository file") {
		t.Fatalf("error = %v, want pre-mutation source rejection", err)
	}
	if refreshed {
		t.Fatal("apt refresh ran after source removal failed")
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
		t.Fatalf("keyring changed after source removal failure: %q", got)
	}
}

func TestDisableRepoRecipeReportsKeyAndAptRefreshFailures(t *testing.T) {
	t.Run("key removal", func(t *testing.T) {
		paths := testRepoRecipePaths(t)
		mustWriteRepoTestFile(t, paths.Source, "old-source\n")
		if err := os.Mkdir(paths.Keyring, 0o700); err != nil {
			t.Fatal(err)
		}
		mustWriteRepoTestFile(t, filepath.Join(paths.Keyring, "keep"), "occupied")
		refreshed := false

		err := disableRepoRecipe(paths.Source, []string{paths.Keyring}, func() ([]byte, error) {
			refreshed = true
			return nil, nil
		})
		if err == nil || !strings.Contains(err.Error(), "snapshot repository file") {
			t.Fatalf("error = %v, want pre-mutation keyring rejection", err)
		}
		if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
			t.Fatalf("source changed after unsafe keyring rejection: %q", got)
		}
		if refreshed {
			t.Fatal("apt refresh ran after keyring removal failed")
		}
	})

	t.Run("apt refresh", func(t *testing.T) {
		paths := testRepoRecipePaths(t)
		mustWriteRepoTestFile(t, paths.Source, "old-source\n")
		mustWriteRepoTestFile(t, paths.Keyring, "old-key")
		mustWriteRepoTestFile(t, paths.StaleKeyrings[0], "old-stale-key")

		err := disableRepoRecipe(paths.Source, []string{paths.Keyring, paths.StaleKeyrings[0], paths.Keyring}, func() ([]byte, error) {
			return []byte("mirror unavailable"), errors.New("exit status 100")
		})
		if err == nil || !strings.Contains(err.Error(), "mirror unavailable") {
			t.Fatalf("error = %v, want apt refresh output", err)
		}
		if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
			t.Fatalf("source was not rolled back after apt failure: %q", got)
		}
		if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
			t.Fatalf("keyring was not rolled back after apt failure: %q", got)
		}
		if got := mustReadRepoTestFile(t, paths.StaleKeyrings[0]); got != "old-stale-key" {
			t.Fatalf("alternate keyring was not rolled back after apt failure: %q", got)
		}
		if repoMutationApplied(err) {
			t.Fatalf("rollback succeeded but mutation was marked applied: %v", err)
		}
		assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
		if runtime.GOOS != "windows" {
			for _, path := range []string{paths.Source, paths.Keyring, paths.StaleKeyrings[0]} {
				info, statErr := os.Stat(path)
				if statErr != nil {
					t.Fatal(statErr)
				}
				if info.Mode().Perm() != repoManagedFileMode {
					t.Fatalf("restored mode for %s = %04o, want %04o", path, info.Mode().Perm(), repoManagedFileMode)
				}
			}
		}
	})
}

func TestRepoMutationSharesPackageOperationLock(t *testing.T) {
	packageOperationMu.Lock()
	locked := true
	defer func() {
		if locked {
			packageOperationMu.Unlock()
		}
	}()

	started := make(chan struct{})
	entered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		_ = runRepoPackageMutation(func() error {
			close(entered)
			return nil
		})
		close(done)
	}()
	<-started

	select {
	case <-entered:
		packageOperationMu.Unlock()
		locked = false
		t.Fatal("repository mutation did not wait for package operation lock")
	case <-time.After(100 * time.Millisecond):
	}

	packageOperationMu.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("repository mutation did not continue after package lock release")
	}
}

func TestPrepareRepoRecipeValidationFailurePreservesPreviousFiles(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	mustWriteRepoTestFile(t, paths.StaleKeyrings[0], "old-stale-key")

	err := prepareAndPublishRepoRecipe(paths, []byte("new-key"), "deb https://example.invalid stable main", func(stagedSource string) ([]byte, error) {
		if stagedSource == paths.Source {
			t.Fatal("validation was run against the live source path")
		}
		return []byte("repository rejected"), errors.New("exit status 100")
	})
	if err == nil || !strings.Contains(err.Error(), "repository rejected") {
		t.Fatalf("error = %v, want staged apt failure", err)
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
		t.Fatalf("keyring changed after validation failure: %q", got)
	}
	if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
		t.Fatalf("source changed after validation failure: %q", got)
	}
	if got := mustReadRepoTestFile(t, paths.StaleKeyrings[0]); got != "old-stale-key" {
		t.Fatalf("stale key was removed before commit: %q", got)
	}
	assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
}

func TestPrepareRepoRecipePublishesSourceLastAndCleansStaleKey(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	mustWriteRepoTestFile(t, paths.StaleKeyrings[0], "old-stale-key")
	baseSource := "deb https://example.invalid stable main"

	err := prepareAndPublishRepoRecipe(paths, []byte("new-key"), baseSource, func(stagedSource string) ([]byte, error) {
		content := mustReadRepoTestFile(t, stagedSource)
		if !strings.Contains(content, "signed-by=") || strings.Contains(content, "signed-by="+paths.Keyring+"]") {
			t.Fatalf("validation source does not reference a staged keyring: %q", content)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "new-key" {
		t.Fatalf("published keyring = %q", got)
	}
	wantSource := signedRepoSource(baseSource, paths.Keyring) + "\n"
	if got := mustReadRepoTestFile(t, paths.Source); got != wantSource {
		t.Fatalf("published source = %q, want %q", got, wantSource)
	}
	if _, err := os.Stat(paths.StaleKeyrings[0]); !os.IsNotExist(err) {
		t.Fatalf("stale keyring still exists, stat error = %v", err)
	}
	for _, path := range []string{paths.Keyring, paths.Source} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o004 == 0 {
			t.Fatalf("%s is not world-readable: mode %o", path, info.Mode().Perm())
		}
	}
	assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
}

func TestPublishRepoRecipeRestoresOldKeyWhenSourceRenameFails(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	stagedKey, err := stageRepoFile(paths.Keyring, "publish-test", []byte("new-key"))
	if err != nil {
		t.Fatal(err)
	}
	stagedSource, err := stageRepoFile(paths.Source, "publish-test", []byte("new-source\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(stagedKey) }()
	defer func() { _ = os.Remove(stagedSource) }()

	originalRename := renameRepoFile
	renameRepoFile = func(oldPath, newPath string) error {
		if newPath == paths.Source && strings.Contains(filepath.Base(oldPath), "publish-test") {
			return errors.New("forced source rename failure")
		}
		return os.Rename(oldPath, newPath)
	}
	defer func() { renameRepoFile = originalRename }()

	err = publishRepoRecipe(paths, stagedKey, stagedSource)
	if err == nil || !strings.Contains(err.Error(), "forced source rename failure") {
		t.Fatalf("error = %v, want source rename failure", err)
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
		t.Fatalf("keyring was not restored: %q", got)
	}
	if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
		t.Fatalf("source changed despite failed commit: %q", got)
	}
	if repoMutationApplied(err) {
		t.Fatalf("successful rollback was marked as a partial mutation: %v", err)
	}
	assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
}

func TestValidateRepoUnixMetadata(t *testing.T) {
	tests := []struct {
		name     string
		mode     os.FileMode
		uid      uint32
		expected os.FileMode
		want     string
	}{
		{name: "managed file", mode: 0o644, expected: 0o644},
		{name: "private journal", mode: 0o600, expected: 0o600},
		{name: "non-root owner", mode: 0o644, uid: 1000, expected: 0o644, want: "want root"},
		{name: "group writable", mode: 0o664, expected: 0o644, want: "group/world-writable"},
		{name: "world writable", mode: 0o646, expected: 0o644, want: "group/world-writable"},
		{name: "unexpected restrictive mode", mode: 0o640, expected: 0o644, want: "want 0644"},
		{name: "journal uses managed mode", mode: 0o644, expected: 0o600, want: "want 0600"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRepoUnixMetadata(test.mode, test.uid, test.expected)
			if test.want == "" && err != nil {
				t.Fatalf("validateRepoUnixMetadata() error = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("validateRepoUnixMetadata() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestOpenRepoRegularFileRejectsSymlinksAndNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	t.Run("directory", func(t *testing.T) {
		file, _, err := openRepoRegularFile(dir)
		if file != nil {
			file.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("openRepoRegularFile() error = %v, want non-regular rejection", err)
		}
	})

	t.Run("symbolic link", func(t *testing.T) {
		target := filepath.Join(dir, "target.list")
		link := filepath.Join(dir, "linked.list")
		mustWriteRepoTestFile(t, target, "deb test")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symbolic links are unavailable on this host: %v", err)
		}
		file, _, err := openRepoRegularFile(link)
		if file != nil {
			file.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("openRepoRegularFile() error = %v, want symlink rejection", err)
		}
	})
}

func TestRecoverRepoTransactionRollsBackInterruptedPublish(t *testing.T) {
	for _, test := range []struct {
		name          string
		publishSource bool
		removeStale   bool
	}{
		{name: "key published only"},
		{name: "key and source published", publishSource: true, removeStale: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := testRepoRecipePaths(t)
			mustWriteRepoTestFile(t, paths.Keyring, "old-key")
			mustWriteRepoTestFile(t, paths.Source, "old-source\n")
			mustWriteRepoTestFile(t, paths.StaleKeyrings[0], "old-stale")
			managedPaths := repoRecipeManagedPaths(paths)
			entries, err := snapshotRepoEntries(managedPaths)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeRepoTransactionJournal(paths.Source, "publish", entries); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				info, statErr := os.Stat(repoJournalPath(paths.Source))
				if statErr != nil {
					t.Fatal(statErr)
				}
				if info.Mode().Perm() != repoJournalFileMode {
					t.Fatalf("journal mode = %04o, want %04o", info.Mode().Perm(), repoJournalFileMode)
				}
			}

			stagedKey, err := stageRepoFile(paths.Keyring, "crash", []byte("new-key"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(stagedKey, paths.Keyring); err != nil {
				t.Fatal(err)
			}
			if test.publishSource {
				stagedSource, stageErr := stageRepoFile(paths.Source, "crash", []byte("new-source\n"))
				if stageErr != nil {
					t.Fatal(stageErr)
				}
				if err := os.Rename(stagedSource, paths.Source); err != nil {
					t.Fatal(err)
				}
			}
			if test.removeStale {
				if err := os.Remove(paths.StaleKeyrings[0]); err != nil {
					t.Fatal(err)
				}
			}

			if err := recoverRepoTransaction(paths.Source, managedPaths); err != nil {
				t.Fatal(err)
			}
			if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
				t.Fatalf("recovered keyring = %q", got)
			}
			if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
				t.Fatalf("recovered source = %q", got)
			}
			if got := mustReadRepoTestFile(t, paths.StaleKeyrings[0]); got != "old-stale" {
				t.Fatalf("recovered stale keyring = %q", got)
			}
			assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
		})
	}
}

func TestRecoverRepoTransactionRejectsUnmanagedJournalPath(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	managedPaths := repoRecipeManagedPaths(paths)
	entries, err := snapshotRepoEntries(managedPaths)
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, repoJournalEntry{Path: filepath.Join(t.TempDir(), "outside.list")})
	if err := writeRepoTransactionJournal(paths.Source, "publish", entries); err != nil {
		t.Fatal(err)
	}
	if err := recoverRepoTransaction(paths.Source, managedPaths); err == nil || !strings.Contains(err.Error(), "unmanaged path") {
		t.Fatalf("recoverRepoTransaction() error = %v, want unmanaged path rejection", err)
	}
	if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
		t.Fatalf("source changed after tampered journal rejection: %q", got)
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
		t.Fatalf("keyring changed after tampered journal rejection: %q", got)
	}
}

func TestPublishRepoRecipeFsyncsEachRenamedParentDirectory(t *testing.T) {
	disableRepoOwnershipCheckForTest(t)
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	sourceDir := filepath.Join(root, "sources")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := repoRecipePaths{
		Keyring: filepath.Join(keyDir, "test.gpg"),
		Source:  filepath.Join(sourceDir, "test.list"),
	}
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	stagedKey, err := stageRepoFile(paths.Keyring, "publish-test", []byte("new-key"))
	if err != nil {
		t.Fatal(err)
	}
	stagedSource, err := stageRepoFile(paths.Source, "publish-test", []byte("new-source\n"))
	if err != nil {
		t.Fatal(err)
	}

	originalRename := renameRepoFile
	originalSync := syncRepoDirectory
	events := make([]string, 0, 8)
	renameRepoFile = func(oldPath, newPath string) error {
		events = append(events, "rename:"+newPath)
		return os.Rename(oldPath, newPath)
	}
	syncRepoDirectory = func(path string) error {
		events = append(events, "sync:"+path)
		return nil
	}
	t.Cleanup(func() {
		renameRepoFile = originalRename
		syncRepoDirectory = originalSync
	})
	if err := publishRepoRecipe(paths, stagedKey, stagedSource); err != nil {
		t.Fatal(err)
	}

	wantOrder := []string{
		"rename:" + repoJournalPath(paths.Source),
		"sync:" + sourceDir,
		"rename:" + paths.Keyring,
		"sync:" + keyDir,
		"rename:" + paths.Source,
		"sync:" + sourceDir,
	}
	next := 0
	for _, event := range events {
		if next < len(wantOrder) && event == wantOrder[next] {
			next++
		}
	}
	if next != len(wantOrder) {
		t.Fatalf("durability events = %#v, want ordered subsequence %#v", events, wantOrder)
	}
}

func TestDisableRepoRecipeMarksMutationWhenRollbackCannotFinish(t *testing.T) {
	paths := testRepoRecipePaths(t)
	mustWriteRepoTestFile(t, paths.Source, "old-source\n")
	mustWriteRepoTestFile(t, paths.Keyring, "old-key")

	originalRename := renameRepoFile
	renameRepoFile = func(oldPath, newPath string) error {
		if newPath == paths.Source && strings.Contains(filepath.Base(oldPath), "restore") {
			return errors.New("forced rollback failure")
		}
		return os.Rename(oldPath, newPath)
	}
	err := disableRepoRecipe(paths.Source, []string{paths.Keyring}, func() ([]byte, error) {
		return []byte("mirror unavailable"), errors.New("exit status 100")
	})
	renameRepoFile = originalRename
	t.Cleanup(func() { renameRepoFile = originalRename })
	if err == nil || !strings.Contains(err.Error(), "forced rollback failure") {
		t.Fatalf("disableRepoRecipe() error = %v, want rollback failure", err)
	}
	if !repoMutationApplied(err) {
		t.Fatalf("rollback failure was not marked as an applied mutation: %v", err)
	}
	if _, statErr := os.Stat(repoJournalPath(paths.Source)); statErr != nil {
		t.Fatalf("recovery journal missing after partial rollback: %v", statErr)
	}
	if recoverErr := recoverRepoTransaction(paths.Source, dedupeRepoPaths([]string{paths.Source, paths.Keyring})); recoverErr != nil {
		t.Fatalf("retry recovery failed: %v", recoverErr)
	}
	if got := mustReadRepoTestFile(t, paths.Source); got != "old-source\n" {
		t.Fatalf("source was not recovered on retry: %q", got)
	}
	if got := mustReadRepoTestFile(t, paths.Keyring); got != "old-key" {
		t.Fatalf("keyring was not recovered on retry: %q", got)
	}
	assertRepoTestPathAbsent(t, repoJournalPath(paths.Source))
}
