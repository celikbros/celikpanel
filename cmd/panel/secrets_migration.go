package main

import (
	"context"
	"log"

	"github.com/alicelik/celikpanel/internal/secrets"
)

// encryptLegacyDBPasswords seals any database-server root password stored as
// plaintext before A4. Runs at every startup and is idempotent: already
// sealed values (enc:v1: prefix) are skipped, so after the first pass this
// scans and touches nothing.
//
// encryptLegacyDBPasswords, A4 öncesinde düz metin saklanmış veritabanı
// sunucusu root parolalarını mühürler. Her açılışta koşar ve idempotenttir:
// zaten mühürlü değerler (enc:v1: öneki) atlanır; ilk geçişten sonra tarar
// ama hiçbir şeye dokunmaz.
func (p *Panel) encryptLegacyDBPasswords(ctx context.Context) error {
	db := p.db.GetDB()
	rows, err := db.QueryContext(ctx,
		`SELECT id, root_password_encrypted FROM database_servers
		 WHERE root_password_encrypted IS NOT NULL AND root_password_encrypted != ''`)
	if err != nil {
		return err
	}

	// Collect first, update after Close: SQLite dislikes writes while a read
	// cursor is open on the same table.
	// Önce topla, Close'tan sonra güncelle: SQLite, aynı tabloda okuma
	// imleci açıkken yazmayı sevmez.
	type pending struct {
		id     int
		sealed string
	}
	var updates []pending
	for rows.Next() {
		var id int
		var stored string
		if err := rows.Scan(&id, &stored); err != nil {
			rows.Close()
			return err
		}
		if secrets.IsEncrypted(stored) {
			continue
		}
		sealed, err := p.secrets.Encrypt(stored)
		if err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, pending{id: id, sealed: sealed})
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, u := range updates {
		if _, err := db.ExecContext(ctx,
			`UPDATE database_servers SET root_password_encrypted = ?, updated_at = datetime('now') WHERE id = ?`,
			u.sealed, u.id); err != nil {
			return err
		}
	}
	if len(updates) > 0 {
		log.Printf("Sealed %d legacy plaintext database root password(s)", len(updates))
	}
	return nil
}
