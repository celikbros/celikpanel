package transport

import "time"

// The concrete RPC request and response structs in this package are the
// protocol contract. Both panel and agent import these exact types so a field
// rename or removal fails at compile time instead of being silently ignored by
// net/rpc's gob decoder. Do not replace this with a hand-written method
// interface: the RPC surface is registered by name and such an interface can
// drift without protecting the wire format.

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
	Type         string `json:"type"` // "mysql" or "postgresql"
	Name         string `json:"name"`
	User         string `json:"user"`
	Password     string `json:"password"`
	OperationID  string `json:"operation_id,omitempty"`
	CleanupToken string `json:"cleanup_token,omitempty"`
}

type CreateDatabaseResponse struct {
	Success           bool   `json:"success"`
	OwnedByOperation  bool   `json:"owned_by_operation,omitempty"`
	CleanupIncomplete bool   `json:"cleanup_incomplete,omitempty"`
	Error             string `json:"error,omitempty"`
}

type DeleteDatabaseRequest struct {
	Type                  string `json:"type"` // "mysql" or "postgresql"
	Name                  string `json:"name"`
	User                  string `json:"user"`
	RequireUserCleanup    bool   `json:"require_user_cleanup,omitempty"`
	RequireOwnershipProof bool   `json:"require_ownership_proof,omitempty"`
	OperationID           string `json:"operation_id,omitempty"`
	CleanupToken          string `json:"cleanup_token,omitempty"`
}

type DeleteDatabaseResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type ChangeDatabasePasswordRequest struct {
	DatabaseType string `json:"database_type"` // "postgresql" or "mariadb"
	Username     string `json:"username"`
	NewPassword  string `json:"new_password"`
}

// CreateSiteRequest contains all data needed to create a site
type CreateSiteRequest struct {
	ExpectedBuildCommit string
	SiteID              int
	SubscriptionID      int
	DomainID            int
	Domain              string
	TempDomain          string
	DocumentRoot        string
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

// ConfigErrorCode is the machine-readable failure contract for the privileged
// configuration editor. Expected operator errors travel in the RPC response
// instead of net/rpc's string-only error channel, so the panel never has to
// infer an HTTP status from human-readable text.
type ConfigErrorCode string

const (
	ConfigErrorPathRefused    ConfigErrorCode = "path_refused"
	ConfigErrorValidationFail ConfigErrorCode = "validation_failed"
)

type ConfigRPCError struct {
	Code    ConfigErrorCode `json:"code"`
	Message string          `json:"message"`
}

type ConfigResponse struct {
	Content string          `json:"Content"`
	Parsed  string          `json:"Parsed"` // JSON string
	Error   *ConfigRPCError `json:"Error,omitempty"`
}

type UpdateConfigArgs struct {
	Path    string
	Content string
}

type UpdateConfigResponse struct {
	Success bool            `json:"success"`
	Error   *ConfigRPCError `json:"error,omitempty"`
}

type ServiceArgs struct {
	ServiceName string
}

type ServiceActionArgs struct {
	ServiceName string
	Action      string
}

type ServiceActionResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// ApplyVhostRequest is the complete, explicit nginx vhost input shared by the
// panel and the privileged agent.
type ApplyVhostRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	SiteID              int      `json:"site_id"`
	SubscriptionID      int      `json:"subscription_id"`
	DomainID            int      `json:"domain_id"`
	Domain              string   `json:"domain"`
	TempDomain          string   `json:"temp_domain"`
	ServerNames         []string `json:"server_names"`
	ACMEChallengeNames  []string `json:"acme_challenge_names,omitempty"`
	DocumentRoot        string   `json:"document_root"`
	PHPSocket           string   `json:"php_socket"`
	SSLType             string   `json:"ssl_type"`
	SSLCert             string   `json:"ssl_cert"`
	SSLKey              string   `json:"ssl_key"`
	RedirectWWW         bool     `json:"redirect_www"`
	ForceHTTPS          bool     `json:"force_https"`
	HSTSEnabled         bool     `json:"hsts_enabled"`
	HSTSMaxAge          int      `json:"hsts_max_age"`
	ProjectType         string   `json:"project_type"`
	AppPort             int      `json:"app_port"`
	ForwardTo           string   `json:"forward_to"`
	ForwardCode         int      `json:"forward_code"`
}

type ApplyVhostResponse struct {
	Config string `json:"config"`
	Error  string `json:"error,omitempty"`
}

type ApplyVhostsRequest struct {
	ExpectedBuildCommit string              `json:"expected_build_commit"`
	Vhosts              []ApplyVhostRequest `json:"vhosts"`
}

type ApplyVhostsResponse struct {
	Applied int    `json:"applied"`
	Error   string `json:"error,omitempty"`
}

// IssuePanelCertificateRequest and response are shared because the build
// commit gate is security-critical: silently omitting ExpectedBuildCommit
// would make every production request fail before certbot runs.
type IssuePanelCertificateRequest struct {
	MutationRequestID   string `json:"mutation_request_id,omitempty"`
	MutationOwnerID     string `json:"mutation_owner_id,omitempty"`
	Domain              string `json:"domain"`
	Email               string `json:"email"`
	TLSDir              string `json:"tls_dir"`
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}

const IssuePanelCertificateErrorActivationPending = "panel_certificate_activation_pending"

type IssuePanelCertificateResponse struct {
	Issued    bool      `json:"issued"`
	ExpiresAt time.Time `json:"expires_at"`
	Detail    string    `json:"detail,omitempty"`
	ErrorCode string    `json:"error_code,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// V2 binds the complete effective issuance payload into the surrounding
// service-mutation qualifier. The wire fields intentionally remain identical
// to V1 so mixed binaries fail by RPC method name rather than partial decoding.
type IssuePanelCertificateV2Request = IssuePanelCertificateRequest
type IssuePanelCertificateV2Response = IssuePanelCertificateResponse

type RestartPanelSoonRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}
