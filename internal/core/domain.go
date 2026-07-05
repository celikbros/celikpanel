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
	ID          string        `json:"ID"`
	Name        string        `json:"Name"`
	Type        ServiceType   `json:"Type"`
	Version     string        `json:"Version"`
	Status      string        `json:"Status"`
	ConfigFiles []ConfigFile  `json:"ConfigFiles,omitempty"`
	IsPrimary   bool          `json:"IsPrimary"`
}

// ConfigFile represents a configuration file
type ConfigFile struct {
	Path      string `json:"path"`
	IsManaged bool   `json:"is_managed"`
}

// --- Multi-Tenant Entities ---

// User represents a panel user
type User struct {
	ID           int
	Username     string
	PasswordHash string
	Email        string
	Role         string // admin, reseller, customer
	ParentID     *int   // who created/owns this user (reseller→customer edge)
	Status       string // active, suspended
	CreatedAt    time.Time
	UpdatedAt    time.Time
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
	WebServer    string  // nginx, apache, litespeed
	NginxConfig  *string
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
	ID                  int
	Name                string // "postgresql", "mariadb", "mssql"
	DisplayName         string // "PostgreSQL", "MariaDB"
	DefaultPort         int
	Icon                string // "🐘", "🐬"
	SupportsUsers       bool
	SupportsDatabases   bool
	CreatedAt           time.Time
}

// DatabaseServer represents a database server instance
type DatabaseServer struct {
	ID                     int
	SubscriptionID         int
	TypeID                 int
	TypeName               string // "PostgreSQL", "MariaDB"
	TypeIcon               string // "🐘", "🐬"
	Name                   string // "PostgreSQL 14 Production"
	Version                string // "14.5"
	Host                   string
	Port                   int
	IsDefault              bool
	RootPasswordEncrypted  string
	ConnectionParams       map[string]interface{} // JSONB
	Status                 string                 // active, inactive, error
	CreatedAt              time.Time
	UpdatedAt              time.Time
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
	DomainID       *int   // Optional: Related domain/site (nullable)
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
