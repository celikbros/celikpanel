// Package secrets provides reversible encryption for credentials the panel
// must send back out in plaintext (e.g. a database server's root password,
// which the driver needs to open a connection). Values are sealed with
// AES-256-GCM under a single key file on disk; the ciphertext format is
// "enc:v1:" + base64(nonce||ciphertext) so stored values are self-describing
// and legacy plaintext rows remain readable during migration.
//
// secrets paketi, panelin düz metin olarak geri vermek zorunda olduğu
// kimlik bilgileri (örn. veritabanı sunucusunun root parolası — sürücü
// bağlantı açmak için ister) için geri çözülebilir şifreleme sağlar.
// Değerler diskteki tek bir anahtar dosyasıyla AES-256-GCM altında mühürlenir;
// biçim "enc:v1:" + base64(nonce||şifreli) olduğundan saklanan değerler
// kendini tanımlar ve eski düz metin satırlar migrasyon boyunca okunabilir kalır.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// prefix marks a value as sealed by this package (format version 1).
// prefix, bir değerin bu paketçe mühürlendiğini işaretler (biçim sürümü 1).
const prefix = "enc:v1:"

const keySize = 32 // AES-256

// Box seals and opens short secret strings with a single symmetric key.
// Box, kısa gizli dizgileri tek bir simetrik anahtarla mühürler ve açar.
type Box struct {
	aead        cipher.AEAD
	fingerprint string
}

// fingerprintLabel domain-separates the identity proof from any other use of
// the key, so the published fingerprint can never be confused with, or reused
// as, key material.
// fingerprintLabel, kimlik kanıtını anahtarın başka herhangi bir kullanımından
// alan olarak ayırır; böylece yayımlanan parmak izi asla anahtar materyaliyle
// karıştırılamaz ya da onun yerine kullanılamaz.
const fingerprintLabel = "celikpanel-secret-key-fingerprint/v1"

// Fingerprint identifies WHICH key this box holds without revealing it: an
// HMAC of a fixed label under the key. Storing it beside the sealed values lets
// a later boot answer the one question the ciphertext cannot answer on its own
// — "is this the key these rows were sealed with?" — before anything is
// written.
//
// That question matters because a missing key file is silently replaced with a
// fresh one. Without a stored identity, a database restored without its key
// looks exactly like a first boot, and the first write under the new key
// creates a database sealed with two keys, half of which can never be opened.
//
// Fingerprint, bu kutunun HANGİ anahtarı tuttuğunu onu açığa vurmadan
// tanımlar: sabit bir etiketin anahtar altındaki HMAC'i. Mühürlü değerlerin
// yanında saklanması, sonraki bir açılışın şifreli metnin tek başına
// cevaplayamadığı tek soruyu — "bu satırlar bu anahtarla mı mühürlendi?" —
// hiçbir şey yazılmadan önce cevaplamasını sağlar.
//
// Bu soru önemlidir çünkü eksik bir anahtar dosyası sessizce yenisiyle
// değiştirilir. Saklanmış bir kimlik olmadan, anahtarsız geri yüklenmiş bir
// veritabanı ilk açılıştan ayırt edilemez ve yeni anahtar altındaki ilk yazım,
// yarısı bir daha asla açılamayacak, iki anahtarla mühürlenmiş bir veritabanı
// yaratır.
func (b *Box) Fingerprint() string {
	return b.fingerprint
}

func keyFingerprint(key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fingerprintLabel))
	return hex.EncodeToString(mac.Sum(nil))
}

// LoadOrCreate reads the 32-byte key at path, generating and persisting a
// fresh one (mode 0600) on first boot. A key of the wrong size is an error,
// not a silent regeneration — regenerating would orphan every stored secret.
//
// LoadOrCreate, path'teki 32 baytlık anahtarı okur; ilk açılışta yenisini
// üretip kalıcılaştırır (kip 0600). Yanlış boyutlu anahtar hatadır, sessiz
// yeniden üretim değil — yeniden üretmek saklanan her sırrı öksüz bırakır.
func LoadOrCreate(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate key: %w", err)
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, fmt.Errorf("write key file: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("key file %s: expected %d bytes, got %d", path, keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, fingerprint: keyFingerprint(key)}, nil
}

// Encrypt seals plain and returns the self-describing ciphertext. The empty
// string stays empty: "no password" must survive a round trip as-is.
// Encrypt, plain'i mühürler ve kendini tanımlayan şifreli metni döndürür.
// Boş dizgi boş kalır: "parola yok" gidiş-dönüşten aynen çıkmalıdır.
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt. Values without the format
// prefix are legacy plaintext and are returned unchanged, which is what lets
// the startup migration and old rows coexist with new ones.
// Decrypt, Encrypt'in ürettiği değeri açar. Biçim öneki olmayan değerler eski
// düz metindir ve olduğu gibi döner; açılış migrasyonu ile eski satırların
// yenileriyle bir arada yaşamasını sağlayan budur.
func (b *Box) Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", fmt.Errorf("malformed ciphertext: %w", err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("malformed ciphertext: too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether stored already carries the sealed format.
// IsEncrypted, stored'un halihazırda mühürlü biçimi taşıyıp taşımadığını söyler.
func IsEncrypted(stored string) bool {
	return strings.HasPrefix(stored, prefix)
}
