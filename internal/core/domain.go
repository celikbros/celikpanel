package core

import (
	"time"
)

// ServiceType represents the type of system service
type ServiceType string

const (
	ServiceNginx    ServiceType = "nginx"
	ServiceApache   ServiceType = "apache"
	ServicePHP      ServiceType = "php-fpm"
	ServicePostgres ServiceType = "postgresql"
	ServiceMySQL    ServiceType = "mysql"
	ServiceSystemd  ServiceType = "systemd"
)

// Service represents a system service (e.g. nginx, php-fpm)
type Service struct {
	ID          string       `json:"ID"`
	Name        string       `json:"Name"`
	Type        ServiceType  `json:"Type"`
	Version     string       `json:"Version"`
	Status      string       `json:"Status"`
	ConfigFiles []ConfigFile `json:"ConfigFiles,omitempty"`
	IsPrimary   bool         `json:"IsPrimary"`
}

// ConfigFile represents a configuration file
type ConfigFile struct {
	Path      string `json:"path"`
	IsManaged bool   `json:"is_managed"`
}

// --- Multi-Tenant Entities ---

// AccountType distinguishes a real account from a restricted additional user.
// The stored Role remains one of admin, reseller or customer for schema
// compatibility; callers must use EffectiveRole for authorization decisions.
type AccountType string

const (
	AccountTypeAccount        AccountType = "account"
	AccountTypeAdditionalUser AccountType = "additional_user"

	EffectiveRoleAdditionalUser string = "additional_user"
)

// EffectiveIdentity is the fail-closed authorization identity derived from a
// persisted user. CustomerID is populated only for customer-scoped identities.
type EffectiveIdentity struct {
	UserID     int
	Role       string
	CustomerID int
}

