package main

import (
	"encoding/json"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// nginxInspect fetches the real, agent-parsed nginx configuration.
// nginxInspect, agent'ın ayrıştırdığı gerçek nginx yapılandırmasını getirir.
func (p *Panel) nginxInspect() (*core.NginxInspectResult, error) {
	var result core.NginxInspectResult
	if err := p.agentClient.Call("Agent.NginxInspect", &transport.Empty{}, &result); err != nil {
		return nil, err
	}
	if result.RateLimits == nil {
		result.RateLimits = []core.NginxRateLimit{}
	}
	return &result, nil
}

// handleNginxGlobalConfig serves the real global directives from `nginx -T`.
// Editing is not implemented yet, so POST is honest about that.
// handleNginxGlobalConfig, `nginx -T`'den gerçek global direktifleri sunar.
// Düzenleme henüz yapılmadı; POST bunu dürüstçe söyler.
func (p *Panel) handleNginxGlobalConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		writeClientError(w, http.StatusNotImplemented, "editing nginx config is not supported yet")
		return
	}
	result, err := p.nginxInspect()
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.Global)
}

// handleNginxSSLConfig serves the real SSL directives from `nginx -T`.
// handleNginxSSLConfig, `nginx -T`'den gerçek SSL direktiflerini sunar.
func (p *Panel) handleNginxSSLConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodPost {
		writeClientError(w, http.StatusNotImplemented, "editing nginx config is not supported yet")
		return
	}
	result, err := p.nginxInspect()
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.SSL)
}

// handleNginxRateLimits serves the real rate-limit zones from `nginx -T`.
// handleNginxRateLimits, `nginx -T`'den gerçek rate-limit bölgelerini sunar.
func (p *Panel) handleNginxRateLimits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	result, err := p.nginxInspect()
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(result.RateLimits)
}
