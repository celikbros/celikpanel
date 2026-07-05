package main

import (
	"fmt"

	"github.com/alicelik/celikpanel/internal/services"
)

// ApplyVhost regenerates a domain's nginx vhost from explicit data (used
// when the project type or hosting settings change). Write → validate →
// reload; a config that fails `nginx -t` is rolled back so nginx never
// stays broken.
//
// ApplyVhost, bir domain'in nginx vhost'unu açık verilerden yeniden üretir
// (proje tipi ya da barındırma ayarları değişince kullanılır). Yaz → doğrula
// → yeniden yükle; `nginx -t`ten geçemeyen yapılandırma geri alınır, nginx
// asla bozuk kalmaz.

type ApplyVhostRequest struct {
	SiteID       int    `json:"site_id"`
	Domain       string `json:"domain"`
	TempDomain   string `json:"temp_domain"`
	DocumentRoot string `json:"document_root"`
	PHPSocket    string `json:"php_socket"`
	SSLType      string `json:"ssl_type"` // none | custom | letsencrypt
	SSLCert      string `json:"ssl_cert"`
	SSLKey       string `json:"ssl_key"`
	ProjectType  string `json:"project_type"`
	AppPort      int    `json:"app_port"`
	ForwardTo    string `json:"forward_to"`
	ForwardCode  int    `json:"forward_code"`
}

type ApplyVhostResponse struct {
	Config string `json:"config"`
	Error  string `json:"error,omitempty"`
}

func (a *Agent) ApplyVhost(req *ApplyVhostRequest, resp *ApplyVhostResponse) error {
	if req.Domain == "" {
		resp.Error = "domain is required"
		return nil
	}

	config, err := a.nginxGen.Render(services.VhostData{
		SiteID:       req.SiteID,
		Domain:       req.Domain,
		TempDomain:   req.TempDomain,
		DocumentRoot: req.DocumentRoot,
		PHPSocket:    req.PHPSocket,
		SSLType:      defaultStr(req.SSLType, "none"),
		SSLCert:      req.SSLCert,
		SSLKey:       req.SSLKey,
		ProjectType:  req.ProjectType,
		AppPort:      req.AppPort,
		ForwardTo:    req.ForwardTo,
		ForwardCode:  req.ForwardCode,
	})
	if err != nil {
		resp.Error = err.Error()
		return nil
	}

	if err := a.nginxGen.WriteVhostFile(req.Domain, config); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := a.nginxGen.ValidateNginx(); err != nil {
		// Roll back rather than leaving nginx unable to reload.
		// nginx'i yeniden yüklenemez bırakmaktansa geri al.
		_ = a.nginxGen.DeleteVhost(req.Domain)
		resp.Error = fmt.Sprintf("nginx validation failed, vhost rolled back: %v", err)
		return nil
	}
	if err := a.nginxGen.ReloadNginx(); err != nil {
		resp.Error = fmt.Sprintf("vhost written but reload failed: %v", err)
		return nil
	}

	resp.Config = config
	return nil
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
