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

// scanUser scans a user row, parsing the two timestamp columns leniently.
// scanUser, bir kullanıcı satırını tarar ve iki zaman damgası sütununu
// hoşgörüyle çözer.
func scanUser(row interface{ Scan(...any) error }, user *core.User) error {
	var createdAt, updatedAt sql.NullString
	err := row.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return err
	}
	user.CreatedAt = parseDBTime(createdAt)
	user.UpdatedAt = parseDBTime(updatedAt)
	return nil
}

func (r *PostgresUserRepository) Create(ctx context.Context, user *core.User) error {
	query := `
		INSERT INTO users (username, password_hash, email, role)
		VALUES (?, ?, ?, ?)
	`
	result, err := r.db.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Email, user.Role)
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
	query := `
		SELECT id, username, password_hash, email, role, created_at, updated_at
		FROM users WHERE id = ?
	`
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
	query := `
		SELECT id, username, password_hash, email, role, created_at, updated_at
		FROM users WHERE username = ?
	`
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
	query := `
		SELECT id, username, password_hash, email, role, created_at, updated_at
		FROM users WHERE email = ?
	`
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
	query := `
		UPDATE users
		SET username = ?, password_hash = ?, email = ?, role = ?, updated_at = datetime('now')
		WHERE id = ?
	`
	_, err := r.db.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Email, user.Role, user.ID)
	return err
}

func (r *PostgresUserRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *PostgresUserRepository) List(ctx context.Context) ([]*core.User, error) {
	query := `
		SELECT id, username, password_hash, email, role, created_at, updated_at
		FROM users ORDER BY created_at DESC
	`
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
