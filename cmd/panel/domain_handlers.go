package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// DomainResponse is the API response for domain listing
type DomainResponse struct {
	ID          int               `json:"id"`
	DomainName  string            `json:"domain_name"`
	PHPVersion  string            `json:"php_version"`
	SSLEnabled  bool              `json:"ssl_enabled"`
	Status      string            `json:"status"`
	ProjectType string            `json:"project_type"`
	CreatedAt   string            `json:"created_at"`
	DiskUsage   int64             `json:"disk_usage"`
	Bandwidth   int64             `json:"bandwidth"`
	ParentID    *int              `json:"parent_id,omitempty"`
	Access      map[string]string `json:"access,omitempty"`
}

const (
	errCodeDNSPublicationPending = "DNS_PUBLICATION_PENDING"
	domainCreateTimeout          = 2 * time.Minute
	domainDNSPublicationTimeout  = 30 * time.Second
)

// domainCreatePartialSuccess is deliberately an error-shaped response with
// enough success context for the client to leave the create flow. The domain
// and site already exist, so asking the user to submit Create again would only
// hit the duplicate-domain guard and strand them in the modal.
type domainCreatePartialSuccess struct {
	Error          string `json:"error"`
	Code           string `json:"code"`
	PartialSuccess bool   `json:"partial_success"`
	DomainID       int    `json:"domain_id"`
	Domain         string `json:"domain"`
	ZoneExists     bool   `json:"zone_exists"`
	Action         string `json:"action"`
}

func writeDomainCreatePartialSuccess(w http.ResponseWriter, domainID int, domain string, zoneExists bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(domainCreatePartialSuccess{
		Error:          "the domain was created, but its DNS setup is incomplete; review it in the domain's DNS management page",
		Code:           errCodeDNSPublicationPending,
		PartialSuccess: true,
		DomainID:       domainID,
		Domain:         domain,
		ZoneExists:     zoneExists,
		Action:         "/domains/" + url.PathEscape(domain) + "?tab=dns",
	})
}

