package services

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// PHPConfigManager handles PHP configuration (php.ini)
type PHPConfigManager struct{}

func NewPHPConfigManager() *PHPConfigManager {
	return &PHPConfigManager{}
}

// GetExtendedConfig reads comprehensive PHP configuration
func (cm *PHPConfigManager) GetExtendedConfig(phpVersion string) (*core.ExtendedPHPConfig, error) {
	phpIni := fmt.Sprintf("/etc/php/%s/fpm/php.ini", phpVersion)
	
	file, err := os.Open(phpIni)
	if err != nil {
		return nil, fmt.Errorf("failed to open php.ini: %v", err)
	}
	defer file.Close()

	config := &core.ExtendedPHPConfig{
		// Defaults
		MemoryLimit:       "128M",
		MaxExecutionTime:  "30",
		MaxInputTime:      "60",
		PostMaxSize:       "8M",
		UploadMaxFilesize: "2M",
		DisplayErrors:     "Off",
		LogErrors:         "On",
		AllowUrlFopen:     "On",
		FileUploads:       "On",
		ShortOpenTag:      "Off",
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
		// Performance & Security
		case "memory_limit":
			config.MemoryLimit = value
		case "max_execution_time":
			config.MaxExecutionTime = value
		case "max_input_time":
			config.MaxInputTime = value
		case "post_max_size":
			config.PostMaxSize = value
		case "upload_max_filesize":
			config.UploadMaxFilesize = value
		case "opcache.enable":
			config.OpcacheEnable = value
		case "disable_functions":
			config.DisableFunctions = value
		
		// Common Settings
		case "include_path":
			config.IncludePath = value
		case "session.save_path":
			config.SessionSavePath = value
		case "realpath_cache_size":
			config.RealpathCacheSize = value
		case "open_basedir":
			config.OpenBasedir = value
		case "error_reporting":
			config.ErrorReporting = value
		case "display_errors":
			config.DisplayErrors = value
		case "log_errors":
			config.LogErrors = value
		case "allow_url_fopen":
			config.AllowUrlFopen = value
		case "file_uploads":
			config.FileUploads = value
		case "short_open_tag":
			config.ShortOpenTag = value
		}
	}

	return config, nil
}

// UpdateExtendedConfig updates comprehensive PHP configuration
func (cm *PHPConfigManager) UpdateExtendedConfig(phpVersion string, config *core.ExtendedPHPConfig) error {
	phpIni := fmt.Sprintf("/etc/php/%s/fpm/php.ini", phpVersion)
	
	content, err := os.ReadFile(phpIni)
	if err != nil {
		return fmt.Errorf("failed to read php.ini: %v", err)
	}

	updated := string(content)
	
	// Performance & Security
	updated = updateOrAddSetting(updated, "memory_limit", config.MemoryLimit)
	updated = updateOrAddSetting(updated, "max_execution_time", config.MaxExecutionTime)
	updated = updateOrAddSetting(updated, "max_input_time", config.MaxInputTime)
	updated = updateOrAddSetting(updated, "post_max_size", config.PostMaxSize)
	updated = updateOrAddSetting(updated, "upload_max_filesize", config.UploadMaxFilesize)
	updated = updateOrAddSetting(updated, "opcache.enable", config.OpcacheEnable)
	updated = updateOrAddSetting(updated, "disable_functions", config.DisableFunctions)
	
	// Common Settings
	updated = updateOrAddSetting(updated, "include_path", config.IncludePath)
	updated = updateOrAddSetting(updated, "session.save_path", config.SessionSavePath)
	updated = updateOrAddSetting(updated, "realpath_cache_size", config.RealpathCacheSize)
	updated = updateOrAddSetting(updated, "open_basedir", config.OpenBasedir)
	updated = updateOrAddSetting(updated, "error_reporting", config.ErrorReporting)
	updated = updateOrAddSetting(updated, "display_errors", config.DisplayErrors)
	updated = updateOrAddSetting(updated, "log_errors", config.LogErrors)
	updated = updateOrAddSetting(updated, "allow_url_fopen", config.AllowUrlFopen)
	updated = updateOrAddSetting(updated, "file_uploads", config.FileUploads)
	updated = updateOrAddSetting(updated, "short_open_tag", config.ShortOpenTag)
	
	// Additional directives
	if config.AdditionalDirectives != "" {
		updated += "\n; Additional custom directives\n"
		updated += config.AdditionalDirectives + "\n"
	}

	if err := os.WriteFile(phpIni, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write php.ini: %v", err)
	}

	return reloadPHPFPM(phpVersion)
}
