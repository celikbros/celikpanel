package services

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// PHPPoolManager handles PHP-FPM pool operations
type PHPPoolManager struct{}

func NewPHPPoolManager() *PHPPoolManager {
	return &PHPPoolManager{}
}

// GetPoolConfig reads detailed pool configuration
func (pm *PHPPoolManager) GetPoolConfig(phpVersion, poolName string) (*core.PHPPoolConfig, error) {
	poolFile := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, poolName)
	
	file, err := os.Open(poolFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open pool file: %v", err)
	}
	defer file.Close()

	config := &core.PHPPoolConfig{
		Name:          poolName,
		PM:            "dynamic",
		PMMaxChildren: 5,
		ListenMode:    "0660",
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, ";") || line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "user":
			config.User = value
		case "group":
			config.Group = value
		case "listen":
			config.Listen = value
		case "listen.owner":
			config.ListenOwner = value
		case "listen.group":
			config.ListenGroup = value
		case "listen.mode":
			config.ListenMode = value
		case "pm":
			config.PM = value
		case "pm.max_children":
			config.PMMaxChildren, _ = strconv.Atoi(value)
		case "pm.start_servers":
			config.PMStartServers, _ = strconv.Atoi(value)
		case "pm.min_spare_servers":
			config.PMMinSpareServers, _ = strconv.Atoi(value)
		case "pm.max_spare_servers":
			config.PMMaxSpareServers, _ = strconv.Atoi(value)
		case "pm.max_requests":
			config.PMMaxRequests, _ = strconv.Atoi(value)
		}
	}

	return config, nil
}

// UpdatePoolConfig updates pool configuration
func (pm *PHPPoolManager) UpdatePoolConfig(phpVersion string, config *core.PHPPoolConfig) error {
	poolFile := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, config.Name)
	
	// Generate pool content
	content := fmt.Sprintf(`[%s]
user = %s
group = %s
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = %s
pm = %s
pm.max_children = %d
pm.start_servers = %d
pm.min_spare_servers = %d
pm.max_spare_servers = %d
pm.max_requests = %d
chdir = /
`,
		config.Name,
		config.User,
		config.Group,
		config.Listen,
		config.ListenOwner,
		config.ListenGroup,
		config.ListenMode,
		config.PM,
		config.PMMaxChildren,
		config.PMStartServers,
		config.PMMinSpareServers,
		config.PMMaxSpareServers,
		config.PMMaxRequests,
	)

	if err := os.WriteFile(poolFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write pool file: %v", err)
	}

	return reloadPHPFPM(phpVersion)
}

// DeletePoolByName deletes a pool by name
func (pm *PHPPoolManager) DeletePoolByName(phpVersion, poolName string) error {
	poolFile := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", phpVersion, poolName)
	
	if err := os.Remove(poolFile); err != nil {
		return fmt.Errorf("failed to delete pool: %v", err)
	}

	return reloadPHPFPM(phpVersion)
}

// ListPoolNames returns list of pool names
func (pm *PHPPoolManager) ListPoolNames(phpVersion string) ([]string, error) {
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", phpVersion)
	
	files, err := os.ReadDir(poolDir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".conf") {
			name := strings.TrimSuffix(file.Name(), ".conf")
			names = append(names, name)
		}
	}

	return names, nil
}

// MigratePool copies pool configuration from old PHP version to new PHP version
func (pm *PHPPoolManager) MigratePool(oldVersion, newVersion, poolName string) error {
	// Get pool config from old version
	oldConfig, err := pm.GetPoolConfig(oldVersion, poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool config from PHP %s: %v", oldVersion, err)
	}

	// Update socket path for new version
	oldConfig.Listen = fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", newVersion, poolName)

	// Create pool in new version
	if err := pm.UpdatePoolConfig(newVersion, oldConfig); err != nil {
		return fmt.Errorf("failed to create pool in PHP %s: %v", newVersion, err)
	}

	// Delete pool from old version
	if err := pm.DeletePoolByName(oldVersion, poolName); err != nil {
		// Log but don't fail - pool already exists in new version
		fmt.Printf("Warning: failed to delete old pool: %v\n", err)
	}

	return nil
}

