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

type ConfigureDNSClusterV2Request struct {
	ServiceMutationBinding
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

type ConfigureDNSClusterV2Response = DNSClusterResponse

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
	DesiredGeneration int64        `json:"desired_generation"`
	Domain            string       `json:"domain"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
}

type SyncDNSZoneResponse struct {
	Synced            bool   `json:"synced"`
	AppliedGeneration int64  `json:"applied_generation"`
	Error             string `json:"error,omitempty"`
}

// V2 binds the complete effective full-zone snapshot and its generation into
// the surrounding durable service-mutation qualifier. Keeping the wire shape
// shared with V1 makes mixed binaries fail by RPC method name instead of
// silently dropping a security-critical field during gob decoding.
type SyncDNSZoneV2Request = SyncDNSZoneRequest
type SyncDNSZoneV2Response = SyncDNSZoneResponse

type DNSSECRequest struct {
	Zone string `json:"zone"`
}

type SecureDNSZoneV2Request struct {
	ServiceMutationBinding
	Zone string `json:"zone"`
}

type DNSSECStatusResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type SecureDNSZoneV2Response = DNSSECStatusResponse

type TLSARequest struct {
	CertPath string `json:"cert_path"`
}

type TLSAResponse struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}
