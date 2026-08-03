//go:build linux

package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func installBackupTestPaths(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	docroot := filepath.Join(root, "site", "public_html")
	if err := os.MkdirAll(docroot, 0o755); err != nil {
		t.Fatal(err)
	}
	oldBase := backupBaseDir
	oldDocumentRoot := backupDocumentRoot
	backupBaseDir = backupRoot
	backupDocumentRoot = func(subscriptionID, domainID int) (string, error) {
		if subscriptionID != 7 || domainID != 9 {
			return "", errors.New("unexpected test scope")
		}
		return docroot, nil
	}
	t.Cleanup(func() {
		backupBaseDir = oldBase
		backupDocumentRoot = oldDocumentRoot
	})
	return backupRoot, docroot
}

func testScope() backupScope {
	return backupScope{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
	}
}

func testCreateRequest(backupType string) *backupspec.CreateRequest {
	scope := testScope()
	return &backupspec.CreateRequest{
		ProtocolVersion: scope.ProtocolVersion,
		SubscriptionID:  scope.SubscriptionID,
		DomainID:        scope.DomainID,
		DomainName:      scope.DomainName,
		Type:            backupType,
		Origin:          backupspec.OriginManual,
	}
}

func writeGzipText(filePath, value string) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(file)
	if _, err := io.WriteString(gz, value); err != nil {
		_ = gz.Close()
		_ = file.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readGzipText(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	return string(data), err
}

func installDatabaseState(t *testing.T, state map[int]string, fail func(backupspec.DatabaseIdentity, string) error, calls *[]string) {
	t.Helper()
	oldDump := dumpDatabaseToFile
	oldRestore := restoreDatabaseFromFile
	dumpDatabaseToFile = func(database backupspec.DatabaseIdentity, destination string) error {
		return writeGzipText(destination, state[database.ID])
	}
	restoreDatabaseFromFile = func(database backupspec.DatabaseIdentity, source string) error {
		value, err := readGzipText(source)
		if err != nil {
			return err
		}
		state[database.ID] = value
		if calls != nil {
			*calls = append(*calls, fmt.Sprintf("%d=%s", database.ID, value))
		}
		if fail != nil {
			return fail(database, value)
		}
		return nil
	}
	t.Cleanup(func() {
		dumpDatabaseToFile = oldDump
		restoreDatabaseFromFile = oldRestore
	})
}

func TestBackupDatabaseIdentityAndArgv(t *testing.T) {
	mysql := backupspec.DatabaseIdentity{ID: 12, Name: "tenant_health_data", Type: "mysql"}
	normalized, err := validateDatabaseIdentity(mysql)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != mysql.Name {
		t.Fatalf("underscore identity changed: %#v", normalized)
	}
	dump, err := databaseDumpCommand(mysql)
	if err != nil {
		t.Fatal(err)
	}
	wantDump := backupCommand{
		Name: "mysqldump",
		Args: []string{"--protocol=socket", "--user=root", "--single-transaction", "--routines", "--triggers", "--skip-lock-tables", "tenant_health_data"},
	}
	if !reflect.DeepEqual(dump, wantDump) {
		t.Fatalf("dump argv = %#v, want %#v", dump, wantDump)
	}
	postgres := backupspec.DatabaseIdentity{ID: 13, Name: "tenant_pg_data", Type: "postgresql"}
	restore, err := databaseRestoreCommand(postgres)
	if err != nil {
		t.Fatal(err)
	}
	wantRestore := backupCommand{
		Name: "sudo",
		Args: []string{"-u", "postgres", "psql", "--set", "ON_ERROR_STOP=on", "--dbname", "tenant_pg_data"},
	}
	if !reflect.DeepEqual(restore, wantRestore) {
		t.Fatalf("restore argv = %#v, want %#v", restore, wantRestore)
	}
	if _, err := databaseDumpCommand(backupspec.DatabaseIdentity{ID: 1, Name: "db;touch_pwned", Type: "mysql"}); err == nil {
		t.Fatal("shell metacharacters were accepted")
	}
}

func TestBackupNameValidationRejectsTraversal(t *testing.T) {
	valid := []string{
		"files-20260728T120000.000000000Z-0123456789abcdef.cpbak",
		"database-42-20260728T120000.000000000Z-0123456789abcdef.cpbak",
		"full_20260728_120000.tar.gz",
		"db_name_with_underscores_20260728_120000.sql.gz",
	}
	for _, name := range valid {
		if !validBackupName(name) {
			t.Errorf("valid name rejected: %s", name)
		}
	}
	invalid := []string{"../backup.cpbak", "..\\backup.cpbak", "/tmp/x", ".hidden", "files-x.cpbak", "a/b"}
	for _, name := range invalid {
		if validBackupName(name) {
			t.Errorf("unsafe name accepted: %s", name)
		}
	}
}

func TestBackupTenantScopeIsolation(t *testing.T) {
	backupRoot, docroot := installBackupTestPaths(t)
	if err := os.WriteFile(filepath.Join(docroot, "index.html"), []byte("tenant-seven"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	info, err := agent.createBackup(testCreateRequest(backupspec.TypeFiles))
	if err != nil {
		t.Fatal(err)
	}
	other := backupScope{ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: 8, DomainID: 10, DomainName: "other.test"}
	otherDir, err := ensureBackupDir(other)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(otherDir, info.Name)
	if err := os.WriteFile(otherPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspectV2Backup(other, otherPath, info.Name); err == nil || !strings.Contains(err.Error(), "scope mismatch") {
		t.Fatalf("cross-tenant package was accepted: %v", err)
	}
	if !strings.HasPrefix(info.Path, filepath.Join(backupRoot, "subscriptions", "7", "domains", "9")) {
		t.Fatalf("backup path is not ID scoped: %s", info.Path)
	}
}

func TestBackupPartialWorkIsRemovedOnFailure(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		if err := os.WriteFile(destination, []byte("partial"), 0o600); err != nil {
			return err
		}
		return errors.New("injected dump failure")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })
	req := testCreateRequest(backupspec.TypeDatabase)
	req.Database = backupspec.DatabaseIdentity{ID: 1, Name: "db_one", Type: "mysql"}
	if _, err := (&Agent{}).createBackup(req); err == nil {
		t.Fatal("backup unexpectedly succeeded")
	}
	dir, err := scopeBackupDir(testScope())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial artifacts remain: %#v", entries)
	}
}

func TestBackupFullZeroDatabasesRestoresFilesAndRemovesStale(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	info, err := agent.createBackup(testCreateRequest(backupspec.TypeFull))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readBackupManifest(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Type != backupspec.TypeFull || len(manifest.Databases) != 0 || manifest.Files.Name == "" {
		t.Fatalf("invalid zero-database full manifest: %#v", manifest)
	}
	if err := os.Remove(filepath.Join(docroot, "target.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "stale.html"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := &backupspec.RestoreResponse{}
	req := &backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      info.Name,
		Databases:       nil,
	}
	if err := agent.restoreBackup(req, resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.SafetyBackup == nil || resp.SafetyBackup.Origin != backupspec.OriginPreRestore {
		t.Fatalf("restore response missing visible safety backup: %#v", resp)
	}
	if _, err := os.Stat(resp.SafetyBackup.Path); err != nil {
		t.Fatalf("safety backup is not durable: %v", err)
	}
	list := &backupspec.ListResponse{}
	if err := agent.ListBackups(&backupspec.ListRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
	}, list); err != nil {
		t.Fatal(err)
	}
	foundSafety := false
	for _, listed := range list.Backups {
		if listed.Name == resp.SafetyBackup.Name && listed.Origin == backupspec.OriginPreRestore {
			foundSafety = true
			break
		}
	}
	if !foundSafety {
		t.Fatalf("safety backup is not visible in ListBackups: %#v", list.Backups)
	}
	if data, err := os.ReadFile(filepath.Join(docroot, "target.html")); err != nil || string(data) != "target" {
		t.Fatalf("target file not restored: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(docroot, "stale.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file survived atomic replacement: %v", err)
	}
}

func TestBackupFullMultipleDatabasesRestoresFilesAndDatabasesSuccessfully(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	databases := []backupspec.DatabaseIdentity{
		{ID: 1, Name: "tenant_one", Type: "mysql"},
		{ID: 2, Name: "tenant_two", Type: "postgresql"},
	}
	state := map[int]string{1: "target-one", 2: "target-two"}
	var calls []string
	installDatabaseState(t, state, nil, &calls)
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("target-files"), 0o644); err != nil {
		t.Fatal(err)
	}

	agent := &Agent{}
	create := testCreateRequest(backupspec.TypeFull)
	create.Databases = databases
	target, err := agent.createBackup(create)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readBackupManifest(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Type != backupspec.TypeFull ||
		manifest.Files.Name != filesPayloadName ||
		len(manifest.Databases) != len(databases) {
		t.Fatalf("invalid full manifest: %#v", manifest)
	}
	if err := validateManifest(manifest, testScope()); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	for _, payload := range sortedPayloads(manifest) {
		if payload.Size < 1 || len(payload.SHA256) != 64 {
			t.Fatalf("payload digest metadata is incomplete: %#v", payload)
		}
	}

	state[1], state[2] = "live-one", "live-two"
	if err := os.Remove(filepath.Join(docroot, "target.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "stale.html"), []byte("stale-files"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := &backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      target.Name,
		Databases:       databases,
	}

	first := &backupspec.RestoreResponse{}
	if err := agent.restoreBackup(restore, first); err != nil {
		t.Fatal(err)
	}
	if !first.Success || first.SafetyBackup == nil ||
		first.SafetyBackup.Origin != backupspec.OriginPreRestore {
		t.Fatalf("successful restore did not expose its safety backup: %#v", first)
	}
	if _, err := os.Stat(first.SafetyBackup.Path); err != nil {
		t.Fatalf("safety backup is unavailable: %v", err)
	}
	if state[1] != "target-one" || state[2] != "target-two" {
		t.Fatalf("database state = %#v", state)
	}
	if data, err := os.ReadFile(filepath.Join(docroot, "target.html")); err != nil ||
		string(data) != "target-files" {
		t.Fatalf("target files were not restored: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(docroot, "stale.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file survived successful full restore: %v", err)
	}
	wantFirstCalls := []string{"1=target-one", "2=target-two"}
	if !reflect.DeepEqual(calls, wantFirstCalls) {
		t.Fatalf("first restore calls = %#v, want %#v", calls, wantFirstCalls)
	}

	// A retry of the same verified package is safe and converges to the same
	// database and document-root state.
	second := &backupspec.RestoreResponse{}
	if err := agent.restoreBackup(restore, second); err != nil {
		t.Fatal(err)
	}
	if !second.Success || second.SafetyBackup == nil ||
		second.SafetyBackup.Name == first.SafetyBackup.Name {
		t.Fatalf("idempotent retry did not create a distinct durable safety point: %#v", second)
	}
	wantAllCalls := []string{
		"1=target-one", "2=target-two",
		"1=target-one", "2=target-two",
	}
	if !reflect.DeepEqual(calls, wantAllCalls) {
		t.Fatalf("retry calls = %#v, want %#v", calls, wantAllCalls)
	}
	if state[1] != "target-one" || state[2] != "target-two" {
		t.Fatalf("retry changed database state: %#v", state)
	}
}

func TestBackupRestoreCommitFailureIsReportedAndSafetyBackupRemains(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	target, err := agent.createBackup(testCreateRequest(backupspec.TypeFiles))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldRemove := backupRemovePublishedOld
	backupRemovePublishedOld = func(string) error {
		return errors.New("injected old-tree cleanup failure")
	}
	t.Cleanup(func() { backupRemovePublishedOld = oldRemove })

	resp := &backupspec.RestoreResponse{}
	err = agent.restoreBackup(&backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      target.Name,
	}, resp)
	if err == nil || !strings.Contains(err.Error(), "durability proof failed") {
		t.Fatalf("commit failure was not reported: %v", err)
	}
	if resp.Success {
		t.Fatal("restore reported success after commit failure")
	}
	if resp.SafetyBackup == nil || resp.SafetyBackup.Origin != backupspec.OriginPreRestore {
		t.Fatalf("safety backup was not retained: %#v", resp)
	}
	if _, statErr := os.Stat(resp.SafetyBackup.Path); statErr != nil {
		t.Fatalf("safety backup is not accessible: %v", statErr)
	}
	if data, readErr := os.ReadFile(filepath.Join(docroot, "target.html")); readErr != nil ||
		string(data) != "target" {
		t.Fatalf("published target is not internally consistent: %q, %v", data, readErr)
	}
}

func TestBackupFullRestoreSetMismatchDoesNotCreateSafetyBackup(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	target, err := agent.createBackup(testCreateRequest(backupspec.TypeFull))
	if err != nil {
		t.Fatal(err)
	}
	req := &backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      target.Name,
		Databases: []backupspec.DatabaseIdentity{
			{ID: 1, Name: "unexpected_db", Type: "mysql"},
		},
	}
	resp := &backupspec.RestoreResponse{}
	if err := agent.restoreBackup(req, resp); err == nil {
		t.Fatal("restore with a mismatched database set unexpectedly succeeded")
	}
	if resp.SafetyBackup != nil {
		t.Fatalf("invalid request created a safety backup: %#v", resp.SafetyBackup)
	}
	list := &backupspec.ListResponse{}
	if err := agent.ListBackups(&backupspec.ListRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
	}, list); err != nil {
		t.Fatal(err)
	}
	if len(list.Backups) != 1 || list.Backups[0].Name != target.Name {
		t.Fatalf("invalid restore changed backup inventory: %#v", list.Backups)
	}
}

func TestBackupFullDatabaseFailureRollsBackAndLeavesFilesUntouched(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	databases := []backupspec.DatabaseIdentity{
		{ID: 1, Name: "tenant_one", Type: "mysql"},
		{ID: 2, Name: "tenant_two", Type: "postgresql"},
	}
	state := map[int]string{1: "target-one", 2: "target-two"}
	var calls []string
	failTargetTwo := true
	installDatabaseState(t, state, func(database backupspec.DatabaseIdentity, value string) error {
		if failTargetTwo && database.ID == 2 && value == "target-two" {
			failTargetTwo = false
			return errors.New("injected target restore failure")
		}
		return nil
	}, &calls)
	if err := os.WriteFile(filepath.Join(docroot, "target.html"), []byte("target-files"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	req := testCreateRequest(backupspec.TypeFull)
	req.Databases = databases
	target, err := agent.createBackup(req)
	if err != nil {
		t.Fatal(err)
	}
	state[1], state[2] = "live-one", "live-two"
	if err := os.Remove(filepath.Join(docroot, "target.html")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "live.html"), []byte("live-files"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreReq := &backupspec.RestoreRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      target.Name,
		Databases:       databases,
	}
	resp := &backupspec.RestoreResponse{}
	err = agent.restoreBackup(restoreReq, resp)
	if err == nil {
		t.Fatal("full restore unexpectedly succeeded")
	}
	if resp.SafetyBackup == nil || resp.SafetyBackup.Origin != backupspec.OriginPreRestore {
		t.Fatalf("safety backup was not exposed: %#v", resp)
	}
	if state[1] != "live-one" || state[2] != "live-two" {
		t.Fatalf("database rollback failed: %#v", state)
	}
	wantCalls := []string{"1=target-one", "2=target-two", "2=live-two", "1=live-one"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("restore/rollback calls = %#v, want %#v", calls, wantCalls)
	}
	if data, readErr := os.ReadFile(filepath.Join(docroot, "live.html")); readErr != nil || string(data) != "live-files" {
		t.Fatalf("live files changed after DB failure: %q, %v", data, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(docroot, "target.html")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target files were published before DB success: %v", statErr)
	}
}

func TestBackupAtomicPublishFailureLeavesDocumentRootUnchanged(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "public_html")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new.html"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.html"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "files.tar.gz")
	if err := safeCreateFilesArchive(source, archive); err != nil {
		t.Fatal(err)
	}
	staged, err := prepareFilesRestore(archive, target)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	oldRename := backupRename
	calls := 0
	backupRename = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected publish failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { backupRename = oldRename })
	if err := staged.Publish(); err == nil {
		t.Fatal("publish unexpectedly succeeded")
	}
	if data, err := os.ReadFile(filepath.Join(target, "old.html")); err != nil || string(data) != "old" {
		t.Fatalf("old document root was not preserved: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target, "new.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file leaked into live document root: %v", err)
	}
}

func TestBackupArchiveRejectsLinksAndTraversal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(source, "regular.txt")
	if err := os.WriteFile(regular, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(regular, filepath.Join(source, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := safeCreateFilesArchive(source, filepath.Join(root, "symlink.tar.gz")); err == nil {
		t.Fatal("symlink was archived")
	}
	if err := os.Remove(filepath.Join(source, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(regular, filepath.Join(source, "hard.txt")); err != nil {
		t.Fatal(err)
	}
	if err := safeCreateFilesArchive(source, filepath.Join(root, "hardlink.tar.gz")); err == nil {
		t.Fatal("hard link was archived")
	}
	malicious := filepath.Join(root, "malicious.tar.gz")
	writeTestTarGz(t, malicious, &tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Mode: 0o600, Size: 1}, []byte("x"))
	destination := filepath.Join(root, "extract")
	if err := safeExtractFilesArchive(malicious, destination); err == nil {
		t.Fatal("traversal archive was extracted")
	}
	linkArchive := filepath.Join(root, "link-entry.tar.gz")
	writeTestTarGz(t, linkArchive, &tar.Header{Name: "link", Typeflag: tar.TypeLink, Linkname: "target"}, nil)
	if err := safeExtractFilesArchive(linkArchive, filepath.Join(root, "extract-link")); err == nil {
		t.Fatal("hard-link tar entry was extracted")
	}
}

func writeTestTarGz(t *testing.T, filePath string, header *tar.Header, data []byte) {
	t.Helper()
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if len(data) > 0 {
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupReadChunkIsBoundedAndUsesNextOffset(t *testing.T) {
	installBackupTestPaths(t)
	dir, err := ensureBackupDir(testScope())
	if err != nil {
		t.Fatal(err)
	}
	name := "files-20260728T120000.000000000Z-0123456789abcdef.cpbak"
	content := []byte(strings.Repeat("x", backupspec.MaxChunkBytes+37))
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
		t.Fatal(err)
	}
	req := &backupspec.ReadChunkRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
		BackupName:      name,
		MaxBytes:        backupspec.MaxChunkBytes * 4,
	}
	var first backupspec.ReadChunkResponse
	if err := (&Agent{}).ReadBackupChunk(req, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != backupspec.MaxChunkBytes || first.Offset != backupspec.MaxChunkBytes || first.EOF {
		t.Fatalf("unexpected first chunk: len=%d offset=%d eof=%v", len(first.Data), first.Offset, first.EOF)
	}
	req.Offset = first.Offset
	var second backupspec.ReadChunkResponse
	if err := (&Agent{}).ReadBackupChunk(req, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Data) != 37 || second.Offset != int64(len(content)) || !second.EOF {
		t.Fatalf("unexpected second chunk: len=%d offset=%d eof=%v", len(second.Data), second.Offset, second.EOF)
	}
}

func TestBackupLegacyFallbackAndFileOnlyFull(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	legacyDir, err := legacyBackupDir(testScope())
	if err != nil {
		t.Fatal(err)
	}
	if err := secureMkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "legacy.html"), []byte("legacy"), 0o644); err != nil {
		t.Fatal(err)
	}
	fullName := "full_20260728_120000.tar.gz"
	if err := safeCreateFilesArchive(docroot, filepath.Join(legacyDir, fullName)); err != nil {
		t.Fatal(err)
	}
	var response backupspec.ListResponse
	req := &backupspec.ListRequest{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  7,
		DomainID:        9,
		DomainName:      "example.test",
	}
	if err := (&Agent{}).ListBackups(req, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Backups) != 1 {
		t.Fatalf("legacy backup not listed: %#v", response.Backups)
	}
	info := response.Backups[0]
	if !info.Legacy || !info.Restorable || info.Type != backupspec.TypeFull || info.DatabaseID != 0 {
		t.Fatalf("legacy full is not explicit file-only compatibility: %#v", info)
	}
}
