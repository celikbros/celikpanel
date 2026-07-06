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

	// A new service exists now; refresh the cached scan so every page keeps
	// reading from cache instead of probing.
	// Artık yeni bir servis var; önbellekteki taramayı tazele ki sayfalar
	// yoklama yapmak yerine önbellekten okumaya devam etsin.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("service scan after install %s: %v", req.ServiceID, err)
	}

	json.NewEncoder(w).Encode(map[string]any{"success": true, "installed": resp.Installed, "detail": resp.Detail})
}
