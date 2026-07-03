package services

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/alicelik/celikpanel/internal/core"
)

const phpPoolTemplate = `[site{{.SiteID}}]
user = {{.Username}}
group = {{.Username}}
listen = {{.Socket}}
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = dynamic
pm.max_children = 5
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
pm.max_requests = 500
chdir = /
`

type PHPFPMManager struct {
	tmpl          *template.Template
	PoolManager   *PHPPoolManager
	ConfigManager *PHPConfigManager
}

func NewPHPFPMManager() (*PHPFPMManager, error) {
	tmpl, err := template.New("pool").Parse(phpPoolTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pool template: %v", err)
	}
	return &PHPFPMManager{
		tmpl:          tmpl,
		PoolManager:   NewPHPPoolManager(),
		ConfigManager: NewPHPConfigManager(),
	}, nil
}

type PoolData struct {
	SiteID   int
	Username string
	Socket   string
}

// CreatePool creates a PHP-FPM pool for a site
func (pm *PHPFPMManager) CreatePool(siteID int, username string, phpVersion string) (string, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return "", err
	}
	// Socket path
	socket := fmt.Sprintf("/var/run/php/php%s-fpm-site%d.sock", phpVersion, siteID)

	data := PoolData{
		SiteID:   siteID,
		Username: username,
		Socket:   socket,
	}

	// Generate pool config
	var buf bytes.Buffer
	err := pm.tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}

	// Write pool file
	filename := fmt.Sprintf("/etc/php/%s/fpm/pool.d/site%d.conf", phpVersion, siteID)
	err = os.WriteFile(filename, []byte(buf.String()), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write pool file: %v", err)
	}

	// Reload PHP-FPM
	err = pm.ReloadPHPFPM(phpVersion)
	if err != nil {
		return "", fmt.Errorf("failed to reload PHP-FPM: %v", err)
	}

	return socket, nil
}

// DeletePool removes a PHP-FPM pool
func (pm *PHPFPMManager) DeletePool(siteID int, phpVersion string) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	filename := fmt.Sprintf("/etc/php/%s/fpm/pool.d/site%d.conf", phpVersion, siteID)
	os.Remove(filename)

	return pm.ReloadPHPFPM(phpVersion)
}

// ListPools lists all PHP-FPM pools for a given version
func (pm *PHPFPMManager) ListPools(phpVersion string) ([]core.PHPPool, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", phpVersion)
	
	files, err := os.ReadDir(poolDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read pool directory: %v", err)
	}

	var pools []core.PHPPool
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".conf") {
			continue
		}

		poolPath := filepath.Join(poolDir, file.Name())
		pool, err := pm.parsePoolFile(poolPath)
		if err != nil {
			// Log error but continue with other pools
			continue
		}
		pools = append(pools, pool)
	}

	return pools, nil
}

// parsePoolFile parses a pool configuration file
func (pm *PHPFPMManager) parsePoolFile(path string) (core.PHPPool, error) {
	file, err := os.Open(path)
	if err != nil {
		return core.PHPPool{}, err
	}
	defer file.Close()

	pool := core.PHPPool{
		PM: "dynamic", // default
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip comments and empty lines
		if strings.HasPrefix(line, ";") || line == "" {
			continue
		}

		// Parse pool name
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			pool.Name = strings.Trim(line, "[]")
			continue
		}

		// Parse key-value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "user":
			pool.User = value
		case "listen":
			// Extract port if it's a TCP socket, otherwise it's a unix socket
			if strings.Contains(value, ":") {
				// TCP socket format: 127.0.0.1:9000
				portParts := strings.Split(value, ":")
				if len(portParts) == 2 {
					fmt.Sscanf(portParts[1], "%d", &pool.Port)
				}
			}
		case "pm":
			pool.PM = value
		}
	}

	return pool, nil
}

