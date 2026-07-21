package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

// DomainPHPSettingsResponse represents PHP settings for a domain
type DomainPHPSettingsResponse struct {
	DomainID   int                 `json:"domain_id"`
	DomainName string              `json:"domain_name"`
	PHPVersion string              `json:"php_version"`
	PoolName   string              `json:"pool_name"`
	PoolConfig *core.PHPPoolConfig `json:"pool_config,omitempty"`
}

// UpdateDomainPHPRequest represents a request to update domain PHP settings
type UpdateDomainPHPRequest struct {
	PHPVersion string `json:"php_version"`
}

// handleDomainPHPSettings handles GET/POST for domain PHP settings
func (p *Panel) handleDomainPHPSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID from URL
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/php")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get domain
	domainRepo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domain, err := domainRepo.GetByID(context.Background(), domainID)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Get site info
	var siteID int
	var phpVersion string
	var phpFPMSocket *string
	query := `SELECT id, php_version, php_fpm_socket FROM sites WHERE domain_id = ? LIMIT 1`
	err = p.db.GetDB().QueryRowContext(context.Background(), query, domainID).Scan(&siteID, &phpVersion, &phpFPMSocket)
	if err != nil {
		http.Error(w, "Site not found for domain", http.StatusNotFound)
		return
	}

	// Determine pool name from socket or site ID
	poolName := fmt.Sprintf("site%d", siteID)
	if phpFPMSocket != nil && *phpFPMSocket != "" {
		// Extract pool name from socket path
		// e.g., /var/run/php/php8.3-fpm-site12.sock -> site12
		parts := strings.Split(*phpFPMSocket, "-")
		if len(parts) >= 2 {
			poolName = strings.TrimSuffix(parts[len(parts)-1], ".sock")
		}
	}

	if r.Method == "GET" {
		// Get pool config from agent
		var poolConfig core.PHPPoolConfig
		req := struct {
			Version  string `json:"version"`
			PoolName string `json:"pool_name"`
		}{
			Version:  phpVersion,
			PoolName: poolName,
		}

		err = p.agentClient.Call("Agent.GetPHPPoolConfig", req, &poolConfig)
		if err != nil {
			// A read must not write. This branch used to build a default pool
			// and POST it to the agent, so merely OPENING the PHP settings page
			// rewrote a root-loaded file under /etc/php — and it wrote
			// `user = <domain name>`, which is not a system user at all (the
			// real pool is created with the site's system user). php-fpm's
			// master refuses to start a pool whose user cannot be resolved, so
			// a GET could take down every site on that PHP version at the next
			// restart. Report the pool as absent instead; creating one is the
			// site-creation path's job, not a page load's.
			//
			// Bir okuma yazmamalıdır. Bu dal eskiden varsayılan bir havuz kurup
			// agent'a POST ediyordu; yani PHP ayarları sayfasını AÇMAK bile
			// /etc/php altında root'un yüklediği bir dosyayı yeniden yazıyordu —
			// üstelik `user = <alan adı>` yazıyordu ki bu hiçbir sistem
			// kullanıcısı değildir (gerçek havuz sitenin sistem kullanıcısıyla
			// oluşturulur). php-fpm master'ı, kullanıcısı çözülemeyen bir havuzu
			// başlatmayı reddeder; yani bir GET, bir sonraki yeniden başlatmada
			// o PHP sürümündeki tüm siteleri düşürebilirdi. Bunun yerine havuz
			// yok diye bildirilir; havuz oluşturmak sayfa yüklemenin değil, site
			// oluşturma yolunun işidir.
			json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
				DomainID:   domainID,
				DomainName: domain.Name,
				PHPVersion: phpVersion,
				PoolName:   poolName,
			})
			return
		}

		json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
			DomainID:   domainID,
			DomainName: domain.Name,
			PHPVersion: phpVersion,
			PoolName:   poolName,
			PoolConfig: &poolConfig,
		})
		return
	}

	if r.Method == "POST" {
		var req UpdateDomainPHPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Validate PHP version
		if req.PHPVersion == "" {
			http.Error(w, "PHP version is required", http.StatusBadRequest)
			return
		}

		// Only migrate if version is actually changing
		if req.PHPVersion != phpVersion {
			// Migrate pool from old version to new version
			migrateReq := struct {
				OldVersion string `json:"old_version"`
				NewVersion string `json:"new_version"`
				PoolName   string `json:"pool_name"`
			}{
				OldVersion: phpVersion,
				NewVersion: req.PHPVersion,
				PoolName:   poolName,
			}

			var migrateResp struct{}
			err = p.agentClient.Call("Agent.MigratePHPPool", migrateReq, &migrateResp)
			if err != nil {
				writeServerError(w, err)
				return
			}

			// Update php_fpm_socket with new version path
			newSocket := fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", req.PHPVersion, poolName)
			updateQuery := `UPDATE sites SET php_version = ?, php_fpm_socket = ?, updated_at = datetime('now') WHERE domain_id = ?`
			_, err = p.db.GetDB().ExecContext(context.Background(), updateQuery, req.PHPVersion, newSocket, domainID)
		} else {
			// Just update version in DB (no migration needed)
			updateQuery := `UPDATE sites SET php_version = ?, updated_at = datetime('now') WHERE domain_id = ?`
			_, err = p.db.GetDB().ExecContext(context.Background(), updateQuery, req.PHPVersion, domainID)
		}

		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "PHP version updated",
		})
	}
}

