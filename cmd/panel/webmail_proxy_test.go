package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestWebmailProxyScrubsClientForwardingHeaders(t *testing.T) {
	var received http.Header
	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		receivedHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://panel.example.test/webmail/", nil)
	request.RemoteAddr = "192.0.2.44:54321"
	request.TLS = &tls.ConnectionState{}
	request.Header.Set("Forwarded", "for=attacker.example")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("X-Forwarded-Proto", "gopher")
	request.Header.Set("X-Real-IP", "203.0.113.10")
	request.Header.Set("CF-Connecting-IP", "203.0.113.11")

	recorder := httptest.NewRecorder()
	newWebmailReverseProxy(target).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("proxy status = %d, want 204; body=%s", recorder.Code, recorder.Body.String())
	}
	if receivedHost != target.Host {
		t.Fatalf("upstream Host = %q, want %q", receivedHost, target.Host)
	}
	if got := received.Get("X-Forwarded-For"); got != "192.0.2.44" {
		t.Fatalf("X-Forwarded-For = %q, want actual client IP", got)
	}
	if got := received.Get("X-Real-IP"); got != "192.0.2.44" {
		t.Fatalf("X-Real-IP = %q, want actual client IP", got)
	}
	if got := received.Get("X-Forwarded-Proto"); got != "https" {
		t.Fatalf("X-Forwarded-Proto = %q, want https", got)
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-Host", "CF-Connecting-IP"} {
		if got := received.Get(header); got != "" {
			t.Fatalf("%s leaked to upstream: %q", header, got)
		}
	}
}

func TestWebmailProxyRateLimitReturnsRetryHint(t *testing.T) {
	previous := webmailRequestLimiter
	webmailRequestLimiter = newRateLimiter(0, time.Minute)
	t.Cleanup(func() { webmailRequestLimiter = previous })

	request := httptest.NewRequest(http.MethodGet, "/webmail/", nil)
	request.RemoteAddr = "192.0.2.45:1234"
	recorder := httptest.NewRecorder()

	(&Panel{}).handleWebmailProxy(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
}
