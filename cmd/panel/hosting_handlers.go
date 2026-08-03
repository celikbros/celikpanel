package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Project-type hosting for a domain — roadmap 3A. A site now declares what
// it is (php / static / node / proxy / forwarding); node projects run as
// supervised systemd apps behind the web server.
//
// Bir domain için proje-tipli barındırma — yol haritası 3A. Bir site artık
// ne olduğunu bildirir (php / static / node / proxy / forwarding); node
// projeleri web sunucusunun arkasında gözetimli systemd uygulamaları olarak
// çalışır.

// validProjectTypes is enforced in code (the column has no CHECK on purpose
// so go/python can be added without a table rebuild).
// validProjectTypes kodda zorlanır (kolonda bilerek CHECK yok; go/python
// tablo yeniden inşası olmadan eklenebilsin).
var validProjectTypes = map[string]bool{
	"php": true, "static": true, "node": true, "proxy": true, "forwarding": true,
}

type hostingSettings struct {
	ProjectType    string `json:"project_type"`
	AppPort        int    `json:"app_port,omitempty"`
	StartCommand   string `json:"start_command,omitempty"`
	RuntimeVersion string `json:"runtime_version,omitempty"`
	ForwardTo      string `json:"forward_to,omitempty"`
	ForwardCode    int    `json:"forward_code,omitempty"`
	PHPVersion     string `json:"php_version,omitempty"`
	DocumentRoot   string `json:"document_root,omitempty"`
}

// handleDomainHosting serves GET/PUT /domains/{id}/hosting. Ownership is
// already enforced by the domain dispatcher.
// handleDomainHosting, GET/PUT /domains/{id}/hosting'i sunar. Sahiplik
// domain yönlendiricisinde zaten uygulanmıştır.
func (p *Panel) handleDomainHosting(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		var s hostingSettings
		var appPort, forwardCode *int
		var startCmd, runtimeVer, forwardTo *string
		err := p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT COALESCE(project_type,'php'), app_port, start_command, runtime_version,
			       forward_to, forward_code, php_version, document_root
			FROM sites WHERE domain_id = ?`, domainID).
			Scan(&s.ProjectType, &appPort, &startCmd, &runtimeVer, &forwardTo, &forwardCode, &s.PHPVersion, &s.DocumentRoot)
		if err != nil {
			writeClientError(w, http.StatusNotFound, "site not found")
			return
		}
		if appPort != nil {
			s.AppPort = *appPort
		}
		if forwardCode != nil {
			s.ForwardCode = *forwardCode
		}
		if startCmd != nil {
			s.StartCommand = *startCmd
		}
		if runtimeVer != nil {
			s.RuntimeVersion = *runtimeVer
		}
		if forwardTo != nil {
			s.ForwardTo = *forwardTo
		}
		json.NewEncoder(w).Encode(s)

	case http.MethodPut:
		p.handleUpdateHosting(w, r, domainID)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// applySiteVhost regenerates a domain's nginx vhost from its DB row — the
// one honest source after any change that alters what the vhost must say
// (PHP socket, project type). Born from a live catch (23 Jul): switching PHP
// versions moved the pool and updated the DB, but the vhost on disk kept
// pointing at the deleted old socket — the site answered 502 until a regen.
// Any handler that changes vhost-relevant site fields calls this afterwards.
// applySiteVhost, bir domain'in nginx vhost'unu DB satırından yeniden üretir —
// vhost'un söylemesi gerekeni değiştiren her değişiklikten sonraki tek dürüst
// kaynak (PHP soketi, proje tipi). Canlı bir yakalamadan doğdu (23 Tem): PHP
// sürümü değiştirmek havuzu taşıyıp DB'yi güncelledi ama diskteki vhost
// silinen eski sokete bakmaya devam etti — site regen'e dek 502 verdi.
// Vhost'u ilgilendiren site alanını değiştiren her handler sonrasında bunu
// çağırır.
func (p *Panel) applySiteVhost(ctx context.Context, domainID int) error {
	return p.applyVhostForDomain(ctx, domainID)
}

// allocateAppPort hands out the first unused local app port from 3001 up.
// allocateAppPort, 3001'den başlayarak kullanılmayan ilk yerel uygulama
// portunu verir.
func (p *Panel) allocateAppPort(ctx context.Context) (int, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT app_port FROM sites WHERE app_port IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	used := map[int]bool{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return 0, err
		}
		used[port] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for port := 3001; port < 4000; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, context.DeadlineExceeded
}

// handleDomainApp serves the supervised-app surface of a domain:
//
//	GET  /domains/{id}/app/status
//	GET  /domains/{id}/app/logs?lines=N
//	POST /domains/{id}/app/{start|stop|restart}
//
// handleDomainApp, bir domain'in gözetimli-uygulama yüzeyini sunar.
func (p *Panel) handleDomainApp(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")

	var siteID int
	var projectType string
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT id, COALESCE(project_type,'php') FROM sites WHERE domain_id = ?`, domainID).
		Scan(&siteID, &projectType)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "site not found")
		return
	}
	if projectType != "node" {
		writeClientError(w, http.StatusConflict, "this domain is not a node project")
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodGet:
		var st transport.AppStatusResponse
		if err := p.callAgent("Agent.AppUnitStatus", &transport.AppControlRequest{SiteID: siteID}, &st); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(st)

	case strings.HasSuffix(r.URL.Path, "/logs") && r.Method == http.MethodGet:
		lines := 100
		if l := r.URL.Query().Get("lines"); l != "" {
			json.Unmarshal([]byte(l), &lines) //nolint — best-effort int parse
		}
		var logs transport.AppLogsResponse
		if err := p.callAgent("Agent.AppUnitLogs", &transport.AppLogsRequest{SiteID: siteID, Lines: lines}, &logs); err != nil {
			writeServerError(w, err)
			return
		}
		if logs.Lines == nil {
			logs.Lines = []string{}
		}
		json.NewEncoder(w).Encode(logs)

	case r.Method == http.MethodPost:
		action := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		var resp transport.AppApplyResponse
		if err := p.callAgent("Agent.ControlAppUnit", &transport.AppControlRequest{SiteID: siteID, Action: action}, &resp); err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
