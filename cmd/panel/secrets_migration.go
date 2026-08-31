package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/secrets"
)

// errSealedSecretUnreadable marks the one migration failure that is a property
// of the *pair* (this database, this secret.key) rather than of the data: an
// already-sealed value that will not open. Decrypt returns unprefixed legacy
// plaintext unchanged, so this can only mean the key does not belong to these
// rows — the shape a restore without its secret.key takes.
//
// It stays an error, and the migration still commits nothing: sealing the
// remaining plaintext rows under a key that cannot read the existing ones is
// exactly the half-and-half state this file exists to prevent. What changes is
// the caller's response — startup degrades the subsystem instead of exiting,
// because a panel that cannot boot cannot be used to re-enter the credentials
// it is complaining about.
//
// errSealedSecretUnreadable, veriye değil *çifte* (bu veritabanı, bu
// secret.key) ait olan tek göç hatasını işaretler: açılmayan, zaten mühürlü bir
// değer. Decrypt öneksiz eski düz metni olduğu gibi döndürdüğü için bu yalnızca
// anahtarın bu satırlara ait olmadığı anlamına gelir — secret.key'siz bir geri
// yüklemenin aldığı biçim.
//
// Hata olarak kalır ve göç yine hiçbir şeyi commit etmez: mevcutları okuyamayan
// bir anahtarla kalan düz metin satırları mühürlemek, tam da bu dosyanın
// engellemek için var olduğu yarı-yarıya durumdur. Değişen, çağıranın yanıtı —
// açılış, süreçten çıkmak yerine alt sistemi kısıtlar; çünkü açılamayan bir
// panel, şikâyet ettiği kimlik bilgilerini yeniden girmek için kullanılamaz.
var errSealedSecretUnreadable = errors.New(
	"a sealed secret could not be opened with the current key",
)

