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
	// SSHDiscoveryReason names why SSHPorts is empty, so a host that has no
	// SSH service at all is never confused with a probe that could not run.
	// Empty means an SSH listener was proven and SSHPorts holds it.
	// SSHDiscoveryReason, SSHPorts'un neden boş olduğunu adlandırır; böylece
	// hiç SSH servisi olmayan bir sunucu, çalışamayan bir yoklamayla asla
	// karıştırılmaz. Boş değer, bir SSH dinleyicisinin kanıtlandığı ve
	// SSHPorts'un onu taşıdığı anlamına gelir.
	SSHDiscoveryReason string `json:"ssh_discovery_reason,omitempty"`
	Error              string `json:"error,omitempty"`
}

// The exact SSHDiscoveryReason vocabulary. Only SSHDiscoveryNoService is a
// state an operator may knowingly accept: a host with no SSH service has no
// door for the firewall to lock. The other two are refusals.
// Tam SSHDiscoveryReason sözlüğü. Operatörün bilerek kabul edebileceği tek
// durum SSHDiscoveryNoService'tir: SSH servisi olmayan bir sunucuda güvenlik
// duvarının kilitleyeceği bir kapı yoktur. Diğer ikisi reddir.
const (
	SSHDiscoveryNoService    = "no_ssh_service"
	SSHDiscoveryNotListening = "ssh_not_listening"
	SSHDiscoveryProbeFailed  = "discovery_failed"
)

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
	AgentCapabilityDNSZoneSyncV3           = "dns_zone_sync_v3"
	AgentCapabilityDNSZoneRecoverV1        = "dns_zone_recover_v1"
	AgentCapabilityDNSEngineSwitchV1       = "dns_engine_switch_v1"
	AgentCapabilityMailTLSSyncV2           = "mail_tls_sync_v2"
)

type CheckInstalledServicesResponse struct {
	Nginx      bool `json:"nginx"`
	Apache     bool `json:"apache"`
	MySQL      bool `json:"mysql"`
	PostgreSQL bool `json:"postgresql"`
	PHP        bool `json:"php"`
}
