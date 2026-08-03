package main

import "testing"

func TestSiteUsageIdentityDerivesExactSiteHomeAndCanonicalDomain(t *testing.T) {
	siteHome, domain, err := siteUsageIdentity(&SiteUsageRequest{
		SubscriptionID: 4,
		DomainID:       13,
		Domain:         "BioVision.Health.",
	})
	if err != nil {
		t.Fatalf("siteUsageIdentity: %v", err)
	}
	if siteHome != "/var/www/celikpanel/subscriptions/4/sites/13" {
		t.Fatalf("site home = %q", siteHome)
	}
	if domain != "biovision.health" {
		t.Fatalf("domain = %q, want biovision.health", domain)
	}
}

func TestSiteUsageIdentityRejectsInvalidIdentityAndDomain(t *testing.T) {
	tests := []struct {
		name string
		req  *SiteUsageRequest
	}{
		{name: "nil request"},
		{
			name: "zero subscription",
			req: &SiteUsageRequest{
				DomainID: 1,
				Domain:   "example.test",
			},
		},
		{
			name: "negative domain id",
			req: &SiteUsageRequest{
				SubscriptionID: 1,
				DomainID:       -1,
				Domain:         "example.test",
			},
		},
		{
			name: "path-like domain",
			req: &SiteUsageRequest{
				SubscriptionID: 1,
				DomainID:       2,
				Domain:         "../nginx/access.log",
			},
		},
		{
			name: "single-label domain",
			req: &SiteUsageRequest{
				SubscriptionID: 1,
				DomainID:       2,
				Domain:         "localhost",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := siteUsageIdentity(test.req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSiteUsageRejectsInvalidRequestBeforeMeasurement(t *testing.T) {
	var resp SiteUsageResponse
	if err := new(Agent).SiteUsage(&SiteUsageRequest{
		SubscriptionID: 1,
		DomainID:       2,
		Domain:         "/var/log/nginx/other",
	}, &resp); err != nil {
		t.Fatalf("SiteUsage returned RPC error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("invalid request did not produce an agent response error")
	}
}