// User represents a panel user
type User struct {
	ID           int
	Username     string
	PasswordHash string
	Email        string
	Role         string // admin, reseller, customer
	AccountType  AccountType
	ParentID     *int   // who created/owns this user (reseller→customer edge)
	Status       string // active, suspended
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsAdditionalUser reports the immutable account marker. It deliberately does
// not imply that the record is valid or authorized; use EffectiveIdentity for
// authorization.
func (u User) IsAdditionalUser() bool {
	return u.AccountType == AccountTypeAdditionalUser
}

// EffectiveRole returns the role callers may use for authorization. Empty
// account_type is accepted only for pre-migration account rows. Unknown or
// internally inconsistent records fail closed by returning an empty role.
func (u User) EffectiveRole() string {
	accountType := u.AccountType
	if len(accountType) == 0 {
		accountType = AccountTypeAccount
	}

	switch accountType {
	case AccountTypeAccount:
		switch u.Role {
		case "admin", "reseller", "customer":
			return u.Role
		default:
			return ""
		}
	case AccountTypeAdditionalUser:
		if u.Role != "customer" || u.ParentID == nil || *u.ParentID <= 0 || (u.ID > 0 && *u.ParentID == u.ID) {
			return ""
		}
		return EffectiveRoleAdditionalUser
	default:
		return ""
	}
}

// EffectiveIdentity derives the authorization subject and customer scope from
// a persisted user. Additional users act within their owning customer's scope
// but retain their own UserID for grants, auditing and session revocation.
func (u User) EffectiveIdentity() (EffectiveIdentity, bool) {
	if u.ID <= 0 {
		return EffectiveIdentity{}, false
	}

	role := u.EffectiveRole()
	if len(role) == 0 {
		return EffectiveIdentity{}, false
	}

	identity := EffectiveIdentity{UserID: u.ID, Role: role}
	switch role {
	case "customer":
		identity.CustomerID = u.ID
	case EffectiveRoleAdditionalUser:
		if u.ParentID == nil || *u.ParentID <= 0 || *u.ParentID == u.ID {
			return EffectiveIdentity{}, false
		}
		identity.CustomerID = *u.ParentID
	}

	return identity, true
}

// ServicePlan is a reusable quota template subscriptions can be created from.
// ServicePlan, aboneliklerin türetilebileceği yeniden kullanılabilir bir kota
// şablonudur.
type ServicePlan struct {
	ID               int
	OwnerID          *int // nil = global (admin) plan
	Name             string
	MaxDomains       int
	MaxDatabases     int
	MaxEmailAccounts int
	DiskQuotaMB      int
	BandwidthQuotaMB int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Subscription represents a resource package
type Subscription struct {
	ID               int
	OwnerID          int
	Name             string
	MaxDomains       int
	MaxDatabases     int
	MaxEmailAccounts int
	DiskQuotaMB      int
	BandwidthQuotaMB int
	Status           string // active, suspended, expired
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Domain represents a domain name
type Domain struct {
	ID              int
	SubscriptionID  int
	Name            string
	ParentDomainID  *int
	DNSZoneID       *int
	Status          string // active, suspended, pending
	IPAddressID     int
	IsTemporary     bool
	TemporarySuffix *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Site represents a web hosting site
type Site struct {
	ID           int
	DomainID     int
	DocumentRoot string
	WebServer    string // nginx, apache, litespeed
	NginxConfig  *string
	// ProjectType mirrors sites.project_type: php, static, node, proxy,
	// forwarding, dnsonly. Empty is treated as "php" (pre-3A rows).
	// ProjectType, sites.project_type karşılığıdır: php, static, node, proxy,
	// forwarding, dnsonly. Boş "php" sayılır (3A öncesi satırlar).
	ProjectType  string
	PHPVersion   string
	PHPFPMSocket *string
	SSLEnabled   bool
	SSLCertPath  *string
	SSLKeyPath   *string
	Status       string // active, suspended
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Database represents a MariaDB database
type Database struct {
	ID             int
	SubscriptionID int
	Name           string
	DBType         string // "postgresql" or "mariadb"
	DBUser         string
	DBPassword     string
	Host           string
	Port           int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Database Management v2 Models

// DatabaseServerType represents a type of database server (PostgreSQL, MariaDB, etc.)
type DatabaseServerType struct {
	ID                int
	Name              string // "postgresql", "mariadb", "mssql"
	DisplayName       string // "PostgreSQL", "MariaDB"
	DefaultPort       int
	Icon              string // "🐘", "🐬"
	SupportsUsers     bool
	SupportsDatabases bool
	CreatedAt         time.Time
}

// DatabaseServer represents a database server instance
type DatabaseServer struct {
	ID                    int
	SubscriptionID        int
	TypeID                int
	TypeName              string // "PostgreSQL", "MariaDB"
	TypeIcon              string // "🐘", "🐬"
	Name                  string // "PostgreSQL 14 Production"
	Version               string // "14.5"
	Host                  string
	Port                  int
	IsDefault             bool
	RootPasswordEncrypted string
	ConnectionParams      map[string]interface{} // JSONB
	Status                string                 // active, inactive, error
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// DatabaseUser represents a database user (per server)
type DatabaseUser struct {
	ID             int
	ServerID       int
	SubscriptionID int
	Username       string
	Password       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DatabaseV2 represents a database (new schema without user/password)
type DatabaseV2 struct {
	ID             int
	ServerID       int
	SubscriptionID int
	DomainID       *int // Optional: Related domain/site (nullable)
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DatabaseGrant represents user access to a database with privileges
type DatabaseGrant struct {
	ID         int
	DatabaseID int
	UserID     int
	Privileges string // "ALL", "SELECT,INSERT,UPDATE"
	CreatedAt  time.Time
}

// IPAddress represents a server IP address
type IPAddress struct {
	ID        int
	Address   string
	Type      string // ipv4, ipv6
	IsShared  bool
	IsPrimary bool
	Status    string
	CreatedAt time.Time
}
