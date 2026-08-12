package transport

type ApplyFirewallRequest struct {
	ServiceMutationBinding
	Enabled  bool  `json:"enabled"`
	TCPPorts []int `json:"tcp_ports"`
	UDPPorts []int `json:"udp_ports"`
	Persist  bool  `json:"persist"`
}

type FirewallStatusResponse struct {
	Enabled          bool   `json:"enabled"`
	EngineAvailable  bool   `json:"engine_available"`
	TCPPorts         []int  `json:"tcp_ports"`
	UDPPorts         []int  `json:"udp_ports"`
	SSHPorts         []int  `json:"ssh_ports"`
	PersistenceState string `json:"persistence_state"`
	PersistenceError string `json:"persistence_error,omitempty"`
	SnapshotVersion  int    `json:"snapshot_version,omitempty"`
	Error            string `json:"error,omitempty"`
}

type SiteUsageRequest struct {
	SiteHome string `json:"site_home"`
	Domain   string `json:"domain"`
}

type SiteUsageResponse struct {
	DiskBytes         int64  `json:"disk_bytes"`
	TrafficMonthBytes int64  `json:"traffic_month_bytes"`
	Error             string `json:"error,omitempty"`
}

type AgentVersionResponse struct {
	Version      string   `json:"version"`
	Commit       string   `json:"commit"`
	Capabilities []string `json:"capabilities,omitempty"`
}

const (
	AgentCapabilityFirewallApplyV2         = "firewall_apply_v2"
	AgentCapabilityPanelCertificateIssueV2 = "panel_certificate_issue_v2"
	AgentCapabilityDNSZoneSyncV2           = "dns_zone_sync_v2"
	AgentCapabilityDNSSECSecureV2          = "dnssec_secure_v2"
	AgentCapabilityDNSClusterConfigureV2   = "dns_cluster_configure_v2"
	AgentCapabilityMailTLSSyncV2           = "mail_tls_sync_v2"
)

type CheckInstalledServicesResponse struct {
	Nginx      bool `json:"nginx"`
	Apache     bool `json:"apache"`
	MySQL      bool `json:"mysql"`
	PostgreSQL bool `json:"postgresql"`
	PHP        bool `json:"php"`
}
