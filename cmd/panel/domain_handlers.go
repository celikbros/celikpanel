package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
)

// DomainResponse is the API response for domain listing
type DomainResponse struct {
	ID          int    `json:"id"`
	DomainName  string `json:"domain_name"`
	PHPVersion  string `json:"php_version"`
	SSLEnabled  bool   `json:"ssl_enabled"`
	Status      string `json:"status"`
	ProjectType string `json:"project_type"`
	CreatedAt   string `json:"created_at"`
	DiskUsage   int64  `json:"disk_usage"`
	Bandwidth   int64  `json:"bandwidth"`
	ParentID    *int   `json:"parent_id,omitempty"`
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
	visible, all, err := p.visibleOwnerIDs(r.Context(), currentCaller(r))
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Build response with proper field names for frontend
	response := make([]DomainResponse, 0, len(domains))
	for _, domain := range domains {
		if !all {
			ownerID, err := p.domainOwnerID(r.Context(), domain.ID)
			if err != nil || !visible[ownerID] {
				continue
			}
		}
		// Query site info from database directly. Usage numbers are the
		// cached measurements — lists never probe the filesystem.
		// Site bilgisini doğrudan veritabanından sorgula. Kullanım sayıları
		// önbellekli ölçümlerdir — listeler asla dosya sistemini yoklamaz.
		var phpVersion, sslType, projectType string
		var diskUsage, bandwidth int64

		query := `SELECT php_version, ssl_type, COALESCE(project_type,'php'),
			COALESCE(disk_usage_bytes,0), COALESCE(traffic_month_bytes,0)
			FROM sites WHERE domain_id = ? LIMIT 1`
		err := p.db.GetDB().QueryRowContext(context.Background(), query, domain.ID).
			Scan(&phpVersion, &sslType, &projectType, &diskUsage, &bandwidth)
		if err != nil {
			// Default values if site not found
			phpVersion = "8.3"
			sslType = "none"
			projectType = "php"
		}

		var parentID *int
		p.db.GetDB().QueryRowContext(context.Background(),
			`SELECT parent_domain_id FROM domains WHERE id = ?`, domain.ID).Scan(&parentID)

		response = append(response, DomainResponse{
			ID:          domain.ID,
			DomainName:  domain.Name,
			PHPVersion:  phpVersion,
			SSLEnabled:  sslType != "none",
			Status:      domain.Status,
			ProjectType: projectType,
			CreatedAt:   domain.CreatedAt.Format("2006-01-02T15:04:05Z"),
			DiskUsage:   diskUsage,
			Bandwidth:   bandwidth,
			ParentID:    parentID,
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
	caps := p.hostingCaps()
	if caps.DNSServer == "" {
		writeClientError(w, http.StatusConflict,
			"no DNS server is installed — install PowerDNS or BIND from Services first; a domain cannot exist here without its zone being served")
		return
	}
	if req.ProjectType == "php" || req.ProjectType == "static" {
		if caps.WebServer == "" {
			writeClientError(w, http.StatusConflict,
				"no web server is installed — install Nginx or Apache from Services, or choose the DNS-only type")
			return
		}
		if req.ProjectType == "php" && len(caps.PHPVersions) == 0 {
			writeClientError(w, http.StatusConflict,
				"PHP-FPM is not installed — install it from Services, or choose the static or DNS-only type")
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
				writeClientError(w, http.StatusConflict, "no subscription on this account; ask your provider to assign a plan")
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
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}
	if err := p.checkSubscriptionQuota(r.Context(), req.SubscriptionID, quotaDisk); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
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
	parentID, parentName, isSubdomain := p.resolveParentDomain(r.Context(), req.SubscriptionID, req.Domain)

	result, err := p.orchestrator.CreateSite(context.Background(), &req)
	if err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "domain.create", "domain", result.DomainID)

	if isSubdomain {
		// Link the child to its parent and add its address record to the
		// parent's zone — no separate zone for a subdomain.
		// Çocuğu ana domain'e bağla ve adres kaydını ana domain'in zone'una
		// ekle — subdomain için ayrı zone yok.
		p.db.GetDB().ExecContext(r.Context(),
			`UPDATE domains SET parent_domain_id = ? WHERE id = ?`, parentID, result.DomainID)
		p.addSubdomainToParentZone(r.Context(), parentName, req.Domain)
	} else {
		// DNS zone with the full default record set. Best-effort: the site
		// itself is already created, and the zone can always be (re)created
		// from the domain's DNS page. Imported domains never pass through
		// here — their records come from the archive.
		// Tam varsayılan kayıt setiyle DNS zone. En-iyi-çaba: sitenin kendisi
		// zaten oluştu ve zone, domain'in DNS sayfasından her zaman (yeniden)
		// oluşturulabilir. İçe aktarılan domain'ler buradan geçmez.
		if _, _, err := p.createZoneWithTemplate(r.Context(), req.Domain); err != nil {
			log.Printf("dns zone template for %s: %v", req.Domain, err)
		} else {
			p.syncZoneToDNS(r.Context(), req.Domain, false)
		}
	}

	json.NewEncoder(w).Encode(result)
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

	// Get domain
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domain, err := domainRepo.GetByID(context.Background(), domainID)
	if err != nil {
		http.Error(w, "domain not found", http.StatusNotFound)
		return
	}

	// A subdomain's DNS lives in its parent's zone, not a zone of its own —
	// resolve the parent so we remove the right record on delete.
	// Bir subdomain'in DNS'i kendi zone'unda değil ana domain'inin
	// zone'undadır — silmede doğru kaydı kaldırmak için ana domain'i çöz.
	var parentDomainID *int
	var parentDomainName string
	p.db.GetDB().QueryRowContext(context.Background(),
		`SELECT parent_domain_id FROM domains WHERE id = ?`, domainID).Scan(&parentDomainID)
	if parentDomainID != nil {
		p.db.GetDB().QueryRowContext(context.Background(),
			`SELECT name FROM domains WHERE id = ?`, *parentDomainID).Scan(&parentDomainName)
	}

	// Tear down the system side first — vhost, PHP pool, app unit, system
	// user, files. If that fails the ledger row stays, the honest error goes
	// out, and the delete can be retried; a row deleted while the site still
	// serves would hide real state.
	// Önce sistem tarafını sök — vhost, PHP havuzu, uygulama unit'i, sistem
	// kullanıcısı, dosyalar. Başarısız olursa defter kaydı kalır, dürüst hata
	// gider ve silme yeniden denenebilir; site sunulmaya devam ederken
	// silinen bir kayıt gerçek durumu gizlerdi.
	var siteID int
	var docroot, phpVersion, projectType string
	err = p.db.GetDB().QueryRowContext(context.Background(),
		`SELECT id, document_root, COALESCE(php_version,''), COALESCE(project_type,'php') FROM sites WHERE domain_id = ?`,
		domainID).Scan(&siteID, &docroot, &phpVersion, &projectType)
	// DNS-only domains have nothing on the OS (no user, no files, no vhost) —
	// there is nothing for the agent to tear down, and its path guard would
	// rightly refuse the empty docroot.
	// Yalnız-DNS domain'lerin OS'ta hiçbir şeyi yok (kullanıcı, dosya, vhost
	// yok) — agent'ın sökeceği bir şey yok; yol koruması boş docroot'u zaten
	// haklı olarak reddederdi.
	if err == nil && projectType != "dnsonly" {
		var agentResp struct {
			Success bool   `json:"success"`
			Error   string `json:"error,omitempty"`
		}
		agentReq := struct {
			SiteID     int    `json:"site_id"`
			Domain     string `json:"domain"`
			Username   string `json:"username"`
			PHPVersion string `json:"php_version"`
			SiteHome   string `json:"site_home"`
		}{
			SiteID:     siteID,
			Domain:     domain.Name,
			Username:   services.SiteUsername(domain.Name),
			PHPVersion: phpVersion,
			SiteHome:   filepath.Dir(docroot),
		}
		if err := p.agentClient.Call("Agent.DeleteSite", &agentReq, &agentResp); err != nil {
			writeServerError(w, err)
			return
		}
		if !agentResp.Success {
			writeClientError(w, http.StatusConflict, "site cleanup failed: "+agentResp.Error)
			return
		}
	}

	// Delete domain (cascades to sites via foreign key)
	if err := domainRepo.Delete(context.Background(), domainID); err != nil {
		writeServerError(w, err)
		return
	}

	if parentDomainName != "" {
		// Subdomain: pull its record out of the parent's zone; the parent
		// zone itself stays (it serves the parent and any siblings).
		// Subdomain: kaydını ana domain'in zone'undan çıkar; ana zone
		// (kendisine ve kardeşlerine hizmet ettiği için) kalır.
		p.removeSubdomainFromParentZone(context.Background(), parentDomainName, domain.Name)
	} else {
		// Top-level domain: drop its whole zone — a zone that keeps answering
		// for a deleted domain is stale, publicly visible state. Records
		// first: the pdns tables are not covered by SQLite's FK pragma here.
		// Tepe-seviye domain: tüm zone'unu düşür — silinmiş bir domain için
		// cevap vermeye devam eden zone bayat, kamuya görünür durumdur.
		if _, err := p.db.GetDB().ExecContext(context.Background(),
			`DELETE FROM pdns_records WHERE domain_id IN (SELECT id FROM pdns_domains WHERE name = ?)`, domain.Name); err == nil {
			p.db.GetDB().ExecContext(context.Background(), `DELETE FROM pdns_domains WHERE name = ?`, domain.Name)
		}
		p.syncZoneToDNS(context.Background(), domain.Name, true)
	}

	// The domain's Let's Encrypt lineage must not outlive the domain: a
	// renewal config for a dead name fails on EVERY certbot run and poisons
	// the whole renewal report. Caught live (Jul 17): deleted
	// celikpanel.cloud's leftover config turned the renewal dry-run into
	// "1 failure". Best-effort — a cert cleanup hiccup must not block the
	// delete that already happened.
	// Domain'in Let's Encrypt soyu domain'den uzun yaşamamalı: ölü bir ad
	// için duran yenileme yapılandırması HER certbot koşusunda başarısız olur
	// ve tüm yenileme raporunu zehirler. Canlıda yakalandı (17 Tem): silinen
	// celikpanel.cloud'un kalıntısı yenileme provasını "1 failure" yaptı.
	// Elden-geldiğince — sertifika temizliği aksaması, olmuş silmeyi engellemez.
	var certResp struct {
		Deleted bool   `json:"deleted"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.DeleteCertLineage", &struct {
		Domain string `json:"domain"`
	}{Domain: domain.Name}, &certResp); err != nil || certResp.Error != "" {
		log.Printf("cert lineage cleanup for %s: %v %s", domain.Name, err, certResp.Error)
	}

	p.audit(r, "domain.delete:"+domain.Name, "domain", domainID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"domain": domain.Name,
	})
}
