package main

import (
	"fmt"
	"log"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// GetPHPConfig returns PHP configuration
func (a *Agent) GetPHPConfig(req transport.GetPHPConfigRequest, resp *transport.GetPHPConfigResponse) error {
	log.Printf("Getting PHP %s config", req.PHPVersion)

	configMgr := services.NewConfigManager()
	settings, err := configMgr.GetPHPSettings(req.PHPVersion)
	if err != nil {
		return fmt.Errorf("failed to get PHP config: %v", err)
	}

	resp.MemoryLimit = settings.MemoryLimit
	resp.MaxExecutionTime = settings.MaxExecutionTime
	resp.UploadMaxFilesize = settings.UploadMaxFilesize
	resp.PostMaxSize = settings.PostMaxSize
	resp.MaxInputVars = settings.MaxInputVars

	return nil
}

// UpdatePHPConfig updates PHP configuration
func (a *Agent) UpdatePHPConfig(req transport.UpdatePHPConfigRequest, resp *struct{}) error {
	log.Printf("Updating PHP %s config", req.PHPVersion)

	configMgr := services.NewConfigManager()
	settings := &services.PHPSettings{
		MemoryLimit:       req.MemoryLimit,
		MaxExecutionTime:  req.MaxExecutionTime,
		UploadMaxFilesize: req.UploadMaxFilesize,
		PostMaxSize:       req.PostMaxSize,
		MaxInputVars:      req.MaxInputVars,
	}

	if err := configMgr.UpdatePHPSettings(req.PHPVersion, settings); err != nil {
		return fmt.Errorf("failed to update PHP config: %v", err)
	}

	// Reload PHP-FPM
	if err := a.phpManager.ReloadPHPFPM(req.PHPVersion); err != nil {
		log.Printf("Warning: Failed to reload PHP-FPM: %v", err)
	}

	return nil
}

// GetMySQLConfig returns MySQL configuration
func (a *Agent) GetMySQLConfig(req struct{}, resp *transport.GetMySQLConfigResponse) error {
	log.Println("Getting MySQL config")

	configMgr := services.NewConfigManager()
	settings, err := configMgr.GetMySQLSettings()
	if err != nil {
		return fmt.Errorf("failed to get MySQL config: %v", err)
	}

	resp.MaxConnections = settings.MaxConnections
	resp.InnodbBufferPool = settings.InnodbBufferPool
	resp.QueryCacheSize = settings.QueryCacheSize
	resp.MaxAllowedPacket = settings.MaxAllowedPacket

	return nil
}

// UpdateMySQLConfig updates MySQL configuration
func (a *Agent) UpdateMySQLConfig(req transport.UpdateMySQLConfigRequest, resp *struct{}) error {
	log.Println("Updating MySQL config")

	configMgr := services.NewConfigManager()
	settings := &services.MySQLSettings{
		MaxConnections:   req.MaxConnections,
		InnodbBufferPool: req.InnodbBufferPool,
		QueryCacheSize:   req.QueryCacheSize,
		MaxAllowedPacket: req.MaxAllowedPacket,
	}

	if err := configMgr.UpdateMySQLSettings(settings); err != nil {
		return fmt.Errorf("failed to update MySQL config: %v", err)
	}

	// Restart MySQL
	if err := a.systemdMgr.Restart("mariadb"); err != nil {
		log.Printf("Warning: Failed to restart MariaDB: %v", err)
	}

	return nil
}
