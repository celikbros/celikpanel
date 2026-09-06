package services

import (
	"fmt"
	"strings"
)

// DatabaseDriver interface for different database types
// This allows extensibility for PostgreSQL, MariaDB, MSSQL, MongoDB, etc.
type DatabaseDriver interface {
	// Connection
	TestConnection() error

	// ServerVersion asks the engine what it is. The panel used to take this
	// from the service scan, which reports "unknown" for an engine it can see
	// running - so the databases screen said "unknown" beside a MariaDB the
	// panel had installed and was connected to. Now that the panel has an
	// account on the engine (R-057), it can ask instead of guessing.
	//
	// ServerVersion, motora ne oldugunu sorar. Panel bunu servis taramasindan
	// aliyordu; o da calistigini gordugu bir motor icin "unknown" bildiriyordu.
	// Panelin artik motorda bir hesabi oldugu icin tahmin etmek yerine
	// sorabilir.
	ServerVersion() (string, error)

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
}

// DriverConfig holds configuration for database drivers
type DriverConfig struct {
	Host string
	Port int
	// Username is the account the panel connects as. R-057 gave the panel an
	// account of its own, so the account can no longer be assumed from the
	// engine type. Empty still means the engine's own superuser, which is what
	// every credential stored before R-057 is.
	//
	// Username, panelin baglandigi hesaptir. R-057 panele kendi hesabini
	// verdi; artik hesap motor tipinden varsayilamaz. Bos deger hala motorun
	// kendi ust yetkili hesabi demektir.
	Username string
	Password string
	Type     string // "postgresql", "mariadb"
}

// driverUsername resolves the account to connect as. A stored credential with
// no username was recorded before the panel could own an account, and every
// one of those is the engine's own superuser - so an empty username keeps
// meaning exactly what it always meant, decided here rather than at each
// driver.
//
// driverUsername, baglanilacak hesabi belirler. Kullanici adi olmayan kayitli
// bir kimlik bilgisi, panel kendi hesabina sahip olamadan once kaydedilmistir
// ve hepsi motorun kendi ust yetkili hesabidir.
func driverUsername(configured, engineSuperuser string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != `` {
		return trimmed
	}
	return engineSuperuser
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