// ListExtensions lists all available PHP extensions and their status
func (pm *PHPFPMManager) ListExtensions(phpVersion string) ([]core.PHPExtension, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	// Get list of available modules
	modsAvailableDir := fmt.Sprintf("/etc/php/%s/mods-available", phpVersion)
	modsEnabledDir := fmt.Sprintf("/etc/php/%s/fpm/conf.d", phpVersion)

	availableFiles, err := os.ReadDir(modsAvailableDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read mods-available: %v", err)
	}

	// Get enabled modules (check for symlinks ending with the module name)
	enabledMap := make(map[string]bool)
	enabledFiles, err := os.ReadDir(modsEnabledDir)
	if err == nil {
		for _, file := range enabledFiles {
			if strings.HasSuffix(file.Name(), ".ini") {
				// Extract module name from filename (e.g., "20-calendar.ini" -> "calendar")
				// Enabled files have format: NN-modulename.ini
				parts := strings.Split(file.Name(), "-")
				if len(parts) >= 2 {
					// Get everything after the first dash, remove .ini
					moduleName := strings.TrimSuffix(strings.Join(parts[1:], "-"), ".ini")
					enabledMap[moduleName] = true
				} else {
					// No prefix, just use the filename
					moduleName := strings.TrimSuffix(file.Name(), ".ini")
					enabledMap[moduleName] = true
				}
			}
		}
	}

	var extensions []core.PHPExtension
	for _, file := range availableFiles {
		if !strings.HasSuffix(file.Name(), ".ini") {
			continue
		}

		extName := strings.TrimSuffix(file.Name(), ".ini")
		extensions = append(extensions, core.PHPExtension{
			Name:    extName,
			Enabled: enabledMap[extName],
		})
	}

	return extensions, nil
}

// EnableExtension enables a PHP extension
func (pm *PHPFPMManager) EnableExtension(phpVersion, extension string) error {
	cmd := exec.Command("/usr/sbin/phpenmod", "-v", phpVersion, "-s", "fpm", extension)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable extension: %s", string(output))
	}

	return pm.ReloadPHPFPM(phpVersion)
}

// DisableExtension disables a PHP extension
func (pm *PHPFPMManager) DisableExtension(phpVersion, extension string) error {
	cmd := exec.Command("/usr/sbin/phpdismod", "-v", phpVersion, "-s", "fpm", extension)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to disable extension: %s", string(output))
	}

	return pm.ReloadPHPFPM(phpVersion)
}

// GetConfig reads PHP configuration from php.ini
func (pm *PHPFPMManager) GetConfig(phpVersion string) (*core.PHPConfig, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	phpIni := fmt.Sprintf("/etc/php/%s/fpm/php.ini", phpVersion)
	
	file, err := os.Open(phpIni)
	if err != nil {
		return nil, fmt.Errorf("failed to open php.ini: %v", err)
	}
	defer file.Close()

	config := &core.PHPConfig{
		MemoryLimit:       "128M",
		MaxExecutionTime:  "30",
		UploadMaxFilesize: "2M",
		PostMaxSize:       "8M",
		DisplayErrors:     "Off",
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		
		// Skip comments and empty lines
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
		case "memory_limit":
			config.MemoryLimit = value
		case "max_execution_time":
			config.MaxExecutionTime = value
		case "upload_max_filesize":
			config.UploadMaxFilesize = value
		case "post_max_size":
			config.PostMaxSize = value
		case "display_errors":
			config.DisplayErrors = value
		}
	}

	return config, nil
}

// UpdateConfig updates PHP configuration in php.ini
func (pm *PHPFPMManager) UpdateConfig(phpVersion string, config *core.PHPConfig) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	phpIni := fmt.Sprintf("/etc/php/%s/fpm/php.ini", phpVersion)
	
	// Read current content
	content, err := os.ReadFile(phpIni)
	if err != nil {
		return fmt.Errorf("failed to read php.ini: %v", err)
	}

	// Update settings
	updated := string(content)
	updated = updateOrAddSetting(updated, "memory_limit", config.MemoryLimit)
	updated = updateOrAddSetting(updated, "max_execution_time", config.MaxExecutionTime)
	updated = updateOrAddSetting(updated, "upload_max_filesize", config.UploadMaxFilesize)
	updated = updateOrAddSetting(updated, "post_max_size", config.PostMaxSize)
	updated = updateOrAddSetting(updated, "display_errors", config.DisplayErrors)

	// Write back
	if err := os.WriteFile(phpIni, []byte(updated), 0644); err != nil { //nosec G703 -- phpVersion validated by ValidatePHPVersion at entry
		return fmt.Errorf("failed to write php.ini: %v", err)
	}

	return pm.ReloadPHPFPM(phpVersion)
}

// ReloadPHPFPM reloads PHP-FPM service
func (pm *PHPFPMManager) ReloadPHPFPM(version string) error {
	serviceName := fmt.Sprintf("php%s-fpm", version)
	cmd := exec.Command("systemctl", "reload", serviceName)
	return cmd.Run()
}

// CheckPHPVersion checks if PHP version is installed
func (pm *PHPFPMManager) CheckPHPVersion(version string) bool {
	cmd := exec.Command("php"+version, "--version")
	err := cmd.Run()
	return err == nil
}