// handleDomainPHPPool handles GET/POST for domain-specific pool configuration
func (p *Panel) handleDomainPHPPool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID from URL
	domainID, err := extractDomainID(r.URL.Path, "/api/v1/domains/", "/php/pool")
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Get site info
	var phpVersion string
	var siteID int
	query := `SELECT id, php_version FROM sites WHERE domain_id = ? LIMIT 1`
	err = p.db.GetDB().QueryRowContext(context.Background(), query, domainID).Scan(&siteID, &phpVersion)
	if err != nil {
		http.Error(w, "Site not found for domain", http.StatusNotFound)
		return
	}

	poolName := fmt.Sprintf("site%d", siteID)

	if r.Method == "GET" {
		var poolConfig core.PHPPoolConfig
		req := struct {
			Version  string `json:"version"`
			PoolName string `json:"pool_name"`
		}{
			Version:  phpVersion,
			PoolName: poolName,
		}

		err = p.agentClient.Call("Agent.GetPHPPoolConfig", req, &poolConfig)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(poolConfig)
		return
	}

	if r.Method == "POST" {
		var req core.PHPPoolConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// The pool's identity is not the caller's to set, and saying so out loud
		// beats ignoring it. The agent already refuses to take identity from a
		// request (that is the layer which cannot be bypassed), but silently
		// dropping these fields would answer "success" to someone who asked for
		// something we did not do — the honesty rule forbids that, and it is
		// also how an attempt goes unnoticed. Refuse explicitly instead.
		//
		// Havuzun kimliğini belirlemek çağıranın işi değildir ve bunu açıkça
		// söylemek, yok saymaktan iyidir. Agent zaten kimliği istekten almayı
		// reddediyor (atlatılamayan katman odur), ama bu alanları sessizce
		// düşürmek, istediğini yapmadığımız birine "başarılı" demek olurdu —
		// dürüstlük kuralı bunu yasaklar ve bir deneme de böyle fark edilmez
		// kalır. Onun yerine açıkça reddet.
		if bad := callerSetPoolIdentity(&req.PoolConfig); bad != "" {
			writeCodedError(w, http.StatusBadRequest, errCodePoolIdentityFixed,
				"a pool's "+bad+" is set by the panel and cannot be changed — only the process-manager settings are adjustable",
				"")
			return
		}

		// Ensure version and pool name match the domain's site
		req.Version = phpVersion
		req.PoolConfig.Name = poolName

		var resp struct{}
		err = p.agentClient.Call("Agent.UpdatePHPPoolConfig", req, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Pool configuration updated",
		})
	}
}

// extractDomainID extracts domain ID from URL path
// e.g., /api/v1/domains/123/php -> 123
func extractDomainID(path, prefix, suffix string) (int, error) {
	// Remove prefix and suffix
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, suffix)

	// Extract ID
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid path")
	}

	return strconv.Atoi(parts[0])
}

// callerSetPoolIdentity names the first identity field a caller tried to set,
// or "" when the request only carries tunables. These fields decide WHO the
// pool runs as and WHICH socket it answers on — the two facts that make a pool
// a security boundary between tenants rather than a performance knob.
// callerSetPoolIdentity, çağıranın ayarlamaya çalıştığı ilk kimlik alanını
// adlandırır; istek yalnız ayar taşıyorsa "" döner. Bu alanlar havuzun KİM
// olarak koşacağına ve HANGİ sokete cevap vereceğine karar verir — bir havuzu
// performans düğmesi değil, kiracılar arası bir güvenlik sınırı yapan iki gerçek.
func callerSetPoolIdentity(c *core.PHPPoolConfig) string {
	switch {
	case c.User != "":
		return "user"
	case c.Group != "":
		return "group"
	case c.Listen != "":
		return "listen socket"
	case c.ListenOwner != "":
		return "listen.owner"
	case c.ListenGroup != "":
		return "listen.group"
	case c.ListenMode != "":
		return "listen.mode"
	}
	return ""
}
