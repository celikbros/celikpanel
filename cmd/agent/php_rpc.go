package main

import (
	"log"

	"github.com/alicelik/celikpanel/internal/transport"
)

// GetPHPPools returns all PHP-FPM pools for a version
func (a *Agent) GetPHPPools(req transport.PHPVersionRequest, resp *[]transport.PHPPool) error {
	log.Printf("Getting PHP %s pools", req.Version)

	pools, err := a.phpManager.ListPools(req.Version)
	if err != nil {
		return err
	}

	*resp = pools
	return nil
}

// GetPHPExtensions returns all PHP extensions for a version
func (a *Agent) GetPHPExtensions(req transport.PHPVersionRequest, resp *[]transport.PHPExtension) error {
	log.Printf("Getting PHP %s extensions", req.Version)

	extensions, err := a.phpManager.ListExtensions(req.Version)
	if err != nil {
		return err
	}

	*resp = extensions
	return nil
}

// TogglePHPExtension enables or disables a PHP extension
func (a *Agent) TogglePHPExtension(req transport.PHPExtensionRequest, resp *transport.Empty) error {
	log.Printf("Toggling PHP %s extension %s to %v", req.Version, req.Extension, req.Enabled)

	var err error
	if req.Enabled {
		err = a.phpManager.EnableExtension(req.Version, req.Extension)
	} else {
		err = a.phpManager.DisableExtension(req.Version, req.Extension)
	}

	return err
}

// GetPHPConfiguration returns PHP configuration for a version
func (a *Agent) GetPHPConfiguration(req transport.PHPVersionRequest, resp *transport.PHPConfig) error {
	log.Printf("Getting PHP %s configuration", req.Version)

	config, err := a.phpManager.GetConfig(req.Version)
	if err != nil {
		return err
	}

	*resp = *config
	return nil
}

// UpdatePHPConfiguration updates PHP configuration for a version
func (a *Agent) UpdatePHPConfiguration(req transport.PHPConfigRequest, resp *transport.Empty) error {
	log.Printf("Updating PHP %s configuration", req.Version)

	return a.phpManager.UpdateConfig(req.Version, &req.Config)
}
