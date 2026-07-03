package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

// PostgresDatabaseV2Repository implements DatabaseV2Repository
type PostgresDatabaseV2Repository struct {
	db *sql.DB
}

// NewPostgresDatabaseV2Repository creates a new database v2 repository
func NewPostgresDatabaseV2Repository(db *sql.DB) *PostgresDatabaseV2Repository {
	return &PostgresDatabaseV2Repository{db: db}
}

// Create inserts a new database
func (r *PostgresDatabaseV2Repository) Create(ctx context.Context, db *core.DatabaseV2) error {
	query := `
		INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name)
		VALUES (?, ?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		db.ServerID,
		db.SubscriptionID,
		db.DomainID,
		db.Name,
	)

	if err != nil {
		return fmt.Errorf("failed to create database: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM databases_v2 WHERE id = ?", id).
		Scan(&db.ID, &db.CreatedAt, &db.UpdatedAt)
}

// GetByID retrieves a database by ID
func (r *PostgresDatabaseV2Repository) GetByID(ctx context.Context, id int) (*core.DatabaseV2, error) {
	query := `
		SELECT id, server_id, subscription_id, domain_id, name, created_at, updated_at
		FROM databases_v2
		WHERE id = ?
	`

	db := &core.DatabaseV2{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&db.ID,
		&db.ServerID,
		&db.SubscriptionID,
		&db.DomainID,
		&db.Name,
		&db.CreatedAt,
		&db.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("database not found")
		}
		return nil, fmt.Errorf("failed to get database: %v", err)
	}

	return db, nil
}

// ListByServer retrieves all databases for a server
func (r *PostgresDatabaseV2Repository) ListByServer(ctx context.Context, serverID int) ([]*core.DatabaseV2, error) {
	query := `
		SELECT id, server_id, subscription_id, domain_id, name, created_at, updated_at
		FROM databases_v2
		WHERE server_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %v", err)
	}
	defer rows.Close()

	var databases []*core.DatabaseV2
	for rows.Next() {
		db := &core.DatabaseV2{}
		err := rows.Scan(
			&db.ID,
			&db.ServerID,
			&db.SubscriptionID,
			&db.DomainID,
			&db.Name,
			&db.CreatedAt,
			&db.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan database: %v", err)
		}

		databases = append(databases, db)
	}

	return databases, nil
}

// Update updates a database
func (r *PostgresDatabaseV2Repository) Update(ctx context.Context, db *core.DatabaseV2) error {
	query := `
		UPDATE databases_v2
		SET name = ?, updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := r.db.ExecContext(ctx, query, db.Name, db.ID)
	if err != nil {
		return fmt.Errorf("failed to update database: %v", err)
	}

	return nil
}

// Delete deletes a database
func (r *PostgresDatabaseV2Repository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM databases_v2 WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete database: %v", err)
	}

	return nil
}
