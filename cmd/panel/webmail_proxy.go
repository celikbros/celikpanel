package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// Reverse proxy for Roundcube webmail. Unlike the db-tool proxy, this one is
// PUBLIC (see isPublicPath): webmail's users are mailbox owners who may have
// no panel account at all, and Roundcube's own login is the authorization.
// The agent serves Roundcube on loopback only (127.0.0.1:8307); this proxy is
// the sole way in and no firewall port opens for it.
//
// Roundcube webmail için ters vekil. db-araç vekilinin aksine bu PUBLIC'tir
// (bkz. isPublicPath): webmail'in kullanıcıları, hiç panel hesabı olmayabilen
// posta kutusu sahipleridir ve yetkilendirme Roundcube'un kendi girişidir.
// Agent Roundcube'u yalnız loopback'te sunar (127.0.0.1:8307); içeri tek yol
// bu vekildir ve onun için güvenlik duvarında port açılmaz.
var (
	webmailTarget         = &url.URL{Scheme: "http", Host: "127.0.0.1:8307"}
	webmailProxy          = newWebmailReverseProxy(webmailTarget)
	webmailRequestLimiter = newRateLimiter(240, time.Minute)
)

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

		request.Host = target.Host
		request.Header.Set("X-Real-Ip", clientIP(request))
		if request.TLS != nil {
			request.Header.Set("X-Forwarded-Proto", "https")
		} else {
			request.Header.Set("X-Forwarded-Proto", "http")
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "Webmail is temporarily unavailable.", http.StatusBadGateway)
	}
	return proxy
}

func (p *Panel) handleWebmailProxy(w http.ResponseWriter, r *http.Request) {
	if !webmailRequestLimiter.allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many webmail requests.", http.StatusTooManyRequests)
		return
	}
	webmailProxy.ServeHTTP(w, r)
}
