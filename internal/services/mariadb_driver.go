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
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", name)
	return d.executeSQL(sql)
}

// DeleteDatabase drops a MariaDB database
func (d *MariaDBDriver) DeleteDatabase(name string) error {
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", name)
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
	sql := fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'localhost' IDENTIFIED BY '%s';", username, password)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// DeleteUser drops a MariaDB user
func (d *MariaDBDriver) DeleteUser(username string) error {
	sql := fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", username)
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// ChangePassword changes a MariaDB user's password
func (d *MariaDBDriver) ChangePassword(username, newPassword string) error {
	sql := fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';", username, newPassword)
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
	var sql string
	if privileges == "ALL" {
		sql = fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", database, user)
	} else {
		sql = fmt.Sprintf("GRANT %s ON `%s`.* TO '%s'@'localhost';", privileges, database, user)
	}
	if err := d.executeSQL(sql); err != nil {
		return err
	}
	return d.executeSQL("FLUSH PRIVILEGES;")
}

// RevokePrivileges revokes privileges from a user on a database
func (d *MariaDBDriver) RevokePrivileges(database, user string) error {
	sql := fmt.Sprintf("REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost';", database, user)
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
