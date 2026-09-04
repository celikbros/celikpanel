package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/secrets"
)

// settingSecretKeyFingerprint records WHICH secret key this database's sealed
// values belong to. It lives in the database, not beside the key, because its
// whole purpose is to survive independently of the key file: a database
// restored without its key must be able to say "you are not my key".
// settingSecretKeyFingerprint, bu veritabanının mühürlü değerlerinin HANGİ gizli
// anahtara ait olduğunu kaydeder. Anahtarın yanında değil veritabanında yaşar,
// çünkü bütün amacı anahtar dosyasından bağımsız olarak hayatta kalmaktır:
// anahtarsız geri yüklenmiş bir veritabanı "sen benim anahtarım değilsin"
// diyebilmelidir.
const settingSecretKeyFingerprint = "secret_key_fingerprint"

// errSecretKeyMismatch is the one failure that means the key and the database do
// not belong together. It is deliberately distinct from a malformed ciphertext:
// a bad row is a data fault in one place, while a mismatched key makes every
// sealed value in every family unreadable at once, and the two want opposite
// responses — repair one row, versus write nothing at all.
// errSecretKeyMismatch, anahtar ile veritabanının birbirine ait olmadığını
// gösteren tek hatadır. Bozuk bir şifreli metinden bilerek ayrıdır: bozuk satır
// tek bir yerdeki veri arızasıdır, uyuşmayan anahtar ise her ailedeki her
// mühürlü değeri aynı anda okunamaz kılar; ikisi zıt yanıt ister — bir satırı
// onarmak ile hiçbir şey yazmamak.
var errSecretKeyMismatch = errors.New(
	"the secret key does not belong to this database",
)

// sealedSecretProbe is one family's "is there anything sealed here, and can this
// key open it" question. Adding a family means adding a probe; forgetting to is
// the failure mode this list exists to prevent, so it is defined once and used
// by both the identity check and nothing else.
// sealedSecretProbe, bir ailenin "burada mühürlü bir şey var mı ve bu anahtar
// onu açabiliyor mu" sorusudur. Yeni bir aile eklemek yeni bir yoklama eklemek
// demektir; bunu unutmak, bu listenin engellemek için var olduğu arıza biçimidir.
type sealedSecretProbe struct {
	family string
	query  string
}

var sealedSecretProbes = []sealedSecretProbe{
	{
		family: "database credentials",
		query: `
			SELECT root_password_encrypted FROM database_servers
			WHERE root_password_encrypted IS NOT NULL AND root_password_encrypted != ''
			UNION ALL
			SELECT password FROM database_users WHERE password != ''`,
	},
	{
		family: "TOTP secrets",
		query:  `SELECT totp_secret FROM users WHERE totp_enabled = 1 AND totp_secret IS NOT NULL AND totp_secret != ''`,
	},
	{
		family: "VPN preshared keys",
		query:  `SELECT preshared_key FROM vpn_peers WHERE preshared_key != ''`,
	},
}

