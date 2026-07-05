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
		// Query site info from database directly
		var phpVersion, sslType, projectType string

		query := `SELECT php_version, ssl_type, COALESCE(project_type,'php') FROM sites WHERE domain_id = ? LIMIT 1`
		err := p.db.GetDB().QueryRowContext(context.Background(), query, domain.ID).Scan(&phpVersion, &sslType, &projectType)
		if err != nil {
			// Default values if site not found
			phpVersion = "8.3"
			sslType = "none"
			projectType = "php"
		}

		response = append(response, DomainResponse{
			ID:          domain.ID,
			DomainName:  domain.Name,
			PHPVersion:  phpVersion,
			SSLEnabled:  sslType != "none",
			Status:      domain.Status,
			ProjectType: projectType,
			CreatedAt:   domain.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
			req.SubscriptionID = 1
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

	// Quota: one more domain must fit in the subscription.
	// Kota: aboneliğe bir domain daha sığmalı.
	if err := p.checkSubscriptionQuota(r.Context(), req.SubscriptionID, quotaDomains); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	// Default PHP version
	if req.PHPVersion == "" {
		req.PHPVersion = "8.3"
	}

	// Default SSL type
	if req.SSLType == "" {
		req.SSLType = "none"
	}

	// Default access method
	if req.AccessMethod == "" {
		req.AccessMethod = "sftp"
	}

	result, err := p.orchestrator.CreateSite(context.Background(), &req)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// DNS zone with the full default record set. Best-effort: the site
	// itself is already created, and the zone can always be (re)created from
	// the domain's DNS page. Imported domains never pass through here —
	// their records come from the archive.
	// Tam varsayılan kayıt setiyle DNS zone. En-iyi-çaba: sitenin kendisi
	// zaten oluştu ve zone, domain'in DNS sayfasından her zaman (yeniden)
	// oluşturulabilir. İçe aktarılan domain'ler buradan geçmez — kayıtları
	// arşivden gelir.
	if _, _, err := p.createZoneWithTemplate(r.Context(), req.Domain); err != nil {
		log.Printf("dns zone template for %s: %v", req.Domain, err)
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

	// Tear down the system side first — vhost, PHP pool, app unit, system
	// user, files. If that fails the ledger row stays, the honest error goes
	// out, and the delete can be retried; a row deleted while the site still
	// serves would hide real state.
	// Önce sistem tarafını sök — vhost, PHP havuzu, uygulama unit'i, sistem
	// kullanıcısı, dosyalar. Başarısız olursa defter kaydı kalır, dürüst hata
	// gider ve silme yeniden denenebilir; site sunulmaya devam ederken
	// silinen bir kayıt gerçek durumu gizlerdi.
	var siteID int
	var docroot, phpVersion string
	err = p.db.GetDB().QueryRowContext(context.Background(),
		`SELECT id, document_root, COALESCE(php_version,'') FROM sites WHERE domain_id = ?`,
		domainID).Scan(&siteID, &docroot, &phpVersion)
	if err == nil {
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

	// Drop the DNS zone too — a zone that keeps answering for a deleted
	// domain is stale, publicly visible state. Records first: the pdns
	// tables are not covered by SQLite's FK pragma guarantees here.
	// DNS zone'u da düşür — silinmiş bir domain için cevap vermeye devam
	// eden zone, bayat ve kamuya görünür durumdur. Önce kayıtlar: pdns
	// tabloları burada SQLite FK pragma garantisi altında değil.
	if _, err := p.db.GetDB().ExecContext(context.Background(),
		`DELETE FROM pdns_records WHERE domain_id IN (SELECT id FROM pdns_domains WHERE name = ?)`, domain.Name); err == nil {
		p.db.GetDB().ExecContext(context.Background(), `DELETE FROM pdns_domains WHERE name = ?`, domain.Name)
	}

	// TODO: Clean up nginx configs, PHP pools, etc. via Agent RPC
	// For now, just delete the database record

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
		"domain": domain.Name,
	})
}