// handleDomains lists all domains
func (p *Panel) handleDomains(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get domains
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domains, err := domainRepo.List(context.Background())
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Filter to the domains the caller owns (admins see all).
	// Çağıranın sahip olduğu domain'lere filtrele (yöneticiler hepsini görür).
	caller := currentCaller(r)
	var visibleOwners map[int]bool
	var visibleDomains map[int]bool
	var all bool
	if caller != nil && caller.isAdditionalUser() {
		visibleDomains, err = p.teamMemberVisibleDomainIDs(r.Context(), caller)
	} else {
		visibleOwners, all, err = p.visibleOwnerIDs(r.Context(), caller)
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Build response with proper field names for frontend
	response := make([]DomainResponse, 0, len(domains))
	for _, domain := range domains {
		var access map[string]string
		if caller != nil && caller.isAdditionalUser() {
			if !visibleDomains[domain.ID] {
				continue
			}
			capabilities, err := p.teamMemberEffectiveDomainCapabilities(r.Context(), caller, domain.ID)
			if errors.Is(err, errNotFound) {
				continue
			}
			if err != nil {
				writeServerError(w, err)
				return
			}
			access = teamMemberAccessResponse(capabilities)
		} else if !all {
			ownerID, err := p.domainOwnerID(r.Context(), domain.ID)
			if err != nil || !visibleOwners[ownerID] {
				continue
			}
		}
		// Query site info from database directly. Usage numbers are the
		// cached measurements — lists never probe the filesystem.
		// Site bilgisini doğrudan veritabanından sorgula. Kullanım sayıları
		// önbellekli ölçümlerdir — listeler asla dosya sistemini yoklamaz.
		var phpVersion, projectType string
		var sslEnabled bool
		var diskUsage, bandwidth int64

		query := `SELECT php_version, COALESCE(ssl_enabled, false), COALESCE(project_type,'php'),
			COALESCE(disk_usage_bytes,0), COALESCE(traffic_month_bytes,0)
			FROM sites WHERE domain_id = ? LIMIT 1`
		err := p.db.GetDB().QueryRowContext(context.Background(), query, domain.ID).
			Scan(&phpVersion, &sslEnabled, &projectType, &diskUsage, &bandwidth)
		if err != nil {
			// Default values if site not found
			phpVersion = "8.3"
			sslEnabled = false
			projectType = "php"
		}

		var parentID *int
		p.db.GetDB().QueryRowContext(context.Background(),
			`SELECT parent_domain_id FROM domains WHERE id = ?`, domain.ID).Scan(&parentID)

		response = append(response, DomainResponse{
			ID:          domain.ID,
			DomainName:  domain.Name,
			PHPVersion:  phpVersion,
			SSLEnabled:  sslEnabled,
			Status:      domain.Status,
			ProjectType: projectType,
			CreatedAt:   domain.CreatedAt.Format("2006-01-02T15:04:05Z"),
			DiskUsage:   diskUsage,
			Bandwidth:   bandwidth,
			ParentID:    parentID,
			Access:      access,
		})
	}

	json.NewEncoder(w).Encode(response)
}

// handleCreateDomain creates a new domain with site
func (p *Panel) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req services.CreateSiteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request")
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

	// Requirement preflight FIRST — before anything is created. What a domain
	// needs depends on the ROLE it plays here: a php site needs a web server +
	// PHP-FPM; a static site a web server; and (D-009) every domain needs a DNS
	// server, because this panel serves its own domains' DNS. Doing this before
	// resolving the subscription matters: a rejected request must leave NOTHING
	// behind — the admin-subscription bootstrap below used to run first, so a
	// blocked "add domain" still created a stray subscription (caught in alpha).
	// Gereksinim ön-denetimi İLK — hiçbir şey oluşturulmadan önce. Bir domain'in
	// neye ihtiyacı olduğu buradaki ROLE bağlıdır: php sitesi web sunucusu +
	// PHP-FPM; statik site web sunucusu; ve (D-009) her domain bir DNS sunucusu
	// ister; çünkü bu panel kendi domain'lerinin DNS'ini sunar. Bunu aboneliği
	// çözmeden ÖNCE yapmak önemli: reddedilen istek arkasında HİÇBİR ŞEY
	// bırakmamalı — aşağıdaki admin-abonelik bootstrap'i önce koşuyordu, bu
	// yüzden engellenen "domain ekle" yine de öksüz bir abonelik oluşturuyordu
	// (alfada yakalandı).
	if req.ProjectType == "" {
		req.ProjectType = "php"
	}
	if !services.CreationProjectTypes[req.ProjectType] {
		writeClientError(w, http.StatusBadRequest, "project_type must be php, static or dnsonly")
		return
	}
	caps, err := p.hostingCaps(r.Context())
	if err != nil {
		writeAgentError(w, err, "hosting capabilities")
		return
	}
	if caps.DNSServer == "" {
		writeCodedError(w, http.StatusConflict, errCodeDNSServerRequired,
			"no DNS server is installed — install PowerDNS or BIND from Services first; a domain cannot exist here without its zone being served",
			"/services")
		return
	}
	if !p.dnsIdentityConfigured(r.Context()) {
		writeCodedError(w, http.StatusConflict, errCodeDNSSettingsRequired,
			"DNS identity is not configured — save the shared nameserver names and operating mode under Settings before adding a domain",
			"/settings")
		return
	}
	if req.ProjectType == "php" || req.ProjectType == "static" {
		if caps.WebServer == "" {
			writeCodedError(w, http.StatusConflict, errCodeWebServerRequired,
				"no web server is installed — install Nginx or Apache from Services, or choose the DNS-only type",
				"/services")
			return
		}
		if req.ProjectType == "php" && len(caps.PHPVersions) == 0 {
			writeCodedError(w, http.StatusConflict, errCodePHPRequired,
				"PHP-FPM is not installed — install it from Services, or choose the static or DNS-only type",
				"/services")
			return
		}
	}

	// Resolve the target subscription and enforce ownership. Admins may
	// create under any subscription (default 1); everyone else must own the
	// subscription they create under.
	// Hedef aboneliği çöz ve sahipliği uygula. Yöneticiler herhangi bir
	// abonelik altında oluşturabilir (varsayılan 1); diğer herkes altında
	// oluşturduğu aboneliğin sahibi olmalıdır.
	caller := currentCaller(r)
	isAdmin := caller != nil && caller.Role == roleAdmin
	if req.SubscriptionID == 0 {
		if isAdmin {
			// Admin default: the admin's own subscription — created on first
			// use. A fresh install has NO subscriptions at all (the placeholder
			// admin and its seed subscription are dropped by migration 006), so
			// the old hard-coded "1" made the very first "add my domain" fail
			// with "subscription not found" — caught live on the Debian 13
			// golden path.
			// Admin varsayılanı: admin'in kendi aboneliği — ilk kullanımda
			// oluşturulur. Taze kurulumda HİÇ abonelik yoktur (yer-tutucu admin
			// ve seed aboneliği migration 006 ile silinir); eski sabit "1",
			// ilk "domain'imi ekle"yi "subscription not found" ile bozuyordu —
			// Debian 13 golden path'inde canlı yakalandı.
			err := p.db.GetDB().QueryRowContext(r.Context(),
				`SELECT id FROM subscriptions WHERE owner_id = ? ORDER BY id LIMIT 1`,
				caller.ID).Scan(&req.SubscriptionID)
			if err != nil {
				res, ierr := p.db.GetDB().ExecContext(r.Context(),
					`INSERT INTO subscriptions (owner_id, name, max_domains, max_databases, status)
					 VALUES (?, 'Admin Subscription', 999, 999, 'active')`, caller.ID)
				if ierr != nil {
					writeServerError(w, ierr)
					return
				}
				id, _ := res.LastInsertId()
				req.SubscriptionID = int(id)
				p.audit(r, "subscription.bootstrap", "subscription", int(id))
			}
		} else if caller != nil {
			// Smart default: fall back to the caller's own subscription so a
			// customer never has to know subscription IDs.
			// Akıllı varsayılan: çağıranın kendi aboneliğine düş; müşteri
			// abonelik kimliği bilmek zorunda kalmasın.
			err := p.db.GetDB().QueryRowContext(r.Context(),
				`SELECT id FROM subscriptions WHERE owner_id = ? ORDER BY id LIMIT 1`,
				caller.ID).Scan(&req.SubscriptionID)
			if err != nil {
				writeCodedError(w, http.StatusConflict, errCodeNoSubscription,
					"no subscription on this account; ask your provider to assign a plan", "")
				return
			}
		}
	}
	if !isAdmin {
		if err := p.canAccessSubscription(r.Context(), caller, req.SubscriptionID); err != nil {
			writeClientError(w, http.StatusForbidden, "subscription not found or not permitted")
			return
		}
	}

	// Quota: one more domain must fit, and the subscription must not already
	// be over its disk quota. Disk is a soft gate at creation — it refuses a
	// new site under an already-full account; it does not (yet) hard-stop a
	// running script from filling the disk (that needs OS filesystem quotas).
	// Kota: bir domain daha sığmalı ve abonelik disk kotasını çoktan aşmış
	// olmamalı. Disk, oluşturmada yumuşak bir kapıdır — zaten dolu bir hesap
	// altında yeni siteyi reddeder; çalışan bir betiğin diski doldurmasını
	// (henüz) sertçe durdurmaz (o, OS dosya sistemi kotası ister).
	if err := p.checkSubscriptionQuota(r.Context(), req.SubscriptionID, quotaDomains); err != nil {
		writeCodedError(w, http.StatusConflict, errCodeQuotaDomains, err.Error(), "")
		return
	}
	if err := p.checkSubscriptionQuota(r.Context(), req.SubscriptionID, quotaDisk); err != nil {
		writeCodedError(w, http.StatusConflict, errCodeQuotaDisk, err.Error(), "")
		return
	}

	// Default PHP version: whatever is actually installed on this host — each
	// distro ships a different one (Ubuntu 24.04 → 8.3, Debian 13 → 8.4), so a
	// constant here pointed pool files at a non-existent /etc/php/<ver> tree.
	// Varsayılan PHP sürümü: bu makinede gerçekten kurulu olan — her dağıtım
	// farklısını taşır (Ubuntu 24.04 → 8.3, Debian 13 → 8.4); buradaki sabit,
	// havuz dosyalarını var olmayan /etc/php/<ver> ağacına yöneltiyordu.
	if req.ProjectType == "php" && req.PHPVersion == "" {
		if req.PHPVersion = services.DetectInstalledPHPVersion(); req.PHPVersion == "" {
			req.PHPVersion = "8.3"
		}
	}
	if req.ProjectType != "php" {
		req.PHPVersion = ""
	}

	// Default SSL type
	if req.SSLType == "" {
		req.SSLType = "none"
	}

	// Default access method
	if req.AccessMethod == "" {
		req.AccessMethod = "sftp"
	}

	// Is this a subdomain of an existing domain in the same subscription? If
	// so it shares the site machinery but not the DNS: no new zone, just a
	// record in the parent's zone. Detected before site creation so we can
	// record the parent link.
	// Bu, aynı abonelikteki var olan bir domain'in subdomain'i mi? Öyleyse
	// site makinesini paylaşır ama DNS'i paylaşmaz: yeni zone yok, yalnız ana
	// domain'in zone'una bir kayıt. Ana domain bağını kaydedebilmek için site
	// oluşturmadan önce tespit edilir.
	parentID, parentName, isSubdomain, err := p.resolveParentDomain(
		r.Context(),
		req.SubscriptionID,
		req.Domain,
	)
	if err != nil {
		if errors.Is(err, errParentDomainUnavailable) {
			writeClientError(w, http.StatusConflict, "the parent domain is not active")
			return
		}
		writeServerError(w, err)
		return
	}
	if isSubdomain {
		req.ParentDomainID = &parentID
	}

	createCtx, cancelCreate := context.WithTimeout(r.Context(), domainCreateTimeout)
	result, err := p.orchestrator.CreateSite(createCtx, &req)
	cancelCreate()
	if err != nil {
		if hostname.IsNamespaceConflict(err) {
			writeClientError(w, http.StatusConflict, "this hostname is already used by a domain, its www name, or an alias")
			return
		}
		writeServerError(w, err)
		return
	}
	p.audit(r, "domain.create", "domain", result.DomainID)
	dnsCtx, cancelDNS := context.WithTimeout(
		context.WithoutCancel(r.Context()),
		domainDNSPublicationTimeout,
	)
	defer cancelDNS()

	if isSubdomain {
		// Link the child to its parent and add its address record to the
		// parent's zone — no separate zone for a subdomain.
		// Çocuğu ana domain'e bağla ve adres kaydını ana domain'in zone'una
		// ekle — subdomain için ayrı zone yok.
		dnsResult, err := p.addSubdomainToParentZone(
			dnsCtx,
			parentName,
			req.Domain,
		)
		if err != nil {
			log.Printf("subdomain DNS publication for %s through %s: %v", req.Domain, parentName, err)
			p.audit(r, "domain.create.dns_pending", "domain", result.DomainID)
			writeDomainCreatePartialSuccess(
				w,
				result.DomainID,
				req.Domain,
				dnsResult.LedgerReady,
			)
			return
		}
	} else {
		// The site already exists, so a DNS failure must not trigger a risky
		// rollback. Return its identity as an explicit partial success: the UI can
		// close Create, refresh the list and continue in this domain's management
		// screen instead of telling the operator to submit a duplicate Create.
		zoneExists, err := p.publishNewTopLevelDomainZone(dnsCtx, req.Domain)
		if err != nil {
			log.Printf("dns zone publication for %s: %v", req.Domain, err)
			writeDomainCreatePartialSuccess(w, result.DomainID, req.Domain, zoneExists)
			return
		}
	}

	json.NewEncoder(w).Encode(result)
}

