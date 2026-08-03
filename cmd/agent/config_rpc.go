package main

import (
	"fmt"
	"log"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

func (a *Agent) GetPHPConfig(req transport.GetPHPConfigRequest, resp *transport.GetPHPConfigResponse) error {
	log.Printf("Getting PHP %s config", req.PHPVersion)
	settings, err := services.NewConfigManager().GetPHPSettings(req.PHPVersion)
	if err != nil {
		return fmt.Errorf("failed to get PHP config: %w", err)
	}
	resp.MemoryLimit = settings.MemoryLimit
	resp.MaxExecutionTime = settings.MaxExecutionTime
	resp.UploadMaxFilesize = settings.UploadMaxFilesize
	resp.PostMaxSize = settings.PostMaxSize
	resp.MaxInputVars = settings.MaxInputVars
	return nil
}

// UpdatePHPConfig returns only after validation, atomic publication and PHP-FPM
// activation have all succeeded. ConfigManager restores the previous live state
// before returning any error.
func (a *Agent) UpdatePHPConfig(req transport.UpdatePHPConfigRequest, _ *transport.Empty) error {
	log.Printf("Updating PHP %s config", req.PHPVersion)
	settings := &services.PHPSettings{
		MemoryLimit: req.MemoryLimit, MaxExecutionTime: req.MaxExecutionTime,
		UploadMaxFilesize: req.UploadMaxFilesize, PostMaxSize: req.PostMaxSize,
		MaxInputVars: req.MaxInputVars,
	}
	if err := services.NewConfigManager().UpdatePHPSettings(req.PHPVersion, settings); err != nil {
		return fmt.Errorf("failed to update PHP config: %w", err)
	}
	return nil
}

func (a *Agent) GetMySQLConfig(_ transport.Empty, resp *transport.GetMySQLConfigResponse) error {
	log.Println("Getting MySQL config")
	settings, err := services.NewConfigManager().GetMySQLSettings()
	if err != nil {
		return fmt.Errorf("failed to get MySQL config: %w", err)
	}
	resp.MaxConnections = settings.MaxConnections
	resp.InnodbBufferPool = settings.InnodbBufferPool
	resp.QueryCacheSize = settings.QueryCacheSize
	resp.MaxAllowedPacket = settings.MaxAllowedPacket
	return nil
}

// UpdateMySQLConfig returns only after validation, atomic publication and
// MariaDB restart have all succeeded. It never logs-and-returns-success on a
// failed restart.
func (a *Agent) UpdateMySQLConfig(req transport.UpdateMySQLConfigRequest, _ *transport.Empty) error {
	log.Println("Updating MySQL config")
	settings := &services.MySQLSettings{
		MaxConnections: req.MaxConnections, InnodbBufferPool: req.InnodbBufferPool,
		QueryCacheSize: req.QueryCacheSize, MaxAllowedPacket: req.MaxAllowedPacket,
	}
	if err := services.NewConfigManager().UpdateMySQLSettings(settings); err != nil {
		return fmt.Errorf("failed to update MySQL config: %w", err)
	}
	return nil
}
