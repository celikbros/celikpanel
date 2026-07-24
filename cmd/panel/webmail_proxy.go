package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
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
var webmailProxy = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "127.0.0.1:8307"})

func (p *Panel) handleWebmailProxy(w http.ResponseWriter, r *http.Request) {
	webmailProxy.ServeHTTP(w, r)
}
