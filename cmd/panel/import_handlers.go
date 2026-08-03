package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
)

// cPanel importer endpoints (admin-only via isAdminOnlyPath). Two steps by
// design: inspect renders an honest preview from the archive; apply runs
// only after the operator confirms, and reports per-step results instead of
// one vague success/failure.
//
// cPanel içe aktarım uçları (isAdminOnlyPath ile yalnızca admin). Bilerek
// iki adım: inspect arşivden dürüst bir önizleme çıkarır; apply ancak
// operatör onaylayınca çalışır ve tek belirsiz başarı/başarısızlık yerine
// adım adım sonuç raporlar.

type cpmovePreview struct {
	Username     string   `json:"username"`
	MainDomain   string   `json:"main_domain"`
	Domains      []string `json:"domains"`
	PublicHTML   bool     `json:"public_html"`
	SiteBytes    int64    `json:"site_bytes"`
	MailAccounts []struct {
		Domain    string `json:"domain"`
		User      string `json:"user"`
		CryptHash string `json:"crypt_hash"`
		QuotaMB   int    `json:"quota_mb"`
	} `json:"mail_accounts"`
	Forwarders []struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	} `json:"forwarders"`
	DNSZones map[string][]struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Prio    int    `json:"prio"`
	} `json:"dns_zones"`
	Databases []struct {
		Name      string `json:"name"`
		DumpBytes int64  `json:"dump_bytes"`
	} `json:"databases"`
	Error string `json:"error,omitempty"`
}

func (p *Panel) inspectCpmove(ctx context.Context, archivePath string) (*cpmovePreview, error) {
	var preview cpmovePreview
	err := p.agentClient.Call("Agent.InspectCpmove", &struct {
		Path string `json:"path"`
	}{Path: archivePath}, &preview)
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

func (p *Panel) handleImportInspect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !filepath.IsAbs(req.Path) {
		writeClientError(w, http.StatusBadRequest, "path must be an absolute path to a cpmove/backup .tar.gz on this server")
		return
	}

	preview, err := p.inspectCpmove(r.Context(), req.Path)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if preview.Error != "" {
		writeClientError(w, http.StatusBadRequest, preview.Error)
		return
	}
	json.NewEncoder(w).Encode(preview)
}

