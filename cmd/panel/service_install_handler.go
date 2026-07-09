package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// handleServiceInstall installs a managed service on demand (admin-only via
// isAdminOnlyPath). The panel installs nothing at setup; the admin adds the
// services they actually want, and the agent installs exactly the whitelisted
// packages for this host's distro.
//
// handleServiceInstall, yönetilen bir servisi talep üzerine kurar
// (isAdminOnlyPath ile yalnız admin). Panel kurulumda hiçbir şey kurmaz;
// yönetici gerçekten istediği servisleri ekler ve agent bu makinenin
// dağıtımı için whitelist'teki paketleri kurar.
func (p *Panel) handleServiceInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServiceID string `json:"service_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}

	// Package installs can take a while (apt fetches + configures); the
	// agent runs it synchronously and reports the real outcome.
	// Paket kurulumları sürebilir (apt indirir + yapılandırır); agent bunu
	// senkron çalıştırır ve gerçek sonucu bildirir.
	var resp struct {
		Installed bool   `json:"installed"`
		Detail    string `json:"detail,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.InstallService", &struct {
		ID string `json:"id"`
	}{ID: req.ServiceID}, &resp); err != nil {
		writeServerError(w, err)
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	// Installing the DNS server is only half the job: point it at our
	// dedicated database and push the existing zones so it answers
	// immediately.
	// DNS sunucusunu kurmak işin yarısıdır: onu bize ayrılmış veritabanına
	// yönlendir ve hemen cevap versin diye mevcut zone'ları it.
	if req.ServiceID == "pdns" {
		var dnsResp struct {
			Synced bool   `json:"synced"`
			Error  string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigurePowerDNSSQLite", &struct{}{}, &dnsResp); err != nil || dnsResp.Error != "" {
			log.Printf("pdns configure after install: %v %s", err, dnsResp.Error)
		} else {
			p.syncAllZones(r.Context())
		}
	}

	// Installing the mail stack is likewise only half done until postfix and
	// dovecot are wired to the panel's virtual mailboxes.
	// Mail yığınını kurmak da, postfix ve dovecot panelin sanal posta
	// kutularına bağlanana dek yarım kalır.
	if req.ServiceID == "postfix" || req.ServiceID == "dovecot" {
		var mailResp struct {
			Configured bool   `json:"configured"`
			Error      string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ConfigureMailStack", &struct{}{}, &mailResp); err != nil || mailResp.Error != "" {
			log.Printf("mail stack configure after install: %v %s", err, mailResp.Error)
		}
	}

	// A new service exists now; refresh the cached scan so every page keeps
	// reading from cache instead of probing.
	// Artık yeni bir servis var; önbellekteki taramayı tazele ki sayfalar
	// yoklama yapmak yerine önbellekten okumaya devam etsin.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("service scan after install %s: %v", req.ServiceID, err)
	}

	// New service may expose new ports; if the firewall is on, open them.
	// Yeni servis yeni port açabilir; güvenlik duvarı açıksa onları aç.
	p.syncFirewall()
	p.audit(r, "service.install:"+req.ServiceID, "service", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "installed": resp.Installed, "detail": resp.Detail})
}

// handleServiceCandidate returns the version apt would install for a service
// (admin-only), so the install modal can show "what will land" honestly.
// handleServiceCandidate, apt'ın bir servis için kuracağı sürümü döndürür
// (yalnız admin); kurulum modalı "ne inecek"i dürüstçe gösterebilsin diye.
func (p *Panel) handleServiceCandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.URL.Query().Get("id")
	if id == "" {
		writeClientError(w, http.StatusBadRequest, "id is required")
		return
	}
	var version string
	_ = p.agentClient.Call("Agent.ServiceCandidateVersion",
		&struct {
			ID string `json:"id"`
		}{ID: id}, &version)
	json.NewEncoder(w).Encode(map[string]string{"version": version})
}

// handleServiceUninstall removes a managed service on demand (admin-only via
// isAdminOnlyPath) — the mirror of install, for shrinking the attack surface.
// Every installed service is exploitable code; taking one back off is a
// first-class action, not a manual SSH chore.
// handleServiceUninstall, yönetilen bir servisi talep üzerine kaldırır
// (isAdminOnlyPath ile yalnız admin) — kurulumun aynası, saldırı yüzeyini
// küçültmek için.
func (p *Panel) handleServiceUninstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ServiceID string `json:"service_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServiceID == "" {
		writeClientError(w, http.StatusBadRequest, "service_id is required")
		return
	}
	var resp struct {
		Removed bool   `json:"removed"`
		Detail  string `json:"detail,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.UninstallService",
		&struct {
			ID string `json:"id"`
		}{ID: req.ServiceID}, &resp); err != nil {
		writeAgentError(w, err, "service uninstall")
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	// Removed service's ports should close; re-sync the firewall.
	// Kaldırılan servisin portları kapanmalı; güvenlik duvarını yeniden senkronla.
	p.syncFirewall()
	p.audit(r, "service.uninstall:"+req.ServiceID, "service", 0)
	json.NewEncoder(w).Encode(map[string]any{"removed": resp.Removed, "detail": resp.Detail, "success": true})
}
