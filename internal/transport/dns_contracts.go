package transport

type DNSEngine string

const (
	DNSEnginePowerDNS DNSEngine = "pdns"
	DNSEngineBIND     DNSEngine = "bind"

	DNSTopologyStandalone = "standalone"

	DNSEngineSwitchPhasePlanned     = "planned"
	DNSEngineSwitchPhaseStaging     = "staging"
	DNSEngineSwitchPhaseStaged      = "staged"
	DNSEngineSwitchPhaseActivating  = "activating"
	DNSEngineSwitchPhaseVerifying   = "verifying"
	DNSEngineSwitchPhaseCommitted   = "committed"
	DNSEngineSwitchPhaseRollingBack = "rolling_back"
	DNSEngineSwitchPhaseRolledBack  = "rolled_back"
	DNSEngineSwitchPhaseFailed      = "failed"
)

func ValidDNSEngine(value DNSEngine) bool {
	return value == DNSEnginePowerDNS || value == DNSEngineBIND
}

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

// SyncDNSZoneV3 binds an exact publication engine and its monotonic activation
// epoch in addition to every V2 full-zone field. V1/V2 intentionally remain
// PowerDNS-only and retain their original wire shape and qualifier.
type SyncDNSZoneV3Request struct {
	ServiceMutationBinding
	Engine            DNSEngine    `json:"engine"`
	EngineEpoch       int64        `json:"engine_epoch"`
	DesiredGeneration int64        `json:"desired_generation"`
	Domain            string       `json:"domain"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
}

type SyncDNSZoneV3Response struct {
	Synced            bool      `json:"synced"`
	Engine            DNSEngine `json:"engine"`
	EngineEpoch       int64     `json:"engine_epoch"`
	AppliedGeneration int64     `json:"applied_generation"`
	Error             string    `json:"error,omitempty"`
}

// DNSEngineSwitchZoneSnapshot is one canonical full-zone member of a durable
// engine-switch manifest. ZoneQualifier is the target-engine V3 commitment.
type DNSEngineSwitchZoneSnapshot struct {
	Ordinal           int          `json:"ordinal"`
	Domain            string       `json:"domain"`
	DesiredGeneration int64        `json:"desired_generation"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
	ZoneQualifier     string       `json:"zone_qualifier"`
}

// SwitchDNSEngineV1Request carries the exact snapshot authorized by the panel.
// SourceEngine is empty only while resolving a legacy/uninitialized host.
type SwitchDNSEngineV1Request struct {
	ServiceMutationBinding
	SourceEngine      DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine      DNSEngine                     `json:"target_engine"`
	SourceEpoch       int64                         `json:"source_epoch"`
	TargetEpoch       int64                         `json:"target_epoch"`
	SourceRevision    int64                         `json:"source_revision"`
	Topology          string                        `json:"topology"`
	Zones             []DNSEngineSwitchZoneSnapshot `json:"zones"`
	ManifestQualifier string                        `json:"manifest_qualifier"`
}

type SwitchDNSEngineV1Response struct {
	Applied      bool      `json:"applied"`
	ActiveEngine DNSEngine `json:"active_engine,omitempty"`
	ActiveEpoch  int64     `json:"active_epoch,omitempty"`
	AppliedZones int       `json:"applied_zones,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// DNSBackendReadinessResponse reports only bounded runtime facts. Detailed
// probe failures stay in agent logs and are not exposed as raw API errors.
type DNSBackendRuntimeState struct {
	Engine    DNSEngine `json:"engine"`
	Installed bool      `json:"installed"`
	Running   bool      `json:"running"`
	Managed   bool      `json:"managed"`
	Unit      string    `json:"unit"`
}

type DNSBackendReadinessResponse struct {
	Engines []DNSBackendRuntimeState `json:"engines"`
	Error   string                   `json:"error,omitempty"`
}

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
