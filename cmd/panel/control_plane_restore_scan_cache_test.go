package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// On the R-003 drill the restored host's components screen reported BIND
// "active (running)" with host A's scan time, on a machine that had never had
// BIND installed. The row was true about the server that died and false about
// this one, and nothing in the reader could tell the difference.
//
// R-003 tatbikatında, geri yüklenen sunucunun bileşenler ekranı, BIND'in hiç
// kurulmadığı bir makinede, A sunucusunun tarama zamanıyla BIND'i "etkin
// (çalışıyor)" bildiriyordu. Satır, ölen sunucu hakkında doğru bu sunucu
// hakkında yanlıştı ve okuyucudaki hiçbir şey ikisini ayırt edemiyordu.
func TestControlPlaneRestoreDiscardsTheArchivedComponentScan(t *testing.T) {
	source := newControlPlaneTestTree(t)
	databasePath := filepath.Join(source.Root.DataDir, controlPlaneDatabaseBasename)
	database, err := paneldb.NewSQLiteDB(databasePath)
	if err != nil {
		t.Fatalf("open the source panel database: %v", err)
	}
	scan := `{"observations":[{"id":"bind","is_installed":true,"status":"active (running)"}]}`
	if _, err := database.GetDB().Exec(
		`INSERT INTO service_scan_cache (id, data, scanned_at)
		 VALUES (1, ?, '2026-09-03T11:04:55Z')`, scan,
	); err != nil {
		database.Close()
		t.Fatalf("seed the archived component scan: %v", err)
	}
	database.Close()

	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath, controlPlaneTestKey, source.Root, &bytes.Buffer{},
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}

	target := newControlPlaneTargetRoots(t)
	var report bytes.Buffer
	if _, err := restoreControlPlaneArchive(
		archivePath, controlPlaneTestKey, target, &report,
	); err != nil {
		t.Fatalf("restore the archive: %v\n%s", err, report.String())
	}

	restored, err := sql.Open(
		"sqlite",
		sqliteSnapshotURI(
			filepath.Join(target.DataDir, controlPlaneDatabaseBasename), true,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var rows int
	if err := restored.QueryRow(
		`SELECT count(*) FROM service_scan_cache`,
	).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the archived component scan survived the restore: %d row(s)", rows)
	}
	// Everything the archive is FOR is still there. The scan is discarded
	// because it observes a machine, not because rows are cheap to drop.
	var marker string
	if err := restored.QueryRow(
		`SELECT value FROM panel_settings WHERE key = ?`, controlPlaneTestMarker,
	).Scan(&marker); err != nil {
		t.Fatalf("the restore lost durable panel state: %v", err)
	}
	if !strings.Contains(report.String(), "the host that died") {
		t.Fatalf("the report does not say the scan was discarded:\n%s", report.String())
	}
}

// A restore with nothing in the cache says nothing about it: the line is a
// report of an action, not a reassurance printed unconditionally.
//
// Önbelleğinde hiçbir şey olmayan bir geri yükleme onun hakkında hiçbir şey
// söylemez: satır, koşulsuz basılan bir güvence değil, bir eylemin raporudur.
func TestControlPlaneRestoreIsSilentWhenNoComponentScanWasArchived(t *testing.T) {
	source := newControlPlaneTestTree(t)
	archivePath := filepath.Join(t.TempDir(), "control-plane.cpbak")
	if _, err := createControlPlaneArchive(
		archivePath, controlPlaneTestKey, source.Root, &bytes.Buffer{},
	); err != nil {
		t.Fatalf("create the archive: %v", err)
	}
	target := newControlPlaneTargetRoots(t)
	var report bytes.Buffer
	if _, err := restoreControlPlaneArchive(
		archivePath, controlPlaneTestKey, target, &report,
	); err != nil {
		t.Fatalf("restore the archive: %v\n%s", err, report.String())
	}
	if strings.Contains(report.String(), "the host that died") {
		t.Fatalf("the report claims a discard that never happened:\n%s", report.String())
	}
}

// kept=1 with nothing named is a summary the operator cannot act on. panel.env
// is kept on every restore where the installer wrote one, and the count said so
// while the report named it only when a key happened to differ.
//
// Hiçbir şey adlandırılmadan kept=1, operatörün üzerine hareket edemeyeceği bir
// özettir. panel.env, yükleyicinin bir tane yazdığı her geri yüklemede
// korunur; sayaç bunu söylüyor, rapor ise ancak bir anahtar farklı olduğunda
// onu adlandırıyordu.
func TestControlPlaneRestoreNamesTheKeptPanelEnvEvenWhenNothingDiffers(t *testing.T) {
	identical := "CELIKPANEL_LISTEN=:2083\nCELIKPANEL_TLS=1\n"
	run := runControlPlanePolicyRestore(
		t,
		map[string]string{"panel.env": identical},
		map[string]string{"panel.env": identical},
	)
	if run.Result.Kept != 1 {
		t.Fatalf("kept=%d, want 1\n%s", run.Result.Kept, run.Report)
	}
	wanted := "panel.env: the installer's configuration is kept"
	if strings.Count(run.Report, wanted) != 1 {
		t.Fatalf("the report does not name the kept panel.env exactly once:\n%s",
			run.Report)
	}
	if strings.Contains(run.Report, "differs from the archive") {
		t.Fatalf("identical keys were reported as differing:\n%s", run.Report)
	}
	if got := controlPlaneTargetFile(t, run.Target, "panel.env"); got != identical {
		t.Fatalf("panel.env was replaced:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(run.Target.ConfDir, "panel.env")); err != nil {
		t.Fatal(err)
	}
}
