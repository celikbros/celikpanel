package core

// PHPPool represents a PHP-FPM pool configuration
type PHPPool struct {
	Name string `json:"name"`
	Port int    `json:"port"`
	User string `json:"user"`
	PM   string `json:"pm"` // dynamic, ondemand, static
}

// PHPExtension represents a PHP extension status
type PHPExtension struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// PHPConfig represents php.ini settings
type PHPConfig struct {
	MemoryLimit       string `json:"memory_limit"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxExecutionTime  string `json:"max_execution_time"`
	DisplayErrors     string `json:"display_errors"`
}

// ExtendedPHPConfig represents comprehensive PHP configuration
type ExtendedPHPConfig struct {
	// Performance & Security
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  string `json:"max_execution_time"`
	MaxInputTime      string `json:"max_input_time"`
	PostMaxSize       string `json:"post_max_size"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	OpcacheEnable     string `json:"opcache_enable"`
	DisableFunctions  string `json:"disable_functions"`
	
	// Common Settings
	IncludePath       string `json:"include_path"`
	SessionSavePath   string `json:"session_save_path"`
	RealpathCacheSize string `json:"realpath_cache_size"`
	OpenBasedir       string `json:"open_basedir"`
	ErrorReporting    string `json:"error_reporting"`
	DisplayErrors     string `json:"display_errors"`
	LogErrors         string `json:"log_errors"`
	AllowUrlFopen     string `json:"allow_url_fopen"`
	FileUploads       string `json:"file_uploads"`
	ShortOpenTag      string `json:"short_open_tag"`
	
	// Additional Directives
	AdditionalDirectives string `json:"additional_directives"`
}

// PHPPoolConfig represents detailed PHP-FPM pool configuration
type PHPPoolConfig struct {
	Name              string `json:"name"`
	User              string `json:"user"`
	Group             string `json:"group"`
	Listen            string `json:"listen"` // socket path or port
	ListenOwner       string `json:"listen_owner"`
	ListenGroup       string `json:"listen_group"`
	ListenMode        string `json:"listen_mode"`
	
	// Process Manager
	PM                string `json:"pm"` // dynamic, ondemand, static
	PMMaxChildren     int    `json:"pm_max_children"`
	PMStartServers    int    `json:"pm_start_servers"`
	PMMinSpareServers int    `json:"pm_min_spare_servers"`
	PMMaxSpareServers int    `json:"pm_max_spare_servers"`
	PMMaxRequests     int    `json:"pm_max_requests"`
}

// PHPVersionRequest is a generic request struct containing the version
type PHPVersionRequest struct {
	Version string `json:"version"`
}

// PHPPoolRequest represents a request to create/update a pool
type PHPPoolRequest struct {
	Version string  `json:"version"`
	Pool    PHPPool `json:"pool"`
}

// PHPPoolConfigRequest represents a request to create/update detailed pool config
type PHPPoolConfigRequest struct {
	Version    string        `json:"version"`
	PoolConfig PHPPoolConfig `json:"pool_config"`
}

// PHPExtensionRequest represents a request to toggle an extension
type PHPExtensionRequest struct {
	Version   string `json:"version"`
	Extension string `json:"extension"`
	Enabled   bool   `json:"enabled"`
}

// PHPConfigRequest represents a request to update php.ini
type PHPConfigRequest struct {
	Version string    `json:"version"`
	Config  PHPConfig `json:"config"`
}

// ExtendedPHPConfigRequest represents a request to update extended PHP config
type ExtendedPHPConfigRequest struct {
	Version string            `json:"version"`
	Config  ExtendedPHPConfig `json:"config"`
}

// DeletePHPPoolRequest represents a request to delete a pool
type DeletePHPPoolRequest struct {
	Version  string `json:"version"`
	PoolName string `json:"pool_name"`
}
