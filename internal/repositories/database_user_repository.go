package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

// PostgresDatabaseUserRepository implements DatabaseUserRepository
type PostgresDatabaseUserRepository struct {
	db *sql.DB
}

// NewPostgresDatabaseUserRepository creates a new database user repository
func NewPostgresDatabaseUserRepository(db *sql.DB) *PostgresDatabaseUserRepository {
	return &PostgresDatabaseUserRepository{db: db}
}

// Create inserts a new database user
func (r *PostgresDatabaseUserRepository) Create(ctx context.Context, user *core.DatabaseUser) error {
	query := `
		INSERT INTO database_users (server_id, subscription_id, username, password)
		VALUES (?, ?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		user.ServerID,
		user.SubscriptionID,
		user.Username,
		user.Password,
	)

	if err != nil {
		return fmt.Errorf("failed to create database user: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM database_users WHERE id = ?", id).
		Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

// GetByID retrieves a database user by ID
func (r *PostgresDatabaseUserRepository) GetByID(ctx context.Context, id int) (*core.DatabaseUser, error) {
	query := `
		SELECT id, server_id, subscription_id, username, password, created_at, updated_at
		FROM database_users
		WHERE id = ?
	`

	user := &core.DatabaseUser{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.ServerID,
		&user.SubscriptionID,
		&user.Username,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("database user not found")
		}
		return nil, fmt.Errorf("failed to get database user: %v", err)
	}

	return user, nil
}

// GetByUsername retrieves a database user by username and server
func (r *PostgresDatabaseUserRepository) GetByUsername(ctx context.Context, serverID int, username string) (*core.DatabaseUser, error) {
	query := `
		SELECT id, server_id, subscription_id, username, password, created_at, updated_at
		FROM database_users
		WHERE server_id = ? AND username = ?
	`

	user := &core.DatabaseUser{}
	err := r.db.QueryRowContext(ctx, query, serverID, username).Scan(
		&user.ID,
		&user.ServerID,
		&user.SubscriptionID,
		&user.Username,
		&user.Password,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("database user not found")
		}
		return nil, fmt.Errorf("failed to get database user by username: %v", err)
	}

	return user, nil
}

// ListByServer retrieves all database users for a server
func (r *PostgresDatabaseUserRepository) ListByServer(ctx context.Context, serverID int) ([]*core.DatabaseUser, error) {
	query := `
		SELECT id, server_id, subscription_id, username, password, created_at, updated_at
		FROM database_users
		WHERE server_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to list database users: %v", err)
	}
	defer rows.Close()

	var users []*core.DatabaseUser
	for rows.Next() {
		user := &core.DatabaseUser{}
		err := rows.Scan(
			&user.ID,
			&user.ServerID,
			&user.SubscriptionID,
			&user.Username,
			&user.Password,
			&user.CreatedAt,
			&user.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan database user: %v", err)
		}

		users = append(users, user)
	}

	return users, nil
}

// Update updates a database user
func (r *PostgresDatabaseUserRepository) Update(ctx context.Context, user *core.DatabaseUser) error {
	query := `
		UPDATE database_users
		SET username = ?, password = ?, updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.ExecContext(ctx, query,
		user.Username,
		user.Password,
		user.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update database user: %v", err)
	}

	return nil
}

// Delete deletes a database user
func (r *PostgresDatabaseUserRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM database_users WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete database user: %v", err)
	}

	return nil
}
