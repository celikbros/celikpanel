package transport

type DKIMStatusRequest struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
}

type DKIMStatusResponse struct {
	HasKey           bool   `json:"has_key"`
	PublicKeyB64     string `json:"public_key_b64"`
	SigningInstalled bool   `json:"signing_installed"`
	Error            string `json:"error,omitempty"`
}

type DKIMEnsureRequest struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
}

type DKIMEnsureResponse struct {
	Created      bool   `json:"created"`
	PublicKeyB64 string `json:"public_key_b64"`
	Error        string `json:"error,omitempty"`
}

type MailPolicy struct {
	MessageSizeMB     int      `json:"message_size_mb"`
	DNSBLZones        []string `json:"dnsbl_zones"`
	OutboundRateLimit int      `json:"outbound_rate_limit"`
}

type MailPolicyResponse struct {
	Policy MailPolicy `json:"policy"`
	Error  string     `json:"error,omitempty"`
}

type MailHealthResponse struct {
	ServerIP       string `json:"server_ip"`
	Myhostname     string `json:"myhostname"`
	HostnameFQDN   bool   `json:"hostname_fqdn"`
	PTR            string `json:"ptr"`
	FCrDNS         bool   `json:"fcrdns"`
	PTRAligned     bool   `json:"ptr_aligned"`
	TLSEnabled     bool   `json:"tls_enabled"`
	OutboundPort25 string `json:"outbound_port_25"`
	Error          string `json:"error,omitempty"`
}

type RBLResult struct {
	Zone   string `json:"zone"`
	Listed bool   `json:"listed"`
	Detail string `json:"detail,omitempty"`
}

type CheckRBLResponse struct {
	IP      string      `json:"ip"`
	Results []RBLResult `json:"results"`
	Error   string      `json:"error,omitempty"`
}