type importStep struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func (p *Panel) handleImportApply(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path           string `json:"path"`
		SubscriptionID int    `json:"subscription_id"`
		Domain         string `json:"domain"` // which domain from the archive to import
		DoFiles        bool   `json:"do_files"`
		DoMail         bool   `json:"do_mail"`
		DoDNS          bool   `json:"do_dns"`
		DoDatabases    bool   `json:"do_databases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !filepath.IsAbs(req.Path) || req.SubscriptionID <= 0 {
		writeClientError(w, http.StatusBadRequest, "path, subscription_id and domain are required")
		return
	}

	preview, err := p.inspectCpmove(r.Context(), req.Path)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if preview.Error != "" {
		writeClientError(w, http.StatusBadRequest, preview.Error)
		return
	}
	if req.Domain == "" {
		req.Domain = preview.MainDomain
	}
	if req.Domain == "" {
		writeClientError(w, http.StatusBadRequest, "the archive does not declare a domain; pass one explicitly")
		return
	}
	canonicalDomain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := hostname.MailFQDN(canonicalDomain); err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Domain = canonicalDomain

	ctx := r.Context()
	steps := []importStep{}
	fail := func(step string, err error) {
		steps = append(steps, importStep{Step: step, OK: false, Detail: err.Error()})
	}
	ok := func(step, detail string) {
		steps = append(steps, importStep{Step: step, OK: true, Detail: detail})
	}

	// 1. Domain + site under the chosen subscription (quota enforced).
	// 1. Seçilen abonelik altında domain + site (kota uygulanır).
	if err := p.checkSubscriptionQuota(ctx, req.SubscriptionID, quotaDomains); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	var domainID, siteID int
	res, err := p.db.GetDB().ExecContext(ctx,
		`INSERT INTO domains (subscription_id, name, status) VALUES (?, ?, 'active')`,
		req.SubscriptionID, req.Domain)
	if err != nil {
		if hostname.IsNamespaceConflict(err) {
			writeClientError(w, http.StatusConflict, "this hostname is already used by a domain, its www name, or an alias")
			return
		}
		writeServerError(w, err)
		return
	}
	id64, _ := res.LastInsertId()
	domainID = int(id64)

	docroot, err := hostingpath.DocumentRoot(req.SubscriptionID, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	res, err = p.db.GetDB().ExecContext(ctx,
		`INSERT INTO sites (domain_id, document_root, php_version, project_type, status) VALUES (?, ?, '8.3', 'php', 'active')`,
		domainID, docroot)
	if err != nil {
		writeServerError(w, err)
		return
	}
	id64, _ = res.LastInsertId()
	siteID = int(id64)
	ok("domain", fmt.Sprintf("%s (id %d, site %d) → %s", req.Domain, domainID, siteID, docroot))

	// 2. Site files.
	// 2. Site dosyaları.
	if req.DoFiles {
		var ext struct {
			Files int    `json:"files"`
			Bytes int64  `json:"bytes"`
			Error string `json:"error,omitempty"`
		}
		err := p.agentClient.Call("Agent.ExtractCpmoveFiles", &struct {
			Path           string `json:"path"`
			SubscriptionID int    `json:"subscription_id"`
			DomainID       int    `json:"domain_id"`
		}{
			Path:           req.Path,
			SubscriptionID: req.SubscriptionID,
			DomainID:       domainID,
		}, &ext)
		switch {
		case err != nil:
			fail("files", err)
		case ext.Error != "":
			fail("files", fmt.Errorf("%s", ext.Error))
		default:
			ok("files", fmt.Sprintf("%d files, %d bytes", ext.Files, ext.Bytes))
		}
	}

	// 3. Mail accounts (passwords preserved via {CRYPT}) + forwarders.
	// 3. Posta hesapları (parolalar {CRYPT} ile korunur) + yönlendirmeler.
	if req.DoMail {
		imported := 0
		for _, acc := range preview.MailAccounts {
			if !strings.EqualFold(acc.Domain, req.Domain) {
				continue
			}
			email := acc.User + "@" + req.Domain
			var done bool
			err := p.agentClient.Call("Agent.ImportMailAccount", &struct {
				Email     string `json:"email"`
				CryptHash string `json:"crypt_hash"`
				QuotaMB   int    `json:"quota_mb"`
			}{Email: email, CryptHash: acc.CryptHash, QuotaMB: acc.QuotaMB}, &done)
			if err != nil {
				fail("mail:"+email, err)
				continue
			}
			quota := acc.QuotaMB
			if quota <= 0 {
				quota = 1024
			}
			_, _ = p.db.GetDB().ExecContext(ctx,
				`INSERT INTO email_accounts (domain_id, address, password_hash, quota_mb) VALUES (?, ?, 'imported-crypt', ?)`,
				domainID, email, quota)
			imported++
		}
		ok("mail", fmt.Sprintf("%d accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)", imported))

		fwd := 0
		for _, f := range preview.Forwarders {
			if !strings.HasSuffix(strings.ToLower(f.Source), "@"+req.Domain) {
				continue
			}
			_, err := p.db.GetDB().ExecContext(ctx,
				`INSERT INTO email_forwardings (domain_id, source, destination) VALUES (?, ?, ?)`,
				domainID, f.Source, f.Destination)
			if err == nil {
				fwd++
			}
		}
		if fwd > 0 {
			p.pushForwardingsToAgent(ctx)
		}
		ok("forwarders", fmt.Sprintf("%d forwarders", fwd))
	}

	// 4. DNS records into our zone (NS/SOA excluded — ours are generated).
	// 4. DNS kayıtları zone'umuza (NS/SOA hariç — bizimkiler üretilir).
	if req.DoDNS {
		records, has := preview.DNSZones[req.Domain]
		if !has {
			for archiveDomain, archiveRecords := range preview.DNSZones {
				canonicalArchiveDomain, canonicalErr := hostname.CanonicalFQDN(archiveDomain)
				if canonicalErr == nil && canonicalArchiveDomain == req.Domain {
					records, has = archiveRecords, true
					break
				}
			}
		}
		if !has {
			ok("dns", "archive has no zone for this domain")
		} else {
			zoneID, err := p.ensureZone(ctx, req.Domain)
			if err != nil {
				fail("dns", err)
			} else {
				count := 0
				for _, rec := range records {
					if rec.Type == "NS" || rec.Type == "SOA" {
						continue
					}
					content := rec.Content
					if rec.Type == "TXT" {
						content = splitTXTContent(content)
					}
					_, err := p.db.GetDB().ExecContext(ctx, `
						INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio)
						VALUES (?, ?, ?, ?, ?, ?)`,
						zoneID, rec.Name, rec.Type, content, rec.TTL, rec.Prio)
					if err == nil {
						count++
					}
				}
				p.syncZoneToDNS(ctx, req.Domain, false)
				ok("dns", fmt.Sprintf("%d records imported (NS/SOA regenerated by the panel)", count))
			}
		}
	}

	// 5. Databases: metadata always; engine create+dump import attempted and
	// reported honestly (fails on hosts without engine access).
	// 5. Veritabanları: metadata her zaman; motor oluşturma+dump içe aktarma
	// denenir ve dürüstçe raporlanır (motor erişimi olmayan makinede düşer).
	if req.DoDatabases {
		p.ensureInstalledDBServers(ctx, req.SubscriptionID)
		for _, db := range preview.Databases {
			var serverID int
			err := p.db.GetDB().QueryRowContext(ctx, `
				SELECT ds.id FROM database_servers ds
				JOIN database_server_types dst ON ds.type_id = dst.id
				WHERE dst.name IN ('mysql','mariadb') ORDER BY ds.is_default DESC, ds.id LIMIT 1`).Scan(&serverID)
			if err != nil {
				fail("database:"+db.Name, fmt.Errorf("no MySQL/MariaDB server registered"))
				continue
			}
			if _, err := p.db.GetDB().ExecContext(ctx, `
				INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name) VALUES (?, ?, ?, ?)`,
				serverID, req.SubscriptionID, domainID, db.Name); err != nil {
				fail("database:"+db.Name, err)
				continue
			}

			var imp struct {
				Imported bool   `json:"imported"`
				Error    string `json:"error,omitempty"`
			}
			err = p.agentClient.Call("Agent.ImportCpmoveDatabase", &struct {
				Path     string `json:"path"`
				DumpName string `json:"dump_name"`
				TargetDB string `json:"target_db"`
			}{Path: req.Path, DumpName: db.Name, TargetDB: db.Name}, &imp)
			switch {
			case err != nil:
				fail("database:"+db.Name, err)
			case imp.Error != "":
				fail("database:"+db.Name, fmt.Errorf("metadata saved; engine import failed: %s", imp.Error))
			default:
				ok("database:"+db.Name, "created and dump imported (db USERS are not migrated; repoint app configs)")
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"domain_id": domainID,
		"steps":     steps,
	})
}

// pushForwardingsToAgent syncs the full forwarding map to postfix (the map
// file is global, so the agent needs the complete list).
// pushForwardingsToAgent, tam yönlendirme haritasını postfix'e eşitler (map
// dosyası globaldir; agent tam listeyi ister).
func (p *Panel) pushForwardingsToAgent(ctx context.Context) {
	type fwd struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	var all []fwd

	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT source, destination FROM email_forwardings`)
	if err != nil {
		return
	}
	for rows.Next() {
		var f fwd
		if rows.Scan(&f.Source, &f.Destination) == nil {
			all = append(all, f)
		}
	}
	rows.Close()

	// Catch-all rides the same virtual-alias map with an "@domain" source.
	// postfix matches the most specific key first, so explicit addresses and
	// forwardings still win; the catch-all only fires for the unmatched.
	// Catch-all, "@domain" kaynağıyla aynı sanal-takma-ad haritasını kullanır.
	// postfix önce en özgül anahtarı eşler; açık adresler ve yönlendirmeler
	// yine kazanır, catch-all yalnız eşleşmeyen için devreye girer.
	caRows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT '@' || d.name, c.destination
		FROM mail_catch_all c JOIN domains d ON d.id = c.domain_id`)
	if err == nil {
		for caRows.Next() {
			var f fwd
			if caRows.Scan(&f.Source, &f.Destination) == nil {
				all = append(all, f)
			}
		}
		caRows.Close()
	}

	var done bool
	_ = p.agentClient.Call("Agent.UpdateMailForwarding", &struct {
		Forwardings []fwd `json:"forwardings"`
	}{Forwardings: all}, &done)
}
