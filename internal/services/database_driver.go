package services

import (
	"fmt"
)

// DatabaseDriver interface for different database types
// This allows extensibility for PostgreSQL, MariaDB, MSSQL, MongoDB, etc.
type DatabaseDriver interface {
	// Connection
	TestConnection() error

	// Database operations
	CreateDatabase(name string) error
	DeleteDatabase(name string) error
	ListDatabases() ([]string, error)

	// User operations
	CreateUser(username, password string) error
	DeleteUser(username string) error
	ChangePassword(username, newPassword string) error
	ListUsers() ([]string, error)

	// Privilege operations
	GrantPrivileges(database, user, privileges string) error
	RevokePrivileges(database, user string) error
	ListUserDatabases(username string) ([]string, error)
}

// DriverConfig holds configuration for database drivers
type DriverConfig struct {
	Host         string
	Port         int
	RootPassword string
	Type         string // "postgresql", "mariadb"
}

// NewDatabaseDriver creates a new database driver based on type
func NewDatabaseDriver(config DriverConfig) (DatabaseDriver, error) {
	switch config.Type {
	case "postgresql":
		return NewPostgreSQLDriver(config), nil
	case "mariadb":
		return NewMariaDBDriver(config), nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}
