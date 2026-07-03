package repositories

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

// DatabaseRepository handles database metadata persistence
type DatabaseRepository interface {
	Create(ctx context.Context, database *core.Database) error
	GetByID(ctx context.Context, id int) (*core.Database, error)
	ListBySubscription(ctx context.Context, subscriptionID int) ([]*core.Database, error)
	Delete(ctx context.Context, id int) error
	Update(ctx context.Context, database *core.Database) error
}

// PostgresDatabaseRepository implements DatabaseRepository using SQLite
type PostgresDatabaseRepository struct {
	db *sql.DB
}

// NewPostgresDatabaseRepository creates a new database repository
func NewPostgresDatabaseRepository(db *sql.DB) *PostgresDatabaseRepository {
	return &PostgresDatabaseRepository{db: db}
}

// Create inserts a new database record
func (r *PostgresDatabaseRepository) Create(ctx context.Context, database *core.Database) error {
	query := `
		INSERT INTO databases (subscription_id, name, db_type, db_user, db_password, host, port)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		database.SubscriptionID,
		database.Name,
		database.DBType,
		database.DBUser,
		database.DBPassword,
		database.Host,
		database.Port,
	)

	if err != nil {
		return fmt.Errorf("failed to create database: %v", err)
	}
	
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	
	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM databases WHERE id = ?", id).
		Scan(&database.ID, &database.CreatedAt, &database.UpdatedAt)
}

// GetByID retrieves a database by ID
func (r *PostgresDatabaseRepository) GetByID(ctx context.Context, id int) (*core.Database, error) {
	query := `
		SELECT id, subscription_id, name, db_type, db_user, db_password, host, port, created_at, updated_at
		FROM databases
		WHERE id = ?
	`

	database := &core.Database{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&database.ID,
		&database.SubscriptionID,
		&database.Name,
		&database.DBType,
		&database.DBUser,
		&database.DBPassword,
		&database.Host,
		&database.Port,
		&database.CreatedAt,
		&database.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("database not found")
		}
		return nil, fmt.Errorf("failed to get database: %v", err)
	}

	return database, nil
}

// ListBySubscription retrieves all databases for a subscription
func (r *PostgresDatabaseRepository) ListBySubscription(ctx context.Context, subscriptionID int) ([]*core.Database, error) {
	query := `
		SELECT id, subscription_id, name, db_type, db_user, db_password, host, port, created_at, updated_at
		FROM databases
		WHERE subscription_id = ?
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %v", err)
	}
	defer rows.Close()

	var databases []*core.Database
	for rows.Next() {
		database := &core.Database{}
		err := rows.Scan(
			&database.ID,
			&database.SubscriptionID,
			&database.Name,
			&database.DBType,
			&database.DBUser,
			&database.DBPassword,
			&database.Host,
			&database.Port,
			&database.CreatedAt,
			&database.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan database: %v", err)
		}
		databases = append(databases, database)
	}

	return databases, nil
}

// Delete removes a database record
func (r *PostgresDatabaseRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM databases WHERE id = ?`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete database: %v", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("database not found")
	}

	return nil
}

// Update modifies a database record
func (r *PostgresDatabaseRepository) Update(ctx context.Context, database *core.Database) error {
	query := `
		UPDATE databases
		SET db_password = ?, updated_at = datetime('now')
		WHERE id = ?
	`

	result, err := r.db.ExecContext(ctx, query, database.DBPassword, database.ID)
	if err != nil {
		return fmt.Errorf("failed to update database: %v", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("database not found")
	}

	return nil
}
