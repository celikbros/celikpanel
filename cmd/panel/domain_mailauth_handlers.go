package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Mail authentication (SPF / DKIM / DMARC) for a domain — roadmap 3C.
// The panel generates recommended records, writes them into the PowerDNS
// zone on request, and verifies them with a live DNS lookup so the status
// shown is what the world actually sees, not what we hope is there.
//
// Bir domain için e-posta kimlik doğrulaması (SPF / DKIM / DMARC) — yol
// haritası 3C. Panel önerilen kayıtları üretir, istek üzerine PowerDNS
// zone'una yazar ve canlı DNS sorgusuyla doğrular; böylece gösterilen durum
// umduğumuz değil, dünyanın gerçekten gördüğüdür.

const dkimSelector = "celik"

type mailAuthRecord struct {
	Name        string `json:"name"`
	Recommended string `json:"recommended"`
	ZoneValue   string `json:"zone_value"`
	DNSValue    string `json:"dns_value"`
	Resolved    bool   `json:"resolved"`
	// Status: ok (live DNS matches), pending (in our zone, not visible in
	// live DNS), missing (nowhere), no_key (DKIM only), no_zone.
	// Durum: ok (canlı DNS eşleşiyor), pending (zone'umuzda var, canlı
	// DNS'te görünmüyor), missing (hiçbir yerde yok), no_key (yalnız DKIM),
	// no_zone.
	Status string `json:"status"`
}

type mailAuthStatus struct {
	Domain           string         `json:"domain"`
	ZoneExists       bool           `json:"zone_exists"`
	SPF              mailAuthRecord `json:"spf"`
	DKIM             mailAuthRecord `json:"dkim"`
	DMARC            mailAuthRecord `json:"dmarc"`
	DKIMSelector     string         `json:"dkim_selector"`
	SigningInstalled bool           `json:"signing_installed"`
}

// handleMailAuth routes the /mail/auth* endpoints (called from the domain
// dispatcher, so ownership is already enforced).
// handleMailAuth, /mail/auth* uç noktalarını yönlendirir (domain
// yönlendiricisinden çağrılır; sahiplik zaten uygulanmıştır).
func (p *Panel) handleMailAuth(w http.ResponseWriter, r *http.Request, domainID int) {
	var domainName string
	if err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domainName); err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/auth") && r.Method == http.MethodGet:
		p.handleMailAuthStatus(w, r, domainName)
	case strings.HasSuffix(r.URL.Path, "/auth/dkim") && r.Method == http.MethodPost:
		p.handleMailAuthDKIM(w, r, domainName)
	case strings.HasSuffix(r.URL.Path, "/auth/apply") && r.Method == http.MethodPost:
		p.handleMailAuthApply(w, r, domainName)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func spfRecommended() string { return "v=spf1 a mx ~all" }

func dmarcRecommended(domain, policy string) string {
	switch policy {
	case "quarantine", "reject":
	default:
		policy = "none"
	}
	return fmt.Sprintf("v=DMARC1; p=%s; rua=mailto:dmarc@%s", policy, domain)
}

func dkimRecommended(publicKeyB64 string) string {
	return "v=DKIM1; k=rsa; p=" + publicKeyB64
}

// zoneTXT returns the concatenated TXT value stored in our PowerDNS zone for
// a record name (quotes stripped, segments joined), or "" when absent.
// zoneTXT, bir kayıt adı için PowerDNS zone'umuzda saklanan birleştirilmiş
// TXT değerini döndürür (tırnaklar soyulur, parçalar birleştirilir); yoksa "".
func (p *Panel) zoneTXT(ctx context.Context, zoneID int, name string) string {
	var content string
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT content FROM pdns_records WHERE domain_id = ? AND type = 'TXT' AND name = ? AND disabled = 0 LIMIT 1`,
		zoneID, name).Scan(&content)
	if err != nil {
		return ""
	}
	return joinTXTSegments(content)
}

// joinTXTSegments turns `"abc" "def"` (PowerDNS TXT storage format) into
// `abcdef`; unquoted content passes through unchanged.
// joinTXTSegments, `"abc" "def"` (PowerDNS TXT saklama biçimi) girdisini
// `abcdef` yapar; tırnaksız içerik olduğu gibi geçer.
func joinTXTSegments(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, `"`) {
		return content
	}
	var b strings.Builder
	inQuote := false
	for _, r := range content {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitTXTContent renders a TXT value in PowerDNS storage format, splitting
// at 255 characters (the DNS string-segment limit — DKIM keys exceed it).
// splitTXTContent, bir TXT değerini PowerDNS saklama biçiminde üretir ve 255
// karakterde böler (DNS dizi-parça sınırı — DKIM anahtarları bunu aşar).
func splitTXTContent(value string) string {
	var segs []string
	for len(value) > 255 {
		segs = append(segs, `"`+value[:255]+`"`)
		value = value[255:]
	}
	segs = append(segs, `"`+value+`"`)
	return strings.Join(segs, " ")
}

// liveTXT queries public DNS for a TXT record. resolved=false means the
// lookup itself failed (offline dev box, NXDOMAIN, .local names) — reported
// honestly instead of being conflated with "record missing".
// liveTXT, bir TXT kaydı için genel DNS'i sorgular. resolved=false, sorgunun
// kendisinin başarısız olduğu anlamına gelir (çevrimdışı geliştirme kutusu,
// NXDOMAIN, .local adları) — "kayıt yok" ile karıştırılmadan dürüstçe
// bildirilir.
func liveTXT(ctx context.Context, name, prefix string) (value string, resolved bool) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	txts, err := net.DefaultResolver.LookupTXT(ctx, name)
	if err != nil {
		return "", false
	}
	for _, txt := range txts {
		if strings.HasPrefix(txt, prefix) {
			return txt, true
		}
	}
	return "", true
}

