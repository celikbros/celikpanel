package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
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
	
	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM users WHERE id = ?", id).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (r *PostgresUserRepository) GetByID(ctx context.Context, id int) (*core.User, error) {
	user := &core.User{}
	query := `
		SELECT id, username, password_hash, email, role, created_at, updated_at
		FROM users WHERE id = ?
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
		&user.CreatedAt, &user.UpdatedAt,
	)
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
	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
		&user.CreatedAt, &user.UpdatedAt,
	)
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
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
		&user.CreatedAt, &user.UpdatedAt,
	)
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
		err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.Role,
			&user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}
