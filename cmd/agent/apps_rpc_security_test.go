//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type wordpressArchiveEntry struct {
	header  tar.Header
	content string
}

func writeWordPressArchive(t *testing.T, entries []wordpressArchiveEntry) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "wordpress.tar.gz")
	file, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := entry.header
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			if header.Size == 0 {
				header.Size = int64(len(entry.content))
			}
		}
		if err := tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if entry.content != "" {
			if _, err := tarWriter.Write([]byte(entry.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func validWordPressArchiveEntries() []wordpressArchiveEntry {
	return []wordpressArchiveEntry{
		{header: tar.Header{Name: "wordpress/", Typeflag: tar.TypeDir, Mode: 0o777}},
		{header: tar.Header{Name: "wordpress/wp-settings.php", Typeflag: tar.TypeReg, Mode: 0o777}, content: "<?php"},
		{header: tar.Header{Name: "wordpress/wp-admin/", Typeflag: tar.TypeDir, Mode: 0o777}},
		{header: tar.Header{Name: "wordpress/wp-includes/", Typeflag: tar.TypeDir, Mode: 0o777}},
	}
}

func TestExtractWordPressArchiveAcceptsOnlyBoundedRegularTree(t *testing.T) {
	destination := t.TempDir()
	archivePath := writeWordPressArchive(t, validWordPressArchiveEntries())
	if err := extractWordPressArchive(archivePath, destination); err != nil {
		t.Fatalf("extractWordPressArchive() error = %v", err)
	}
	info, err := os.Lstat(filepath.Join(destination, "wp-settings.php"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("wp-settings.php mode = %o, want 0640", info.Mode().Perm())
	}
}

func TestExtractWordPressArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry wordpressArchiveEntry
	}{
		{
			name: "traversal",
			entry: wordpressArchiveEntry{
				header:  tar.Header{Name: "wordpress/../../outside", Typeflag: tar.TypeReg},
				content: "owned",
			},
		},
		{
			name: "absolute path",
			entry: wordpressArchiveEntry{
				header:  tar.Header{Name: "/wordpress/outside", Typeflag: tar.TypeReg},
				content: "owned",
			},
		},
		{
			name: "backslash path",
			entry: wordpressArchiveEntry{
				header:  tar.Header{Name: "wordpress" + string(rune(92)) + "outside", Typeflag: tar.TypeReg},
				content: "owned",
			},
		},
		{
			name: "symbolic link",
			entry: wordpressArchiveEntry{
				header: tar.Header{Name: "wordpress/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"},
			},
		},
		{
			name: "device",
			entry: wordpressArchiveEntry{
				header: tar.Header{Name: "wordpress/device", Typeflag: tar.TypeChar},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			destination := t.TempDir()
			archivePath := writeWordPressArchive(t, []wordpressArchiveEntry{
				{header: tar.Header{Name: "wordpress/", Typeflag: tar.TypeDir}},
				test.entry,
			})
			if err := extractWordPressArchive(archivePath, destination); err == nil {
				t.Fatal("extractWordPressArchive() accepted an unsafe archive")
			}
		})
	}
}

func TestExtractWordPressArchiveRejectsDuplicateEntry(t *testing.T) {
	destination := t.TempDir()
	archivePath := writeWordPressArchive(t, []wordpressArchiveEntry{
		{header: tar.Header{Name: "wordpress/", Typeflag: tar.TypeDir}},
		{header: tar.Header{Name: "wordpress/wp-admin/", Typeflag: tar.TypeDir}},
		{header: tar.Header{Name: "wordpress/wp-admin/", Typeflag: tar.TypeDir}},
	})
	if err := extractWordPressArchive(archivePath, destination); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("extractWordPressArchive() error = %v, want duplicate rejection", err)
	}
}

func TestPublishWordPressStageRollsBackWhenFinalCleanupFails(t *testing.T) {
	const domain = "wordpress.example"
	sitesDir := t.TempDir()
	documentRoot := filepath.Join(sitesDir, "site", "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-settings.php"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectInstallableDocumentRoot(documentRoot, domain)
	if err != nil {
		t.Fatal(err)
	}

	originalRemove := wordpressRemove
	wordpressRemove = func(name string) error {
		return errors.New("injected backup cleanup failure")
	}
	t.Cleanup(func() { wordpressRemove = originalRemove })

	warning, preserve, safe, err := publishWordPressStage(
		stageDir, documentRoot, sitesDir, domain, snapshot, nil,
	)
	if err != nil || warning == "" || !preserve || safe {
		t.Fatalf("publish result warning=%q preserve=%v safe=%v err=%v", warning, preserve, safe, err)
	}
	content, err := os.ReadFile(filepath.Join(documentRoot, "wp-settings.php"))
	if err != nil || string(content) != "new" {
		t.Fatalf("published WordPress tree missing: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(stageDir); err != nil {
		t.Fatalf("old document root was not preserved for deferred cleanup: %v", err)
	}
}

func TestPublishWordPressStageRollsBackWhenOwnershipFinalizationFails(t *testing.T) {
	const domain = "wordpress.example"
	sitesDir := t.TempDir()
	documentRoot := filepath.Join(sitesDir, "site", "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-settings.php"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectInstallableDocumentRoot(documentRoot, domain)
	if err != nil {
		t.Fatal(err)
	}

	_, _, safe, err := publishWordPressStage(
		stageDir,
		documentRoot,
		sitesDir,
		domain,
		snapshot,
		func(wordpressPathExchange) error { return errors.New("injected ownership failure") },
	)
	if err == nil || !safe {
		t.Fatalf("publishWordPressStage() error=%v safe=%v, want safely restored failure", err, safe)
	}
	entries, readErr := os.ReadDir(documentRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("document root contains %d entries after ownership rollback", len(entries))
	}
	if _, statErr := os.Stat(filepath.Join(stageDir, "wp-settings.php")); statErr != nil {
		t.Fatalf("staged tree was not preserved after ownership rollback: %v", statErr)
	}
}

func TestPublishWordPressStagePreservesCanonicalPlaceholderRecoveryTree(t *testing.T) {
	const domain = "wordpress.example"
	sitesDir := t.TempDir()
	documentRoot := filepath.Join(sitesDir, "site", "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	placeholderName, placeholderContent := celikPanelSitePlaceholder(domain, "php")
	if err := os.WriteFile(
		filepath.Join(documentRoot, placeholderName), placeholderContent, 0o640,
	); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-settings.php"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectInstallableDocumentRoot(documentRoot, domain)
	if err != nil {
		t.Fatal(err)
	}

	warning, preserve, safe, err := publishWordPressStage(
		stageDir, documentRoot, sitesDir, domain, snapshot, nil,
	)
	if err != nil || warning == "" || !preserve || safe {
		t.Fatalf("publish result warning=%q preserve=%v safe=%v err=%v", warning, preserve, safe, err)
	}
	content, readErr := os.ReadFile(filepath.Join(stageDir, placeholderName))
	if readErr != nil || string(content) != string(placeholderContent) {
		t.Fatalf("placeholder recovery copy changed: content=%q err=%v", content, readErr)
	}
	info, statErr := os.Stat(stageDir)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("recovery root mode=%v, want 0700", info.Mode())
	}
}

func TestLinuxWordPressPathExchangeLocksAndRestoresSiteHome(t *testing.T) {
	sitesDir := t.TempDir()
	siteDir := filepath.Join(sitesDir, "site")
	documentRoot := filepath.Join(siteDir, "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(siteDir, 0o750); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	exchange, err := prepareWordPressPathExchange(stageDir, documentRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer exchange.Close()
	if err := exchange.LockPaths(); err != nil {
		t.Fatal(err)
	}
	lockedInfo, err := os.Stat(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	if lockedInfo.Mode().Perm()&0o222 != 0 {
		t.Fatalf("site-home mode remained writable while locked: %v", lockedInfo.Mode())
	}
	if err := exchange.UnlockPaths(); err != nil {
		t.Fatal(err)
	}
	restoredInfo, err := os.Stat(siteDir)
	if err != nil {
		t.Fatal(err)
	}
	if restoredInfo.Mode().Perm() != 0o750 {
		t.Fatalf("site-home mode=%v after unlock, want 0750", restoredInfo.Mode())
	}
}

func TestRequireWordPressStagingParentRejectsWritableParent(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := requireWordPressStagingParent(directory); err == nil {
		t.Fatal("group/world-writable staging parent was accepted")
	}
}

func TestInspectInstallableDocumentRootAcceptsOnlyExactCelikPanelPlaceholder(t *testing.T) {
	const domain = "wordpress.example"
	documentRoot := t.TempDir()
	name, content := celikPanelSitePlaceholder(domain, "php")
	if err := os.WriteFile(filepath.Join(documentRoot, name), content, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectInstallableDocumentRoot(documentRoot, domain); err != nil {
		t.Fatalf("exact CelikPanel placeholder was rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(documentRoot, name), append(content, 'x'), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectInstallableDocumentRoot(documentRoot, domain); err == nil {
		t.Fatal("modified placeholder was accepted")
	}
}

func TestInspectInstallableDocumentRootRefusesCustomerContent(t *testing.T) {
	documentRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(documentRoot, "index.html"), []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectInstallableDocumentRoot(documentRoot, "wordpress.example"); err == nil {
		t.Fatal("customer content was accepted")
	}
}

func TestWordPressInstalledModesProtectConfigAndPreserveGroupInheritance(t *testing.T) {
	if mode := wordpressInstalledMode("wp-content", true); mode != 0o750|os.ModeSetgid {
		t.Fatalf("directory mode=%v, want 2750", mode)
	}
	if mode := wordpressInstalledMode("wp-config.php", false); mode != 0o600 {
		t.Fatalf("wp-config mode=%v, want 0600", mode)
	}
	if mode := wordpressInstalledMode("index.php", false); mode != 0o640 {
		t.Fatalf("regular file mode=%v, want 0640", mode)
	}
}

func TestPublishWordPressStageUsesHeldDirectoryDescriptorsAcrossPathRebind(t *testing.T) {
	const domain = "wordpress.example"
	sitesDir := t.TempDir()
	siteDir := filepath.Join(sitesDir, "site")
	documentRoot := filepath.Join(siteDir, "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-settings.php"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectInstallableDocumentRoot(documentRoot, domain)
	if err != nil {
		t.Fatal(err)
	}

	originalPrepare := wordpressPrepareExchange
	movedSite := siteDir + ".moved"
	wordpressPrepareExchange = func(stage, root string) (wordpressPathExchange, error) {
		exchanger, prepareErr := prepareWordPressPathExchange(stage, root)
		if prepareErr != nil {
			return nil, prepareErr
		}
		if renameErr := os.Rename(siteDir, movedSite); renameErr != nil {
			_ = exchanger.Close()
			return nil, renameErr
		}
		if mkdirErr := os.MkdirAll(documentRoot, 0o750); mkdirErr != nil {
			_ = exchanger.Close()
			return nil, mkdirErr
		}
		if writeErr := os.WriteFile(filepath.Join(documentRoot, "tenant.txt"), []byte("keep"), 0o640); writeErr != nil {
			_ = exchanger.Close()
			return nil, writeErr
		}
		return exchanger, nil
	}
	t.Cleanup(func() { wordpressPrepareExchange = originalPrepare })

	_, _, safe, err := publishWordPressStage(
		stageDir, documentRoot, sitesDir, domain, snapshot, nil,
	)
	if err == nil || !safe {
		t.Fatalf("path rebind result error=%v safe=%v, want safely restored failure", err, safe)
	}
	content, readErr := os.ReadFile(filepath.Join(documentRoot, "tenant.txt"))
	if readErr != nil || string(content) != "keep" {
		t.Fatalf("rebound customer path was modified: content=%q err=%v", content, readErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(movedSite, "public_html"))
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("original document root was not restored: entries=%v err=%v", entries, readErr)
	}
}

func TestPublishWordPressStageRestoresWhenPublishedTreeFsyncFails(t *testing.T) {
	const domain = "wordpress.example"
	sitesDir := t.TempDir()
	documentRoot := filepath.Join(sitesDir, "site", "public_html")
	if err := os.MkdirAll(documentRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	stageDir, err := os.MkdirTemp(sitesDir, ".wordpress-stage-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "wp-settings.php"), []byte("new"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectInstallableDocumentRoot(documentRoot, domain)
	if err != nil {
		t.Fatal(err)
	}

	originalSync := wordpressSyncPublished
	wordpressSyncPublished = func(wordpressPathExchange) error {
		return errors.New("injected fsync failure")
	}
	t.Cleanup(func() { wordpressSyncPublished = originalSync })

	_, _, safe, err := publishWordPressStage(
		stageDir, documentRoot, sitesDir, domain, snapshot, nil,
	)
	if err == nil || !safe {
		t.Fatalf("fsync failure result error=%v safe=%v, want safely restored failure", err, safe)
	}
	entries, readErr := os.ReadDir(documentRoot)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("document root was not restored after fsync failure: entries=%v err=%v", entries, readErr)
	}
}
