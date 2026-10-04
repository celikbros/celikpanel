package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

const (
	panelAvailabilityPath   = "/api/v1/panel/availability"
	panelRecoveryStatusPath = "/api/v1/recovery/status"
)

func (p *Panel) panelAvailabilityState() string {
	if p.startupGate != nil && p.startupGate.ready.Load() {
		return "ready"
	}
	return "starting"
}

// Availability is only a startup observation, never permission to mutate.
// This fixed shape reveals no operation or server configuration to tenants.
// Hazır olma bilgisi değişiklik yetkisi değildir; kiracıya işlem ayrıntısı açmaz.
func (p *Panel) handlePanelAvailability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if currentCaller(r) == nil {
		writeCodedError(w, http.StatusUnauthorized, errCodeAuthRequired, "authentication required", "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Schema string `json:"schema"`
		State  string `json:"state"`
	}{"celikpanel-panel-availability/v1", p.panelAvailabilityState()})
}

// Read the exact native observation without Agent RPC or candidate version
// negotiation. The record cannot authorize a retry, unlock or new update.
// Tam yerel gözlem okunur; Agent RPC, yeni işlem veya kilit açma yapılmaz.
func (p *Panel) handleRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	p.serveRecoveryStatus(w, r, recoveryobs.Read)
}

// The injected reader keeps HTTP authorization tests isolated from host state.
func (p *Panel) serveRecoveryStatus(w http.ResponseWriter, r *http.Request, read func(string) recoveryobs.Status) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	caller := currentCaller(r)
	if caller == nil || !caller.hasAccountRole(roleAdmin) {
		writeCodedError(w, http.StatusForbidden, "admin_only", "administrator access required", "")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	ids := query["request_id"]
	if err != nil || len(r.URL.RawQuery) > 96 || len(query) != 1 || len(ids) != 1 || !recoveryobs.ValidRequestID(ids[0]) {
		writeCodedError(w, http.StatusBadRequest, "invalid_request", "a valid operation ID is required", "")
		return
	}
	status := read(ids[0])
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		recoveryobs.Status
		PanelState string `json:"panel_state"`
	}{status, p.panelAvailabilityState()})
}

func (p *Panel) registerRecoveryRoutes(mux *http.ServeMux) {
	mux.HandleFunc(panelAvailabilityPath, p.handlePanelAvailability)
	mux.HandleFunc(panelRecoveryStatusPath, p.handleRecoveryStatus)
}

// This mux is separate from the normal mux while that mux is being registered.
// Its exact method/path allowlist admits only existing authentication and small
// read surfaces. It never reaches host management, proxies or update starts.
// Açılış mux'ı ayrı ve sabittir; olağan yönetim veya yeni güncelleme başlatamaz.
func (p *Panel) startupRecoveryHandler(webRoot, certPath, keyPath string) http.Handler {
	mux := http.NewServeMux()
	p.registerRecoveryRoutes(mux)
	mux.HandleFunc("/api/v1/auth/login", p.handleLogin)
	mux.HandleFunc("/api/v1/auth/login/totp", p.handleLoginTOTP)
	mux.HandleFunc("/api/v1/auth/logout", p.handleLogout)
	mux.HandleFunc("/api/v1/auth/me", p.handleMe)
	mux.HandleFunc("/api/v1/auth/demo", p.handleDemoAccounts)
	mux.HandleFunc(panelLicenseAccessPath, p.handleLicenseAccess)
	mux.HandleFunc(panelAccessAddressPath, panelAccessAddressHandler(certPath, keyPath))
	authenticated := csrfProtect(p.requireAuth(mux))
	frontend := frontendHandler(webRoot)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := false
		switch r.Method {
		case http.MethodGet:
			switch r.URL.Path {
			case panelAvailabilityPath, panelRecoveryStatusPath, panelLicenseAccessPath,
				panelAccessAddressPath, "/api/v1/auth/me", "/api/v1/auth/demo":
				allowed = true
			}
		case http.MethodPost:
			switch r.URL.Path {
			case "/api/v1/auth/login", "/api/v1/auth/login/totp", "/api/v1/auth/logout":
				allowed = true
			}
		}
		if allowed {
			authenticated.ServeHTTP(w, r)
			return
		}
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			!strings.HasPrefix(r.URL.Path, "/api") &&
			!strings.HasPrefix(r.URL.Path, "/dbtool") && !strings.HasPrefix(r.URL.Path, "/webmail") {
			frontend.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "3")
		writeCodedError(w, http.StatusServiceUnavailable, "PANEL_STARTING",
			"Panel management is still starting. Read-only connection and recovery status remain available.", "")
	})
}
