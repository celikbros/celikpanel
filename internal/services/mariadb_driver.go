package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// MariaDBDriver implements DatabaseDriver for MariaDB
type MariaDBDriver struct {
	host         string
	port         int
	rootPassword string
}

const mariaDBCommandTimeout = 30 * time.Second

func quoteMySQLOptionValue(value string) (string, error) {
	if strings.IndexByte(value, 0) >= 0 {
		return ``, fmt.Errorf(`MySQL option value contains NUL`)
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, string(rune(13)), `\r`)
	value = strings.ReplaceAll(value, string(rune(10)), `\n`)
	value = strings.ReplaceAll(value, string(rune(9)), `\t`)
	value = strings.ReplaceAll(value, string(rune(34)), string([]rune{92, 34}))
	return string(rune(34)) + value + string(rune(34)), nil
}

func (d *MariaDBDriver) writeMySQLClientFile() (string, func(), error) {
	file, err := os.CreateTemp(``, `celikpanel-mysql-*.cnf`)
	if err != nil {
		return ``, nil, fmt.Errorf(`create protected MySQL client file: %w`, err)
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	fail := func(cause error) (string, func(), error) {
		_ = file.Close()
		cleanup()
		return ``, nil, cause
	}
	if err := file.Chmod(0o600); err != nil {
		return fail(fmt.Errorf(`protect MySQL client file: %w`, err))
	}
	if _, err := fmt.Fprintln(file, `[client]`); err != nil {
		return fail(fmt.Errorf(`write MySQL client file: %w`, err))
	}
	writeOption := func(key, value string) error {
		quoted, err := quoteMySQLOptionValue(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(file, key+`=`+quoted)
		return err
	}
	if err := writeOption(`user`, `root`); err != nil {
		return fail(fmt.Errorf(`write MySQL client user: %w`, err))
	}
	if d.rootPassword != `` {
		if err := writeOption(`password`, d.rootPassword); err != nil {
			return fail(fmt.Errorf(`write MySQL client password: %w`, err))
		}
	}
	if d.host != `` {
		if err := writeOption(`host`, d.host); err != nil {
			return fail(fmt.Errorf(`write MySQL client host: %w`, err))
		}
	}
	if d.port > 0 {
		if _, err := fmt.Fprintln(file, `port=`+fmt.Sprint(d.port)); err != nil {
			return fail(fmt.Errorf(`write MySQL client port: %w`, err))
		}
	}
	if d.host != `` || d.port > 0 {
		if _, err := fmt.Fprintln(file, `protocol=tcp`); err != nil {
			return fail(fmt.Errorf(`write MySQL client protocol: %w`, err))
		}
	}
	if err := file.Close(); err != nil {
		cleanup()
		return ``, nil, fmt.Errorf(`close MySQL client file: %w`, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		cleanup()
		return ``, nil, fmt.Errorf(`inspect MySQL client file: %w`, err)
	}
	if runtime.GOOS != `windows` && info.Mode().Perm() != 0o600 {
		cleanup()
		return ``, nil, fmt.Errorf(`MySQL client file mode is %o, want 600`, info.Mode().Perm())
	}
	return path, cleanup, nil
}

// NewMariaDBDriver creates a new MariaDB driver
func NewMariaDBDriver(config DriverConfig) *MariaDBDriver {
	return &MariaDBDriver{
		host:         config.Host,
		port:         config.Port,
		rootPassword: config.RootPassword,
	}
}

func (d *MariaDBDriver) mysqlCommand(ctx context.Context, sql string) (*exec.Cmd, func(), error) {
	path, cleanup, err := d.writeMySQLClientFile()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(
		ctx,
		`mysql`,
		`--defaults-extra-file=`+path,
		`--batch`,
		`--raw`,
	)
	cmd.Stdin = strings.NewReader(sql)
	return cmd, cleanup, nil
}

func (d *MariaDBDriver) runSQL(sql string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mariaDBCommandTimeout)
	defer cancel()
	cmd, cleanup, err := d.mysqlCommand(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf(`MariaDB command timed out: %w`, ctx.Err())
		}
		// R-053. The client wrote why it failed and this call used to throw it
		// away, so every layer above saw "exit status 1" and could tell an
		// engine that refused a password from an engine that was not running
		// only by guessing. The output is read here, where it exists, and what
		// it means is kept - not the text, which can echo a statement.
		// R-053. Istemci neden basarisiz oldugunu yazdi ve bu cagri onu atardi.
		// Cikti burada okunur ve ne anlama geldigi saklanir; metin degil.
		return nil, WrapDatabaseEngineFailure(
			fmt.Errorf(`MariaDB command failed: %w`, err), output,
		)
	}
	return output, nil
}

// TestConnection tests MariaDB connection
func (d *MariaDBDriver) TestConnection() error {
	_, err := d.runSQL(`SELECT 1;`)
	if err != nil {
		return fmt.Errorf(`MariaDB connection failed: %w`, err)
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
	output, err := d.runSQL(`SHOW DATABASES;`)
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
	output, err := d.runSQL(`SELECT User FROM mysql.user WHERE Host='localhost';`)
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

// executeSQL executes SQL command via mysql CLI
func (d *MariaDBDriver) executeSQL(sql string) error {
	_, err := d.runSQL(sql)
	return err
}
