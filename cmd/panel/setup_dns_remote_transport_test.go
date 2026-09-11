package main

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRemoteDNSEndpointAndPublicAddressRestrictions(t *testing.T) {
	for _, endpoint := range []string{
		"http://dns.example.com", "https://127.0.0.1", "https://[::1]",
		"https://user:secret@dns.example.com", "https://dns.example.com/api",
		"https://dns.example.com?secret=hidden", "https://dns.example.com?",
		"https://dns.example.com#hidden", "https://dns.example.com/%2f",
		"https://dns.example.com:0", "https://dns.example.com:65536",
		"https://dns.example.com:02083", "https://dns.example.com:",
		"https://singlelabel", "https://dns.example.com\\@127.0.0.1",
	} {
		if _, err := canonicalRemoteDNSEndpoint(endpoint); err == nil {
			t.Fatalf("accepted unsafe endpoint %q", endpoint)
		}
	}
	for raw, want := range map[string]string{
		" https://DNS.Example.Com./ ":   "https://dns.example.com",
		"https://dns.example.com:443":   "https://dns.example.com",
		"https://dns.example.com:2083/": "https://dns.example.com:2083",
	} {
		if got, err := canonicalRemoteDNSEndpoint(raw); err != nil || got != want {
			t.Fatalf("canonical endpoint %q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{
		"0.0.0.1", "10.0.0.1", "100.64.1.1", "127.0.0.1", "169.254.169.254",
		"172.16.0.1", "192.0.0.9", "192.0.2.1", "192.168.1.1", "198.18.1.1",
		"198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:8.8.8.8", "64:ff9b::7f00:1",
		"100::1", "2001::1", "2001:db8::1", "2002:7f00:1::1", "3fff::1",
		"5f00::1", "fc00::1", "fe80::1%eth0", "ff02::1",
	} {
		if remoteDNSPublicAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("accepted non-server address %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		if !remoteDNSPublicAddress(netip.MustParseAddr(raw)) {
			t.Fatalf("rejected public address %s", raw)
		}
	}
}

func remoteDNSTestTransport(t *testing.T, handler http.Handler) (string, remoteDNSTransportDependencies, *atomic.Int32) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	t.Cleanup(server.Close)
	name := server.Certificate().DNSNames[0]
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	var dials atomic.Int32
	dialer := &net.Dialer{}
	deps := remoteDNSTransportDependencies{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "8.8.8.8:443" {
				t.Errorf("transport did not pin its approved address: %s %s", network, address)
			}
			dials.Add(1)
			// Test-only mapping of a proved public address to an isolated TLS
			// listener. Production uses the OS dialer without this substitution.
			return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
		roots: roots,
	}
	return "https://" + name, deps, &dials
}

func TestRemoteDNSTransportPinsFreshResolutionAndDoesNotUseProxy(t *testing.T) {
	var calls atomic.Int32
	endpoint, deps, dials := remoteDNSTestTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/dns/remote/receiver/status" || r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" {
			t.Error("wire request violated the machine contract")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ready":true}`)
	}))
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	var lookups int
	deps.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	var result struct {
		Ready bool `json:"ready"`
	}
	if err := remoteDNSHTTPJSONWith(context.Background(), endpoint, "/api/v1/dns/remote/receiver/status", "test-secret", struct{}{}, &result, deps); err != nil || !result.Ready {
		t.Fatalf("first trusted exchange failed: %v", err)
	}
	if err := remoteDNSHTTPJSONWith(context.Background(), endpoint, "/api/v1/dns/remote/receiver/status", "test-secret", struct{}{}, &result, deps); err == nil {
		t.Fatal("second exchange accepted rebound private address")
	}
	if lookups != 2 || calls.Load() != 1 || dials.Load() != 1 {
		t.Fatal("stale resolution, transport reuse or private dial occurred")
	}
}

func TestRemoteDNSTransportRejectsMixedDNSAndUntrustedTLS(t *testing.T) {
	endpoint, deps, dials := remoteDNSTestTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	base := deps.lookup
	deps.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("169.254.169.254")}, nil
	}
	var result struct{}
	if err := remoteDNSHTTPJSONWith(context.Background(), endpoint, "/api/v1/dns/remote/accept", "test-secret", struct{}{}, &result, deps); err == nil || dials.Load() != 0 {
		t.Fatal("mixed DNS answer attempted a connection")
	}
	deps.lookup = base
	deps.roots = x509.NewCertPool()
	if err := remoteDNSHTTPJSONWith(context.Background(), endpoint, "/api/v1/dns/remote/accept", "test-secret", struct{}{}, &result, deps); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}

func TestRemoteDNSTransportResponseBoundaryAndSecretRedaction(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"redirect", "application/json", `{"ready":true}`, 307},
		{"denied", "text/html", "test-secret-private-body", 401},
		{"HTML", "text/html", "test-secret-private-body", 200},
		{"too_large", "application/json", strings.Repeat("x", remoteDNSJSONLimit+1), 200},
		{"trailing", "application/json", `{"ready":true}{}`, 200},
		{"unknown", "application/json", `{"ready":true,"injected":1}`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			endpoint, deps, _ := remoteDNSTestTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Location", "https://attacker.example/steal")
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			var result struct {
				Ready bool `json:"ready"`
			}
			err := remoteDNSHTTPJSONWith(context.Background(), endpoint, "/api/v1/dns/remote/receiver/status", "test-secret", struct{}{}, &result, deps)
			if err == nil || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "private-body") || calls.Load() != 1 {
				t.Fatalf("unsafe remote response handling: %v", err)
			}
			var status *remoteDNSHTTPError
			if test.status != 200 && (!errors.As(err, &status) || status.StatusCode != test.status) {
				t.Fatal("remote HTTP status was not preserved safely")
			}
		})
	}
}

func TestRemoteDNSTransportRejectsInvalidRequestBeforeResolution(t *testing.T) {
	var resolved bool
	deps := remoteDNSTransportDependencies{lookup: func(context.Context, string, string) ([]netip.Addr, error) { resolved = true; return nil, nil }, dial: (&net.Dialer{}).DialContext}
	for _, test := range []struct {
		path, secret string
		request      any
	}{
		{"/api/v1/config", "test-secret", struct{}{}},
		{"/api/v1/dns/remote/accept", "secret\r\nInjected: yes", struct{}{}},
		{"/api/v1/dns/remote/accept", "test-secret", strings.Repeat("x", remoteDNSJSONLimit)},
	} {
		var result struct{}
		if err := remoteDNSHTTPJSONWith(context.Background(), "https://dns.example.com", test.path, test.secret, test.request, &result, deps); err == nil {
			t.Fatal("invalid outgoing request accepted")
		}
	}
	if resolved {
		t.Fatal("invalid input caused network activity")
	}
}
