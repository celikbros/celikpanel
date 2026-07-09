package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Hosting capabilities — what this server can actually host right now. The
// Add Domain dialog reads this to offer only choices that can work: a php
// site needs a web server + PHP-FPM, a static site needs a web server, a
// DNS-only domain needs nothing (its zone is served once a DNS server runs,
// or DNS stays at an external provider). The requirement follows the role
// the domain will play on this server — not a fixed service list.
//
// Barındırma yetenekleri — bu sunucunun şu anda gerçekten neyi
// barındırabildiği. Add Domain penceresi bunu okuyup yalnız çalışabilecek
// seçenekleri sunar: php sitesi web sunucusu + PHP-FPM ister, statik site web
// sunucusu ister, yalnız-DNS domain hiçbir şey istemez (zone'u bir DNS
// sunucusu koşunca sunulur ya da DNS dış sağlayıcıda kalır). Gereksinim,
// domain'in bu sunucuda üstleneceği ROLÜ izler — sabit bir servis listesini
// değil.

type hostingCapabilities struct {
	WebServer      string   `json:"web_server"`      // "nginx", "apache" or "" when none
	PHPVersions    []string `json:"php_versions"`    // installed FPM versions, newest first
	DNSServer      string   `json:"dns_server"`      // "pdns", "bind" or ""
	MailServer     bool     `json:"mail_server"`     // postfix present
	DatabaseServer string   `json:"database_server"` // "mariadb", "postgresql" or ""
}

// hostingCaps computes the current capability set from installed services
// (package presence via the agent) plus the real PHP trees on disk.
// hostingCaps, mevcut yetenek kümesini kurulu servislerden (agent üzerinden
// paket varlığı) ve diskteki gerçek PHP ağaçlarından hesaplar.
func (p *Panel) hostingCaps() hostingCapabilities {
	caps := hostingCapabilities{PHPVersions: services.DetectInstalledPHPVersions()}

	var installed []string
	_ = p.agentClient.Call("Agent.InstalledServiceIDs", &transport.Empty{}, &installed)
	for _, id := range installed {
		switch id {
		case "nginx", "apache":
			if caps.WebServer == "" {
				caps.WebServer = id
			}
		case "pdns", "bind":
			if caps.DNSServer == "" {
				caps.DNSServer = id
			}
		case "postfix":
			caps.MailServer = true
		case "mariadb", "postgresql":
			if caps.DatabaseServer == "" {
				caps.DatabaseServer = id
			}
		}
	}
	return caps
}

// handleHostingCapabilities: GET, any authenticated user — customers add
// domains too and deserve the same honest picture of what can work.
// handleHostingCapabilities: GET, kimliği doğrulanmış herkes — müşteriler de
// domain ekler ve neyin çalışabileceğinin aynı dürüst resmini hak eder.
func (p *Panel) handleHostingCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(p.hostingCaps())
}
