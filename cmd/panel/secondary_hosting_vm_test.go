//go:build linux

package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
)

// This opt-in test daemon retains the real remote HTTPS protocol and system CA
// verification, but routes one exact fixture name/address to the other isolated
// guest. Production retains its public-address and TLS requirements unchanged.
func TestSecondaryHostingDisposableDaemon(t *testing.T) {
	profile := os.Getenv("CELIKPANEL_SETUP_PROFILE_VM")
	if profile == "" {
		t.Skip("requires the disposable secondary-hosting QEMU driver")
	}
	if profile != "dnsprimary" && profile != "dnssecondary" {
		t.Fatal("invalid secondary-hosting fixture profile")
	}
	marker, err := os.ReadFile("/var/lib/celikpanel-profile-vm/secondary-hosting-fixture")
	if err != nil || string(marker) != "secondary-hosting-20260912\n" {
		t.Fatal("missing secondary-hosting fixture identity")
	}
	oldResolvers, oldExchange := setupDNSPublicResolvers, remoteDNSExchange
	t.Cleanup(func() {
		setupDNSPublicResolvers, remoteDNSExchange = oldResolvers, oldExchange
	})
	setupDNSPublicResolvers = func() []hostResolver { return []hostResolver{setupDNSResolverAt("127.0.0.1:5353")} }
	remoteDNSExchange = func(ctx context.Context, endpoint, path, secret string, request, response any) error {
		if endpoint != "https://panel.dnsprimary.setup.test:2083" {
			return errors.New("unexpected disposable DNS authority endpoint")
		}
		return remoteDNSHTTPJSONWith(ctx, endpoint, path, secret, request, response, remoteDNSTransportDependencies{
			lookup: func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if network != "ip" || host != "panel.dnsprimary.setup.test" {
					return nil, errors.New("unexpected disposable DNS lookup")
				}
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			},
			dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" || address != "8.8.8.8:2083" {
					return nil, errors.New("unexpected disposable DNS dial")
				}
				return (&net.Dialer{}).DialContext(ctx, "tcp", "192.0.2.10:2083")
			},
		})
	}
	runServerSetupDisposableProfileDaemon(t, profile)
}
