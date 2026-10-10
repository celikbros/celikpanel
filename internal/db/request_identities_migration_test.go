package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// Migration 43 (D-029): one row per identified state-changing request.
// Göç 43 (D-029): kimliği olan her durum değiştiren istek için bir satır.

const requestIdentitiesMigrationVersion = 43

func TestRequestIdentitiesMigrationContracts(t *testing.T) {
	database, err := NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	t.Cleanup(database.Close)
	db := database.GetDB()

	var filename string
	if err := db.QueryRow(`SELECT filename FROM schema_migrations WHERE version = ?`,
		requestIdentitiesMigrationVersion).Scan(&filename); err != nil {
		t.Fatalf("ledger entry %d: %v", requestIdentitiesMigrationVersion, err)
	}
	if filename != "043_request_identities.sql" {
		t.Fatalf("ledger filename=%q", filename)
	}

	const id = "0123456789abcdef0123456789abcdef"
	hash := strings.Repeat("a", 64)
	insert := `INSERT INTO request_identities
		(id, actor_user_id, method, route, request_sha256, status, created_at, expires_at)
		VALUES (?, 1, 'POST', '/api/v1/vpn/peers', ?, ?, 10, 20)`
	if _, err := db.Exec(insert, id, hash, "running"); err != nil {
		t.Fatalf("insert running row: %v", err)
	}
	// Defaults: nothing about the answer is known or kept yet.
	var code, retained int
	var contentType string
	var body []byte
	var finished int64
	if err := db.QueryRow(`
		SELECT response_status, response_retained, response_content_type, response_body, finished_at
		FROM request_identities WHERE id = ?`, id).Scan(&code, &retained, &contentType, &body, &finished); err != nil {
		t.Fatal(err)
	}
	if code != 0 || retained != 0 || contentType != "" || body != nil || finished != 0 {
		t.Fatalf("defaults: code=%d retained=%d type=%q body=%v finished=%d", code, retained, contentType, body, finished)
	}

	for name, statement := range map[string][]any{
		"the same identity twice": {insert, id, hash, "running"},
		"an unknown status":       {insert, strings.Repeat("b", 32), hash, "finished"},
		"a short identity":        {insert, strings.Repeat("c", 31), hash, "running"},
		"a long identity":         {insert, strings.Repeat("c", 33), hash, "running"},
		"a short request hash":    {insert, strings.Repeat("d", 32), strings.Repeat("a", 63), "running"},
	} {
		if _, err := db.Exec(statement[0].(string), statement[1:]...); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := db.Exec(`UPDATE request_identities SET response_retained = 2 WHERE id = ?`, id); err == nil {
		t.Fatal("response_retained accepted a value that is neither 0 nor 1")
	}
	for _, status := range []string{"done", "interrupted"} {
		if _, err := db.Exec(`UPDATE request_identities SET status = ? WHERE id = ?`, status, id); err != nil {
			t.Fatalf("status %q: %v", status, err)
		}
	}

	var index string
	if err := db.QueryRow(`
		SELECT name FROM sqlite_master
		WHERE type = 'index' AND tbl_name = 'request_identities' AND name = 'request_identities_expiry'`,
	).Scan(&index); err != nil {
		t.Fatalf("expiry index: %v", err)
	}
}

// A database at the previous ledger entry gains the table on the next start
// and keeps everything else; a second start changes nothing.
// Önceki defter girdisindeki veritabanı sonraki açılışta tabloyu kazanır.
func TestRequestIdentitiesMigrationAppliesToAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if _, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('before-43', 'hash', 'before-43@example.test', 'admin')`); err != nil {
		t.Fatal(err)
	}
	// Put the database back to how the previous release left it.
	if _, err := db.Exec(`DROP TABLE request_identities`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = ?`, requestIdentitiesMigrationVersion); err != nil {
		t.Fatal(err)
	}
	database.Close()

	for start := 0; start < 2; start++ {
		database, err = NewSQLiteDB(path)
		if err != nil {
			t.Fatalf("start %d: %v", start, err)
		}
		db = database.GetDB()
		var rows, users, ledger int
		if err := db.QueryRow(`SELECT COUNT(*) FROM request_identities`).Scan(&rows); err != nil {
			t.Fatalf("start %d: table missing: %v", start, err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'before-43'`).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`,
			requestIdentitiesMigrationVersion).Scan(&ledger); err != nil {
			t.Fatal(err)
		}
		if rows != 0 || users != 1 || ledger != 1 {
			t.Fatalf("start %d: rows=%d users=%d ledger=%d", start, rows, users, ledger)
		}
		database.Close()
	}
}

// The release before this one refuses a database that already carries ledger
// entry 43; rollback is the pre-update snapshot restore, as for every earlier
// migration. This pins the refusal the rollback note relies on.
// Bu sürümden önceki sürüm, 43. defter girdisini taşıyan veritabanını reddeder.
func TestOlderReleaseRefusesALedgerWithTheRequestIdentitiesEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database, err := NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	older := make([]embeddedMigration, 0, len(embedded))
	for _, migration := range embedded {
		if migration.version < requestIdentitiesMigrationVersion {
			older = append(older, migration)
		}
	}
	// Migration 44 (D-031) came after it; the release before 43 knew neither.
	if len(older) != len(embedded)-2 || embedded[len(embedded)-1].version != managedSiteFilesMigrationVersion {
		t.Fatalf("the embedded migrations after %d are not exactly 43 and 44", requestIdentitiesMigrationVersion-1)
	}
	err = database.verifyMigrationLedgerCoverage(t.Context(), older)
	database.Close()
	if err == nil || !strings.Contains(err.Error(), "no matching embedded migration") {
		t.Fatalf("an older release accepted the newer ledger: %v", err)
	}
}
