package transport

type DeleteSiteRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	SiteID              int    `json:"site_id"`
	SubscriptionID      int    `json:"subscription_id"`
	DomainID            int    `json:"domain_id"`
	Domain              string `json:"domain"`
	Username            string `json:"username"`
	PHPVersion          string `json:"php_version"`
	// SiteHome is retained for rolling RPC compatibility, but the agent
	// derives the authoritative value from SubscriptionID and DomainID and
	// rejects a non-empty mismatch.
	SiteHome string `json:"site_home"`
}

type DeleteSiteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}
