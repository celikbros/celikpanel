package services

import (
	"fmt"
	"os/exec"
	"strings"
)

// MariaDBDriver implements DatabaseDriver for MariaDB
type MariaDBDriver struct {
	host         string
	port         int
	rootPassword string
}

// NewMariaDBDriver creates a new MariaDB driver
func NewMariaDBDriver(config DriverConfig) *MariaDBDriver {
	return &MariaDBDriver{
		host:         config.Host,
		port:         config.Port,
		rootPassword: config.RootPassword,
	}
}

// TestConnection tests MariaDB connection
func (d *MariaDBDriver) TestConnection() error {
	cmd := exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", d.rootPassword), "-e", "SELECT 1;")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("MariaDB connection failed: %v", err)
	}
	return nil
}

// CreateDatabase creates a MariaDB database
func (d *MariaDBDriver) CreateDatabase(name string) error {
	ident, err := QuoteMySQLIdentifier(name)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", ident)
	return d.executeSQL(sql)
}

// DeleteDatabase drops a MariaDB database
func (d *MariaDBDriver) DeleteDatabase(name string) error {
	ident, err := QuoteMySQLIdentifier(name)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS %s;", ident)
	return d.executeSQL(sql)
}

// ListDatabases lists all MariaDB databases
func (d *MariaDBDriver) ListDatabases() ([]string, error) {
	cmd := exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", d.rootPassword), "-e", "SHOW DATABASES;")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %v", err)
	}

	var databases []string
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		line = strings.TrimSpace(line)
		if line != "" && line != "information_schema" && line != "performance_schema" && line != "mysql" {
			databases = append(databases, line)
		}
	}
	return databases, nil
}

// CreateUser creates a MariaDB user (localhost only)
func (d *MariaDBDriver) CreateUser(username, password string) error {
	// The user name is a MySQL account name literal here, not a backtick
	// identifier, so it is validated as an identifier and then quoted as a
	// string literal.
	// Kullanıcı adı burada ters tırnaklı tanımlayıcı değil, MySQL hesap adı
	// literalidir; bu yüzden tanımlayıcı olarak doğrulanıp string literali
	// olarak tırnaklanır.
	if err := ValidateSQLIdentifier(username); err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	userLiteral, err := QuoteMySQLStringLiteral(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	pwLiteral, err := QuoteMySQLStringLiteral(password)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}
	sql := fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s;", userLiteral, pwLiteral)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// DeleteUser drops a MariaDB user
func (d *MariaDBDriver) DeleteUser(username string) error {
	if err := ValidateSQLIdentifier(username); err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	userLiteral, err := QuoteMySQLStringLiteral(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	sql := fmt.Sprintf("DROP USER IF EXISTS %s@'localhost';", userLiteral)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// ChangePassword changes a MariaDB user's password
func (d *MariaDBDriver) ChangePassword(username, newPassword string) error {
	if err := ValidateSQLIdentifier(username); err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	userLiteral, err := QuoteMySQLStringLiteral(username)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	pwLiteral, err := QuoteMySQLStringLiteral(newPassword)
	if err != nil {
		return fmt.Errorf("invalid password: %w", err)
	}
	sql := fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s;", userLiteral, pwLiteral)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// ListUsers lists all MariaDB users
func (d *MariaDBDriver) ListUsers() ([]string, error) {
	cmd := exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", d.rootPassword), "-e", "SELECT User FROM mysql.user WHERE Host='localhost';")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}

	var users []string
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // Skip header
		}
		line = strings.TrimSpace(line)
		if line != "" && line != "root" && line != "mysql.sys" {
			users = append(users, line)
		}
	}
	return users, nil
}

// GrantPrivileges grants privileges to a user on a database
func (d *MariaDBDriver) GrantPrivileges(database, user, privileges string) error {
	dbIdent, err := QuoteMySQLIdentifier(database)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	if err := ValidateSQLIdentifier(user); err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	userLiteral, err := QuoteMySQLStringLiteral(user)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}

	privClause := "ALL PRIVILEGES"
	if privileges != "ALL" {
		privClause, err = ValidatePrivileges(privileges)
		if err != nil {
			return fmt.Errorf("invalid privileges: %w", err)
		}
	}
	sql := fmt.Sprintf("GRANT %s ON %s.* TO %s@'localhost';", privClause, dbIdent, userLiteral)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// RevokePrivileges revokes privileges from a user on a database
func (d *MariaDBDriver) RevokePrivileges(database, user string) error {
	dbIdent, err := QuoteMySQLIdentifier(database)
	if err != nil {
		return fmt.Errorf("invalid database name: %w", err)
	}
	if err := ValidateSQLIdentifier(user); err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	userLiteral, err := QuoteMySQLStringLiteral(user)
	if err != nil {
		return fmt.Errorf("invalid username: %w", err)
	}
	sql := fmt.Sprintf("REVOKE ALL PRIVILEGES ON %s.* FROM %s@'localhost';", dbIdent, userLiteral)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// ListUserDatabases lists databases a user has access to
func (d *MariaDBDriver) ListUserDatabases(username string) ([]string, error) {
	// This would require parsing SHOW GRANTS - simplified for now
	return []string{}, nil
}

// executeSQL executes SQL command via mysql CLI
func (d *MariaDBDriver) executeSQL(sql string) error {
	cmd := exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", d.rootPassword), "-e", sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SQL error: %v, output: %s", err, string(output))
	}
	return nil
}
