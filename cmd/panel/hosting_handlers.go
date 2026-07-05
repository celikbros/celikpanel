package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

func (p *Panel) handleUpdateHosting(w http.ResponseWriter, r *http.Request, domainID int) {
	var req hostingSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validProjectTypes[req.ProjectType] {
		writeClientError(w, http.StatusBadRequest, "project_type must be one of php, static, node, proxy, forwarding")
		return
	}

	// Type-specific validation, honest and specific.
	// Tipe özgü doğrulama; dürüst ve belirli.
	switch req.ProjectType {
	case "node":
		if strings.TrimSpace(req.StartCommand) == "" {
			writeClientError(w, http.StatusBadRequest, "start_command is required for node projects")
			return
		}
	case "forwarding":
		if !strings.HasPrefix(req.ForwardTo, "http://") && !strings.HasPrefix(req.ForwardTo, "https://") {
			writeClientError(w, http.StatusBadRequest, "forward_to must be an http(s) URL")
			return
		}
		if req.ForwardCode != 301 && req.ForwardCode != 302 {
			req.ForwardCode = 301
		}
	case "proxy":
		if !strings.HasPrefix(req.ForwardTo, "http://") && !strings.HasPrefix(req.ForwardTo, "https://") {
			writeClientError(w, http.StatusBadRequest, "forward_to (upstream URL) is required for proxy projects")
			return
		}
	}

	// node needs a local port; allocate the first free one when not given.
	// node yerel port ister; verilmemişse ilk boş portu tahsis et.
	if req.ProjectType == "node" && req.AppPort == 0 {
		port, err := p.allocateAppPort(r.Context())
		if err != nil {
			writeServerError(w, err)
			return
		}
		req.AppPort = port
	}

	var siteID int
	var docroot, oldType string
	var sslEnabled bool
	var phpSocket, sslCert, sslKey *string
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT id, document_root, COALESCE(project_type,'php'), ssl_enabled,
		        php_fpm_socket, ssl_cert_path, ssl_key_path
		 FROM sites WHERE domain_id = ?`, domainID).
		Scan(&siteID, &docroot, &oldType, &sslEnabled, &phpSocket, &sslCert, &sslKey)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "site not found")
		return
	}

	if _, err := p.db.GetDB().ExecContext(r.Context(), `
		UPDATE sites SET project_type = ?, app_port = ?, start_command = ?,
		       runtime_version = ?, forward_to = ?, forward_code = ?,
		       updated_at = datetime('now')
		WHERE id = ?`,
		req.ProjectType, nullIfZero(req.AppPort), nullIfEmpty(req.StartCommand),
		nullIfEmpty(req.RuntimeVersion), nullIfEmpty(req.ForwardTo), req.ForwardCode, siteID); err != nil {
		writeServerError(w, err)
		return
	}

	var domainName string
	_ = p.db.GetDB().QueryRowContext(r.Context(), `SELECT name FROM domains WHERE id = ?`, domainID).Scan(&domainName)

	// Supervised app lifecycle follows the type change.
	// Gözetimli uygulama yaşam döngüsü tip değişikliğini izler.
	if req.ProjectType == "node" {
		var resp struct {
			Unit  string `json:"unit"`
			Error string `json:"error,omitempty"`
		}
		err := p.agentClient.Call("Agent.ApplyAppUnit", &struct {
			SiteID      int    `json:"site_id"`
			Description string `json:"description"`
			WorkDir     string `json:"work_dir"`
			Command     string `json:"command"`
			Port        int    `json:"port"`
			NodeVersion string `json:"node_version"`
			RunAsUser   string `json:"run_as_user,omitempty"`
		}{
			SiteID:      siteID,
			Description: domainName,
			WorkDir:     docroot,
			Command:     req.StartCommand,
			Port:        req.AppPort,
			NodeVersion: req.RuntimeVersion,
			// Until per-site system users land, apps run as the web user in
			// production — never root.
			// Site başına sistem kullanıcıları gelene dek uygulamalar üretimde
			// web kullanıcısı olarak çalışır — asla root değil.
			RunAsUser: "www-data",
		}, &resp)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}
	} else if oldType == "node" {
		var resp struct {
			Unit  string `json:"unit"`
			Error string `json:"error,omitempty"`
		}
		_ = p.agentClient.Call("Agent.RemoveAppUnit", &struct {
			SiteID int    `json:"site_id"`
			Action string `json:"action"`
		}{SiteID: siteID}, &resp)
	}

	// Regenerate the vhost so nginx reflects the new project type. A
	// validation failure rolls the vhost back on the agent side; the settings
	// stay saved and the honest error reaches the caller.
	// Vhost'u yeniden üret; nginx yeni proje tipini yansıtsın. Doğrulama
	// hatasında agent vhost'u geri alır; ayarlar kayıtlı kalır ve dürüst hata
	// çağırana ulaşır.
	sslType := "none"
	if sslEnabled {
		sslType = "custom"
	}
	vhostReq := struct {
		SiteID       int    `json:"site_id"`
		Domain       string `json:"domain"`
		TempDomain   string `json:"temp_domain"`
		DocumentRoot string `json:"document_root"`
		PHPSocket    string `json:"php_socket"`
		SSLType      string `json:"ssl_type"`
		SSLCert      string `json:"ssl_cert"`
		SSLKey       string `json:"ssl_key"`
		ProjectType  string `json:"project_type"`
		AppPort      int    `json:"app_port"`
		ForwardTo    string `json:"forward_to"`
		ForwardCode  int    `json:"forward_code"`
	}{
		SiteID: siteID, Domain: domainName, DocumentRoot: docroot,
		SSLType: sslType, ProjectType: req.ProjectType,
		AppPort: req.AppPort, ForwardTo: req.ForwardTo, ForwardCode: req.ForwardCode,
	}
	if phpSocket != nil {
		vhostReq.PHPSocket = *phpSocket
	}
	if sslCert != nil {
		vhostReq.SSLCert = *sslCert
	}
	if sslKey != nil {
		vhostReq.SSLKey = *sslKey
	}

	var vhostResp struct {
		Config string `json:"config"`
		Error  string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.ApplyVhost", &vhostReq, &vhostResp); err != nil {
		writeServerError(w, err)
		return
	}
	if vhostResp.Error != "" {
		writeClientError(w, http.StatusConflict, vhostResp.Error)
		return
	}

	json.NewEncoder(w).Encode(map[string]any{"success": true, "app_port": req.AppPort})
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
		if rows.Scan(&port) == nil {
			used[port] = true
		}
	}
	for port := 3001; port < 4000; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, context.DeadlineExceeded
}

func nullIfZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
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
		var st struct {
			Exists   bool   `json:"exists"`
			Active   string `json:"active"`
			PID      int    `json:"pid"`
			MemoryMB int64  `json:"memory_mb"`
			CPUUsec  int64  `json:"cpu_usec"`
			Uptime   string `json:"uptime"`
		}
		if err := p.agentClient.Call("Agent.AppUnitStatus", &struct {
			SiteID int    `json:"site_id"`
			Action string `json:"action"`
		}{SiteID: siteID}, &st); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(st)

	case strings.HasSuffix(r.URL.Path, "/logs") && r.Method == http.MethodGet:
		lines := 100
		if l := r.URL.Query().Get("lines"); l != "" {
			json.Unmarshal([]byte(l), &lines) //nolint — best-effort int parse
		}
		var logs struct {
			Lines []string `json:"lines"`
			Error string   `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.AppUnitLogs", &struct {
			SiteID int `json:"site_id"`
			Lines  int `json:"lines"`
		}{SiteID: siteID, Lines: lines}, &logs); err != nil {
			writeServerError(w, err)
			return
		}
		if logs.Lines == nil {
			logs.Lines = []string{}
		}
		json.NewEncoder(w).Encode(logs)

	case r.Method == http.MethodPost:
		action := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		var resp struct {
			Unit  string `json:"unit"`
			Error string `json:"error,omitempty"`
		}
		if err := p.agentClient.Call("Agent.ControlAppUnit", &struct {
			SiteID int    `json:"site_id"`
			Action string `json:"action"`
		}{SiteID: siteID, Action: action}, &resp); err != nil {
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
