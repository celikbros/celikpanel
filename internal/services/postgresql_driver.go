package services

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgreSQLDriver implements DatabaseDriver for PostgreSQL
type PostgreSQLDriver struct {
	host         string
	port         int
	rootPassword string
}

// NewPostgreSQLDriver creates a new PostgreSQL driver
func NewPostgreSQLDriver(config DriverConfig) *PostgreSQLDriver {
	return &PostgreSQLDriver{
		host:         config.Host,
		port:         config.Port,
		rootPassword: config.RootPassword,
	}
}

// getDB establishes a connection to the PostgreSQL server
func (d *PostgreSQLDriver) getDB(dbname string) (*sql.DB, error) {
	dsn := fmt.Sprintf("postgres://postgres:%s@%s:%d/%s?sslmode=disable",
		d.rootPassword, d.host, d.port, dbname)
	return sql.Open("pgx", dsn)
}

// TestConnection tests PostgreSQL connection
func (d *PostgreSQLDriver) TestConnection() error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("failed to open connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("PostgreSQL connection failed: %v", err)
	}
	return nil
}

// CreateDatabase creates a PostgreSQL database
func (d *PostgreSQLDriver) CreateDatabase(name string) error {
	// Connect to 'postgres' db to create new db
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	// Sanitize name (basic check)
	if strings.Contains(name, "\"") || strings.Contains(name, ";") {
		return fmt.Errorf("invalid database name")
	}

	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE \"%s\" WITH ENCODING='UTF8';", name))
	if err != nil {
		return fmt.Errorf("failed to create database: %v", err)
	}
	return nil
}

// DeleteDatabase drops a PostgreSQL database
func (d *PostgreSQLDriver) DeleteDatabase(name string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	// Sanitize name
	if strings.Contains(name, "\"") || strings.Contains(name, ";") {
		return fmt.Errorf("invalid database name")
	}

	// Terminate connections first
	killQuery := fmt.Sprintf(`
		SELECT pg_terminate_backend(pid) 
		FROM pg_stat_activity 
		WHERE datname = '%s' AND pid <> pg_backend_pid()
	`, name)
	_, _ = db.Exec(killQuery)

	_, err = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", name))
	if err != nil {
		return fmt.Errorf("failed to drop database: %v", err)
	}
	return nil
}

// ListDatabases lists all PostgreSQL databases
func (d *PostgreSQLDriver) ListDatabases() ([]string, error) {
	db, err := d.getDB("postgres")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT datname FROM pg_database WHERE datistemplate = false;")
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %v", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		databases = append(databases, name)
	}
	return databases, nil
}

// CreateUser creates a PostgreSQL user
func (d *PostgreSQLDriver) CreateUser(username, password string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	// Sanitize
	if strings.Contains(username, "\"") || strings.Contains(username, ";") {
		return fmt.Errorf("invalid username")
	}

	// Check if user exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", username).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {
		return nil // Idempotent
	}

	_, err = db.Exec(fmt.Sprintf("CREATE USER \"%s\" WITH PASSWORD '%s';", username, password))
	if err != nil {
		return fmt.Errorf("failed to create user: %v", err)
	}
	return nil
}

// DeleteUser drops a PostgreSQL user
func (d *PostgreSQLDriver) DeleteUser(username string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	if strings.Contains(username, "\"") || strings.Contains(username, ";") {
		return fmt.Errorf("invalid username")
	}

	_, err = db.Exec(fmt.Sprintf("DROP USER IF EXISTS \"%s\";", username))
	if err != nil {
		return fmt.Errorf("failed to drop user: %v", err)
	}
	return nil
}

// ChangePassword changes a PostgreSQL user's password
func (d *PostgreSQLDriver) ChangePassword(username, newPassword string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	if strings.Contains(username, "\"") || strings.Contains(username, ";") {
		return fmt.Errorf("invalid username")
	}

	_, err = db.Exec(fmt.Sprintf("ALTER USER \"%s\" WITH PASSWORD '%s';", username, newPassword))
	if err != nil {
		return fmt.Errorf("failed to change password: %v", err)
	}
	return nil
}

// ListUsers lists all PostgreSQL users
func (d *PostgreSQLDriver) ListUsers() ([]string, error) {
	db, err := d.getDB("postgres")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT usename FROM pg_user;")
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		users = append(users, name)
	}
	return users, nil
}

// GrantPrivileges grants privileges to a user on a database
func (d *PostgreSQLDriver) GrantPrivileges(database, user, privileges string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	if strings.Contains(database, "\"") || strings.Contains(user, "\"") {
		return fmt.Errorf("invalid input")
	}

	// Grant connect on database
	_, err = db.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE \"%s\" TO \"%s\";", database, user))
	if err != nil {
		return fmt.Errorf("failed to grant connect: %v", err)
	}

	// For tables, we need to connect to the specific database
	dbSpecific, err := d.getDB(database)
	if err != nil {
		return err
	}
	defer dbSpecific.Close()

	if privileges == "ALL" {
		// Grant schema usage
		_, err = dbSpecific.Exec(fmt.Sprintf("GRANT ALL ON SCHEMA public TO \"%s\";", user))
		if err != nil {
			return fmt.Errorf("failed to grant schema usage: %v", err)
		}
		// Grant all tables
		_, err = dbSpecific.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO \"%s\";", user))
		if err != nil {
			return fmt.Errorf("failed to grant table privileges: %v", err)
		}
		// Default privileges for future tables
		_, err = dbSpecific.Exec(fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO \"%s\";", user))
		if err != nil {
			return fmt.Errorf("failed to alter default privileges: %v", err)
		}
	} else {
		// Simplified for now
		_, err = dbSpecific.Exec(fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA public TO \"%s\";", privileges, user))
		if err != nil {
			return fmt.Errorf("failed to grant privileges: %v", err)
		}
	}

	return nil
}

// RevokePrivileges revokes privileges from a user on a database
func (d *PostgreSQLDriver) RevokePrivileges(database, user string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return err
	}
	defer db.Close()

	if strings.Contains(database, "\"") || strings.Contains(user, "\"") {
		return fmt.Errorf("invalid input")
	}

	_, err = db.Exec(fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE \"%s\" FROM \"%s\";", database, user))
	if err != nil {
		return fmt.Errorf("failed to revoke privileges: %v", err)
	}
	
	// Also revoke on tables
	dbSpecific, err := d.getDB(database)
	if err != nil {
		// If DB doesn't exist, ignore
		return nil
	}
	defer dbSpecific.Close()
	
	_, _ = dbSpecific.Exec(fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM \"%s\";", user))

	return nil
}

// ListUserDatabases lists databases a user has access to
func (d *PostgreSQLDriver) ListUserDatabases(username string) ([]string, error) {
	return []string{}, nil
}
