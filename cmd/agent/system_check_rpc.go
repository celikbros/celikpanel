package main

import (
	"os/exec"
)

// System Check RPC Methods

// CheckInstalledServicesRequest represents a request to check installed services
type CheckInstalledServicesRequest struct{}

// CheckInstalledServicesResponse represents installed services
type CheckInstalledServicesResponse struct {
	Nginx      bool `json:"nginx"`
	Apache     bool `json:"apache"`
	MySQL      bool `json:"mysql"`
	PostgreSQL bool `json:"postgresql"`
	PHP        bool `json:"php"`
}

// CheckInstalledServices checks which services are installed on the system
func (a *Agent) CheckInstalledServices(req CheckInstalledServicesRequest, resp *CheckInstalledServicesResponse) error {
	// Check nginx
	cmd := exec.Command("which", "nginx")
	if err := cmd.Run(); err == nil {
		resp.Nginx = true
	}

	// Check apache
	cmd = exec.Command("which", "apache2")
	if err := cmd.Run(); err == nil {
		resp.Apache = true
	} else {
		// Try httpd (CentOS/RHEL)
		cmd = exec.Command("which", "httpd")
		if err := cmd.Run(); err == nil {
			resp.Apache = true
		}
	}

	// Check MySQL
	cmd = exec.Command("which", "mysql")
	if err := cmd.Run(); err == nil {
		resp.MySQL = true
	}

	// Check PostgreSQL
	cmd = exec.Command("which", "psql")
	if err := cmd.Run(); err == nil {
		resp.PostgreSQL = true
	}

	// Check PHP
	cmd = exec.Command("which", "php")
	if err := cmd.Run(); err == nil {
		resp.PHP = true
	}

	return nil
}
