package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

// parseDBTime tolerates the timestamp formats that end up in SQLite. The
// modernc driver returns TEXT columns as strings, and database/sql will
// not assign a string to time.Time, so we parse it ourselves rather than
// scanning directly. Unparseable or NULL values become the zero time.
//
// parseDBTime, SQLite'da oluşan zaman damgası biçimlerini hoş görür.
// modernc sürücüsü TEXT sütunlarını string olarak döndürür ve database/sql
// bir string'i time.Time'a atamaz; bu yüzden doğrudan taramak yerine
// kendimiz çözeriz. Çözülemeyen ya da NULL değerler sıfır zaman olur.
func parseDBTime(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return t
		}
	}
	return time.Time{}
}

// userColumns is the canonical select list; scanUser must match it.
// userColumns standart seçim listesidir; scanUser onunla eşleşmelidir.
const userColumns = `id, username, password_hash, email, role, COALESCE(account_type,'account'), parent_id, COALESCE(status,'active'), created_at, updated_at`

// scanUser scans a user row, parsing the two timestamp columns leniently.
// scanUser, bir kullanıcı satırını tarar ve iki zaman damgası sütununu
// hoşgörüyle çözer.
func scanUser(row interface{ Scan(...any) error }, user *core.User) error {
	var createdAt, updatedAt sql.NullString
	var parentID sql.NullInt64
	var accountType string
	err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
		&accountType, &parentID, &user.Status, &createdAt, &updatedAt,
	)
	if err != nil {
		return err
	}
	if parentID.Valid {
		v := int(parentID.Int64)
		user.ParentID = &v
	}
	user.AccountType = core.AccountType(accountType)
	user.CreatedAt = parseDBTime(createdAt)
	user.UpdatedAt = parseDBTime(updatedAt)
	return nil
}

func (r *PostgresUserRepository) Create(ctx context.Context, user *core.User) error {
	if user.Status == "" {
		user.Status = "active"
	}
	if user.AccountType == "" {
		user.AccountType = core.AccountTypeAccount
	}
	if user.AccountType != core.AccountTypeAccount {
		return fmt.Errorf("unsupported account type %q: additional users require the dedicated atomic creation path", user.AccountType)
	}
	query := `
		INSERT INTO users (username, password_hash, email, role, account_type, parent_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	var parent any
	if user.ParentID != nil {
		parent = *user.ParentID
	}
	result, err := r.db.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Email, user.Role, user.AccountType, parent, user.Status)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	var createdAt, updatedAt sql.NullString
	if err := r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM users WHERE id = ?", id).
		Scan(&user.ID, &createdAt, &updatedAt); err != nil {
		return err
	}
	user.CreatedAt = parseDBTime(createdAt)
	user.UpdatedAt = parseDBTime(updatedAt)
	return nil
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id int) (*core.User, error) {
	user := &core.User{}
	query := `SELECT ` + userColumns + ` FROM users WHERE id = ?`
	err := scanUser(r.db.QueryRowContext(ctx, query, id), user)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("user not found: %v", err)
	}
	return user, nil
}

func (r *PostgresUserRepository) GetByUsername(ctx context.Context, username string) (*core.User, error) {
	user := &core.User{}
	query := `SELECT ` + userColumns + ` FROM users WHERE username = ?`
	err := scanUser(r.db.QueryRowContext(ctx, query, username), user)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("user not found: %v", err)
	}
	return user, nil
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (*core.User, error) {
	user := &core.User{}
	query := `SELECT ` + userColumns + ` FROM users WHERE email = ?`
	err := scanUser(r.db.QueryRowContext(ctx, query, email), user)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("user not found: %v", err)
	}
	return user, nil
}

func (r *PostgresUserRepository) Update(ctx context.Context, user *core.User) error {
	if user.Status == "" {
		user.Status = "active"
	}
	query := `
		UPDATE users
		SET username = ?, password_hash = ?, email = ?, role = ?, status = ?, updated_at = datetime('now')
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Email, user.Role, user.Status, user.ID)
	return err
}

// UpdateAndRevokeSessions applies the user update and removes every existing
// session in one transaction. Password and suspension changes use this path so
// a failed revocation cannot leave an updated credential with old sessions
// still active.
func (r *PostgresUserRepository) UpdateAndRevokeSessions(ctx context.Context, user *core.User) error {
	if user.Status == "" {
		user.Status = "active"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var storedRole, storedAccountType, storedStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT role, COALESCE(NULLIF(account_type, ''), 'account'),
		       COALESCE(NULLIF(status, ''), 'active')
		FROM users
		WHERE id = ?
	`, user.ID).Scan(&storedRole, &storedAccountType, &storedStatus); err != nil {
		return err
	}
	revokeTeamMembers := storedRole == "customer" &&
		storedAccountType == string(core.AccountTypeAccount) &&
		storedStatus != "suspended" && user.Status == "suspended"

	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET username = ?, password_hash = ?, email = ?, role = ?, status = ?,
		    auth_epoch = auth_epoch + 1, updated_at = datetime('now')
		WHERE id = ?
	`, user.Username, user.PasswordHash, user.Email, user.Role, user.Status, user.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, user.ID); err != nil {
		return err
	}
	if revokeTeamMembers {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET auth_epoch = auth_epoch + 1, updated_at = datetime('now')
			WHERE parent_id = ?
			  AND role = 'customer'
			  AND account_type = 'additional_user'
		`, user.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM sessions
			WHERE user_id IN (
				SELECT id
				FROM users
				WHERE parent_id = ?
				  AND role = 'customer'
				  AND account_type = 'additional_user'
			)
		`, user.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresUserRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *PostgresUserRepository) List(ctx context.Context) ([]*core.User, error) {
	query := `SELECT ` + userColumns + ` FROM users ORDER BY created_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*core.User
	for rows.Next() {
		user := &core.User{}
		if err := scanUser(rows, user); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}
