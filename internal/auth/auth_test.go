package auth

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was stored in plaintext")
	}

	ok, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword(correct) = %v, %v; want true, nil", ok, err)
	}

	ok, err = VerifyPassword("wrong password", hash)
	if err != nil || ok {
		t.Fatalf("VerifyPassword(wrong) = %v, %v; want false, nil", ok, err)
	}
}

func TestHashPasswordUniqueSalt(t *testing.T) {
	// Same password must yield different hashes (random salt).
	// Aynı parola farklı özetler vermelidir (rastgele tuz).
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Fatal("two hashes of the same password are identical; salt not random")
	}
}

func TestVerifyPasswordRejectsGarbage(t *testing.T) {
	if _, err := VerifyPassword("x", "not-a-valid-hash"); err == nil {
		t.Fatal("VerifyPassword accepted a malformed hash")
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT);
		CREATE TABLE sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO users (id, username) VALUES (42, 'admin');
	`)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewSessionStore(newTestDB(t))

	token, err := store.Create(ctx, 42)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" {
		t.Fatal("empty session token")
	}

	userID, err := store.Validate(ctx, token)
	if err != nil || userID != 42 {
		t.Fatalf("Validate = %d, %v; want 42, nil", userID, err)
	}

	if err := store.Delete(ctx, token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Validate(ctx, token); err != ErrSessionInvalid {
		t.Fatalf("Validate after delete = %v; want ErrSessionInvalid", err)
	}
}

func TestSessionValidateRejectsUnknownToken(t *testing.T) {
	store := NewSessionStore(newTestDB(t))
	if _, err := store.Validate(context.Background(), "deadbeef"); err != ErrSessionInvalid {
		t.Fatalf("Validate(unknown) = %v; want ErrSessionInvalid", err)
	}
}

func TestSessionStoresOnlyHash(t *testing.T) {
	// The raw token must never appear in the table; only its hash.
	// Ham jeton tabloda asla görünmemeli; yalnızca özeti.
	ctx := context.Background()
	db := newTestDB(t)
	store := NewSessionStore(db)

	token, _ := store.Create(ctx, 42)

	var stored string
	if err := db.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if stored == token {
		t.Fatal("raw token stored in database; expected a hash")
	}
}
