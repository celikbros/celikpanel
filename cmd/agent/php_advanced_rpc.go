package main

import (
	"log"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Pool Management RPCs

// GetPHPPoolConfig returns detailed pool configuration
func (a *Agent) GetPHPPoolConfig(req transport.GetPHPPoolConfigRequest, resp *transport.PHPPoolConfig) error {
	log.Printf("Getting pool config for %s (PHP %s)", req.PoolName, req.Version)

	config, err := a.phpManager.PoolManager.GetPoolConfig(req.Version, req.PoolName)
	if err != nil {
		return err
	}

	*resp = *config
	return nil
}

// UpdatePHPPoolConfig updates pool configuration
func (a *Agent) UpdatePHPPoolConfig(req transport.PHPPoolConfigRequest, resp *transport.Empty) error {
	log.Printf("Updating pool config for %s (PHP %s)", req.PoolConfig.Name, req.Version)

	return a.phpManager.PoolManager.UpdatePoolConfig(req.Version, &req.PoolConfig)
}

// DeletePHPPool deletes a pool
func (a *Agent) DeletePHPPool(req transport.DeletePHPPoolRequest, resp *transport.Empty) error {
	log.Printf("Deleting pool %s (PHP %s)", req.PoolName, req.Version)

	return a.phpManager.PoolManager.DeletePoolByName(req.Version, req.PoolName)
}

// Configuration Management RPCs

// GetExtendedPHPConfig returns comprehensive PHP configuration
func (a *Agent) GetExtendedPHPConfig(req transport.PHPVersionRequest, resp *transport.ExtendedPHPConfig) error {
	log.Printf("Getting extended PHP %s configuration", req.Version)

	config, err := a.phpManager.ConfigManager.GetExtendedConfig(req.Version)
	if err != nil {
		return err
	}

	*resp = *config
	return nil
}

// UpdateExtendedPHPConfig updates comprehensive PHP configuration
func (a *Agent) UpdateExtendedPHPConfig(req transport.ExtendedPHPConfigRequest, resp *transport.Empty) error {
	log.Printf("Updating extended PHP %s configuration", req.Version)

	return a.phpManager.ConfigManager.UpdateExtendedConfig(req.Version, &req.Config)
}

// MigratePHPPool migrates a pool from one PHP version to another
func (a *Agent) MigratePHPPool(req transport.MigratePHPPoolRequest, resp *transport.Empty) error {
	log.Printf("Migrating pool %s from PHP %s to PHP %s", req.PoolName, req.OldVersion, req.NewVersion)

	return a.phpManager.PoolManager.MigratePool(req.OldVersion, req.NewVersion, req.PoolName)
}
