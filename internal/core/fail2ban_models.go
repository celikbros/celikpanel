package core

// Fail2banJail represents a jail configuration/status
type Fail2banJail struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"` // Currently running
	Banned  int    `json:"banned"` // Number of currently banned IPs
}

// Fail2banBannedIP represents a banned IP address
type Fail2banBannedIP struct {
	IP      string `json:"ip"`
	Jail    string `json:"jail"`
	Time    string `json:"time"` // Time of ban
	Country string `json:"country"`
}

// Fail2banConfig represents global fail2ban settings
type Fail2banConfig struct {
	BanTime  string   `json:"ban_time"`
	FindTime string   `json:"find_time"`
	MaxRetry int      `json:"max_retry"`
	IgnoreIP []string `json:"ignore_ip"` // Whitelist
}

// Fail2banJailRequest represents a request to enable/disable a jail
type Fail2banJailRequest struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Fail2banUnbanRequest represents a request to unban an IP
type Fail2banUnbanRequest struct {
	IP   string `json:"ip"`
	Jail string `json:"jail"`
}

// Fail2banConfigRequest represents a request to update global config
type Fail2banConfigRequest struct {
	Config Fail2banConfig `json:"config"`
}

// Fail2banStatusResult is the real, agent-sourced fail2ban state. Installed
// is false when fail2ban-client is absent.
// Fail2banStatusResult, agent'tan gelen gerçek fail2ban durumudur.
// fail2ban-client yoksa Installed false olur.
type Fail2banStatusResult struct {
	Installed bool               `json:"installed"`
	Jails     []Fail2banJail     `json:"jails"`
	Banned    []Fail2banBannedIP `json:"banned"`
}
