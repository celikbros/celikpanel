package auth_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
)

func TestCreateForAuthEpochConditionallyCreatesSession(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), `panel.sqlite`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	result, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role, status)
		VALUES ('epoch-user', 'hash', 'epoch@example.test', 'customer', 'active')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	store := auth.NewSessionStore(database.GetDB())

	if _, err := store.CreateForAuthEpoch(t.Context(), int(userID), 0, false); err != nil {
		t.Fatalf(`current epoch session: %v`, err)
	}
	if _, err := store.CreateForAuthEpoch(t.Context(), int(userID), 0, true); !errors.Is(err, auth.ErrAuthStateChanged) {
		t.Fatalf(`disabled TOTP condition error = %v`, err)
	}
	if _, err := database.GetDB().Exec(`UPDATE users SET auth_epoch = 1 WHERE id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateForAuthEpoch(t.Context(), int(userID), 0, false); !errors.Is(err, auth.ErrAuthStateChanged) {
		t.Fatalf(`stale epoch error = %v`, err)
	}
	if _, err := database.GetDB().Exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateForAuthEpoch(t.Context(), int(userID), 1, false); !errors.Is(err, auth.ErrAuthStateChanged) {
		t.Fatalf(`suspended state error = %v`, err)
	}

	var sessions int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf(`conditional session count = %d, want 1`, sessions)
	}
}
