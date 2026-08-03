package services

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgreSQLDriver implements DatabaseDriver for PostgreSQL
type PostgreSQLDriver struct {
	host         string
	port         int
	rootPassword string
	openDB       func(string) (*sql.DB, error)
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
	if d.openDB != nil {
		return d.openDB(dbname)
	}
	dsn := postgreSQLDSN(d.host, d.port, dbname, d.rootPassword)
	return sql.Open("pgx", dsn)
}

func postgreSQLDSN(host string, port int, dbname, password string) string {
	query := url.Values{}
	query.Set("sslmode", "disable")
	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("postgres", password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + dbname,
		RawQuery: query.Encode(),
	}).String()
}

// TestConnection tests PostgreSQL connection
func (d *PostgreSQLDriver) TestConnection() error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("PostgreSQL connection failed: %w", err)
	}
	return nil
}

// CreateDatabase creates a PostgreSQL database
func (d *PostgreSQLDriver) CreateDatabase(name string) error {
	// Connect to 'postgres' db to create new db
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("open PostgreSQL control database for create: %w", err)
	}
	defer db.Close()

	ident, err := QuotePGIdentifier(name)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s WITH ENCODING='UTF8';", ident))
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	return nil
}

// DeleteDatabase drops a PostgreSQL database
func (d *PostgreSQLDriver) DeleteDatabase(name string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("open PostgreSQL control database for delete: %w", err)
	}
	defer db.Close()

	ident, err := QuotePGIdentifier(name)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}

	// Terminate connections first. Refuse to report a successful delete if the
	// server could not terminate every matching backend.
	// The name here is a value comparison, so a bound parameter is the right tool.
	// Önce bağlantıları sonlandır. Buradaki ad bir değer karşılaştırmasıdır,
	// bu yüzden bağlı parametre doğru araçtır.
	var terminated bool
	if err := db.QueryRow(`
		SELECT COALESCE(bool_and(pg_terminate_backend(pid)), TRUE)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, name).Scan(&terminated); err != nil {
		return fmt.Errorf("failed to terminate database connections: %w", err)
	}
	if !terminated {
		return fmt.Errorf("failed to terminate every connection to database %q", name)
	}

	_, err = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", ident))
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}
	return nil
}

// ListDatabases lists all PostgreSQL databases
func (d *PostgreSQLDriver) ListDatabases() ([]string, error) {
	db, err := d.getDB("postgres")
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL control database for listing databases: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT datname FROM pg_database WHERE datistemplate = false;")
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read listed database: %w", err)
		}
		databases = append(databases, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while listing databases: %w", err)
	}
	return databases, nil
}

// CreateUser creates a PostgreSQL user
func (d *PostgreSQLDriver) CreateUser(username, password string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("open PostgreSQL control database for creating user: %w", err)
	}
	defer db.Close()

	ident, err := QuotePGIdentifier(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	pwLiteral, err := QuotePGStringLiteral(password)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}

	// Check if user exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", username).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check PostgreSQL user existence: %w", err)
	}

	if exists {
		return nil // Idempotent
	}

	_, err = db.Exec(fmt.Sprintf("CREATE USER %s WITH PASSWORD %s;", ident, pwLiteral))
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// DeleteUser drops a PostgreSQL user
func (d *PostgreSQLDriver) DeleteUser(username string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("open PostgreSQL control database for deleting user: %w", err)
	}
	defer db.Close()

	ident, err := QuotePGIdentifier(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("DROP USER IF EXISTS %s;", ident))
	if err != nil {
		return fmt.Errorf("failed to drop user: %w", err)
	}
	return nil
}

// ChangePassword changes a PostgreSQL user's password
func (d *PostgreSQLDriver) ChangePassword(username, newPassword string) error {
	db, err := d.getDB("postgres")
	if err != nil {
		return fmt.Errorf("open PostgreSQL control database for changing password: %w", err)
	}
	defer db.Close()

	ident, err := QuotePGIdentifier(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	pwLiteral, err := QuotePGStringLiteral(newPassword)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}

	_, err = db.Exec(fmt.Sprintf("ALTER USER %s WITH PASSWORD %s;", ident, pwLiteral))
	if err != nil {
		return fmt.Errorf("failed to change password: %w", err)
	}
	return nil
}

// ListUsers lists all PostgreSQL users
func (d *PostgreSQLDriver) ListUsers() ([]string, error) {
	db, err := d.getDB("postgres")
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL control database for listing users: %w", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT usename FROM pg_user;")
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read listed PostgreSQL user: %w", err)
		}
		users = append(users, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while listing users: %w", err)
	}
	return users, nil
}

// GrantPrivileges grants privileges to a user on a database
func (d *PostgreSQLDriver) GrantPrivileges(database, user, privileges string) error {
	dbIdent, err := QuotePGIdentifier(database)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	userIdent, err := QuotePGIdentifier(user)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}

	dbSpecific, err := d.getDB(database)
	if err != nil {
		return fmt.Errorf("open target database for grant: %w", err)
	}
	defer dbSpecific.Close()
	tx, err := dbSpecific.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin privilege grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Keep every physical grant in one transaction. A later schema/table/default
	// privilege failure must not leave CONNECT or a subset of rights behind.
	if _, err = tx.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s;", dbIdent, userIdent)); err != nil {
		return fmt.Errorf("failed to grant connect: %w", err)
	}

	if privileges == "ALL" {
		// Grant schema usage
		_, err = tx.Exec(fmt.Sprintf("GRANT ALL ON SCHEMA public TO %s;", userIdent))
		if err != nil {
			return fmt.Errorf("failed to grant schema usage: %w", err)
		}
		// Grant all tables
		_, err = tx.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO %s;", userIdent))
		if err != nil {
			return fmt.Errorf("failed to grant table privileges: %w", err)
		}
		// Default privileges for future tables
		_, err = tx.Exec(fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO %s;", userIdent))
		if err != nil {
			return fmt.Errorf("failed to alter default privileges: %w", err)
		}
	} else {
		privList, err := ValidatePrivileges(privileges)
		if err != nil {
			return fmt.Errorf("invalid privileges: %w", err)
		}
		_, err = tx.Exec(fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA public TO %s;", privList, userIdent))
		if err != nil {
			return fmt.Errorf("failed to grant privileges: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit privilege grant: %w", err)
	}
	return nil
}

// RevokePrivileges revokes privileges from a user on a database
func (d *PostgreSQLDriver) RevokePrivileges(database, user string) error {
	dbIdent, err := QuotePGIdentifier(database)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	userIdent, err := QuotePGIdentifier(user)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}

	dbSpecific, err := d.getDB(database)
	if err != nil {
		return fmt.Errorf("open target database for revoke: %w", err)
	}
	defer dbSpecific.Close()
	tx, err := dbSpecific.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin privilege revoke: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	statements := []struct {
		label string
		sql   string
	}{
		{"database privileges", fmt.Sprintf("REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s;", dbIdent, userIdent)},
		{"schema privileges", fmt.Sprintf("REVOKE ALL PRIVILEGES ON SCHEMA public FROM %s;", userIdent)},
		{"table privileges", fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %s;", userIdent)},
		{"sequence privileges", fmt.Sprintf("REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %s;", userIdent)},
		{"default table privileges", fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM %s;", userIdent)},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.sql); err != nil {
			return fmt.Errorf("failed to revoke %s: %w", statement.label, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit privilege revoke: %w", err)
	}
	return nil
}
