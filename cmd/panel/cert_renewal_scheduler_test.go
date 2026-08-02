package main

import (
	"path/filepath"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestRunDueCertRenewalsRejectsPartialJobListOnScanError(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)

	result, err := database.GetDB().Exec(`
		INSERT INTO users (username,password_hash,email,role)
		VALUES ('renewal-admin','x','renewal@example.test','admin')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.GetDB().Exec(`
		INSERT INTO subscriptions (owner_id,name,status)
		VALUES (?, 'renewal-test', 'active')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"valid-renewal.example.test", "corrupt-renewal.example.test"} {
		if _, err := database.GetDB().Exec(`
			INSERT INTO domains (subscription_id,name,status)
			VALUES (?, ?, 'active')`, subscriptionID, domain); err != nil {
			t.Fatal(err)
		}
	}

	expiresAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := database.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id,type,cert_path,key_path,expires_at,auto_renew,renewal_status,status
		)
		SELECT id,'custom','/tmp/valid.crt','/tmp/valid.key',?,0,'','active'
		FROM domains WHERE name = 'valid-renewal.example.test'`, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO ssl_certificates (
			domain_id,type,cert_path,key_path,expires_at,auto_renew,renewal_status,status
		)
		SELECT id,'custom','/tmp/corrupt.crt','/tmp/corrupt.key',?,'not-a-bool','','active'
		FROM domains WHERE name = 'corrupt-renewal.example.test'`, expiresAt); err != nil {
		t.Fatal(err)
	}

	panel := &Panel{db: database}
	panel.runDueCertRenewals()

	var status string
	if err := database.GetDB().QueryRow(`
		SELECT sc.renewal_status
		FROM ssl_certificates sc
		JOIN domains d ON d.id = sc.domain_id
		WHERE d.name = 'valid-renewal.example.test'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("valid row was processed from a partial job list: status=%q", status)
	}
}
