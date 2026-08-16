package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The deliverability health screen: every signal that decides "inbox or
// spam", one endpoint. Server facts (PTR/FCrDNS, HELO, TLS, port 25) come
// from the agent; domain facts (SPF/DKIM/DMARC via live DNS, blacklists,
// mail-certificate, DNSSEC) are assembled here. The UI paints it as one
// traffic-light card instead of five scattered tabs.
//
// Teslim edilebilirlik sağlık ekranı: "gelen kutusu mu spam mı"ya karar
// veren her sinyal, tek uçta. Sunucu gerçekleri (PTR/FCrDNS, HELO, TLS,
// port 25) agent'tan gelir; domain gerçekleri (canlı DNS'ten SPF/DKIM/DMARC,
// kara listeler, posta sertifikası, DNSSEC) burada birleştirilir. Arayüz
// bunu beş dağınık sekme yerine tek trafik-ışığı kartı olarak çizer.

type healthCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok | warn | fail | unknown
	Detail string `json:"detail,omitempty"`
}

// mailDNSSECSecured reports PowerDNS signing state only when PowerDNS is the
// exact durable active publisher. A retained standby database must never make
// an active BIND zone look DNSSEC-secured.
func (p *Panel) mailDNSSECSecured(ctx context.Context, domain string) bool {
	publisher, ready, err := p.activeDNSPublisher(ctx)
	if err != nil || !ready || publisher.Engine != transport.DNSEnginePowerDNS ||
		publisher.Epoch < 1 {
		return false
	}
	var status transport.DNSSECStatusResponse
	if err := p.callAgentContext(
		ctx, "Agent.DNSSECStatus",
		&transport.DNSSECRequest{Zone: domain}, &status,
	); err != nil || status.Error != "" {
		return false
	}
	return status.Secured
}

func (p *Panel) handleMailHealth(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	ctx := r.Context()
	domain := p.domainNameByID(ctx, domainID)
	if domain == "" {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return
	}

	var checks []healthCheck
	add := func(id, status, detail string) {
		checks = append(checks, healthCheck{ID: id, Status: status, Detail: detail})
	}

	// --- Server facts from the agent / Sunucu gerçekleri agent'tan
	var mh transport.MailHealthResponse
	if err := p.callAgent("Agent.MailHealth", &transport.Empty{}, &mh); err != nil {
		writeAgentError(w, err, "mail health")
		return
	}

	switch {
	case mh.PTR == "":
		add("ptr", "fail", mh.ServerIP)
	case mh.FCrDNS:
		add("ptr", "ok", mh.PTR)
	default:
		add("ptr", "warn", mh.PTR)
	}
	if mh.HostnameFQDN {
		add("helo", "ok", mh.Myhostname)
	} else {
		add("helo", "warn", mh.Myhostname)
	}
	if mh.TLSEnabled {
		add("tls", "ok", "")
	} else {
		add("tls", "fail", "")
	}
	add("port25", map[string]string{"open": "ok", "blocked": "fail"}[mh.OutboundPort25], "")
	if mh.OutboundPort25 == "unknown" {
		checks[len(checks)-1].Status = "unknown"
	}

	// --- SPF / DKIM / DMARC: what the world resolves; if the record sits
	// in our zone but the world cannot see it yet (delegation pending), that
	// is a warn, not a fail.
	// SPF / DKIM / DMARC: dünyanın çözdüğü; kayıt bizim zone'daysa ama dünya
	// henüz göremiyorsa (delegasyon bekliyor) bu fail değil warn'dur.
	var zoneID int
	_ = p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, domain).Scan(&zoneID)
	checkTXT := func(id, name, prefix string) {
		if v, ok := liveTXT(ctx, name, prefix); ok && v != "" {
			add(id, "ok", "")
			return
		}
		if zoneID > 0 && strings.HasPrefix(p.zoneTXT(ctx, zoneID, name), prefix) {
			add(id, "warn", "pending")
			return
		}
		add(id, "fail", "")
	}
	checkTXT("spf", domain, "v=spf1")
	checkTXT("dkim", dkimSelector+"._domainkey."+domain, "v=DKIM1")
	checkTXT("dmarc", "_dmarc."+domain, "v=DMARC1")

	// --- Blacklists / Kara listeler
	var rbl transport.CheckRBLResponse
	if p.callAgent("Agent.CheckRBL", &transport.Empty{}, &rbl) == nil && rbl.Error == "" && len(rbl.Results) > 0 {
		listed := 0
		for _, res := range rbl.Results {
			if res.Listed {
				listed++
			}
		}
		if listed == 0 {
			add("rbl", "ok", "")
		} else {
			add("rbl", "fail", "")
		}
	} else {
		add("rbl", "unknown", "")
	}

	// --- Mail certificate / Posta sertifikası
	var secureMail int
	_ = p.db.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(MAX(secure_mail), 0) FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'`, domainID).Scan(&secureMail)
	if secureMail == 1 {
		add("mailcert", "ok", "")
	} else {
		add("mailcert", "warn", "")
	}

	// --- DNSSEC
	if p.mailDNSSECSecured(ctx, domain) {
		add("dnssec", "ok", "")
	} else {
		add("dnssec", "warn", "")
	}

	// Overall: worst status wins. / Genel: en kötü durum kazanır.
	overall := "ok"
	for _, c := range checks {
		if c.Status == "fail" {
			overall = "fail"
			break
		}
		if c.Status == "warn" || c.Status == "unknown" {
			overall = "warn"
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"domain": domain, "server_ip": mh.ServerIP, "myhostname": mh.Myhostname,
		"expected_ptr": strings.ToLower(mh.Myhostname),
		"overall":      overall, "checks": checks,
	})
}
