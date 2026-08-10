package main

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWebmailProxyScrubsClientForwardingHeaders(t *testing.T) {
	var received http.Header
	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		receivedHost = r.Host
		w.Header().Add("Set-Cookie", "roundcube_sessid=mail-session; Path=/webmail/; HttpOnly")
		w.Header().Add("Set-Cookie", sessionCookieName+"=upstream-overwrite; Path=/")
		w.Header().Add("Set-Cookie", impersonatorCookieName+"=upstream-overwrite; Path=/")
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
	request.Header.Set("Authorization", "Bearer panel-secret")
	request.Header.Set("Proxy-Authorization", "Basic panel-proxy-secret")
	request.Header.Add("Cookie", "roundcube_sessid=keep-mail; "+sessionCookieName+"=panel-session; "+impersonatorCookieName+"=panel-impersonator")

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
	if got := received.Get("Authorization"); got != "" {
		t.Fatalf("Authorization leaked upstream: %q", got)
	}
	if got := received.Get("Proxy-Authorization"); got != "" {
		t.Fatalf("Proxy-Authorization leaked upstream: %q", got)
	}
	if got := received.Get("Cookie"); got != "roundcube_sessid=keep-mail" {
		t.Fatalf("upstream Cookie = %q, want only Roundcube cookie", got)
	}
	setCookies := recorder.Result().Header.Values("Set-Cookie")
	if len(setCookies) != 1 || !strings.HasPrefix(setCookies[0], "roundcube_sessid=") {
		t.Fatalf("downstream Set-Cookie = %#v, want only Roundcube cookie", setCookies)
	}
}

func TestWebmailCookieFilteringFailsClosedOnMalformedProtectedNames(t *testing.T) {
	header := http.Header{}
	header.Add("Cookie", "roundcube_sessid=keep; celikpanel_session\\=must-not-leak; celikpanel_session malformed-secret")
	header.Add("Cookie", impersonatorCookieName+"=must-not-leak-either")
	stripWebmailPanelCredentials(header)
	if got := header.Get("Cookie"); got != "roundcube_sessid=keep" {
		t.Fatalf("filtered Cookie = %q", got)
	}

	header.Add("Set-Cookie", "roundcube_sessid=keep-response; Path=/webmail/")
	header.Add("Set-Cookie", "celikpanel_session\\=must-not-reach-browser; Path=/")
	header.Add("Set-Cookie", sessionCookieName+"=must-not-reach-browser; Path=/")
	stripWebmailProtectedSetCookies(header)
	setCookies := header.Values("Set-Cookie")
	if len(setCookies) != 1 || !strings.HasPrefix(setCookies[0], "roundcube_sessid=") {
		t.Fatalf("filtered Set-Cookie = %#v", setCookies)
	}
}

func TestWebmailReadinessRequiresDirectHTTP200(t *testing.T) {
	redirectReached := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectReached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	tests := []struct {
		name   string
		status int
		want   bool
	}{
		{name: "ready", status: http.StatusOK, want: true},
		{name: "other success is not the login page", status: http.StatusNoContent},
		{name: "server failure", status: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("bounded body"))
			}))
			defer server.Close()
			client := &http.Client{Timeout: time.Second}
			if got := webmailEndpointReady(context.Background(), client, server.URL); got != test.want {
				t.Fatalf("ready = %v, want %v", got, test.want)
			}
		})
	}

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirector.Close()
	client := &http.Client{
		Timeout: time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if webmailEndpointReady(context.Background(), client, redirector.URL) {
		t.Fatal("redirect was accepted as ready")
	}
	if redirectReached {
		t.Fatal("readiness followed an external redirect")
	}
}

func TestWebmailReadinessTimeoutFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &http.Client{Timeout: 20 * time.Millisecond}
	if webmailEndpointReady(context.Background(), client, server.URL) {
		t.Fatal("timed-out webmail endpoint was reported ready")
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
