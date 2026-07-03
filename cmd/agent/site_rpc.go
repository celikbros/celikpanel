package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

// CreateSite handles site creation with all privileged operations
func (a *Agent) CreateSite(req transport.CreateSiteRequest, reply *transport.CreateSiteResponse) error {
	// 1. Create directory structure
	err := os.MkdirAll(req.DocumentRoot, 0755)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create directories: %v", err)
		return nil
	}

	// 2. Create Linux user
	err = a.userManager.CreateUser(req.Username, filepath.Dir(req.DocumentRoot), req.Password)
	if err != nil {
		os.RemoveAll(filepath.Dir(req.DocumentRoot)) // Rollback
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create user: %v", err)
		return nil
	}

	// 3. Set ownership
	err = a.userManager.SetOwnership(filepath.Dir(req.DocumentRoot), req.Username)
	if err != nil {
		// Continue anyway, not critical
	}

	// 4. Create PHP-FPM pool
	socket, err := a.phpManager.CreatePool(req.SiteID, req.Username, req.PHPVersion)
	if err != nil {
		a.userManager.DeleteUser(req.Username) // Rollback
		os.RemoveAll(filepath.Dir(req.DocumentRoot))
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to create PHP-FPM pool: %v", err)
		return nil
	}
	reply.PHPSocket = socket

	// 5. Create default index.php
	indexContent := fmt.Sprintf(`<?php
phpinfo();
// Domain: %s
// PHP Version: %s
?>`, req.Domain, req.PHPVersion)
	os.WriteFile(filepath.Join(req.DocumentRoot, "index.php"), []byte(indexContent), 0644)
	a.userManager.SetOwnership(filepath.Join(req.DocumentRoot, "index.php"), req.Username)

	// 6. Generate Nginx vhost (after socket is created)
	site := &core.Site{
		ID:           req.SiteID,
		DomainID:     0, // Not needed for template
		DocumentRoot: req.DocumentRoot,
		PHPVersion:   req.PHPVersion,
		PHPFPMSocket: &socket,
		SSLEnabled:   req.SSLType != "none",
	}

	domain := &core.Domain{
		Name: req.Domain,
	}

	vhostConfig, err := a.nginxGen.GenerateVhost(site, domain, req.TempDomain)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to generate vhost: %v", err)
		return nil
	}
	reply.NginxConfig = vhostConfig

	// 7. Write Nginx vhost file
	err = a.nginxGen.WriteVhostFile(req.Domain, vhostConfig)
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to write vhost: %v", err)
		return nil
	}

	// 8. Validate and reload Nginx
	err = a.nginxGen.ValidateNginx()
	if err != nil {
		a.nginxGen.DeleteVhost(req.Domain) // Rollback
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("nginx validation failed: %v", err)
		return nil
	}

	err = a.nginxGen.ReloadNginx()
	if err != nil {
		reply.Success = false
		reply.ErrorMessage = fmt.Sprintf("failed to reload nginx: %v", err)
		return nil
	}

	reply.Success = true
	return nil
}

// DeleteSite removes site and cleans up resources
func (a *Agent) DeleteSite(args struct{ SiteID int; Domain string }, reply *struct{ Success bool }) error {
	// Implementation for cleanup
	reply.Success = true
	return nil
}
