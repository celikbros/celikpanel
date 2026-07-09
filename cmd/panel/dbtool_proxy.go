package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Reverse proxy for the database web tools (phpMyAdmin, phpPgAdmin). The
// agent serves them on loopback only (127.0.0.1:8306); this proxy is the sole
// way in, and it demands an authenticated ADMIN panel session first — the
// tools are never exposed to the network and no firewall port opens for them.
// The tools keep their own login (database credentials) as the second layer.
//
// Veritabanı web araçları (phpMyAdmin, phpPgAdmin) için ters vekil. Agent
// onları yalnız loopback'te sunar (127.0.0.1:8306); içeri tek yol bu vekildir
// ve önce kimlik doğrulamalı bir YÖNETİCİ panel oturumu ister — araçlar ağa
// asla açılmaz, onlar için güvenlik duvarında port da açılmaz. Araçların
// kendi girişi (veritabanı kimlik bilgileri) ikinci katman olarak kalır.

var dbToolProxy = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "127.0.0.1:8306"})

func (p *Panel) handleDBToolProxy(w http.ResponseWriter, r *http.Request) {
	// requireAuth has already validated the session and isAdminOnlyPath has
	// enforced the admin role; this is defence in depth.
	// requireAuth oturumu doğruladı, isAdminOnlyPath admin rolünü uyguladı;
	// bu, derinlemesine savunmadır.
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		http.Error(w, "administrator access required", http.StatusForbidden)
		return
	}
	dbToolProxy.ServeHTTP(w, r)
}
