package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
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

type cpmovePreview = transport.CpmoveInspectResponse

func (p *Panel) inspectCpmove(ctx context.Context, archivePath string) (*cpmovePreview, error) {
	var preview cpmovePreview
	err := p.callAgent("Agent.InspectCpmove", &transport.CpmoveInspectRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Path:                archivePath,
	}, &preview)
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
	req.Domain, err = canonicalCpmoveImportDomain(preview, req.Domain)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	steps := []importStep{}
	complete := true
	fail := func(step string, err error) {
		complete = false
		steps = append(steps, importStep{Step: step, OK: false, Detail: err.Error()})
	}
	ok := func(step, detail string) {
		steps = append(steps, importStep{Step: step, OK: true, Detail: detail})
	}

	// 1. Domain + site under the chosen subscription (quota enforced).
	// 1. Seçilen abonelik altında domain + site (kota uygulanır).
	caps, err := p.hostingCaps(ctx)
	if err != nil {
		writeAgentError(w, err, "hosting capabilities")
		return
	}
	if caps.DNSServer == "" || !p.dnsIdentityConfigured(ctx) {
		writeClientError(w, http.StatusConflict, "DNS server identity must be configured before importing a domain")
		return
	}
	if caps.WebServer == "" || len(caps.PHPVersions) == 0 {
		writeClientError(w, http.StatusConflict, "a web server and a managed PHP-FPM version are required for cPanel imports")
		return
	}
	if req.DoMail && !caps.MailServer {
		writeClientError(w, http.StatusConflict, "Postfix must be installed before importing mail accounts")
		return
	}
	if req.DoDatabases {
		hasMySQL := false
		for _, engine := range caps.DatabaseServers {
			if engine == "mariadb" {
				hasMySQL = true
				break
			}
		}
		if !hasMySQL {
			writeClientError(w, http.StatusConflict, "MariaDB/MySQL must be installed before importing databases")
			return
		}
		for _, database := range preview.Databases {
			if err := services.ValidateSQLIdentifier(database.Name); err != nil {
				writeClientError(w, http.StatusBadRequest, "archive contains an invalid database name")
				return
			}
		}
	}
	if req.DoDNS {
		if records, has := cpmoveDNSRecordsForDomain(preview, req.Domain); has {
			if _, err := normalizeCpmoveDNSRecords(req.Domain, records); err != nil {
				writeClientError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	if err := p.checkSubscriptionQuota(ctx, req.SubscriptionID, quotaDomains); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}
	if err := p.checkSubscriptionQuota(ctx, req.SubscriptionID, quotaDisk); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	createCtx, cancelCreate := context.WithTimeout(ctx, domainCreateTimeout)
	created, err := p.orchestrator.CreateSite(createCtx, &services.CreateSiteRequest{
		SubscriptionID: req.SubscriptionID,
		Domain:         req.Domain,
		ProjectType:    "php",
		PHPVersion:     caps.PHPVersions[0],
		SSLType:        "none",
		AccessMethod:   "sftp",
		InitialStatus:  "pending",
	})
	cancelCreate()
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeClientError(w, http.StatusConflict, "domain already exists on this server")
			return
		}
		writeServerError(w, err)
		return
	}
	domainID, siteID := created.DomainID, created.SiteID
	ok("domain", fmt.Sprintf("%s (id %d, site %d) → %s", req.Domain, domainID, siteID, created.DocumentRoot))

	// 2. Site files.
	// 2. Site dosyaları.
	if req.DoFiles {
		var ext transport.CpmoveExtractResponse
		err := p.callAgent("Agent.ExtractCpmoveFiles", &transport.CpmoveExtractRequest{
			ExpectedBuildCommit: strings.TrimSpace(buildCommit),
			Path:                req.Path,
			SubscriptionID:      req.SubscriptionID,
			DomainID:            domainID,
		}, &ext)
		switch {
		case err != nil:
			fail("files", err)
		case ext.Error != "":
			fail("files", fmt.Errorf("%s", ext.Error))
		case !ext.Complete:
			fail("files", fmt.Errorf("agent did not confirm complete atomic extraction"))
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
			email, err := transport.CanonicalMailboxForDomain(acc.User, req.Domain)
			if err != nil {
				fail("mail:"+acc.User, err)
				continue
			}
			if len(acc.CryptHash) == 0 || len(acc.CryptHash) > 4096 ||
				strings.ContainsAny(acc.CryptHash, ":\r\n\x00") {
				fail("mail:"+email, fmt.Errorf("invalid imported password hash"))
				continue
			}
			quota := acc.QuotaMB
			if quota <= 0 {
				quota = 1024
			}
			if quota > transport.MaxMailboxQuotaMB {
				fail("mail:"+email, fmt.Errorf("mail quota is outside the allowed range"))
				continue
			}

			p.mailMutationMu.Lock()
			if guardErr := p.ensureMailDomainMutable(ctx, domainID); guardErr != nil {
				p.mailMutationMu.Unlock()
				writeMailDomainMutationError(w, guardErr)
				return
			}
			tx, txErr := p.db.GetDB().BeginTx(ctx, nil)
			if txErr == nil {
				_, txErr = tx.ExecContext(ctx,
					"INSERT INTO email_accounts (domain_id, address, password_hash, quota_mb) VALUES (?, ?, 'imported-crypt', ?)",
					domainID, email, quota)
			}
			if txErr == nil {
				txErr = p.callMailMutation(ctx, "Agent.ImportMailAccount", &transport.ImportMailAccountRequest{
					Email: email, CryptHash: acc.CryptHash, QuotaMB: quota,
				})
			}
			if txErr != nil {
				if tx != nil {
					txErr = rollbackMailTx(tx, txErr)
				}
				p.mailMutationMu.Unlock()
				fail("mail:"+email, txErr)
				continue
			}
			if txErr = tx.Commit(); txErr != nil {
				compCtx, cancel := mailCompensationContext(ctx)
				compErr := p.callMailMutation(compCtx, "Agent.DeleteMailAccount",
					&transport.DeleteMailAccountRequest{Email: email})
				cancel()
				if compErr != nil {
					txErr = errors.Join(txErr, fmt.Errorf("agent compensation failed: %w", compErr))
				}
				p.mailMutationMu.Unlock()
				fail("mail:"+email, txErr)
				continue
			}
			p.mailMutationMu.Unlock()
			imported++
		}
		ok("mail", fmt.Sprintf("%d accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)", imported))

		forwardings := make([]transport.MailForwarding, 0, len(preview.Forwarders))
		for _, f := range preview.Forwarders {
			source, err := transport.CanonicalMailboxForDomain(f.Source, req.Domain)
			if err != nil {
				fail("forwarder:"+f.Source, err)
				continue
			}
			destination, err := transport.CanonicalMailAddress(f.Destination)
			if err != nil {
				fail("forwarder:"+source, err)
				continue
			}
			forwardings = append(forwardings, transport.MailForwarding{
				Source: source, Destination: destination,
			})
		}
		if len(forwardings) == 0 {
			ok("forwarders", "0 forwarders")
		} else {
			p.mailMutationMu.Lock()
			err := p.mutateForwardings(ctx, domainID, func(tx *sql.Tx) error {
				for _, forwarding := range forwardings {
					if _, err := tx.ExecContext(ctx,
						"INSERT INTO email_forwardings (domain_id, source, destination) VALUES (?, ?, ?)",
						domainID, forwarding.Source, forwarding.Destination); err != nil {
						return err
					}
				}
				return nil
			})
			p.mailMutationMu.Unlock()
			if err != nil {
				if errors.Is(err, errDomainDeletionPending) {
					writeMailDomainMutationError(w, err)
					return
				}
				fail("forwarders", err)
			} else {
				ok("forwarders", fmt.Sprintf("%d forwarders", len(forwardings)))
			}
		}
	}

	// 4. DNS records into our zone (NS/SOA excluded — ours are generated).
	// 4. DNS kayıtları zone'umuza (NS/SOA hariç — bizimkiler üretilir).
	records, hasImportedZone := cpmoveDNSRecordsForDomain(preview, req.Domain)
	dnsDetail := "panel DNS template created; archive DNS import was not selected"
	var dnsErr error
	if req.DoDNS && hasImportedZone {
		var zoneID int
		zoneID, dnsErr = p.ensureZone(ctx, req.Domain)
		if dnsErr == nil {
			var count int
			count, dnsErr = replaceCpmoveDNSRecords(ctx, p.db.GetDB(), zoneID, req.Domain, records)
			dnsDetail = fmt.Sprintf("%d records imported atomically (NS/SOA regenerated by the panel)", count)
		}
	} else {
		_, _, dnsErr = p.createZoneWithTemplate(ctx, req.Domain)
		if req.DoDNS {
			dnsDetail = "archive has no zone for this domain; panel DNS template created"
		}
	}
	if dnsErr == nil {
		publishCtx, cancelPublish := context.WithTimeout(context.WithoutCancel(ctx), domainDNSPublicationTimeout)
		dnsErr = p.syncZoneToDNS(publishCtx, req.Domain, false)
		cancelPublish()
	}
	if dnsErr != nil {
		fail("dns", dnsErr)
	} else {
		ok("dns", dnsDetail)
	}

	// 5. Databases: metadata always; engine create+dump import attempted and
	// reported honestly (fails on hosts without engine access).
	// 5. Veritabanları: metadata her zaman; motor oluşturma+dump içe aktarma
	// denenir ve dürüstçe raporlanır (motor erişimi olmayan makinede düşer).
	if req.DoDatabases {
		if err := p.ensureInstalledDBServers(ctx, req.SubscriptionID); err != nil {
			fail("databases", err)
		} else {
			var serverID int
			err := p.db.GetDB().QueryRowContext(ctx, `
				SELECT ds.id FROM database_servers ds
				JOIN database_server_types dst ON ds.type_id = dst.id
				WHERE ds.subscription_id = ? AND ds.status = 'active'
				  AND dst.name IN ('mysql','mariadb')
				ORDER BY ds.is_default DESC, ds.id LIMIT 1`,
				req.SubscriptionID,
			).Scan(&serverID)
			if err != nil {
				fail("databases", fmt.Errorf("no active MySQL/MariaDB server registered for this subscription"))
			}
			for _, db := range preview.Databases {
				if err != nil {
					continue
				}

				var imp transport.CpmoveImportDBResponse
				importErr := p.callAgent("Agent.ImportCpmoveDatabase", &transport.CpmoveImportDBRequest{
					ExpectedBuildCommit: strings.TrimSpace(buildCommit),
					Path:                req.Path,
					DumpName:            db.Name,
					TargetDB:            db.Name,
				}, &imp)
				switch {
				case importErr != nil:
					fail("database:"+db.Name, importErr)
				case imp.Error != "":
					fail("database:"+db.Name, fmt.Errorf("engine import failed before metadata was created: %s", imp.Error))
				case !imp.Imported:
					fail("database:"+db.Name, fmt.Errorf("agent did not confirm the database import"))
				default:
					_, metadataErr := p.db.GetDB().ExecContext(ctx, `
						INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name)
						VALUES (?, ?, ?, ?)`,
						serverID, req.SubscriptionID, domainID, db.Name)
					if metadataErr != nil {
						fail("database:"+db.Name, fmt.Errorf(
							"database was imported physically but metadata could not be recorded; manual reconciliation is required: %w",
							metadataErr,
						))
						continue
					}
					ok("database:"+db.Name, "created exclusively and dump imported (db USERS are not migrated; repoint app configs)")
				}
			}
		}
	}

	status := "pending"
	finalCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), domainDNSPublicationTimeout)
	defer cancelFinalize()
	if complete {
		if err := setCpmoveImportStatus(finalCtx, p.db.GetDB(), domainID, siteID, "active"); err != nil {
			fail("finalize", err)
		} else {
			status = "active"
		}
	}
	if !complete {
		w.WriteHeader(http.StatusAccepted)
		p.audit(r, "import.cpanel.incomplete", "domain", domainID)
	} else {
		p.audit(r, "import.cpanel.complete", "domain", domainID)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"domain_id": domainID,
		"site_id":   siteID,
		"status":    status,
		"steps":     steps,
	})
}

// pushForwardingsToAgent syncs the full forwarding map to postfix (the map
// file is global, so the agent needs the complete list).
// pushForwardingsToAgent, tam yönlendirme haritasını postfix'e eşitler (map
// dosyası globaldir; agent tam listeyi ister).
func (p *Panel) pushForwardingsToAgentLegacy(ctx context.Context) error {
	return p.pushForwardingsToAgent(ctx)
	/*
		var all []transport.MailForwarding

		rows, err := p.db.GetDB().QueryContext(ctx, `SELECT source, destination FROM email_forwardings`)
		if err != nil {
			return
		}
		for rows.Next() {
			var f transport.MailForwarding
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
				var f transport.MailForwarding
				if caRows.Scan(&f.Source, &f.Destination) == nil {
					all = append(all, f)
				}
			}
			caRows.Close()
		}

		var done bool
		_ = p.callAgent("Agent.UpdateMailForwarding", &transport.UpdateMailForwardingRequest{
			Forwardings: all,
		}, &done)
	*/
}
