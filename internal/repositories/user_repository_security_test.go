package repositories_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
)

func newUserSessionRepositoryFixture(t *testing.T) (*repositories.PostgresUserRepository, *paneldb.SQLiteDB, *core.User) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), `panel.sqlite`))
	if err != nil {
		t.Fatalf(`open user repository database: %v`, err)
	}
	t.Cleanup(database.Close)
	repository := repositories.NewPostgresUserRepository(database.GetDB())
	user := &core.User{
		Username:     `session-user`,
		PasswordHash: `old-hash`,
		Email:        `session-user@example.test`,
		Role:         `customer`,
		Status:       `active`,
	}
	if err := repository.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ('session-one', ?, '2099-01-01T00:00:00Z'),
		       ('session-two', ?, '2099-01-01T00:00:00Z')`, user.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	return repository, database, user
}

func TestUpdateAndRevokeSessionsChangesPasswordAndRemovesAllSessions(t *testing.T) {
	repository, database, user := newUserSessionRepositoryFixture(t)
	user.PasswordHash = `new-hash`

	if err := repository.UpdateAndRevokeSessions(context.Background(), user); err != nil {
		t.Fatalf(`UpdateAndRevokeSessions: %v`, err)
	}
	var storedHash string
	var sessions int
	var epoch int64
	if err := database.GetDB().QueryRow(`SELECT password_hash FROM users WHERE id = ?`, user.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, user.ID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if storedHash != `new-hash` || sessions != 0 || epoch != 1 {
		t.Fatalf(`stored hash/sessions/epoch = %q/%d/%d, want new-hash/0/1`, storedHash, sessions, epoch)
	}
}

func TestUpdateAndRevokeSessionsRollsBackWhenSessionDeleteFails(t *testing.T) {
	repository, database, user := newUserSessionRepositoryFixture(t)
	if _, err := database.GetDB().Exec(`
		CREATE TRIGGER block_session_revoke
		BEFORE DELETE ON sessions
		BEGIN
			SELECT RAISE(ABORT, 'blocked');
		END`); err != nil {
		t.Fatal(err)
	}
	user.PasswordHash = `new-hash`

	if err := repository.UpdateAndRevokeSessions(context.Background(), user); err == nil {
		t.Fatal(`UpdateAndRevokeSessions succeeded despite blocked revocation`)
	}
	var storedHash string
	var sessions int
	var epoch int64
	if err := database.GetDB().QueryRow(`SELECT password_hash FROM users WHERE id = ?`, user.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, user.ID).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if storedHash != `old-hash` || sessions != 2 || epoch != 0 {
		t.Fatalf(`rollback left hash/sessions/epoch = %q/%d/%d, want old-hash/2/0`, storedHash, sessions, epoch)
	}
}