func (p *Panel) publishNewTopLevelDomainZone(ctx context.Context, domain string) (bool, error) {
	if _, _, err := p.createZoneWithTemplate(ctx, domain); err != nil {
		return false, fmt.Errorf("create DNS zone: %w", err)
	}
	if err := p.syncZoneToDNS(ctx, domain, false); err != nil {
		return true, fmt.Errorf("publish DNS zone: %w", err)
	}
	return true, nil
}

// handleDeleteDomain deletes a domain
func (p *Panel) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "DELETE" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from URL path
	idStr := r.URL.Path[len("/api/v1/domains/"):]
	domainID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid domain ID", http.StatusBadRequest)
		return
	}
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	ctx, cancel := sslDurableContext(r.Context())
	defer cancel()

	// Get domain
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		http.Error(w, "domain not found", http.StatusNotFound)
		return
	}
	if err := p.ensureHSTSAllowsHostnameRemoval(ctx, domainID); err != nil {
		if message, guarded := hstsRemovalConflictMessage(err, "the domain"); guarded {
			writeClientError(w, http.StatusConflict, message)
		} else {
			writeServerError(w, err)
		}
		return
	}

	// A subdomain's DNS lives in its parent's zone, not a zone of its own —
	// resolve the parent so we remove the right record on delete.
	// Bir subdomain'in DNS'i kendi zone'unda değil ana domain'inin
	// zone'undadır — silmede doğru kaydı kaldırmak için ana domain'i çöz.
	var parentDomainID *int
	var parentDomainName string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT parent_domain_id FROM domains WHERE id = ?`, domainID).
		Scan(&parentDomainID); err != nil {
		writeServerError(w, fmt.Errorf("read domain parent identity: %w", err))
		return
	}
	if parentDomainID != nil {
		if err := p.db.GetDB().QueryRowContext(ctx,
			`SELECT name FROM domains WHERE id = ?`, *parentDomainID).
			Scan(&parentDomainName); err != nil {
			writeServerError(w, fmt.Errorf("read parent domain identity: %w", err))
			return
		}
	}

	var siteID, siteSubscriptionID int
	var docroot, phpVersion, projectType string
	hasSite := true
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT s.id, d.subscription_id, s.document_root,
		        COALESCE(s.php_version,''), COALESCE(s.project_type,'php')
		 FROM sites s
		 JOIN domains d ON d.id = s.domain_id
		 WHERE s.domain_id = ?`,
		domainID).Scan(&siteID, &siteSubscriptionID, &docroot, &phpVersion, &projectType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Historic DNS-only domains may predate the placeholder site row.
			// They have no OS resources to remove.
			hasSite = false
			projectType = "dnsonly"
		} else {
			writeServerError(w, err)
			return
		}
	}

	if hasSite && projectType != "dnsonly" {
		if err := hostingpath.ValidateDocumentRoot(
			docroot,
			siteSubscriptionID,
			domainID,
		); err != nil {
			writeServerError(w, fmt.Errorf("stored site identity is inconsistent: %w", err))
			return
		}
		if err := p.requireMatchingAgentBuild(ctx); err != nil {
			writeClientError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
	}

	// Capture agent-generated staged lineages before the cascading domain
	// delete removes the certificate ledger. Canonical legacy/domain lineages
	// are handled separately by the agent and never authorize global deletion.
	lineageRows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT DISTINCT lineage_name
		FROM ssl_certificates
		WHERE domain_id = ?
		  AND type = 'letsencrypt'
		  AND lineage_name IS NOT NULL`, domainID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	var (
		deleteCanonicalLineage bool
		stagedLineages         []string
	)
	canonicalDomainName := strings.ToLower(strings.TrimSpace(domain.Name))
	for lineageRows.Next() {
		var lineage string
		if err := lineageRows.Scan(&lineage); err != nil {
			lineageRows.Close()
			writeServerError(w, err)
			return
		}
		lineage = strings.ToLower(strings.TrimSpace(lineage))
		switch {
		case lineage == canonicalDomainName:
			deleteCanonicalLineage = true
		case strings.HasPrefix(lineage, "cp-site-"):
			stagedLineages = append(stagedLineages, lineage)
		default:
			lineageRows.Close()
			writeServerError(w, fmt.Errorf(
				"refuse unknown managed certificate lineage %q for domain %s",
				lineage,
				domain.Name,
			))
			return
		}
	}
	if err := lineageRows.Err(); err != nil {
		lineageRows.Close()
		writeServerError(w, err)
		return
	}
	lineageRows.Close()

	if err := p.markDomainDeletionPending(ctx, domainID); err != nil {
		writeServerError(w, err)
		return
	}

	_, err = p.prepareDomainMailTLSRemoval(ctx, domainID)
	if err != nil {
		if restoreErr := p.restoreDomainStatus(ctx, domainID, domain.Status); restoreErr != nil {
			p.writeDomainDeletionPending(
				w,
				r,
				domainID,
				domain.Name,
				"mail_tls_cleanup",
				fmt.Errorf("%v; restore domain status: %w", err, restoreErr),
			)
			return
		}
		writeServerError(w, err)
		return
	}

	// Tear down the system side first — vhost, PHP pool, app unit, system
	// user, files. If that fails the ledger row stays, the honest error goes
	// out, and the delete can be retried; a row deleted while the site still
	// serves would hide real state.
	// Önce sistem tarafını sök — vhost, PHP havuzu, uygulama unit'i, sistem
	// kullanıcısı, dosyalar. Başarısız olursa defter kaydı kalır, dürüst hata
	// gider ve silme yeniden denenebilir; site sunulmaya devam ederken
	// silinen bir kayıt gerçek durumu gizlerdi.
	// DNS-only domains have nothing on the OS (no user, no files, no vhost) —
	// there is nothing for the agent to tear down, and its path guard would
	// rightly refuse the empty docroot.
	// Yalnız-DNS domain'lerin OS'ta hiçbir şeyi yok (kullanıcı, dosya, vhost
	// yok) — agent'ın sökeceği bir şey yok; yol koruması boş docroot'u zaten
	// haklı olarak reddederdi.
	if hasSite && projectType != "dnsonly" {
		siteHome, err := hostingpath.SiteHome(siteSubscriptionID, domainID)
		if err != nil {
			writeServerError(w, fmt.Errorf("derive stored site home: %w", err))
			return
		}
		var agentResp transport.DeleteSiteResponse
		agentReq := transport.DeleteSiteRequest{
			ExpectedBuildCommit: strings.TrimSpace(buildCommit),
			SiteID:              siteID,
			SubscriptionID:      siteSubscriptionID,
			DomainID:            domainID,
			Domain:              domain.Name,
			Username:            services.SiteUsername(domain.Name),
			PHPVersion:          phpVersion,
			SiteHome:            siteHome,
		}
		if err := p.callAgentContext(ctx, "Agent.DeleteSite", &agentReq, &agentResp); err != nil {
			p.writeDomainDeletionPending(
				w, r, domainID, domain.Name, "site_cleanup", err,
			)
			return
		}
		if !agentResp.Success {
			p.writeDomainDeletionPending(
				w,
				r,
				domainID,
				domain.Name,
				"site_cleanup",
				fmt.Errorf("agent did not confirm complete site cleanup: %s", agentResp.Error),
			)
			return
		}
	}

	if err := p.removeDomainDNSForDeletion(ctx, domain.Name, parentDomainName); err != nil {
		p.writeDomainDeletionPending(
			w, r, domainID, domain.Name, "dns_cleanup", err,
		)
		return
	}
	if err := p.removeManagedDomainCertificates(
		ctx,
		domain.Name,
		deleteCanonicalLineage,
		stagedLineages,
	); err != nil {
		p.writeDomainDeletionPending(
			w, r, domainID, domain.Name, "certificate_cleanup", err,
		)
		return
	}
	if err := p.finalizeDomainDeletion(ctx, domainID); err != nil {
		p.writeDomainDeletionPending(
			w, r, domainID, domain.Name, "ledger_delete", err,
		)
		return
	}

	p.audit(r, "domain.delete:"+domain.Name, "domain", domainID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"domain": domain.Name,
	})
}
