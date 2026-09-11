package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const remoteDNSJSONLimit = 1 << 20

// Remote responses are never reflected into logs or browser errors: they may
// contain submitted credentials, private DNS data or untrusted HTML.
type remoteDNSHTTPError struct{ StatusCode int }

func (e *remoteDNSHTTPError) Error() string { return "remote DNS request was rejected" }

func canonicalRemoteDNSEndpoint(raw string) (string, error) {
	invalid := errors.New("remote DNS endpoint must be an HTTPS panel address")
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 512 || strings.Contains(raw, "#") {
		return "", invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return "", invalid
	}
	host, err := hostname.CanonicalFQDN(u.Hostname())
	if err != nil || strings.ContainsAny(u.Host, "[%\\") || strings.HasSuffix(u.Host, ":") {
		return "", invalid
	}
	port := u.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return "", invalid
		}
		if number != 443 {
			host = net.JoinHostPort(host, port)
		}
	}
	return "https://" + host, nil
}

// Conservative public-server policy, including documentation/transition ranges.
// Reviewed against IANA's special-purpose registries on 2026-09-11:
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
// Special-purpose anycast exceptions are not general-purpose panel endpoints.
var remoteDNSExcludedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"), netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/3"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
}

func remoteDNSPublicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, block := range remoteDNSExcludedNetworks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

// Only unit tests call the dependency-bearing implementation. Production has
// no environment/configuration option to replace resolution or TLS trust.
type remoteDNSTransportDependencies struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
	roots  *x509.CertPool
}

func remoteDNSHTTPJSON(ctx context.Context, endpoint, path, secret string, request, response any) error {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	return remoteDNSHTTPJSONWith(ctx, endpoint, path, secret, request, response, remoteDNSTransportDependencies{
		lookup: net.DefaultResolver.LookupNetIP,
		dial:   dialer.DialContext,
	})
}

func remoteDNSHTTPJSONWith(ctx context.Context, endpoint, path, secret string, request, response any, deps remoteDNSTransportDependencies) error {
	endpoint, err := canonicalRemoteDNSEndpoint(endpoint)
	if err != nil {
		return err
	}
	if !remoteDNSMachinePath(path) || secret == "" || len(secret) > 256 || deps.lookup == nil || deps.dial == nil || response == nil {
		return errors.New("invalid remote DNS exchange")
	}
	for _, c := range secret {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return errors.New("invalid remote DNS credential")
		}
	}
	payload, err := json.Marshal(request)
	if err != nil || len(payload) > remoteDNSJSONLimit {
		return errors.New("remote DNS request exceeds its JSON contract")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u, _ := url.Parse(endpoint)
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "443"
	}
	addresses, err := deps.lookup(ctx, "ip", host)
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		return errors.New("remote DNS address could not be resolved")
	}
	// Reject the entire answer when even one address is unsafe. Filtering it
	// out would conceal a mixed public/private or rebinding configuration.
	for _, address := range addresses {
		if !remoteDNSPublicAddress(address) {
			return errors.New("remote DNS endpoint must resolve only to public server addresses")
		}
	}
	approved := append([]netip.Addr(nil), addresses...)
	transport := &http.Transport{
		Proxy:                  nil,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		ForceAttemptHTTP2:      false,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSClientConfig:        &tls.Config{ServerName: host, RootCAs: deps.roots, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != net.JoinHostPort(host, port) {
				return nil, errors.New("remote DNS transport target changed")
			}
			for _, ip := range approved {
				if conn, err := deps.dial(dialCtx, "tcp", net.JoinHostPort(ip.String(), port)); err == nil {
					return conn, nil
				}
				if dialCtx.Err() != nil {
					break
				}
			}
			return nil, errors.New("remote DNS endpoint could not be reached")
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return errors.New("remote DNS request could not be prepared")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("remote DNS HTTPS exchange failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &remoteDNSHTTPError{StatusCode: resp.StatusCode}
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || resp.Header.Get("Content-Encoding") != "" {
		return errors.New("remote DNS response is not JSON")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, remoteDNSJSONLimit+1))
	if err != nil || len(raw) == 0 || len(raw) > remoteDNSJSONLimit {
		return errors.New("remote DNS response exceeds its JSON contract")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(response) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("remote DNS response does not match its contract")
	}
	return nil
}
