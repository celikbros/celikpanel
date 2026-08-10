package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	paneltransport "github.com/alicelik/celikpanel/internal/transport"
)

// Reverse proxy for Roundcube webmail. Unlike the db-tool proxy, this one is
// PUBLIC (see isPublicPath): webmail's users are mailbox owners who may have
// no panel account at all, and Roundcube's own login is the authorization.
// The agent serves Roundcube only through a root-bound Unix socket; this proxy
// is the sole network-facing way in and no firewall port opens for it.
//
// Roundcube webmail için ters vekil. db-araç vekilinin aksine bu PUBLIC'tir
// (bkz. isPublicPath): webmail'in kullanıcıları, hiç panel hesabı olmayabilen
// posta kutusu sahipleridir ve yetkilendirme Roundcube'un kendi girişidir.
// Agent Roundcube'u yalnız root'un bağlayabildiği Unix socket'te sunar; içeri
// ağdan tek yol bu vekildir ve onun için güvenlik duvarında port açılmaz.
const (
	webmailPublicPath              = "/webmail/"
	webmailInternalHost            = "celikpanel-webmail.internal"
	webmailReadinessTimeout        = time.Second
	webmailUpstreamHeaderTimeout   = 30 * time.Second
	webmailReadinessBodyDrainLimit = 8 << 10
)

var (
	webmailTarget            = &url.URL{Scheme: "http", Host: webmailInternalHost}
	webmailUpstreamTransport = newWebmailUnixTransport(paneltransport.WebmailSocketPath())
	webmailProxy             = newWebmailUnixReverseProxy(webmailTarget, webmailUpstreamTransport)
	webmailReadinessClient   = &http.Client{
		Transport: webmailUpstreamTransport,
		Timeout:   webmailReadinessTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	webmailRequestLimiter = newRateLimiter(240, time.Minute)
)

func newWebmailUnixTransport(socketPath string) *http.Transport {
	dialer := &net.Dialer{Timeout: webmailReadinessTimeout}
	return &http.Transport{
		// Never honor HTTP_PROXY for this privileged local boundary and never
		// fall back to TCP. Every HTTP connection is the fixed Unix socket.
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: webmailUpstreamHeaderTimeout,
	}
}

func newWebmailUnixReverseProxy(target *url.URL, transport http.RoundTripper) *httputil.ReverseProxy {
	proxy := newWebmailReverseProxy(target)
	proxy.Transport = transport
	return proxy
}

func newWebmailReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)

		// The panel is directly exposed and therefore must never trust client
		// supplied proxy identity headers. ReverseProxy will append the actual
		// RemoteAddr to a clean X-Forwarded-For header after this director runs.
		for _, header := range []string{
			"Forwarded",
			"X-Forwarded-For",
			"X-Forwarded-Host",
			"X-Forwarded-Proto",
			"X-Real-Ip",
			"Client-Ip",
			"True-Client-Ip",
			"Cf-Connecting-Ip",
		} {
			request.Header.Del(header)
		}
		stripWebmailPanelCredentials(request.Header)

		request.Host = target.Host
		request.Header.Set("X-Real-Ip", clientIP(request))
		if request.TLS != nil {
			request.Header.Set("X-Forwarded-Proto", "https")
		} else {
			request.Header.Set("X-Forwarded-Proto", "http")
		}
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		stripWebmailProtectedSetCookies(response.Header)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Webmail is temporarily unavailable.", http.StatusBadGateway)
	}
	return proxy
}

func protectedPanelCookie(name string) bool {
	return name == sessionCookieName || name == impersonatorCookieName
}

func cookiePairName(raw string) (string, bool) {
	pair := strings.TrimSpace(raw)
	equals := strings.IndexByte(pair, '=')
	if equals <= 0 {
		return "", false
	}
	name := strings.TrimSpace(pair[:equals])
	if name == "" {
		return "", false
	}
	for _, r := range name {
		if r == 0x22 || r == 0x5c {
			return "", false
		}
		if r <= 0x20 || r >= 0x7f || strings.ContainsRune("()<>@,;:\"/[]?={}", r) {
			return "", false
		}
	}
	return name, true
}

func stripWebmailPanelCredentials(header http.Header) {
	header.Del("Authorization")
	header.Del("Proxy-Authorization")

	var kept []string
	for _, line := range header.Values("Cookie") {
		for _, pair := range strings.Split(line, ";") {
			name, valid := cookiePairName(pair)
			if !valid || protectedPanelCookie(name) {
				continue
			}
			kept = append(kept, strings.TrimSpace(pair))
		}
	}
	header.Del("Cookie")
	if len(kept) != 0 {
		header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func stripWebmailProtectedSetCookies(header http.Header) {
	raw := append([]string(nil), header.Values("Set-Cookie")...)
	header.Del("Set-Cookie")
	for _, value := range raw {
		name, valid := cookiePairName(strings.SplitN(value, ";", 2)[0])
		if !valid || protectedPanelCookie(name) {
			continue
		}
		header.Add("Set-Cookie", value)
	}
}

func webmailEndpointReady(ctx context.Context, client *http.Client, endpoint string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Accept", "text/html")
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, webmailReadinessBodyDrainLimit)); err != nil {
		return false
	}
	return response.StatusCode == http.StatusOK
}

func probeWebmailReadiness(ctx context.Context) bool {
	probeContext, cancel := context.WithTimeout(ctx, webmailReadinessTimeout)
	defer cancel()
	return webmailEndpointReady(
		probeContext,
		webmailReadinessClient,
		"http://"+webmailInternalHost+webmailPublicPath,
	)
}

func (p *Panel) handleWebmailProxy(w http.ResponseWriter, r *http.Request) {
	if !webmailRequestLimiter.allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many webmail requests.", http.StatusTooManyRequests)
		return
	}
	webmailProxy.ServeHTTP(w, r)
}
