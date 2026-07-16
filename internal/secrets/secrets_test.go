package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Round trip: what goes in must come out, and the stored form must not
// contain the plaintext.
// Gidiş-dönüş: giren çıkmalı ve saklanan biçim düz metni içermemeli.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	box := newTestBox(t)

	for _, plain := range []string{"s3cret!", "türkçe-şifre", strings.Repeat("x", 500)} {
		sealed, err := box.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		if !IsEncrypted(sealed) {
			t.Fatalf("Encrypt(%q) = %q: missing format prefix", plain, sealed)
		}
		if strings.Contains(sealed, plain) {
			t.Fatalf("ciphertext leaks plaintext: %q", sealed)
		}
		got, err := box.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plain {
			t.Fatalf("round trip: got %q, want %q", got, plain)
		}
	}
}

// The empty string means "no password" and must survive unchanged.
// Boş dizgi "parola yok" demektir ve değişmeden çıkmalıdır.
func TestEmptyStringPassesThrough(t *testing.T) {
	box := newTestBox(t)
	sealed, err := box.Encrypt("")
	if err != nil || sealed != "" {
		t.Fatalf("Encrypt(\"\") = %q, %v; want \"\", nil", sealed, err)
	}
	got, err := box.Decrypt("")
	if err != nil || got != "" {
		t.Fatalf("Decrypt(\"\") = %q, %v; want \"\", nil", got, err)
	}
}

// Legacy plaintext (no prefix) must be returned as-is — this is what keeps
// pre-A4 rows readable until the startup migration rewrites them.
// Eski düz metin (öneksiz) olduğu gibi dönmeli — A4 öncesi satırları açılış
// migrasyonu yeniden yazana dek okunur tutan budur.
func TestLegacyPlaintextPassesThrough(t *testing.T) {
	box := newTestBox(t)
	got, err := box.Decrypt("old-plaintext-password")
	if err != nil {
		t.Fatalf("Decrypt(legacy): %v", err)
	}
	if got != "old-plaintext-password" {
		t.Fatalf("Decrypt(legacy) = %q", got)
	}
}

// Tampered ciphertext must fail loudly, not decrypt to garbage.
// Kurcalanmış şifreli metin sessizce çöpe çözülmemeli, açıkça hata vermelidir.
func TestTamperDetection(t *testing.T) {
	box := newTestBox(t)
	sealed, err := box.Encrypt("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if tampered == sealed {
		tampered = sealed[:len(sealed)-2] + "BB"
	}
	if _, err := box.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt(tampered) succeeded; want error")
	}
}

// The key must persist: a second LoadOrCreate on the same path must open
// what the first one sealed.
// Anahtar kalıcı olmalı: aynı yola ikinci LoadOrCreate, ilkinin
// mühürlediğini açabilmelidir.
func TestKeyPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := first.Encrypt("persist-me")
	if err != nil {
		t.Fatal(err)
	}

	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := second.Decrypt(sealed)
	if err != nil || got != "persist-me" {
		t.Fatalf("Decrypt after reload = %q, %v; want \"persist-me\", nil", got, err)
	}
}

// A wrong-size key file is corruption; regenerating would orphan every
// stored secret, so it must be a hard error.
// Yanlış boyutlu anahtar dosyası bozulmadır; yeniden üretmek saklanan her
// sırrı öksüz bırakır, bu yüzden kesin hata olmalıdır.
func TestWrongSizeKeyIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("LoadOrCreate(short key) succeeded; want error")
	}
}

func newTestBox(t *testing.T) *Box {
	t.Helper()
	box, err := LoadOrCreate(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	return box
}
