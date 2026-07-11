package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/alicelik/celikpanel/internal/core"
)

// PostgresDatabaseServerRepository implements DatabaseServerRepository
type PostgresDatabaseServerRepository struct {
	db *sql.DB
}

// NewPostgresDatabaseServerRepository creates a new database server repository
func NewPostgresDatabaseServerRepository(db *sql.DB) *PostgresDatabaseServerRepository {
	return &PostgresDatabaseServerRepository{db: db}
}

// Create inserts a new database server
func (r *PostgresDatabaseServerRepository) Create(ctx context.Context, server *core.DatabaseServer) error {
	var paramsJSON []byte
	var err error
	if server.ConnectionParams != nil {
		paramsJSON, err = json.Marshal(server.ConnectionParams)
		if err != nil {
			return fmt.Errorf("failed to marshal connection params: %v", err)
		}
	}

	query := `
		INSERT INTO database_servers (subscription_id, type_id, name, version, host, port, is_default, root_password_encrypted, connection_params, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.ExecContext(ctx, query,
		server.SubscriptionID,
		server.TypeID,
		server.Name,
		server.Version,
		server.Host,
		server.Port,
		server.IsDefault,
		server.RootPasswordEncrypted,
		paramsJSON,
		server.Status,
	)

	if err != nil {
		return fmt.Errorf("failed to create database server: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	return r.db.QueryRowContext(ctx, "SELECT id, created_at, updated_at FROM database_servers WHERE id = ?", id).
		Scan(&server.ID, scanTime(&server.CreatedAt), scanTime(&server.UpdatedAt))
}

// GetByID retrieves a database server by ID
func (r *PostgresDatabaseServerRepository) GetByID(ctx context.Context, id int) (*core.DatabaseServer, error) {
	// TypeName here carries the CANONICAL engine name (dst.name:
	// "postgresql"/"mariadb") — it is what the driver layer consumes.
	// List* methods fill the human display_name instead; do not mix them.
	// TypeName burada KANONİK motor adını taşır (dst.name) — sürücü
	// katmanının tükettiği değer budur. List* metotları insan-yüzlü
	// display_name doldurur; ikisini karıştırmayın.
	query := `
		SELECT ds.id, ds.subscription_id, ds.type_id, dst.name, ds.name, ds.version, ds.host, ds.port, ds.is_default, ds.root_password_encrypted, ds.connection_params, ds.status, ds.created_at, ds.updated_at
		FROM database_servers ds
		JOIN database_server_types dst ON dst.id = ds.type_id
		WHERE ds.id = ?
	`

	server := &core.DatabaseServer{}
	var paramsJSON []byte
	var rootPassword sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&server.ID,
		&server.SubscriptionID,
		&server.TypeID,
		&server.TypeName,
		&server.Name,
		&server.Version,
		&server.Host,
		&server.Port,
		&server.IsDefault,
		&rootPassword,
		&paramsJSON,
		&server.Status,
		scanTime(&server.CreatedAt),
		scanTime(&server.UpdatedAt),
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("database server not found")
		}
		return nil, fmt.Errorf("failed to get database server: %v", err)
	}

	if rootPassword.Valid {
		server.RootPasswordEncrypted = rootPassword.String
	}

	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &server.ConnectionParams); err != nil {
			return nil, fmt.Errorf("failed to unmarshal connection params: %v", err)
		}
	}

	return server, nil
}

// ListBySubscription retrieves all database servers for a subscription
func (r *PostgresDatabaseServerRepository) ListBySubscription(ctx context.Context, subscriptionID int) ([]*core.DatabaseServer, error) {
	query := `
		SELECT ds.id, ds.subscription_id, ds.type_id, dst.display_name, dst.icon, ds.name, ds.version, ds.host, ds.port, ds.is_default, ds.root_password_encrypted, ds.connection_params, ds.status, ds.created_at, ds.updated_at
		FROM database_servers ds
		JOIN database_server_types dst ON ds.type_id = dst.id
		WHERE ds.subscription_id = ?
		ORDER BY ds.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list database servers: %v", err)
	}
	defer rows.Close()

	var servers []*core.DatabaseServer
	for rows.Next() {
		server := &core.DatabaseServer{}
		var paramsJSON []byte
		var rootPassword sql.NullString

		err := rows.Scan(
			&server.ID,
			&server.SubscriptionID,
			&server.TypeID,
			&server.TypeName,
			&server.TypeIcon,
			&server.Name,
			&server.Version,
			&server.Host,
			&server.Port,
			&server.IsDefault,
			&rootPassword,
			&paramsJSON,
			&server.Status,
			scanTime(&server.CreatedAt),
			scanTime(&server.UpdatedAt),
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan database server: %v", err)
		}

		if rootPassword.Valid {
			server.RootPasswordEncrypted = rootPassword.String
		}

		if len(paramsJSON) > 0 {
			if err := json.Unmarshal(paramsJSON, &server.ConnectionParams); err != nil {
				return nil, fmt.Errorf("failed to unmarshal connection params: %v", err)
			}
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// ListByType retrieves database servers by type for a subscription
func (r *PostgresDatabaseServerRepository) ListByType(ctx context.Context, subscriptionID int, serverType string) ([]*core.DatabaseServer, error) {
	query := `
		SELECT ds.id, ds.subscription_id, ds.type_id, ds.name, ds.version, ds.host, ds.port, ds.is_default, ds.root_password_encrypted, ds.connection_params, ds.status, ds.created_at, ds.updated_at
		FROM database_servers ds
		JOIN database_server_types dst ON ds.type_id = dst.id
		WHERE ds.subscription_id = ? AND dst.name = ?
		ORDER BY ds.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, subscriptionID, serverType)
	if err != nil {
		return nil, fmt.Errorf("failed to list database servers by type: %v", err)
	}
	defer rows.Close()

	var servers []*core.DatabaseServer
	for rows.Next() {
		server := &core.DatabaseServer{}
		var paramsJSON []byte
		var rootPassword sql.NullString

		err := rows.Scan(
			&server.ID,
			&server.SubscriptionID,
			&server.TypeID,
			&server.Name,
			&server.Version,
			&server.Host,
			&server.Port,
			&server.IsDefault,
			&rootPassword,
			&paramsJSON,
			&server.Status,
			scanTime(&server.CreatedAt),
			scanTime(&server.UpdatedAt),
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan database server: %v", err)
		}
		
		if rootPassword.Valid {
			server.RootPasswordEncrypted = rootPassword.String
		}

		if len(paramsJSON) > 0 {
			if err := json.Unmarshal(paramsJSON, &server.ConnectionParams); err != nil {
				return nil, fmt.Errorf("failed to unmarshal connection params: %v", err)
			}
		}

		servers = append(servers, server)
	}

	return servers, nil
}

// Update updates a database server
func (r *PostgresDatabaseServerRepository) Update(ctx context.Context, server *core.DatabaseServer) error {
	var paramsJSON []byte
	var err error
	if server.ConnectionParams != nil {
		paramsJSON, err = json.Marshal(server.ConnectionParams)
		if err != nil {
			return fmt.Errorf("failed to marshal connection params: %v", err)
		}
	}

	query := `
		UPDATE database_servers
		SET type_id = ?, name = ?, version = ?, host = ?, port = ?, is_default = ?, root_password_encrypted = ?, connection_params = ?, status = ?, updated_at = datetime('now')
		WHERE id = ?
	`

	_, err = r.db.ExecContext(ctx, query,
		server.TypeID,
		server.Name,
		server.Version,
		server.Host,
		server.Port,
		server.IsDefault,
		server.RootPasswordEncrypted,
		paramsJSON,
		server.Status,
		server.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update database server: %v", err)
	}

	return nil
}

// Delete deletes a database server
func (r *PostgresDatabaseServerRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM database_servers WHERE id = ?`

	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete database server: %v", err)
	}

	return nil
}