func deriveStatus(recommended, zoneValue, dnsValue string, resolved bool) string {
	if resolved && dnsValue == recommended {
		return "ok"
	}
	if zoneValue == recommended {
		return "pending"
	}
	return "missing"
}

func (p *Panel) handleMailAuthStatus(w http.ResponseWriter, r *http.Request, domain string) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")

	st := mailAuthStatus{Domain: domain, DKIMSelector: dkimSelector}

	var zoneID int
	zoneErr := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, domain).Scan(&zoneID)
	st.ZoneExists = zoneErr == nil

	// DKIM key state from the agent (never creates a key on GET).
	// DKIM anahtar durumu agent'tan (GET'te asla anahtar oluşturmaz).
	var dkimSt struct {
		HasKey           bool   `json:"has_key"`
		PublicKeyB64     string `json:"public_key_b64"`
		SigningInstalled bool   `json:"signing_installed"`
		Error            string `json:"error,omitempty"`
	}
	_ = p.agentClient.Call("Agent.GetDKIMStatus",
		&struct{ Domain, Selector string }{Domain: domain, Selector: dkimSelector}, &dkimSt)
	st.SigningInstalled = dkimSt.SigningInstalled

	// SPF
	st.SPF.Name = domain
	st.SPF.Recommended = spfRecommended()
	if st.ZoneExists {
		st.SPF.ZoneValue = p.zoneTXT(ctx, zoneID, domain)
	}
	st.SPF.DNSValue, st.SPF.Resolved = liveTXT(ctx, domain, "v=spf1")
	st.SPF.Status = deriveStatus(st.SPF.Recommended, st.SPF.ZoneValue, st.SPF.DNSValue, st.SPF.Resolved)

	// DKIM
	dkimName := dkimSelector + "._domainkey." + domain
	st.DKIM.Name = dkimName
	if dkimSt.HasKey {
		st.DKIM.Recommended = dkimRecommended(dkimSt.PublicKeyB64)
		if st.ZoneExists {
			st.DKIM.ZoneValue = p.zoneTXT(ctx, zoneID, dkimName)
		}
		st.DKIM.DNSValue, st.DKIM.Resolved = liveTXT(ctx, dkimName, "v=DKIM1")
		st.DKIM.Status = deriveStatus(st.DKIM.Recommended, st.DKIM.ZoneValue, st.DKIM.DNSValue, st.DKIM.Resolved)
	} else {
		st.DKIM.Status = "no_key"
	}

	// DMARC (compare only the prefix we manage; rua etc. may be customised)
	// DMARC (yalnızca yönettiğimiz kayıt; rua vb. özelleştirilmiş olabilir)
	dmarcName := "_dmarc." + domain
	st.DMARC.Name = dmarcName
	st.DMARC.Recommended = dmarcRecommended(domain, "none")
	if st.ZoneExists {
		st.DMARC.ZoneValue = p.zoneTXT(ctx, zoneID, dmarcName)
	}
	st.DMARC.DNSValue, st.DMARC.Resolved = liveTXT(ctx, dmarcName, "v=DMARC1")
	// Any valid DMARC record counts: the policy is the owner's choice.
	// Geçerli herhangi bir DMARC kaydı sayılır: politika sahibinin seçimidir.
	switch {
	case st.DMARC.Resolved && strings.HasPrefix(st.DMARC.DNSValue, "v=DMARC1"):
		st.DMARC.Status = "ok"
	case strings.HasPrefix(st.DMARC.ZoneValue, "v=DMARC1"):
		st.DMARC.Status = "pending"
	default:
		st.DMARC.Status = "missing"
	}

	json.NewEncoder(w).Encode(st)
}

