//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

func scheduledDatabaseBackupRequest(jobKey, databaseName string) *backupspec.CreateRequest {
	req := testCreateRequest(backupspec.TypeDatabase)
	req.Origin = backupspec.OriginScheduled
	req.JobKey = jobKey
	req.Database = backupspec.DatabaseIdentity{
		ID: 1, Name: databaseName, Type: "mysql",
	}
	return req
}

func publishedBackupCount(t *testing.T) int {
	t.Helper()
	dir, err := scopeBackupDir(testScope())
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && generatedBackupName.MatchString(entry.Name()) {
			count++
		}
	}
	return count
}

func TestBackupJobKeyConcurrentCallsPublishOnce(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		if physicalCalls.Add(1) == 1 {
			close(started)
			<-release
		}
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	agent := &Agent{}
	req := scheduledDatabaseBackupRequest("schedule:concurrent", "tenant_one")
	type result struct {
		response backupspec.CreateResponse
		err      error
	}
	call := func(done chan<- result) {
		var response backupspec.CreateResponse
		err := agent.CreateBackup(req, &response)
		done <- result{response: response, err: err}
	}
	firstDone := make(chan result, 1)
	secondDone := make(chan result, 1)
	go call(firstDone)
	<-started
	go call(secondDone)
	select {
	case result := <-secondDone:
		t.Fatalf("duplicate call returned before the publisher completed: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	first := <-firstDone
	second := <-secondDone
	if first.err != nil || second.err != nil ||
		!first.response.Success || !second.response.Success {
		t.Fatalf("create results first=%+v second=%+v", first, second)
	}
	if first.response.Backup.Name != second.response.Backup.Name {
		t.Fatalf("duplicate calls returned different backups: %q != %q",
			first.response.Backup.Name, second.response.Backup.Name)
	}
	if physicalCalls.Load() != 1 {
		t.Fatalf("physical backup calls=%d, want 1", physicalCalls.Load())
	}
	if count := publishedBackupCount(t); count != 1 {
		t.Fatalf("published backups=%d, want 1", count)
	}
}

func TestBackupJobKeySurvivesAgentRestart(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		physicalCalls.Add(1)
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	req := scheduledDatabaseBackupRequest("schedule:restart", "tenant_one")
	var first backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(req, &first); err != nil || !first.Success {
		t.Fatalf("first create response=%+v err=%v", first, err)
	}
	var afterRestart backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(req, &afterRestart); err != nil || !afterRestart.Success {
		t.Fatalf("restart retry response=%+v err=%v", afterRestart, err)
	}
	if first.Backup.Name != afterRestart.Backup.Name {
		t.Fatalf("restart retry returned %q, want %q", afterRestart.Backup.Name, first.Backup.Name)
	}
	if physicalCalls.Load() != 1 {
		t.Fatalf("physical backup calls=%d, want 1", physicalCalls.Load())
	}
	manifest, err := readBackupManifest(filepath.Join(
		backupBaseDir, "subscriptions", "7", "domains", "9", first.Backup.Name,
	))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.JobKey != req.JobKey {
		t.Fatalf("manifest job key=%q, want %q", manifest.JobKey, req.JobKey)
	}
}

func TestBackupJobKeyTruncatedPublishedManifestFailsClosed(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		physicalCalls.Add(1)
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	agent := &Agent{}
	req := scheduledDatabaseBackupRequest("schedule:truncated", "tenant_one")
	var first backupspec.CreateResponse
	if err := agent.CreateBackup(req, &first); err != nil || !first.Success {
		t.Fatalf("first create response=%+v err=%v", first, err)
	}
	packagePath := filepath.Join(
		backupBaseDir, "subscriptions", "7", "domains", "9", first.Backup.Name,
	)
	if err := os.WriteFile(packagePath, []byte("truncated package"), 0o600); err != nil {
		t.Fatal(err)
	}

	var retry backupspec.CreateResponse
	if err := agent.CreateBackup(req, &retry); err != nil {
		t.Fatal(err)
	}
	if retry.Success || !strings.Contains(retry.Error, "inspect published backup candidate") {
		t.Fatalf("truncated publication was not rejected: %+v", retry)
	}
	if physicalCalls.Load() != 1 {
		t.Fatalf("truncated publication triggered a second physical backup: calls=%d", physicalCalls.Load())
	}
	if count := publishedBackupCount(t); count != 1 {
		t.Fatalf("published backups=%d, want the single corrupt package retained", count)
	}
}

func TestBackupJobKeyRetriesPhysicalFailure(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		if physicalCalls.Add(1) == 1 {
			return errors.New("injected physical failure")
		}
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	req := scheduledDatabaseBackupRequest("schedule:retry", "tenant_one")
	var failed backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(req, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Success || failed.Error == "" || publishedBackupCount(t) != 0 {
		t.Fatalf("failed create left a publication: %+v", failed)
	}
	var retried backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(req, &retried); err != nil || !retried.Success {
		t.Fatalf("retry response=%+v err=%v", retried, err)
	}
	if physicalCalls.Load() != 2 || publishedBackupCount(t) != 1 {
		t.Fatalf("physical calls=%d published=%d, want 2/1",
			physicalCalls.Load(), publishedBackupCount(t))
	}
}

func TestBackupJobKeyConflictFailsClosed(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		physicalCalls.Add(1)
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	agent := &Agent{}
	var first backupspec.CreateResponse
	if err := agent.CreateBackup(
		scheduledDatabaseBackupRequest("schedule:conflict", "tenant_one"), &first,
	); err != nil || !first.Success {
		t.Fatalf("first response=%+v err=%v", first, err)
	}
	var conflict backupspec.CreateResponse
	if err := agent.CreateBackup(
		scheduledDatabaseBackupRequest("schedule:conflict", "tenant_two"), &conflict,
	); err != nil {
		t.Fatal(err)
	}
	if conflict.Success || !strings.Contains(conflict.Error, "conflicts") {
		t.Fatalf("conflicting request was not rejected: %+v", conflict)
	}
	if physicalCalls.Load() != 1 || publishedBackupCount(t) != 1 {
		t.Fatalf("conflict changed inventory: calls=%d backups=%d",
			physicalCalls.Load(), publishedBackupCount(t))
	}
}

func TestBackupJobKeyTamperedPublishedManifestFailsClosed(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		physicalCalls.Add(1)
		return writeGzipText(destination, "unexpected")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	scope := testScope()
	backupDir, err := ensureBackupDir(scope)
	if err != nil {
		t.Fatal(err)
	}
	workDir, err := os.MkdirTemp(filepath.Dir(backupDir), ".tampered-package-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })
	payloadName := databasePayloadName(1)
	payloadPath := filepath.Join(workDir, payloadName)
	if err := os.MkdirAll(filepath.Dir(payloadPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeGzipText(payloadPath, "tampered"); err != nil {
		t.Fatal(err)
	}
	payload, err := describePayload(payloadPath, payloadName)
	if err != nil {
		t.Fatal(err)
	}
	manifest := backupManifest{
		Version:        backupspec.ProtocolVersion,
		Type:           backupspec.TypeDatabase,
		Origin:         backupspec.OriginScheduled,
		JobKey:         "schedule:tampered",
		SubscriptionID: scope.SubscriptionID,
		DomainID:       scope.DomainID + 1,
		CreatedAt:      time.Now().UTC(),
		Databases: []manifestDatabase{{
			Identity: backupspec.DatabaseIdentity{ID: 1, Name: "tenant_one", Type: "mysql"},
			Payload:  payload,
		}},
	}
	name, err := newBackupName(backupspec.TypeDatabase, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishBackupPackage(filepath.Join(backupDir, name), workDir, manifest); err != nil {
		t.Fatal(err)
	}

	var response backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(
		scheduledDatabaseBackupRequest("schedule:tampered", "tenant_one"), &response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == "" {
		t.Fatalf("tampered publication was not rejected: %+v", response)
	}
	if physicalCalls.Load() != 0 {
		t.Fatalf("tampered publication triggered physical backup: calls=%d", physicalCalls.Load())
	}
}

func TestScheduledBackupRequiresJobKey(t *testing.T) {
	installBackupTestPaths(t)
	req := scheduledDatabaseBackupRequest("", "tenant_one")
	var response backupspec.CreateResponse
	if err := (&Agent{}).CreateBackup(req, &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error != "scheduled backup job key is required" {
		t.Fatalf("missing job key response=%+v", response)
	}
}
