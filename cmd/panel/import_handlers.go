package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// inspectCpmove reads the archive on the server. Only the apply of an import
// asks for the mailboxes' password hashes (withMailHashes); a preview never
// does, so a preview never holds one.
// inspectCpmove arşivi sunucuda okur. Posta kutularının parola özetlerini
// yalnızca içe aktarımın uygulanması ister; önizleme asla istemez.
func (p *Panel) inspectCpmove(ctx context.Context, archivePath string, withMailHashes bool) (*cpmovePreview, error) {
	var preview cpmovePreview
	err := p.callAgent("Agent.InspectCpmove", &transport.CpmoveInspectRequest{
		ExpectedBuildCommit: strings.TrimSpace(buildCommit),
		Path:                archivePath,
		IncludeMailHashes:   withMailHashes,
	}, &preview)
	if err != nil {
		return nil, err
	}
	return &preview, nil
}

// importPreviewAnswer is everything the browser is told about an archive
// before an import. It is its own type, with every field named here, so that a
// field added to the Agent's answer does not reach the browser by itself
// (11 Oct 2026: the preview was the Agent's answer encoded as it came, and it
// carried each mailbox's password hash).
//
// A mailbox is its address, its quota and one fact about its password:
// whether the archive holds one that the import will keep. Nothing else.
//
// importPreviewAnswer, içe aktarımdan önce tarayıcıya arşiv hakkında söylenen
// her şeydir. Kendi türüdür: Agent yanıtına eklenen bir alan tarayıcıya
// kendiliğinden ulaşmaz. Bir posta kutusu adresi, kotası ve parolası hakkında
// tek bir bilgidir: arşivde içe aktarımın koruyacağı bir parola var mı.
type importPreviewAnswer struct {
	Username     string                                 `json:"username"`
	MainDomain   string                                 `json:"main_domain"`
	Domains      []string                               `json:"domains"`
	PublicHTML   bool                                   `json:"public_html"`
	SiteBytes    int64                                  `json:"site_bytes"`
	MailAccounts []importPreviewMailbox                 `json:"mail_accounts"`
	Forwarders   []transport.CpmoveForwarder            `json:"forwarders"`
	DNSZones     map[string][]transport.CpmoveDNSRecord `json:"dns_zones"`
	Databases    []transport.CpmoveDatabase             `json:"databases"`
}

type importPreviewMailbox struct {
	Domain      string `json:"domain"`
	User        string `json:"user"`
	QuotaMB     int    `json:"quota_mb"`
	HasPassword bool   `json:"has_password"`
}

func importPreviewFor(preview *cpmovePreview) importPreviewAnswer {
	answer := importPreviewAnswer{
		Username: preview.Username, MainDomain: preview.MainDomain,
		Domains: preview.Domains, PublicHTML: preview.PublicHTML, SiteBytes: preview.SiteBytes,
		MailAccounts: make([]importPreviewMailbox, 0, len(preview.MailAccounts)),
		Forwarders:   preview.Forwarders, DNSZones: preview.DNSZones, Databases: preview.Databases,
	}
	for _, account := range preview.MailAccounts {
		answer.MailAccounts = append(answer.MailAccounts, importPreviewMailbox{
			Domain: account.Domain, User: account.User, QuotaMB: account.QuotaMB,
			HasPassword: account.HasPassword || account.CryptHash != "",
		})
	}
	if answer.Domains == nil {
		answer.Domains = []string{}
	}
	if answer.Forwarders == nil {
		answer.Forwarders = []transport.CpmoveForwarder{}
	}
	if answer.DNSZones == nil {
		answer.DNSZones = map[string][]transport.CpmoveDNSRecord{}
	}
	if answer.Databases == nil {
		answer.Databases = []transport.CpmoveDatabase{}
	}
	return answer
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

	preview, err := p.inspectCpmove(r.Context(), req.Path, false)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if preview.Error != "" {
		writeClientError(w, http.StatusBadRequest, preview.Error)
		return
	}
	json.NewEncoder(w).Encode(importPreviewFor(preview))
}