// encryptLegacyDBPasswords seals legacy database-server root passwords and
// database-user passwords. It runs at every startup and is idempotent:
// already sealed values are decrypted and validated before they are trusted.
// The collected updates are committed atomically so startup never accepts a
// corrupt ciphertext or a partial migration.
//
// encryptLegacyDBPasswords, A4 öncesinde düz metin saklanmış veritabanı
// sunucusu root parolalarını mühürler. Her açılışta koşar ve idempotenttir:
// zaten mühürlü değerler (enc:v1: öneki) atlanır; ilk geçişten sonra tarar
// ama hiçbir şeye dokunmaz.
func (p *Panel) encryptLegacyDBPasswords(ctx context.Context) error {
	if p.secrets == nil {
		return fmt.Errorf(`migrate database credentials: secret box unavailable`)
	}
	db := p.db.GetDB()
	rows, err := db.QueryContext(ctx, `
		SELECT 'server', id, root_password_encrypted
		FROM database_servers
		WHERE root_password_encrypted IS NOT NULL AND root_password_encrypted != ''
		UNION ALL
		SELECT 'user', id, password
		FROM database_users
		WHERE password != ''`)
	if err != nil {
		return err
	}

	// Collect first, update after Close: SQLite dislikes writes while a read
	// cursor is open on the same table.
	// Önce topla, Close'tan sonra güncelle: SQLite, aynı tabloda okuma
	// imleci açıkken yazmayı sevmez.
	type pending struct {
		kind   string
		id     int
		sealed string
	}
	var updates []pending
	for rows.Next() {
		var kind string
		var id int
		var stored string
		if err := rows.Scan(&kind, &id, &stored); err != nil {
			rows.Close()
			return err
		}
		plain, err := p.secrets.Decrypt(stored)
		if err != nil {
			rows.Close()
			return fmt.Errorf(
				`validate database %s credential %d: %w: %w`,
				kind, id, errSealedSecretUnreadable, err,
			)
		}
		if len(plain) == 0 {
			rows.Close()
			return fmt.Errorf(`validate database %s credential %d: empty secret`, kind, id)
		}
		if secrets.IsEncrypted(stored) {
			continue
		}
		sealed, err := p.secrets.Encrypt(plain)
		if err != nil {
			rows.Close()
			return fmt.Errorf(`seal database %s credential %d: %w`, kind, id, err)
		}
		updates = append(updates, pending{kind: kind, id: id, sealed: sealed})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, u := range updates {
		var query string
		switch u.kind {
		case "server":
			query = `UPDATE database_servers SET root_password_encrypted = ?, updated_at = datetime('now') WHERE id = ?`
		case "user":
			query = `UPDATE database_users SET password = ?, updated_at = datetime('now') WHERE id = ?`
		}
		if _, err := tx.ExecContext(ctx, query, u.sealed, u.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(updates) > 0 {
		log.Printf("Sealed %d legacy plaintext database credential(s)", len(updates))
	}
	return nil
}

// encryptLegacyTOTPSecrets validates every configured TOTP secret and seals
// all legacy plaintext rows in one transaction. Encrypted values are decrypted
// before they are trusted, so a malformed or wrong-key ciphertext stops startup
// instead of silently disabling or bypassing the second factor.
func (p *Panel) encryptLegacyTOTPSecrets(ctx context.Context) error {
	if p.secrets == nil {
		return fmt.Errorf("migrate TOTP secrets: secret box unavailable")
	}
	db := p.db.GetDB()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, totp_secret, totp_enabled
		FROM users
		WHERE totp_enabled = 1
		   OR (totp_secret IS NOT NULL AND totp_secret != '')
		ORDER BY id`)
	if err != nil {
		return err
	}
	type pending struct {
		id       int
		original string
		sealed   string
	}
	var updates []pending
	for rows.Next() {
		var id int
		var stored *string
		var enabled int
		if err := rows.Scan(&id, &stored, &enabled); err != nil {
			rows.Close()
			return err
		}
		if stored == nil || *stored == "" {
			rows.Close()
			return fmt.Errorf("migrate TOTP secret for user %d: enabled account has no secret", id)
		}
		plain, err := p.secrets.Decrypt(*stored)
		if err != nil {
			rows.Close()
			return fmt.Errorf(
				"migrate TOTP secret for user %d: %w: %w",
				id, errSealedSecretUnreadable, err,
			)
		}
		if !auth.ValidateTOTPSecret(plain) {
			rows.Close()
			return fmt.Errorf("migrate TOTP secret for user %d: invalid secret", id)
		}
		if secrets.IsEncrypted(*stored) {
			continue
		}
		sealed, err := p.secrets.Encrypt(plain)
		if err != nil {
			rows.Close()
			return fmt.Errorf("migrate TOTP secret for user %d: %w", id, err)
		}
		updates = append(updates, pending{id: id, original: *stored, sealed: sealed})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, update := range updates {
		result, err := tx.ExecContext(ctx, `
			UPDATE users SET totp_secret = ?
			WHERE id = ? AND totp_secret = ?`, update.sealed, update.id, update.original)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return fmt.Errorf("migrate TOTP secret for user %d: concurrent update", update.id)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(updates) > 0 {
		log.Printf("Sealed %d legacy plaintext TOTP secret(s)", len(updates))
	}
	return nil
}

// encryptLegacyVPNPresharedKeys seals WireGuard preshared keys that predate
// encrypted VPN storage. Existing ciphertext is decrypted and validated before
// it is trusted. The migration is idempotent and fails startup on partial work.
// encryptLegacyVPNPresharedKeys şifreli VPN depolamasından önce yazılmış
// WireGuard ön paylaşımlı anahtarlarını mühürler. Tekrarlanabilir çalışır ve
// yarım kalan bir işlemde panel başlangıcını durdurur.
func (p *Panel) encryptLegacyVPNPresharedKeys(ctx context.Context) error {
	if p.secrets == nil {
		return fmt.Errorf(`migrate VPN preshared keys: secret box unavailable`)
	}
	db := p.db.GetDB()
	rows, err := db.QueryContext(ctx, `
		SELECT id, preshared_key
		FROM vpn_peers
		WHERE preshared_key != ''`)
	if err != nil {
		return err
	}

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
		plain, err := p.secrets.Decrypt(stored)
		if err != nil {
			rows.Close()
			return fmt.Errorf(
				`validate VPN preshared key %d: %w: %w`,
				id, errSealedSecretUnreadable, err,
			)
		}
		if len(plain) == 0 {
			rows.Close()
			return fmt.Errorf(`validate VPN preshared key %d: empty secret`, id)
		}
		if secrets.IsEncrypted(stored) {
			continue
		}
		sealed, err := p.secrets.Encrypt(plain)
		if err != nil {
			rows.Close()
			return fmt.Errorf(`seal VPN preshared key %d: %w`, id, err)
		}
		updates = append(updates, pending{id: id, sealed: sealed})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `
			UPDATE vpn_peers
			SET preshared_key = ?, updated_at = datetime('now')
			WHERE id = ?`, update.sealed, update.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(updates) > 0 {
		log.Printf("Sealed %d legacy plaintext VPN preshared key(s)", len(updates))
	}
	return nil
}
