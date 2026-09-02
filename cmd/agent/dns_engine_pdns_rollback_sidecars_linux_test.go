//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func stagePDNSRollbackDir(t *testing.T) (live, backup, candidate string) {
	t.Helper()
	dir := t.TempDir()
	live = filepath.Join(dir, "pdns.sqlite3")
	t.Setenv("CELIKPANEL_PDNS_DB", live)
	requestID := "b9e7d7c1f680a84aebb1a6c9c8136fb3"
	backup = pdnsSwitchBackupPath(requestID)
	candidate = pdnsSwitchCandidatePath(requestID)
	return live, backup, candidate
}

func writeStalePDNSSidecars(t *testing.T, live string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.WriteFile(live+suffix, []byte("stale generation"), 0o640); err != nil {
			t.Fatalf("stage sidecar %s: %v", suffix, err)
		}
	}
}

func sidecarsPresent(live string) []string {
	var present []string
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(live + suffix); err == nil {
			present = append(present, suffix)
		}
	}
	return present
}

// The helper removes exactly the three sidecars and touches nothing else.
// Yardımcı tam olarak üç yan dosyayı kaldırır, başka hiçbir şeye dokunmaz.
func TestRemovePDNSDatabaseSidecarsClearsExactlyTheThree(t *testing.T) {
	live, _, _ := stagePDNSRollbackDir(t)
	if err := os.WriteFile(live, []byte("main"), 0o640); err != nil {
		t.Fatal(err)
	}
	writeStalePDNSSidecars(t, live)
	bystander := filepath.Join(filepath.Dir(live), "unrelated.sqlite3-wal")
	if err := os.WriteFile(bystander, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := removePDNSDatabaseSidecars(live); err != nil {
		t.Fatalf("remove sidecars: %v", err)
	}
	if present := sidecarsPresent(live); len(present) != 0 {
		t.Fatalf("sidecars survived: %v", present)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("the main file must be untouched: %v", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("an unrelated file must be untouched: %v", err)
	}
	// Absent sidecars are not an error: rollback runs on hosts that never had
	// them.
	// Yok olan yan dosyalar hata değildir: geri alma, onları hiç görmemiş
	// sunucularda da koşar.
	if err := removePDNSDatabaseSidecars(live); err != nil {
		t.Fatalf("a second removal must be a no-op: %v", err)
	}
}

// The S-6 Boston attempt-2 shape, reduced. The target PowerDNS ran against the
// candidate and left that generation's WAL and SHM beside the path; rollback
// then put the backup main file back underneath them and SQLite replayed a
// foreign WAL into it — "malformed". After rollback the path must hold the
// backup bytes and nothing else.
//
// Ownership is set with the real pdns account. On a host without one the
// rename has already happened when the lookup fails, so the test accepts that
// exact error and still asserts the durable outcome.
//
// S-6 Boston 2. denemesinin indirgenmiş biçimi. Hedef PowerDNS adaya karşı
// koştu ve o neslin WAL ile SHM dosyalarını yolun yanında bıraktı; geri alma
// ardından yedek ana dosyayı onların altına koydu ve SQLite yabancı bir WAL'ı
// içine yeniden oynattı — "bozuk". Geri almadan sonra yolda yedeğin baytları
// ve başka hiçbir şey olmamalıdır.
//
// Sahiplik gerçek pdns hesabıyla ayarlanır. Hesabı olmayan bir sunucuda arama
// başarısız olduğunda yeniden adlandırma çoktan olmuştur; test tam o hatayı
// kabul eder ve kalıcı sonucu yine de doğrular.
func TestRestorePDNSDatabaseDiscardsTheDiscardedGenerationsSidecars(t *testing.T) {
	live, backup, candidate := stagePDNSRollbackDir(t)
	backupBytes := []byte("backup generation main file")
	if err := os.WriteFile(backup, backupBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("candidate"), 0o640); err != nil {
		t.Fatal(err)
	}
	writeStalePDNSSidecars(t, live)
	sum := sha256.Sum256(backupBytes)

	err := restorePDNSDatabase(dnsEngineSwitchJournal{
		MutationRequestID: "b9e7d7c1f680a84aebb1a6c9c8136fb3",
		PDNSCandidatePath: candidate,
		PDNSBackupPath:    backup,
		PDNSBackupSHA256:  hex.EncodeToString(sum[:]),
		PDNSBackupSize:    int64(len(backupBytes)),
	})
	ownershipUnavailable := false
	if err != nil {
		var unknown user.UnknownUserError
		if !errors.As(err, &unknown) {
			t.Fatalf("restore failed for a reason other than a missing pdns account: %v", err)
		}
		ownershipUnavailable = true
	}

	// The two assertions that carry the incident: sidecars gone, backup bytes
	// live. Both happen before the ownership step, so they hold either way.
	// Olayı taşıyan iki iddia: yan dosyalar gitti, yedek baytları canlı. İkisi
	// de sahiplik adımından önce gerçekleşir, dolayısıyla her durumda tutar.
	if present := sidecarsPresent(live); len(present) != 0 {
		t.Fatalf("the discarded generation's sidecars survived rollback: %v", present)
	}
	got, readErr := os.ReadFile(live)
	if readErr != nil || string(got) != string(backupBytes) {
		t.Fatalf("live must be the backup bytes after rollback, got %q err=%v", got, readErr)
	}
	// Candidate removal comes after ownership in production order; on a host
	// without a pdns account the function has already returned by then.
	// Aday silme, üretim sırasında sahiplikten sonra gelir; pdns hesabı olmayan
	// bir sunucuda işlev o noktaya gelmeden dönmüştür.
	if !ownershipUnavailable {
		if _, statErr := os.Stat(candidate); !os.IsNotExist(statErr) {
			t.Fatalf("the candidate must be removed, stat gave %v", statErr)
		}
	}
}

// A switch that started from no database has no backup to restore, but the
// candidate generation can still have left sidecars. Rollback must clear them
// too, or the next forward switch refuses the path at staging.
// Veritabanı olmadan başlayan bir geçişin geri yüklenecek yedeği yoktur, ama
// aday nesli yine de yan dosya bırakmış olabilir. Geri alma onları da
// temizlemelidir; yoksa bir sonraki ileri geçiş yolu daha hazırlıkta reddeder.
func TestRestorePDNSDatabaseWithoutBackupStillClearsSidecars(t *testing.T) {
	live, backup, candidate := stagePDNSRollbackDir(t)
	writeStalePDNSSidecars(t, live)

	if err := restorePDNSDatabase(dnsEngineSwitchJournal{
		MutationRequestID: "b9e7d7c1f680a84aebb1a6c9c8136fb3",
		PDNSCandidatePath: candidate,
		PDNSBackupPath:    backup,
	}); err != nil {
		t.Fatalf("rollback from an empty source must succeed: %v", err)
	}
	if present := sidecarsPresent(live); len(present) != 0 {
		t.Fatalf("sidecars survived an empty-source rollback: %v", present)
	}
	if err := requireNoPDNSDatabaseSidecars(live); err != nil {
		t.Fatalf("the path must be stageable again: %v", err)
	}
}
