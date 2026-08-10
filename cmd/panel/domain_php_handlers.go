package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// DomainPHPSettingsResponse represents PHP settings for a domain
type DomainPHPSettingsResponse struct {
	DomainID          int                 `json:"domain_id"`
	DomainName        string              `json:"domain_name"`
	PHPVersion        string              `json:"php_version"`
	AvailableVersions []string            `json:"available_versions"`
	PoolName          string              `json:"pool_name"`
	PoolConfig        *core.PHPPoolConfig `json:"pool_config,omitempty"`
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
		availableVersions := domainPHPAvailableVersions(phpVersion, nil)
		discoveredVersions, discoveryErr := p.phpVersionsFromAgent(r.Context())
		if discoveryErr != nil {
			// The page already had a tolerant read contract for a missing pool
			// configuration. Keep that contract when discovery is temporarily
			// unavailable: the site's stored current version is known and is the
			// only honest fallback, while the internal failure remains visible in
			// the server log. Never invent another version.
			log.Printf("discover PHP versions for domain %d: %v", domainID, discoveryErr)
		} else {
			availableVersions = domainPHPAvailableVersions(phpVersion, discoveredVersions)
		}

		// Get pool config from agent
		var poolConfig core.PHPPoolConfig
		req := transport.GetPHPPoolConfigRequest{
			Version:  phpVersion,
			PoolName: poolName,
		}

		err = p.callAgent("Agent.GetPHPPoolConfig", req, &poolConfig)
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
				DomainID:          domainID,
				DomainName:        domain.Name,
				PHPVersion:        phpVersion,
				AvailableVersions: availableVersions,
				PoolName:          poolName,
			})
			return
		}

		json.NewEncoder(w).Encode(DomainPHPSettingsResponse{
			DomainID:          domainID,
			DomainName:        domain.Name,
			PHPVersion:        phpVersion,
			AvailableVersions: availableVersions,
			PoolName:          poolName,
			PoolConfig:        &poolConfig,
		})
		return
	}

	if r.Method == "POST" {
		var req UpdateDomainPHPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Validate the closed version shape before asking the agent or building
		// a socket path. Then require the exact managed version reported by the
		// agent; even a no-op switch to the stored version fails closed when that
		// version is no longer discoverable.
		if err := services.ValidatePHPVersion(req.PHPVersion); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid PHP version")
			return
		}
		discoveredVersions, err := p.phpVersionsFromAgent(r.Context())
		if err != nil {
			writeAgentError(w, err, "PHP version discovery")
			return
		}
		if !containsDomainPHPVersion(
			domainPHPAvailableVersions("", discoveredVersions),
			req.PHPVersion,
		) {
			writeClientError(w, http.StatusBadRequest, "PHP version is not available")
			return
		}

		// Only migrate if version is actually changing
		if req.PHPVersion != phpVersion {
			// Migrate pool from old version to new version
			migrateReq := transport.MigratePHPPoolRequest{
				OldVersion: phpVersion,
				NewVersion: req.PHPVersion,
				PoolName:   poolName,
			}

			var migrateResp transport.Empty
			err = p.callAgent("Agent.MigratePHPPool", migrateReq, &migrateResp)
			if err != nil {
				writeServerError(w, err)
				return
			}

			// Update php_fpm_socket with new version path
			newSocket := fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", req.PHPVersion, poolName)
			updateQuery := `UPDATE sites SET php_version = ?, php_fpm_socket = ?, updated_at = datetime('now') WHERE domain_id = ?`
			_, err = p.db.GetDB().ExecContext(context.Background(), updateQuery, req.PHPVersion, newSocket, domainID)

			// The vhost must follow the socket. Caught live (23 Jul): the
			// pool moved, the DB updated, and nginx kept proxying to the
			// DELETED old socket — the site answered 502 while everything
			// reported success. A failed regen is a failed request: the
			// operator must know the site is dark, not read "updated".
			// Vhost soketi izlemek zorundadır. Canlıda yakalandı (23 Tem):
			// havuz taşındı, DB güncellendi ve nginx SİLİNEN eski sokete
			// vekillik etmeye devam etti — her şey başarı bildirirken site
			// 502 veriyordu. Başarısız regen başarısız istektir: operatör
			// "güncellendi" değil, sitenin karanlık olduğunu okumalı.
			if err == nil {
				if verr := p.applySiteVhost(r.Context(), domainID); verr != nil {
					log.Printf("vhost regen after php switch (domain %d): %v", domainID, verr)
					writeServerError(w, verr)
					return
				}
			}
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

// domainPHPAvailableVersions closes the picker contract over validated
// major.minor versions, removes duplicate agent rows and sorts newest first.
// The stored current version is included only when it has the same safe shape;
// it is known domain state, not a guessed installed version.
func domainPHPAvailableVersions(current string, discovered []string) []string {
	seen := make(map[string]struct{}, len(discovered)+1)
	versions := make([]string, 0, len(discovered)+1)
	add := func(version string) {
		if services.ValidatePHPVersion(version) != nil {
			return
		}
		if _, exists := seen[version]; exists {
			return
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	for _, version := range discovered {
		add(version)
	}
	add(current)
	sort.Slice(versions, func(i, j int) bool {
		leftMajor, leftMinor := domainPHPVersionParts(versions[i])
		rightMajor, rightMinor := domainPHPVersionParts(versions[j])
		if leftMajor != rightMajor {
			return leftMajor > rightMajor
		}
		if leftMinor != rightMinor {
			return leftMinor > rightMinor
		}
		return versions[i] < versions[j]
	})
	return versions
}

func domainPHPVersionParts(version string) (int, int) {
	major, minor, _ := strings.Cut(version, ".")
	majorNumber, _ := strconv.Atoi(major)
	minorNumber, _ := strconv.Atoi(minor)
	return majorNumber, minorNumber
}

func containsDomainPHPVersion(versions []string, target string) bool {
	for _, version := range versions {
		if version == target {
			return true
		}
	}
	return false
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

		err = p.callAgent("Agent.GetPHPPoolConfig", req, &poolConfig)
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

		var resp transport.Empty
		err = p.callAgent("Agent.UpdatePHPPoolConfig", req, &resp)
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