// verifySecretKeyIdentity answers, before a single secret is written, whether
// this key belongs to this database.
//
// The check exists because a missing key file is silently replaced with a fresh
// one (internal/secrets.LoadOrCreate), so a database restored without its key is
// indistinguishable from a first boot. Without this, the first migration to find
// an all-plaintext family seals it under the new key while every previously
// sealed family stays unreadable — one database, two keys, and half of it lost
// with no error at the moment of loss.
//
// Three outcomes:
//   - the recorded fingerprint matches: proceed;
//   - it differs: the key and the database are not a pair, and NOTHING may be
//     written in any family;
//   - nothing is recorded yet: either a fresh database, or one that predates
//     this check. Those are told apart by evidence rather than assumption — if
//     any family holds a sealed value, this key must open one before its
//     identity is adopted.
//
// verifySecretKeyIdentity, tek bir sır yazılmadan önce bu anahtarın bu
// veritabanına ait olup olmadığını cevaplar. Üç sonuç: kayıtlı parmak izi
// uyuyorsa devam; farklıysa hiçbir ailede hiçbir şey yazılamaz; hiç kayıt yoksa
// ya taze bir veritabanıdır ya da bu denetimden eskidir — ve bunlar varsayımla
// değil kanıtla ayrılır: herhangi bir ailede mühürlü değer varsa, kimliği
// benimsenmeden önce bu anahtarın onlardan birini açması gerekir.
func (p *Panel) verifySecretKeyIdentity(ctx context.Context) error {
	if p == nil || p.secrets == nil {
		return errors.New("verify secret key identity: secret box unavailable")
	}
	current := p.secrets.Fingerprint()
	if current == "" {
		return errors.New("verify secret key identity: key fingerprint is unavailable")
	}

	recorded := p.setting(ctx, settingSecretKeyFingerprint)
	if recorded == current {
		return nil
	}
	if recorded != "" {
		// The database remembers a different key. This is the restored-without-
		// its-key case, and the correct action is to write nothing at all.
		// Veritabanı başka bir anahtarı hatırlıyor. Bu, anahtarsız geri yükleme
		// durumudur ve doğru eylem hiçbir şey yazmamaktır.
		return fmt.Errorf(
			"%w: this database was sealed with a different key", errSecretKeyMismatch,
		)
	}

	// No identity recorded. Adopt it only on evidence.
	// Kayıtlı kimlik yok. Yalnız kanıtla benimse.
	sealed, family, err := p.firstSealedSecret(ctx)
	if err != nil {
		return fmt.Errorf("verify secret key identity: %w", err)
	}
	if sealed != "" {
		if _, err := p.secrets.Decrypt(sealed); err != nil {
			return fmt.Errorf(
				"%w: %s cannot be opened with it", errSecretKeyMismatch, family,
			)
		}
	}
	if err := p.setSetting(ctx, settingSecretKeyFingerprint, current); err != nil {
		return fmt.Errorf("record secret key identity: %w", err)
	}
	return nil
}

// firstSealedSecret returns any one sealed value in the database, with the
// family it came from, or "" when nothing is sealed anywhere. One value is
// enough: every family is sealed with the same key, so opening one proves the
// pairing for all of them.
// firstSealedSecret, veritabanındaki herhangi bir mühürlü değeri geldiği aileyle
// birlikte döndürür; hiçbir yerde mühürlü değer yoksa "" döner. Bir değer
// yeterlidir: bütün aileler aynı anahtarla mühürlenir, dolayısıyla birini açmak
// hepsi için eşleşmeyi kanıtlar.
func (p *Panel) firstSealedSecret(ctx context.Context) (string, string, error) {
	db := p.db.GetDB()
	for _, probe := range sealedSecretProbes {
		rows, err := db.QueryContext(ctx, probe.query)
		if err != nil {
			// A family whose table does not exist yet is not evidence of
			// anything; a real query fault is.
			// Tablosu henüz olmayan bir aile hiçbir şeyin kanıtı değildir;
			// gerçek bir sorgu arızası ise kanıttır.
			if isMissingTableError(err) {
				continue
			}
			return "", "", fmt.Errorf("probe %s: %w", probe.family, err)
		}
		var found string
		for rows.Next() {
			var stored sql.NullString
			if err := rows.Scan(&stored); err != nil {
				rows.Close()
				return "", "", fmt.Errorf("probe %s: %w", probe.family, err)
			}
			if stored.Valid && secrets.IsEncrypted(stored.String) {
				found = stored.String
				break
			}
		}
		if err := rows.Err(); err != nil && found == "" {
			rows.Close()
			return "", "", fmt.Errorf("probe %s: %w", probe.family, err)
		}
		if err := rows.Close(); err != nil {
			return "", "", fmt.Errorf("probe %s: %w", probe.family, err)
		}
		if found != "" {
			return found, probe.family, nil
		}
	}
	return "", "", nil
}

// isMissingTableError distinguishes "this family has no table on this schema
// version" from a real query fault. Only the first may be skipped: skipping a
// real fault would let the identity check adopt a key it never actually proved.
// isMissingTableError, "bu şema sürümünde bu ailenin tablosu yok" ile gerçek bir
// sorgu arızasını ayırır. Yalnız ilki atlanabilir: gerçek bir arızayı atlamak,
// kimlik denetiminin hiç kanıtlamadığı bir anahtarı benimsemesine yol açardı.
func isMissingTableError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}
