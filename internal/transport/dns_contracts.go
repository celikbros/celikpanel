package transport

// DNSClusterRequest configures whether one PowerDNS node serves zones alone
// or exchanges zones with one peer.
type DNSClusterRequest struct {
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

type DNSClusterResponse struct {
	Applied bool   `json:"applied"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

type DNSClusterReadinessResponse struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

type ZoneRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

type SyncDNSZoneRequest struct {
	ServiceMutationBinding
	Domain   string       `json:"domain"`
	Delete   bool         `json:"delete"`
	ZoneType string       `json:"zone_type,omitempty"`
	Records  []ZoneRecord `json:"records"`
}

type SyncDNSZoneResponse struct {
	Synced bool   `json:"synced"`
	Error  string `json:"error,omitempty"`
}

type DNSSECRequest struct {
	Zone string `json:"zone"`
}

type DNSSECStatusResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type TLSARequest struct {
	CertPath string `json:"cert_path"`
}

type TLSAResponse struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}
