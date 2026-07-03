package services

import (
	_ "embed"
	"bytes"
	"fmt"
	"os"
	"os/exec"
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
		PHPSocket:       *site.PHPFPMSocket,
		SSLType:         sslType,
		SSLAutoRedirect: site.SSLEnabled,
	}

	// Set SSL paths if enabled
	if site.SSLCertPath != nil && site.SSLKeyPath != nil {
		data.SSLCert = *site.SSLCertPath
		data.SSLKey = *site.SSLKeyPath
	}

	var buf bytes.Buffer
	err := ng.tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	return buf.String(), nil
}

// WriteVhostFile writes vhost config to file
func (ng *NginxGenerator) WriteVhostFile(domain string, config string) error {
	filename := fmt.Sprintf("/etc/nginx/sites-available/%s.conf", domain)
	err := os.WriteFile(filename, []byte(config), 0644)
	if err != nil {
		return fmt.Errorf("failed to write vhost file: %v", err)
	}

	// Create symlink in sites-enabled
	symlinkPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s.conf", domain)
	os.Remove(symlinkPath) // Remove if exists
	err = os.Symlink(filename, symlinkPath)
	if err != nil {
		return fmt.Errorf("failed to create symlink: %v", err)
	}

	return nil
}

// ValidateNginx runs nginx -t to validate configuration
func (ng *NginxGenerator) ValidateNginx() error {
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx validation failed: %s", string(output))
	}
	return nil
}

// ReloadNginx reloads nginx service
func (ng *NginxGenerator) ReloadNginx() error {
	cmd := exec.Command("systemctl", "reload", "nginx")
	return cmd.Run()
}

// DeleteVhost removes vhost config files
func (ng *NginxGenerator) DeleteVhost(domain string) error {
	// Remove symlink
	symlinkPath := fmt.Sprintf("/etc/nginx/sites-enabled/%s.conf", domain)
	os.Remove(symlinkPath)

	// Remove config file
	filename := fmt.Sprintf("/etc/nginx/sites-available/%s.conf", domain)
	os.Remove(filename)

	return nil
}