type importStep struct {
	Step   string `json:"step"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Code   string `json:"code,omitempty"`
}

func safeImportDNSFailure(err error) (code, detail string) {
	var publicationErr *dnsAgentPublicationError
	if errors.As(err, &publicationErr) {
		return errCodeDNSPublicationFailed, "DNS publication failed"
	}
	return errCodeInternal, "DNS state could not be published"
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

	// The archive is read again on the server for the apply: the browser sends
	// the archive's path and the owner's choices, never what the preview held.
	// The mailboxes' password hashes are asked for only when mail is imported.
	// Arşiv, uygulama için sunucuda yeniden okunur: tarayıcı arşivin yolunu ve
	// sahibin seçimlerini gönderir, önizlemenin içeriğini asla göndermez.
	preview, err := p.inspectCpmove(r.Context(), req.Path, req.DoMail)
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
	failCoded := func(step, code, detail string) {
		complete = false
		steps = append(steps, importStep{
			Step: step, OK: false, Detail: detail, Code: code,
		})
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

	var importParentID *int
	remoteConnectionID := ""
	parentID, parentName, isSubdomain, err := p.resolveParentDomain(ctx, req.SubscriptionID, req.Domain)
	if err != nil {
		writeClientError(w, http.StatusConflict, "the parent domain is not available")
		return
	}
	if isSubdomain {
		inherited, err := p.domainDNSManagementMode(ctx, parentName)
		if err != nil {
			writeServerError(w, err)
			return
		}
		// Remote/external imports preserve the exact parent association.
		// The local importer keeps its pre-existing zone workflow.
		caps.DNSManagementMode = inherited
		if inherited != setupDNSModeLocal {
			importParentID = &parentID
		}
	}
	if caps.DNSManagementMode == setupDNSModeExisting {
		remoteConnectionID, err = p.remoteDNSConnectionForCreation(ctx, parentName)
		if err != nil {
			writeRemoteDNSUnavailable(w)
			return
		}
	}
	if caps.DNSManagementMode == setupDNSModeLocal && caps.DNSServer == "" {
		writeCodedError(w, http.StatusConflict, errCodeDNSServerRequired,
			"choose and activate a managed BIND or PowerDNS engine before importing a domain",
			"/settings?section=dns")
		return
	}
	if caps.DNSManagementMode == setupDNSModeLocal && !caps.DNSIdentityReady {
		writeCodedError(w, http.StatusConflict, errCodeDNSSettingsRequired,
			"DNS identity must be configured before importing a domain",
			"/settings?section=dns")
		return
	}
	if caps.DNSManagementMode == setupDNSModeExternal && req.DoDNS {
		writeCodedError(w, http.StatusConflict, errCodeExternalDNSManaged, "archive DNS records must be copied to your external provider; turn off DNS import to import the hosted resources", "")
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
		SubscriptionID:        req.SubscriptionID,
		Domain:                req.Domain,
		ProjectType:           "php",
		PHPVersion:            caps.PHPVersions[0],
		SSLType:               "none",
		AccessMethod:          "sftp",
		InitialStatus:         "pending",
		DNSManagement:         caps.DNSManagementMode,
		DNSRemoteConnectionID: remoteConnectionID,
		ParentDomainID:        importParentID,
	})
	cancelCreate()
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeClientError(w, http.StatusConflict, "domain already exists on this server")
			return
		}
		if refusal, ok := hostingRootNotTraversable(err); ok {
			writeHostingRootNotTraversable(w, refusal)
			return
		}
		if refusal, confirmed, ok := webServerRefusedConfig(err); ok {
			writeSiteWebServerRefused(w, currentCaller(r), req.Domain, refusal, confirmed, true)
			return
		}
		if _, stable := classifyStableAgentError(err); stable {
			writeServerError(w, err)
			return
		}
		// The site is the import's first step, and every other step comes
		// after it: when it fails, nothing of the archive was imported. That
		// much is verified and is said, instead of a bare 500 (measured on
		// Arch, 11 Oct 2026: every import answered "internal server error").
		// Site, içe aktarımın ilk adımıdır; başarısız olursa arşivden hiçbir
		// şey içe aktarılmamıştır. Çıplak bir 500 yerine bu söylenir.
		log.Printf("[502] cPanel import: the site for %s could not be created: %s",
			req.Domain, boundedAgentDiagnostic(err.Error()))
		writeCodedError(w, http.StatusBadGateway, errCodeImportSiteNotCreated, importSiteNotCreatedMessage, "")
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
			ok("files", fmt.Sprintf("%d files, %d bytes", ext.Files, ext.Bytes)+importOutsideSiteFolder(&ext))
			// A member the files step refused by its name was left out while
			// the rest was imported. It is a part of the archive that was not
			// imported, and is listed as one (12 Oct 2026; it used to be left
			// out without a word and the import answered `active`). It does
			// not keep the domain from being marked as finished: every part
			// that was chosen is there, and importing the archive again would
			// refuse the same member.
			// Dosya adımının adı yüzünden reddettiği üye, arşivin içe
			// aktarılmayan bir parçasıdır ve öyle listelenir. Alan adının
			// bitmiş diye işaretlenmesini engellemez.
			steps = append(steps, importRefusedMemberSteps(&ext)...)
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
			if len(acc.CryptHash) == 0 {
				// The archive names the mailbox and holds no password for it
				// (a suspended mailbox, for one). It is not created with a
				// password nobody chose.
				// Arşiv posta kutusunun adını verir, parolasını tutmaz.
				// Kimsenin seçmediği bir parolayla oluşturulmaz.
				fail("mail:"+email, errors.New(importMailboxWithoutPassword))
				continue
			}
			if len(acc.CryptHash) > 4096 ||
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
	if caps.DNSManagementMode == setupDNSModeExternal {
		ok("dns", "external DNS ownership preserved; verify provider records before publishing the site")
	} else if caps.DNSManagementMode == setupDNSModeExisting {
		source, hasImportedZone := cpmoveDNSRecordsForDomain(preview, req.Domain)
		publishCtx, cancelPublish := context.WithTimeout(context.WithoutCancel(ctx), domainDNSPublicationTimeout)
		var publishErr error
		if req.DoDNS && hasImportedZone {
			normalized, err := normalizeCpmoveDNSRecords(req.Domain, source)
			if err != nil {
				publishErr = err
			} else {
				records := make([]DNSRecord, 0, len(normalized))
				for _, record := range normalized {
					records = append(records, DNSRecord{Name: record.Name, Type: record.Type, Content: record.Content, TTL: record.TTL, Prio: record.Prio})
				}
				publishErr = p.setRemoteDomainDNSRecords(publishCtx, req.Domain, records)
			}
		} else {
			publishErr = p.ensureRemoteDomainDNS(publishCtx, req.Domain)
		}
		cancelPublish()
		if publishErr != nil {
			log.Printf("remote import DNS publication pending for %s: %v", req.Domain, publishErr)
			failCoded("dns", errCodeDNSPublicationPending, "The domain was imported; remote DNS publication remains pending. Retry publication from the domain DNS page.")
		} else {
			ok("dns", "DNS records published through the domain's connected authority")
		}
	} else {
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
			log.Printf(
				"cPanel import DNS publication %s: %s",
				req.Domain, boundedAgentDiagnostic(dnsErr.Error()),
			)
			code, detail := safeImportDNSFailure(dnsErr)
			failCoded("dns", code, detail)
		} else {
			ok("dns", dnsDetail)
		}

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

	finalCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), domainDNSPublicationTimeout)
	defer cancelFinalize()
	if complete {
		if err := setCpmoveImportStatus(finalCtx, p.db.GetDB(), domainID, siteID, "active"); err != nil {
			fail("finalize", err)
		}
	}
	answer := importApplyAnswerFor(req.Domain, domainID, siteID, steps)
	if answer.Status != importStatusComplete {
		p.audit(r, "import.cpanel.incomplete", "domain", domainID)
	} else {
		p.audit(r, "import.cpanel.complete", "domain", domainID)
	}
	_ = json.NewEncoder(w).Encode(answer)
}

const importMailboxWithoutPassword = "not imported: the archive holds no password for this mailbox"

// What the files step left out, in the answer (12 Oct 2026).
//
// importRefusedMemberAbsolute is the line of a member the archive names with
// an absolute path; the import screen has its own words for it.
const importRefusedMemberAbsolute = "not imported: the archive names this entry with an absolute path, and an import writes only below the site's own folder; nothing was written for it"

// importRefusedMemberSteps lists each refused member as a step that was not
// imported: `member:<name>`, at most transport.CpmoveRefusedMemberLimit of
// them, then one `members:<n>` for the n that are not listed. The count is
// never lost.
func importRefusedMemberSteps(ext *transport.CpmoveExtractResponse) []importStep {
	var steps []importStep
	listed := ext.Refused
	if len(listed) > transport.CpmoveRefusedMemberLimit {
		listed = listed[:transport.CpmoveRefusedMemberLimit]
	}
	for _, member := range listed {
		detail := "not imported: the files step refused this entry of the archive by its name; nothing was written for it"
		if member.Reason == transport.CpmoveRefusedAbsolutePath {
			detail = importRefusedMemberAbsolute
		}
		steps = append(steps, importStep{Step: "member:" + boundedAgentDiagnostic(member.Name), OK: false, Detail: detail})
	}
	if rest := ext.RefusedCount - len(listed); rest > 0 {
		steps = append(steps, importStep{
			Step: fmt.Sprintf("members:%d", rest), OK: false,
			Detail: fmt.Sprintf("not imported: %d more entries of the archive were refused by their names in the same way; %d in all", rest, ext.RefusedCount),
		})
	}
	return steps
}

// importOutsideSiteFolder says, on the files step's own line, how much of the
// archive is not below homedir/public_html and was therefore not copied by
// this step, and where it is. It is not a failure and not a part that is
// missing from the import: every cPanel archive holds the account's mail
// directories, the home directory's other folders and its metadata. The
// databases, mailboxes, forwarders and DNS records are read from their own
// entries by the steps that are listed for them.
func importOutsideSiteFolder(ext *transport.CpmoveExtractResponse) string {
	if ext.OutsideCount <= 0 {
		return ""
	}
	groups := make([]string, 0, len(ext.OutsideGroups))
	for index, group := range ext.OutsideGroups {
		if index >= transport.CpmoveOutsideGroupLimit+1 {
			break
		}
		groups = append(groups, fmt.Sprintf("%s (%d)", boundedAgentDiagnostic(group.Name), group.Count))
	}
	where := ""
	if len(groups) > 0 {
		where = ": " + strings.Join(groups, ", ")
	}
	return fmt.Sprintf(". %d other entries of the archive are outside the site folder (homedir/public_html) and are not copied by this step%s. "+
		"The databases, mailboxes, forwarders and DNS records are read from their own entries by their own steps; mailbox contents and the other folders of the home directory are not imported",
		ext.OutsideCount, where)
}

const (
	errCodeImportSiteNotCreated = "IMPORT_SITE_NOT_CREATED"
	importSiteNotCreatedMessage = "The import did not start: the site for this domain could not be created on this server, " +
		"so no file, mailbox, DNS record or database of the archive was imported. " +
		"Whether a part of the new site itself was left behind is not known from this answer: open Domains to see whether the domain is listed. " +
		"The server owner reads the step that failed on the server with sudo journalctl -u celikpanel-agent, corrects it, " +
		"and starts the import again; nothing starts it again automatically."
)

// The answer to an import whose every step ended (11 Oct 2026; D-024).
//
// An import that could not finish one of its parts used to answer `202` with
// `status: pending`, the words of work that is still going on. Nothing is
// going on: every step has ended, each with a verified result, and nothing
// runs a failed step again. Measured on Debian 13 and Ubuntu 24.04: the files
// step failed, the domain, the mailbox and the database were imported, and the
// answer read as if the site files were still on their way.
//
// So the answer is `200` with one of two results:
//
//   - `status: "active"`: every chosen part was imported and the domain is in
//     service;
//   - `status: "partial"`, `code: IMPORT_PARTIAL`: a verified partial result.
//     `imported` and `not_imported` name the parts, `message` says what stays
//     and what the owner can do, `domain_status` is the state the domain was
//     left in ("pending": created and kept, not marked as finished).
//
// `steps` is unchanged: one entry per step with its own line.
//
// Her adımı bitmiş bir içe aktarımın yanıtı. Bir parçasını bitiremeyen içe
// aktarım eskiden `202` ve `status: pending` ile, yani süren bir işin
// sözleriyle yanıtlanıyordu. Süren bir şey yoktur: her adım doğrulanmış bir
// sonuçla bitmiştir ve başarısız adımı hiçbir şey yeniden çalıştırmaz. Yanıt
// artık `200`'dür: ya `active` ya da hangi parçaların aktarıldığını ve
// hangilerinin aktarılmadığını adlarıyla söyleyen `partial`.
type importApplyAnswer struct {
	DomainID     int          `json:"domain_id"`
	SiteID       int          `json:"site_id"`
	Domain       string       `json:"domain"`
	Status       string       `json:"status"`
	DomainStatus string       `json:"domain_status"`
	Code         string       `json:"code,omitempty"`
	Message      string       `json:"message,omitempty"`
	Imported     []string     `json:"imported"`
	NotImported  []string     `json:"not_imported"`
	Steps        []importStep `json:"steps"`
}

const (
	importStatusComplete = "active"
	importStatusPartial  = "partial"
	errCodeImportPartial = "IMPORT_PARTIAL"
)

func importApplyAnswerFor(domain string, domainID, siteID int, steps []importStep) importApplyAnswer {
	answer := importApplyAnswer{
		DomainID: domainID, SiteID: siteID, Domain: domain,
		Status: importStatusComplete, DomainStatus: "active",
		Imported: []string{}, NotImported: []string{}, Steps: steps,
	}
	for _, step := range steps {
		if step.Step == "finalize" {
			// Not a part of the archive: the domain could not be marked as
			// finished. It is said in the message, not listed as a part.
			continue
		}
		if step.OK {
			answer.Imported = append(answer.Imported, step.Step)
		} else {
			answer.NotImported = append(answer.NotImported, step.Step)
		}
	}
	complete := true
	for _, step := range steps {
		if !step.OK {
			complete = false
		}
	}
	if complete {
		return answer
	}
	answer.Status, answer.Code = importStatusPartial, errCodeImportPartial
	if importOnlyRefusedMembers(steps) {
		// Every chosen part is there and the domain was marked as finished;
		// what is missing is entries the archive names in a way no import
		// places.
		answer.Message = importRefusedMembersMessage(domain, answer.Imported, answer.NotImported)
		return answer
	}
	answer.DomainStatus = "pending"
	answer.Message = importPartialMessage(domain, answer.Imported, answer.NotImported)
	return answer
}

// importOnlyRefusedMembers reports that the only steps that were not imported
// are archive members refused by their names.
func importOnlyRefusedMembers(steps []importStep) bool {
	refused := false
	for _, step := range steps {
		if step.OK {
			continue
		}
		if !strings.HasPrefix(step.Step, "member:") && !strings.HasPrefix(step.Step, "members:") {
			return false
		}
		refused = true
	}
	return refused
}

func importRefusedMembersMessage(domain string, imported, notImported []string) string {
	return "The import ended and every part that was chosen was imported; " + domain + " is in service. " +
		"Imported: " + strings.Join(imported, ", ") + ". " +
		"Not imported: " + strings.Join(notImported, ", ") + ". " +
		"These entries of the archive were refused by their names, and nothing was written for them; the reason of each is in its step below. " +
		"If one of them is a file the site needs, add it with the file manager of " + domain + ". " +
		"Importing the archive again refuses the same entries; nothing continues by itself."
}

func importPartialMessage(domain string, imported, notImported []string) string {
	list := func(parts []string) string {
		if len(parts) == 0 {
			return "none"
		}
		return strings.Join(parts, ", ")
	}
	missing := "Not imported: " + list(notImported) + ". "
	if len(notImported) == 0 {
		missing = "Every part was imported, but the domain could not be marked as finished. "
	}
	return "The import ended with a part of the archive not imported, and it does not continue by itself. " +
		"Imported: " + list(imported) + ". " + missing +
		"The domain " + domain + " was created and is kept; it is left marked as not finished. " +
		"The reason of each part that was not imported is in its step below. " +
		"The server owner either adds the missing parts by hand on the domain's own pages, " +
		"or removes " + domain + " on the Domains page, corrects what the step names and imports the archive again; " +
		"an import into a domain that already exists is refused, so nothing is imported twice."
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
