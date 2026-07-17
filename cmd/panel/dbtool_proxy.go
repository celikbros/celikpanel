package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Reverse proxy for the database web tools (phpMyAdmin, phpPgAdmin). The
// agent serves them on loopback only (127.0.0.1:8306); this proxy is the sole
// way in, and it demands an authenticated panel session first — the tools are
// never exposed to the network and no firewall port opens for them. Any role
// may pass (B1 role split, Jul 17): the real authorization layer is the
// tools' own login — a tenant holds credentials only for their own database
// users, which the panel creates tenant-scoped.
//
// Veritabanı web araçları (phpMyAdmin, phpPgAdmin) için ters vekil. Agent
// onları yalnız loopback'te sunar (127.0.0.1:8306); içeri tek yol bu vekildir
// ve önce kimlik doğrulamalı bir panel oturumu ister — araçlar ağa asla
// açılmaz, onlar için güvenlik duvarında port da açılmaz. Her rol geçebilir
// (B1 rol ayrımı, 17 Tem): gerçek yetki katmanı araçların kendi girişidir —
// kiracının elinde yalnız kendi veritabanı kullanıcılarının kimlik bilgileri
// vardır; panel onları kiracı-kapsamlı oluşturur.

var dbToolProxy = httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: "127.0.0.1:8306"})

func (p *Panel) handleDBToolProxy(w http.ResponseWriter, r *http.Request) {
	// requireAuth has already validated the session; this is defence in
	// depth against a future public-path regression.
	// requireAuth oturumu zaten doğruladı; bu, ileride olası bir açık-yol
	// gerilemesine karşı derinlemesine savunmadır.
	if currentCaller(r) == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	dbToolProxy.ServeHTTP(w, r)
}
