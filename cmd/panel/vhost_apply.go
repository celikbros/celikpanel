package main

import (
	"context"
	"errors"
)

// applyVhostForDomain regenerates a domain's nginx vhost from the current
// database state via the agent (write → validate → reload). Every flow that
// changes what nginx should serve — SSL install, project type, forwarding —
// must end with this call, otherwise the change exists only in the ledger.
// applyVhostForDomain, bir domain'in nginx vhost'unu güncel veritabanı
// durumundan agent aracılığıyla yeniden üretir (yaz → doğrula → yeniden
// yükle). nginx'in sunacağı şeyi değiştiren her akış — SSL kurulumu, proje
// tipi, yönlendirme — bu çağrıyla bitmeli; yoksa değişiklik yalnız defterde
// kalır.
func (p *Panel) applyVhostForDomain(ctx context.Context, domainID int) error {
	var (
		siteID                       int
		domainName, docroot          string
		phpSocket, certPath, keyPath *string
		sslEnabled                   bool
		projectType                  string
		appPort, forwardCode         *int
		forwardTo                    *string
	)
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.id, d.name, s.document_root, s.php_fpm_socket,
		       s.ssl_enabled, s.ssl_cert_path, s.ssl_key_path,
		       COALESCE(s.project_type,'php'), s.app_port, s.forward_to, s.forward_code
		FROM sites s JOIN domains d ON d.id = s.domain_id
		WHERE s.domain_id = ?`, domainID).
		Scan(&siteID, &domainName, &docroot, &phpSocket,
			&sslEnabled, &certPath, &keyPath,
			&projectType, &appPort, &forwardTo, &forwardCode)
	if err != nil {
		return err
	}

	req := struct {
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
		SSLType: "none", ProjectType: projectType,
	}
	if phpSocket != nil {
		req.PHPSocket = *phpSocket
	}
	if sslEnabled && certPath != nil && keyPath != nil && *certPath != "" && *keyPath != "" {
		req.SSLType = "custom"
		req.SSLCert = *certPath
		req.SSLKey = *keyPath
	}
	if appPort != nil {
		req.AppPort = *appPort
	}
	if forwardTo != nil {
		req.ForwardTo = *forwardTo
	}
	if forwardCode != nil {
		req.ForwardCode = *forwardCode
	}

	var resp struct {
		Config string `json:"config"`
		Error  string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.ApplyVhost", &req, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}
