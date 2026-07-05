package services

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/alicelik/celikpanel/internal/core"
)

//go:embed templates/nginx/vhost.conf.tmpl
var vhostTemplate string

type NginxGenerator struct {
	tmpl *template.Template
}

func NewNginxGenerator() (*NginxGenerator, error) {
	tmpl, err := template.New("vhost").Parse(vhostTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %v", err)
	}
	return &NginxGenerator{tmpl: tmpl}, nil
}

type VhostData struct {
	SiteID          int
	Domain          string
	TempDomain      string
	DocumentRoot    string
	PHPSocket       string
	SSLType         string
	SSLCert         string
	SSLKey          string
	SSLAutoRedirect bool
	// Project-type fields (roadmap 3A). Upstream is derived: node projects
	// proxy to 127.0.0.1:AppPort, proxy projects to ForwardTo.
	// Proje-tipi alanları (yol haritası 3A). Upstream türetilir: node
	// projeleri 127.0.0.1:AppPort'a, proxy projeleri ForwardTo'ya vekillenir.
	ProjectType string
	AppPort     int
	ForwardTo   string
	ForwardCode int
	Upstream    string
}

// Render executes the vhost template over prepared data, deriving the
// proxy upstream for node/proxy types.
// Render, hazırlanmış veriyle vhost şablonunu çalıştırır; node/proxy
// tiplerinde vekil upstream'ini türetir.
func (ng *NginxGenerator) Render(data VhostData) (string, error) {
	if data.ProjectType == "" {
		data.ProjectType = "php"
	}
	switch data.ProjectType {
	case "php":
		// A php vhost without an FPM socket would render `unix:` with no
		// path — nginx rejects it. Refuse honestly instead of writing a
		// config that cannot work.
		// FPM soketi olmayan bir php vhost'u yolsuz `unix:` üretir — nginx
		// reddeder. Çalışamayacak bir yapılandırma yazmak yerine dürüstçe
		// reddet.
		if data.PHPSocket == "" {
			return "", fmt.Errorf("php project has no PHP-FPM socket configured for this site")
		}
	case "node":
		data.Upstream = fmt.Sprintf("http://127.0.0.1:%d", data.AppPort)
	case "proxy":
		data.Upstream = data.ForwardTo
	}
	if data.ForwardCode == 0 {
		data.ForwardCode = 301
	}

	var buf bytes.Buffer
	if err := ng.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	return buf.String(), nil
}

// GenerateVhost generates nginx vhost config from site data
func (ng *NginxGenerator) GenerateVhost(site *core.Site, domain *core.Domain, tempDomain string) (string, error) {
	// Determine SSL type from enabled flag
	sslType := "none"
	if site.SSLEnabled {
		sslType = "custom" // Will be letsencrypt or custom based on cert paths
	}

	data := VhostData{
		SiteID:          site.ID,
		Domain:          domain.Name,
		TempDomain:      tempDomain,
		DocumentRoot:    site.DocumentRoot,
		SSLType:         sslType,
		SSLAutoRedirect: site.SSLEnabled,
	}
	// A missing FPM socket must not panic vhost generation (non-PHP types).
	// Eksik FPM soketi vhost üretimini panikletmemeli (PHP dışı tipler).
	if site.PHPFPMSocket != nil {
		data.PHPSocket = *site.PHPFPMSocket
	}

	// Set SSL paths if enabled
	if site.SSLCertPath != nil && site.SSLKeyPath != nil {
		data.SSLCert = *site.SSLCertPath
		data.SSLKey = *site.SSLKeyPath
	}

	return ng.Render(data)
}

// nginxDir is /etc/nginx in production; CELIKPANEL_NGINX_DIR redirects vhost
// output for a non-root development agent. In that dev mode validate/reload
// are skipped (the files are not part of the live nginx config).
// nginxDir üretimde /etc/nginx'tir; CELIKPANEL_NGINX_DIR, root olmayan
// geliştirme agent'ı için vhost çıktısını yönlendirir. O dev modunda
// doğrulama/yeniden yükleme atlanır (dosyalar canlı nginx config'inin
// parçası değildir).
var nginxDir = os.Getenv("CELIKPANEL_NGINX_DIR")

func nginxDevMode() bool { return nginxDir != "" }

func vhostPaths(domain string) (available, enabled string) {
	base := "/etc/nginx"
	if nginxDevMode() {
		base = nginxDir
	}
	return fmt.Sprintf("%s/sites-available/%s.conf", base, domain),
		fmt.Sprintf("%s/sites-enabled/%s.conf", base, domain)
}

// WriteVhostFile writes vhost config to file
func (ng *NginxGenerator) WriteVhostFile(domain string, config string) error {
	filename, symlinkPath := vhostPaths(domain)
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	err := os.WriteFile(filename, []byte(config), 0644)
	if err != nil {
		return fmt.Errorf("failed to write vhost file: %v", err)
	}

	// Create symlink in sites-enabled
	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0o755); err != nil {
		return err
	}
	os.Remove(symlinkPath) // Remove if exists
	err = os.Symlink(filename, symlinkPath)
	if err != nil {
		return fmt.Errorf("failed to create symlink: %v", err)
	}

	return nil
}

// ValidateNginx runs nginx -t to validate configuration. Skipped in dev
// mode: the redirected vhost files are not part of the live nginx config,
// so validating it would prove nothing about them.
// ValidateNginx, yapılandırmayı doğrulamak için nginx -t çalıştırır. Dev
// modunda atlanır: yönlendirilmiş vhost dosyaları canlı nginx config'inin
// parçası değildir; onu doğrulamak bunlar hakkında bir şey kanıtlamaz.
func (ng *NginxGenerator) ValidateNginx() error {
	if nginxDevMode() {
		return nil
	}
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx validation failed: %s", string(output))
	}
	return nil
}

// ReloadNginx reloads nginx service
func (ng *NginxGenerator) ReloadNginx() error {
	if nginxDevMode() {
		return nil
	}
	cmd := exec.Command("systemctl", "reload", "nginx")
	return cmd.Run()
}

// DeleteVhost removes vhost config files
func (ng *NginxGenerator) DeleteVhost(domain string) error {
	filename, symlinkPath := vhostPaths(domain)
	os.Remove(symlinkPath)
	os.Remove(filename)
	return nil
}