// handleMailAuthDKIM generates (or reuses) the DKIM key pair via the agent.
// handleMailAuthDKIM, DKIM anahtar çiftini agent üzerinden üretir (ya da
// mevcut olanı kullanır).
func (p *Panel) handleMailAuthDKIM(w http.ResponseWriter, r *http.Request, domain string) {
	w.Header().Set("Content-Type", "application/json")

	var resp struct {
		Created      bool   `json:"created"`
		PublicKeyB64 string `json:"public_key_b64"`
		Error        string `json:"error,omitempty"`
	}
	err := p.agentClient.Call("Agent.EnsureDKIMKey",
		&struct{ Domain, Selector string }{Domain: domain, Selector: dkimSelector}, &resp)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	// A new key must reach the signing tables, or the record passes and the
	// signature never appears.
	// Yeni anahtar imzalama tablolarına ulaşmalı; yoksa kayıt var, imza yok.
	var sign struct {
		Configured bool   `json:"configured"`
		Error      string `json:"error,omitempty"`
	}
	_ = p.agentClient.Call("Agent.ConfigureDKIMSigning", &struct{}{}, &sign)
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"created":     resp.Created,
		"signing":     sign.Configured,
		"recommended": dkimRecommended(resp.PublicKeyB64),
	})
}

// ensureZone returns the ledger zone id, creating only the authoritative
// SOA/NS baseline needed by the mail-auth flow. It uses the server's shared
// nameserver identity and current cluster kind; all rows are committed
// together, so a failed baseline never leaves a half-created zone behind.
// ensureZone, defterdeki zone kimliğini döndürür; yoksa posta-kimlik akışının
// gerektirdiği yetkili SOA/NS temelini oluşturur. Sunucunun ortak ad sunucusu
// kimliğini ve güncel küme türünü kullanır; bütün satırlar birlikte kaydedilir,
// dolayısıyla başarısız bir temel yarım zone bırakmaz.
func (p *Panel) ensureZone(ctx context.Context, domain string) (int, error) {
	ns1, ns2 := p.configuredNameservers(ctx)
	if ns1 == "" || ns2 == "" {
		return 0, fmt.Errorf("nameserver identity is not configured")
	}
	if !p.dnsIdentityConfigured(ctx) {
		return 0, fmt.Errorf("DNS operating mode is not configured")
	}
	zoneType := p.dnsZoneType(ctx)

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var zoneID int
	err = tx.QueryRowContext(ctx, `SELECT id FROM pdns_domains WHERE name = ?`, domain).Scan(&zoneID)
	if err == nil {
		return zoneID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO pdns_domains (name, type) VALUES (?, ?)`, domain, zoneType)
	if err != nil {
		return 0, err
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	zoneID = int(id64)

	soa := fmt.Sprintf("%s hostmaster.%s %s 10800 3600 604800 3600",
		ns1, domain, time.Now().UTC().Format("2006010200"))
	for _, record := range []struct {
		typ     string
		content string
	}{
		{typ: "SOA", content: soa},
		{typ: "NS", content: ns1},
		{typ: "NS", content: ns2},
	} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, ?, ?, 3600)`,
			zoneID, domain, record.typ, record.content); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return zoneID, nil
}

// upsertTXT replaces (or inserts) the TXT record for a name in the zone.
// upsertTXT, zone'daki bir ad için TXT kaydını değiştirir (ya da ekler).
func (p *Panel) upsertTXT(ctx context.Context, zoneID int, name, value string) error {
	pool := p.db.GetDB()
	content := splitTXTContent(value)

	res, err := pool.ExecContext(ctx,
		`UPDATE pdns_records SET content = ?, disabled = 0 WHERE domain_id = ? AND type = 'TXT' AND name = ?`,
		content, zoneID, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = pool.ExecContext(ctx,
		`INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, 'TXT', ?, 3600)`,
		zoneID, name, content)
	return err
}

// handleMailAuthApply writes the requested record into the zone.
// handleMailAuthApply, istenen kaydı zone'a yazar.
func (p *Panel) handleMailAuthApply(w http.ResponseWriter, r *http.Request, domain string) {
	w.Header().Set("Content-Type", "application/json")

	var req struct {
		Record      string `json:"record"` // spf | dkim | dmarc
		DMARCPolicy string `json:"dmarc_policy,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var name, value string
	switch req.Record {
	case "spf":
		name, value = domain, spfRecommended()
	case "dmarc":
		name, value = "_dmarc."+domain, dmarcRecommended(domain, req.DMARCPolicy)
	case "dkim":
		var st struct {
			HasKey           bool   `json:"has_key"`
			PublicKeyB64     string `json:"public_key_b64"`
			SigningInstalled bool   `json:"signing_installed"`
			Error            string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.GetDKIMStatus",
			&struct{ Domain, Selector string }{Domain: domain, Selector: dkimSelector}, &st); err != nil {
			writeServerError(w, err)
			return
		}
		if !st.HasKey {
			writeClientError(w, http.StatusConflict, "generate the DKIM key first")
			return
		}
		name, value = dkimSelector+"._domainkey."+domain, dkimRecommended(st.PublicKeyB64)
	default:
		writeClientError(w, http.StatusBadRequest, "record must be spf, dkim or dmarc")
		return
	}

	zoneID, err := p.ensureZone(r.Context(), domain)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.upsertTXT(r.Context(), zoneID, name, value); err != nil {
		writeServerError(w, err)
		return
	}

	p.syncZoneToDNS(r.Context(), domain, false)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "name": name, "value": value})
}
