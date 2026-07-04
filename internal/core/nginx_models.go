package core

// NginxGlobalConfig represents global nginx settings
type NginxGlobalConfig struct {
	WorkerProcesses     string `json:"worker_processes"`
	WorkerConnections   string `json:"worker_connections"`
	KeepaliveTimeout    string `json:"keepalive_timeout"`
	ClientMaxBodySize   string `json:"client_max_body_size"`
	ServerTokens        string `json:"server_tokens"` // on/off
	Gzip                string `json:"gzip"`          // on/off
}

// NginxSSLConfig represents default SSL settings
type NginxSSLConfig struct {
	SSLCiphers          string `json:"ssl_ciphers"`
	SSLProtocols        string `json:"ssl_protocols"`
	SSLPreferServerCiphers string `json:"ssl_prefer_server_ciphers"`
}

// NginxRateLimit represents a rate limiting zone
type NginxRateLimit struct {
	Name       string `json:"name"`
	Zone       string `json:"zone"` // e.g. $binary_remote_addr
	Size       string `json:"size"` // e.g. 10m
	Rate       string `json:"rate"` // e.g. 10r/s
}

// NginxGlobalConfigRequest represents a request to update global config
type NginxGlobalConfigRequest struct {
	Config NginxGlobalConfig `json:"config"`
}

// NginxSSLConfigRequest represents a request to update SSL config
type NginxSSLConfigRequest struct {
	Config NginxSSLConfig `json:"config"`
}

// NginxInspectResult is the real, agent-sourced nginx configuration read
// from `nginx -T`. Installed is false when nginx is absent.
// NginxInspectResult, agent'ın `nginx -T` ile okuduğu gerçek nginx
// yapılandırmasıdır. nginx yoksa Installed false olur.
type NginxInspectResult struct {
	Installed  bool             `json:"installed"`
	Global     NginxGlobalConfig `json:"global"`
	SSL        NginxSSLConfig    `json:"ssl"`
	RateLimits []NginxRateLimit  `json:"rate_limits"`
}
