package transport

import "github.com/alicelik/celikpanel/internal/core"

// AgentRPC defines the interface between Panel and Agent
type AgentRPC interface {
	ListServices() ([]core.Service, error)
	GetConfig(path string) (string, error)
	UpdateConfig(path string, content string) error
	ReloadConfig(serviceName string) error
	StartService(serviceName string) error
	StopService(serviceName string) error
	RestartService(serviceName string) error

	// Site Management
	CreateSite(req CreateSiteRequest) (*CreateSiteResponse, error)
	DeleteSite(siteID int, domain string) error
}

// Service configuration requests/responses

type GetPHPConfigRequest struct {
	PHPVersion string `json:"php_version"` // e.g., "8.3"
}

type GetPHPConfigResponse struct {
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxInputVars      int    `json:"max_input_vars"`
}

type UpdatePHPConfigRequest struct {
	PHPVersion        string `json:"php_version"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxInputVars      int    `json:"max_input_vars"`
}

type GetMySQLConfigResponse struct {
	MaxConnections   int    `json:"max_connections"`
	InnodbBufferPool string `json:"innodb_buffer_pool_size"`
	QueryCacheSize   string `json:"query_cache_size"`
	MaxAllowedPacket string `json:"max_allowed_packet"`
}

type UpdateMySQLConfigRequest struct {
	MaxConnections   int    `json:"max_connections"`
	InnodbBufferPool string `json:"innodb_buffer_pool_size"`
	QueryCacheSize   string `json:"query_cache_size"`
	MaxAllowedPacket string `json:"max_allowed_packet"`
}

// Database management RPC types

type CreateDatabaseRequest struct {
	DatabaseType string `json:"database_type"` // "postgresql" or "mariadb"
	DatabaseName string `json:"database_name"`
	Username     string `json:"username"`
	Password     string `json:"password"`
}

type CreateDatabaseResponse struct {
	Success bool   `json:"success"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Error   string `json:"error,omitempty"`
}

type DeleteDatabaseRequest struct {
	DatabaseType string `json:"database_type"` // "postgresql" or "mariadb"
	DatabaseName string `json:"database_name"`
	Username     string `json:"username"`
}

type ChangeDatabasePasswordRequest struct {
	DatabaseType string `json:"database_type"` // "postgresql" or "mariadb"
	Username     string `json:"username"`
	NewPassword  string `json:"new_password"`
}

// CreateSiteRequest contains all data needed to create a site
type CreateSiteRequest struct {
	SiteID         int
	SubscriptionID int
	DomainID       int
	Domain         string
	TempDomain     string
	DocumentRoot   string
	// ProjectType decides what the agent actually builds: "php" (FPM pool +
	// index.php) or "static" (no PHP at all). Empty means "php" for backward
	// compatibility. "dnsonly" never reaches the agent — there is nothing to
	// build for it.
	// ProjectType, agent'ın gerçekte ne kuracağına karar verir: "php" (FPM
	// havuzu + index.php) ya da "static" (hiç PHP yok). Boş, geriye uyumluluk
	// için "php" demektir. "dnsonly" agent'a hiç ulaşmaz — kurulacak şey yok.
	ProjectType string
	PHPVersion  string
	SSLType     string
	Username    string
	Password    string
}

// CreateSiteResponse contains results of site creation
type CreateSiteResponse struct {
	Success      bool
	NginxConfig  string
	PHPSocket    string
	ErrorMessage string
}

// Data structures for RPC calls

type Empty struct{}

type GetConfigArgs struct {
	Path string
}

type ConfigResponse struct {
	Content string
	Parsed  string // JSON string
}

type UpdateConfigArgs struct {
	Path    string
	Content string
}

type ServiceArgs struct {
	ServiceName string
}
