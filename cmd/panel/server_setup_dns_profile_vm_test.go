//go:build linux

package main

import (
	"os"
	"testing"
)

// The two fresh DNS guests use a controlled nameserver resolver on port5353.
// The real agent, BIND listener on53, catalog transfer, peer proof, HTTPS,
// renewal and firewall remain unchanged. This is not public DNS validation.
func TestServerSetupDisposableDNSProfileDaemon(t *testing.T) {
	profile := os.Getenv("CELIKPANEL_SETUP_PROFILE_VM")
	if profile == "" {
		t.Skip("requires the explicit disposable paired DNS profile driver")
	}
	if profile != "dnsprimary" && profile != "dnssecondary" {
		t.Fatal("not an authorized disposable DNS profile fixture")
	}
	original := setupDNSPublicResolvers
	setupDNSPublicResolvers = func() []hostResolver { return []hostResolver{setupDNSResolverAt("127.0.0.1:5353")} }
	t.Cleanup(func() { setupDNSPublicResolvers = original })
	runServerSetupDisposableProfileDaemon(t, profile)
}
