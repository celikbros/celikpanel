package main

import (
	"errors"
	"path/filepath"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	secretstore "github.com/alicelik/celikpanel/internal/secrets"
)

// newSecretIdentityPanel builds a panel over a fresh database and a key file at
// a caller-chosen path, so a test can hand the same database a different key —
// which is exactly what restoring a backup without its key does.
func newSecretIdentityPanel(t *testing.T, dbPath, keyPath string) *Panel {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(dbPath)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(database.Close)
	box, err := secretstore.LoadOrCreate(keyPath)
	if err != nil {
		t.Fatalf("open secret box: %v", err)
	}
	return &Panel{db: database, secrets: box}
}

// A fresh database adopts the key it is first opened with, and says so durably.
func TestSecretKeyIdentityIsAdoptedOnAFreshDatabase(t *testing.T) {
	dir := t.TempDir()
	panel := newSecretIdentityPanel(t,
		filepath.Join(dir, "panel.sqlite"), filepath.Join(dir, "secret.key"))

	if err := panel.verifySecretKeyIdentity(t.Context()); err != nil {
		t.Fatalf("a fresh database must adopt its key: %v", err)
	}
	recorded := panel.setting(t.Context(), settingSecretKeyFingerprint)
	if recorded == "" {
		t.Fatal("adoption must be recorded durably, or the next boot cannot check it")
	}
	if recorded != panel.secrets.Fingerprint() {
		t.Fatalf("recorded identity %q does not match the key in use", recorded)
	}
	// The fingerprint identifies the key; it must not be the key.
	if len(recorded) != 64 {
		t.Fatalf("fingerprint should be a 32-byte HMAC in hex, got %d chars", len(recorded))
	}

	// Re-verifying is idempotent — every boot runs this.
	if err := panel.verifySecretKeyIdentity(t.Context()); err != nil {
		t.Fatalf("re-verifying the same pairing must succeed: %v", err)
	}
}

// The case this exists for: the database is restored, the key file is not, and
// LoadOrCreate silently mints a new key. The pairing must be refused before a
// single secret is written — a mismatch caught after the first write is a
// database sealed with two keys.
func TestSecretKeyIdentityRefusesADatabaseRestoredWithoutItsKey(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panel.sqlite")

	original := newSecretIdentityPanel(t, dbPath, filepath.Join(dir, "original.key"))
	if err := original.verifySecretKeyIdentity(t.Context()); err != nil {
		t.Fatalf("original pairing must be adopted: %v", err)
	}
	originalFingerprint := original.setting(t.Context(), settingSecretKeyFingerprint)

	// Same database, different key file — the backup arrived without its key.
	restored := newSecretIdentityPanel(t, dbPath, filepath.Join(dir, "replacement.key"))
	if restored.secrets.Fingerprint() == originalFingerprint {
		t.Fatal("the replacement key must differ, or the test proves nothing")
	}

	err := restored.verifySecretKeyIdentity(t.Context())
	if err == nil {
		t.Fatal("a database sealed with another key must be refused")
	}
	if !errors.Is(err, errSecretKeyMismatch) {
		t.Fatalf("the refusal must be classified as a key mismatch, got %v", err)
	}
	// The recorded identity is evidence and must survive the refusal: overwriting
	// it would erase the only proof of which key the rows belong to.
	if after := restored.setting(t.Context(), settingSecretKeyFingerprint); after != originalFingerprint {
		t.Fatalf("a refused key must not overwrite the recorded identity: %q", after)
	}
}

// A database that predates this check has no recorded identity. It is adopted
// only on evidence: if something is sealed, the key must open it. Here it can.
func TestSecretKeyIdentityAdoptsAPreFingerprintDatabaseThatOpens(t *testing.T) {
	dir := t.TempDir()
	panel := newSecretIdentityPanel(t,
		filepath.Join(dir, "panel.sqlite"), filepath.Join(dir, "secret.key"))

	sealed, err := panel.secrets.Encrypt("a-real-password")
	if err != nil {
		t.Fatal(err)
	}
	seedSealedVPNPeer(t, panel, sealed)

	if err := panel.verifySecretKeyIdentity(t.Context()); err != nil {
		t.Fatalf("a key that opens the existing rows must be adopted: %v", err)
	}
	if panel.setting(t.Context(), settingSecretKeyFingerprint) != panel.secrets.Fingerprint() {
		t.Fatal("adoption must record the proven key")
	}
}

// The same shape, but the key does not open what is stored. Adoption on
// assumption instead of evidence is what would seal the rest under a stranger.
func TestSecretKeyIdentityRefusesAPreFingerprintDatabaseThatWillNotOpen(t *testing.T) {
	dir := t.TempDir()
	panel := newSecretIdentityPanel(t,
		filepath.Join(dir, "panel.sqlite"), filepath.Join(dir, "secret.key"))

	// Sealed by some other key: well-formed, unopenable.
	other, err := secretstore.LoadOrCreate(filepath.Join(dir, "stranger.key"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := other.Encrypt("someone-elses-password")
	if err != nil {
		t.Fatal(err)
	}
	seedSealedVPNPeer(t, panel, foreign)

	err = panel.verifySecretKeyIdentity(t.Context())
	if err == nil {
		t.Fatal("a key that cannot open the existing rows must be refused")
	}
	if !errors.Is(err, errSecretKeyMismatch) {
		t.Fatalf("the refusal must be classified as a key mismatch, got %v", err)
	}
	if panel.setting(t.Context(), settingSecretKeyFingerprint) != "" {
		t.Fatal("an unproven key must never be recorded as this database's identity")
	}
}

// Plaintext is not evidence of a pairing: a database holding only unsealed rows
// has nothing to prove a key against, and adopting is correct there.
func TestPlaintextRowsAreNotTreatedAsSealedEvidence(t *testing.T) {
	dir := t.TempDir()
	panel := newSecretIdentityPanel(t,
		filepath.Join(dir, "panel.sqlite"), filepath.Join(dir, "secret.key"))

	seedSealedVPNPeer(t, panel, "legacy-plaintext-key")

	sealed, family, err := panel.firstSealedSecret(t.Context())
	if err != nil {
		t.Fatalf("probing must not fail on a plaintext row: %v", err)
	}
	if sealed != "" {
		t.Fatalf("a plaintext row is not sealed evidence, got %q from %s", sealed, family)
	}
	if err := panel.verifySecretKeyIdentity(t.Context()); err != nil {
		t.Fatalf("an all-plaintext database must adopt its key: %v", err)
	}
}

func seedSealedVPNPeer(t *testing.T, panel *Panel, presharedKey string) {
	t.Helper()
	if _, err := panel.db.GetDB().Exec(
		`INSERT INTO vpn_peers (name, public_key, preshared_key, ip)
		 VALUES ('probe-peer', 'pubkey', ?, '10.8.0.2/32')`,
		presharedKey,
	); err != nil {
		t.Fatalf("seed VPN peer: %v", err)
	}
}
