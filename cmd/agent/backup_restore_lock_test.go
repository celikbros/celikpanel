package main

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

// D-029. One restore per domain at a time; the second is refused, it does not
// wait.
// D-029. Alan adı başına aynı anda tek geri yükleme; ikincisi reddedilir.

func restoreLockTestScope(subscriptionID, domainID int) backupScope {
	return backupScope{
		ProtocolVersion: backupspec.ProtocolVersion,
		SubscriptionID:  subscriptionID,
		DomainID:        domainID,
		DomainName:      "example.test",
	}
}

func TestOnlyOneRestoreOfADomainHoldsTheLock(t *testing.T) {
	release, ok := tryLockBackupRestore(restoreLockTestScope(41, 42))
	if !ok {
		t.Fatal("the first restore of a domain was refused")
	}
	if _, again := tryLockBackupRestore(restoreLockTestScope(41, 42)); again {
		t.Fatal("a second restore of the same domain was admitted")
	}
	// Another domain, and the same domain number under another subscription,
	// are not held up.
	for _, other := range []backupScope{restoreLockTestScope(41, 43), restoreLockTestScope(40, 42)} {
		otherRelease, otherOK := tryLockBackupRestore(other)
		if !otherOK {
			t.Fatalf("restore of %+v was refused because of another domain", other)
		}
		otherRelease()
	}
	release()
	after, ok := tryLockBackupRestore(restoreLockTestScope(41, 42))
	if !ok {
		t.Fatal("the domain stayed locked after its restore ended")
	}
	after()
	backupRestoresRunning.Lock()
	defer backupRestoresRunning.Unlock()
	if len(backupRestoresRunning.domains) != 0 {
		t.Fatalf("%d domains are still marked as restoring", len(backupRestoresRunning.domains))
	}
}

func TestConcurrentRestoreClaimsNeverOverlap(t *testing.T) {
	scope := restoreLockTestScope(51, 52)
	var holders, admitted, refused atomic.Int32
	var overlapped atomic.Bool
	var group sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for attempt := 0; attempt < 50; attempt++ {
				release, ok := tryLockBackupRestore(scope)
				if !ok {
					refused.Add(1)
					continue
				}
				if holders.Add(1) > 1 {
					overlapped.Store(true)
				}
				admitted.Add(1)
				holders.Add(-1)
				release()
			}
		}()
	}
	close(start)
	group.Wait()
	if overlapped.Load() {
		t.Fatal("two restores of the same domain held the lock at once")
	}
	if admitted.Load() == 0 || admitted.Load()+refused.Load() != 16*50 {
		t.Fatalf("admitted=%d refused=%d", admitted.Load(), refused.Load())
	}
}

// The refusal comes before the Agent resolves the backup, reads the document
// root or writes a safety backup: the request names a backup that does not
// exist and a domain no test path knows, and is still answered as busy.
// Ret, Agent yedeği çözmeden, belge kökünü okumadan ya da güvenlik yedeği
// yazmadan önce gelir.
func TestRestoreBackupIsRefusedWhileAnotherRestoreOfTheDomainRuns(t *testing.T) {
	oldBase := backupBaseDir
	backupBaseDir = t.TempDir()
	t.Cleanup(func() { backupBaseDir = oldBase })
	scope := restoreLockTestScope(61, 62)
	release, ok := tryLockBackupRestore(scope)
	if !ok {
		t.Fatal("could not claim the domain")
	}
	request := &backupspec.RestoreRequest{
		ProtocolVersion: scope.ProtocolVersion,
		SubscriptionID:  scope.SubscriptionID,
		DomainID:        scope.DomainID,
		DomainName:      scope.DomainName,
		BackupName:      "files-20261009T120000.000000000Z-0123456789abcdef.cpbak",
	}
	var response backupspec.RestoreResponse
	if err := (&Agent{}).RestoreBackup(request, &response); err != nil {
		t.Fatalf("the refusal must be an answer, not a transport error: %v", err)
	}
	if response.Success || response.Error != backupspec.RestoreInProgress || response.SafetyBackup != nil {
		t.Fatalf("response=%+v", response)
	}
	release()
	// With the domain free the same request gets past the lock and fails on
	// the backup it names, which is the answer it had before D-029.
	response = backupspec.RestoreResponse{}
	if err := (&Agent{}).RestoreBackup(request, &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.Error == "" || response.Error == backupspec.RestoreInProgress {
		t.Fatalf("after release: response=%+v", response)
	}
	backupRestoresRunning.Lock()
	defer backupRestoresRunning.Unlock()
	if len(backupRestoresRunning.domains) != 0 {
		t.Fatal("a failed restore left its domain locked")
	}
}
