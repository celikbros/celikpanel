//go:build linux

package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

// D-029. A manual backup now carries the request's identity as its job key,
// and two restores of one domain never interleave.
// D-029. Elle alınan yedek artık isteğin kimliğini iş anahtarı olarak taşır ve
// bir alan adının iki geri yüklemesi asla iç içe geçmez.

func manualDatabaseBackupRequest(jobKey string) *backupspec.CreateRequest {
	req := testCreateRequest(backupspec.TypeDatabase)
	req.JobKey = jobKey
	req.Database = backupspec.DatabaseIdentity{ID: 1, Name: "tenant_one", Type: "mysql"}
	return req
}

func TestManualBackupWithTheRequestJobKeyPublishesOnce(t *testing.T) {
	installBackupTestPaths(t)
	oldDump := dumpDatabaseToFile
	var physicalCalls atomic.Int32
	dumpDatabaseToFile = func(_ backupspec.DatabaseIdentity, destination string) error {
		physicalCalls.Add(1)
		return writeGzipText(destination, "snapshot")
	}
	t.Cleanup(func() { dumpDatabaseToFile = oldDump })

	agent := &Agent{}
	const jobKey = "request:0123456789abcdef0123456789abcdef"
	if !backupspec.ValidJobKey(jobKey) {
		t.Fatal("the request job key is not a valid job key")
	}
	names := make([]string, 3)
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := range names {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			var response backupspec.CreateResponse
			if err := agent.CreateBackup(manualDatabaseBackupRequest(jobKey), &response); err != nil || !response.Success {
				t.Errorf("arrival %d: err=%v response=%+v", index, err, response)
				return
			}
			if response.Backup.Origin != backupspec.OriginManual {
				t.Errorf("arrival %d: origin=%q", index, response.Backup.Origin)
			}
			names[index] = response.Backup.Name
		}(index)
	}
	close(start)
	group.Wait()
	if names[0] == "" || names[1] != names[0] || names[2] != names[0] {
		t.Fatalf("the same request was given different backups: %q", names)
	}
	if physicalCalls.Load() != 1 {
		t.Fatalf("the database was dumped %d times, want once", physicalCalls.Load())
	}
	if count := publishedBackupCount(t); count != 1 {
		t.Fatalf("published backups=%d, want 1", count)
	}

	// Before D-029 the Panel sent no job key for a manual backup: the Agent
	// took no lock and every arrival built its own archive.
	for attempt := 0; attempt < 3; attempt++ {
		var response backupspec.CreateResponse
		if err := agent.CreateBackup(manualDatabaseBackupRequest(""), &response); err != nil || !response.Success {
			t.Fatalf("keyless create %d: err=%v response=%+v", attempt, err, response)
		}
	}
	if count := publishedBackupCount(t); count != 4 {
		t.Fatalf("published backups after three keyless creates=%d, want 4", count)
	}
}

// Two restores of the same domain arrive while the first is still writing its
// safety backup. The second is refused before it does anything; the first
// finishes and the document root holds exactly the backup's content.
// Aynı alan adının iki geri yüklemesi, ilki güvenlik yedeğini yazarken gelir.
func TestSecondRestoreOfADomainIsRefusedWhileTheFirstRuns(t *testing.T) {
	_, docroot := installBackupTestPaths(t)
	if err := os.WriteFile(filepath.Join(docroot, "index.html"), []byte("backed up"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := &Agent{}
	info, err := agent.createBackup(testCreateRequest(backupspec.TypeFiles))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docroot, "index.html"), []byte("changed later"), 0o644); err != nil {
		t.Fatal(err)
	}
	request := func() *backupspec.RestoreRequest {
		return &backupspec.RestoreRequest{
			ProtocolVersion: backupspec.ProtocolVersion,
			SubscriptionID:  7, DomainID: 9, DomainName: "example.test",
			BackupName: info.Name,
		}
	}

	// Hold the domain exactly as a running restore does, then let three more
	// arrive at once.
	release, ok := tryLockBackupRestore(testScope())
	if !ok {
		t.Fatal("could not claim the domain")
	}
	before := publishedBackupCount(t)
	var group sync.WaitGroup
	for index := 0; index < 3; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			var response backupspec.RestoreResponse
			if err := agent.RestoreBackup(request(), &response); err != nil {
				t.Errorf("arrival %d: %v", index, err)
				return
			}
			if response.Success || response.Error != backupspec.RestoreInProgress || response.SafetyBackup != nil {
				t.Errorf("arrival %d was not refused as busy: %+v", index, response)
			}
		}(index)
	}
	group.Wait()
	if after := publishedBackupCount(t); after != before {
		t.Fatalf("a refused restore wrote %d safety backups", after-before)
	}
	if data, err := os.ReadFile(filepath.Join(docroot, "index.html")); err != nil || string(data) != "changed later" {
		t.Fatalf("a refused restore changed the document root: %q %v", data, err)
	}
	release()

	var response backupspec.RestoreResponse
	if err := agent.RestoreBackup(request(), &response); err != nil || !response.Success {
		t.Fatalf("restore after the domain was free: err=%v response=%+v", err, response)
	}
	if data, err := os.ReadFile(filepath.Join(docroot, "index.html")); err != nil || string(data) != "backed up" {
		t.Fatalf("document root after restore: %q %v", data, err)
	}
	if after := publishedBackupCount(t); after != before+1 {
		t.Fatalf("one restore wrote %d safety backups, want 1", after-before)
	}
}
