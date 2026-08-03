package transport

type CpmoveMailAccount struct {
	Domain    string `json:"domain"`
	User      string `json:"user"`
	CryptHash string `json:"crypt_hash"`
	QuotaMB   int    `json:"quota_mb"`
}

type CpmoveForwarder struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type CpmoveDNSRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Prio    int    `json:"prio"`
}

type CpmoveDatabase struct {
	Name      string `json:"name"`
	DumpBytes int64  `json:"dump_bytes"`
}

type CpmoveInspectRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
}

type CpmoveInspectResponse struct {
	Username     string                       `json:"username"`
	MainDomain   string                       `json:"main_domain"`
	Domains      []string                     `json:"domains"`
	PublicHTML   bool                         `json:"public_html"`
	SiteBytes    int64                        `json:"site_bytes"`
	MailAccounts []CpmoveMailAccount          `json:"mail_accounts"`
	Forwarders   []CpmoveForwarder            `json:"forwarders"`
	DNSZones     map[string][]CpmoveDNSRecord `json:"dns_zones"`
	Databases    []CpmoveDatabase             `json:"databases"`
	Error        string                       `json:"error,omitempty"`
}

type CpmoveExtractRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
	SubscriptionID      int    `json:"subscription_id"`
	DomainID            int    `json:"domain_id"`
}

type CpmoveExtractResponse struct {
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
	Error    string `json:"error,omitempty"`
}

type CpmoveImportDBRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	Path                string `json:"path"`
	DumpName            string `json:"dump_name"`
	TargetDB            string `json:"target_db"`
}

type CpmoveImportDBResponse struct {
	Imported bool   `json:"imported"`
	Error    string `json:"error,omitempty"`
}
