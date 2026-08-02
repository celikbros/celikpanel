package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

// PostgresDatabaseGrantRepository implements DatabaseGrantRepository
type PostgresDatabaseGrantRepository struct {
	db sqlExecutor
}

// NewPostgresDatabaseGrantRepository creates a new database grant repository
func NewPostgresDatabaseGrantRepository(db sqlExecutor) *PostgresDatabaseGrantRepository {
	return &PostgresDatabaseGrantRepository{db: db}
}

// Grant creates a new grant (user access to database)
func (r *PostgresDatabaseGrantRepository) Grant(ctx context.Context, grant *core.DatabaseGrant) error {
	query := `
		INSERT INTO database_user_grants (database_id, user_id, privileges)
		VALUES (?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		grant.DatabaseID,
		grant.UserID,
		grant.Privileges,
	)

	if err != nil {
		return fmt.Errorf("failed to create grant: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	return r.db.QueryRowContext(ctx, "SELECT id, created_at FROM database_user_grants WHERE id = ?", id).
		Scan(&grant.ID, scanTime(&grant.CreatedAt))
}

// Revoke removes a grant
func (r *PostgresDatabaseGrantRepository) Revoke(ctx context.Context, databaseID, userID int) error {
	query := `DELETE FROM database_user_grants WHERE database_id = ? AND user_id = ?`

	_, err := r.db.ExecContext(ctx, query, databaseID, userID)
	if err != nil {
		return fmt.Errorf("failed to revoke grant: %v", err)
	}

	return nil
}

// ListByDatabase retrieves all grants for a database
func (r *PostgresDatabaseGrantRepository) ListByDatabase(ctx context.Context, databaseID int) ([]*core.DatabaseGrant, error) {
	query := `
		SELECT id, database_id, user_id, privileges, created_at
		FROM database_user_grants
		WHERE database_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list grants by database: %v", err)
	}
	defer rows.Close()

	var grants []*core.DatabaseGrant
	for rows.Next() {
		grant := &core.DatabaseGrant{}
		err := rows.Scan(
			&grant.ID,
			&grant.DatabaseID,
			&grant.UserID,
			&grant.Privileges,
			scanTime(&grant.CreatedAt),
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan grant: %v", err)
		}

		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list grants by database: %v", err)
	}

	return grants, nil
}

// ListByUser retrieves all grants for a user
func (r *PostgresDatabaseGrantRepository) ListByUser(ctx context.Context, userID int) ([]*core.DatabaseGrant, error) {
	query := `
		SELECT id, database_id, user_id, privileges, created_at
		FROM database_user_grants
		WHERE user_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list grants by user: %v", err)
	}
	defer rows.Close()

	var grants []*core.DatabaseGrant
	for rows.Next() {
		grant := &core.DatabaseGrant{}
		err := rows.Scan(
			&grant.ID,
			&grant.DatabaseID,
			&grant.UserID,
			&grant.Privileges,
			scanTime(&grant.CreatedAt),
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan grant: %v", err)
		}

		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to list grants by user: %v", err)
	}

	return grants, nil
}

// GetByID retrieves a grant by ID
func (r *PostgresDatabaseGrantRepository) GetByID(ctx context.Context, id int) (*core.DatabaseGrant, error) {
	query := `
		SELECT id, database_id, user_id, privileges, created_at
		FROM database_user_grants
		WHERE id = ?
	`

	grant := &core.DatabaseGrant{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&grant.ID,
		&grant.DatabaseID,
		&grant.UserID,
		&grant.Privileges,
		scanTime(&grant.CreatedAt),
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("grant not found")
		}
		return nil, fmt.Errorf("failed to get grant: %v", err)
	}

	return grant, nil
}

// Delete deletes a grant
func (r *PostgresDatabaseGrantRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM database_user_grants WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete grant: %v", err)
	}

	return nil
}
