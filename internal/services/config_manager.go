package services

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ConfigManager manages service configuration files
type ConfigManager struct{}

func NewConfigManager() *ConfigManager {
	return &ConfigManager{}
}

// PHPSettings represents common PHP configuration
type PHPSettings struct {
	MemoryLimit       string `json:"memory_limit"`        // e.g., "256M"
	MaxExecutionTime  int    `json:"max_execution_time"`  // seconds
	UploadMaxFilesize string `json:"upload_max_filesize"` // e.g., "64M"
	PostMaxSize       string `json:"post_max_size"`       // e.g., "64M"
	MaxInputVars      int    `json:"max_input_vars"`      // number
}

// MySQLSettings represents common MySQL configuration
type MySQLSettings struct {
	MaxConnections    int    `json:"max_connections"`
	InnodbBufferPool  string `json:"innodb_buffer_pool_size"` // e.g., "1G"
	QueryCacheSize    string `json:"query_cache_size"`        // e.g., "64M"
	MaxAllowedPacket  string `json:"max_allowed_packet"`      // e.g., "64M"
}

// GetPHPSettings reads current PHP settings from php.ini
func (cm *ConfigManager) GetPHPSettings(phpVersion string) (*PHPSettings, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	phpIni := fmt.Sprintf("/etc/php/%s/fpm/php.ini", phpVersion)
	
	file, err := os.Open(phpIni)
	if err != nil {
		return nil, fmt.Errorf("failed to open php.ini: %v", err)
	}
	defer file.Close()
	
	settings := &PHPSettings{
		MemoryLimit:       "128M",
		MaxExecutionTime:  30,
		UploadMaxFilesize: "2M",
		PostMaxSize:       "8M",
		MaxInputVars:      1000,
	}
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, ";") || line == "" {
			continue
		}
		
		if strings.HasPrefix(line, "memory_limit") {
			settings.MemoryLimit = cm.extractValue(line)
		} else if strings.HasPrefix(line, "max_execution_time") {
			if val, err := strconv.Atoi(cm.extractValue(line)); err == nil {
				settings.MaxExecutionTime = val
			}
		} else if strings.HasPrefix(line, "upload_max_filesize") {
			settings.UploadMaxFilesize = cm.extractValue(line)
		} else if strings.HasPrefix(line, "post_max_size") {
			settings.PostMaxSize = cm.extractValue(line)
		} else if strings.HasPrefix(line, "max_input_vars") {
			if val, err := strconv.Atoi(cm.extractValue(line)); err == nil {
				settings.MaxInputVars = val
			}
		}
	}
	
	return settings, nil
}

// UpdatePHPSettings updates PHP configuration
func (cm *ConfigManager) UpdatePHPSettings(phpVersion string, settings *PHPSettings) error {
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
	updated = cm.updateOrAddSetting(updated, "memory_limit", settings.MemoryLimit)
	updated = cm.updateOrAddSetting(updated, "max_execution_time", fmt.Sprintf("%d", settings.MaxExecutionTime))
	updated = cm.updateOrAddSetting(updated, "upload_max_filesize", settings.UploadMaxFilesize)
	updated = cm.updateOrAddSetting(updated, "post_max_size", settings.PostMaxSize)
	updated = cm.updateOrAddSetting(updated, "max_input_vars", fmt.Sprintf("%d", settings.MaxInputVars))
	
	// Write back
	return os.WriteFile(phpIni, []byte(updated), 0644) //nosec G703 -- phpVersion validated by ValidatePHPVersion at entry
}

// GetMySQLSettings reads current MySQL settings from my.cnf
func (cm *ConfigManager) GetMySQLSettings() (*MySQLSettings, error) {
	myCnf := "/etc/mysql/mariadb.conf.d/50-server.cnf"
	
	file, err := os.Open(myCnf)
	if err != nil {
		return nil, fmt.Errorf("failed to open my.cnf: %v", err)
	}
	defer file.Close()
	
	settings := &MySQLSettings{
		MaxConnections:   151,
		InnodbBufferPool: "128M",
		QueryCacheSize:   "16M",
		MaxAllowedPacket: "16M",
	}
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		
		if strings.HasPrefix(line, "max_connections") {
			if val, err := strconv.Atoi(cm.extractValue(line)); err == nil {
				settings.MaxConnections = val
			}
		} else if strings.HasPrefix(line, "innodb_buffer_pool_size") {
			settings.InnodbBufferPool = cm.extractValue(line)
		} else if strings.HasPrefix(line, "query_cache_size") {
			settings.QueryCacheSize = cm.extractValue(line)
		} else if strings.HasPrefix(line, "max_allowed_packet") {
			settings.MaxAllowedPacket = cm.extractValue(line)
		}
	}
	
	return settings, nil
}

// UpdateMySQLSettings updates MySQL configuration
func (cm *ConfigManager) UpdateMySQLSettings(settings *MySQLSettings) error {
	myCnf := "/etc/mysql/mariadb.conf.d/50-server.cnf"
	
	// Read current content
	content, err := os.ReadFile(myCnf)
	if err != nil {
		return fmt.Errorf("failed to read my.cnf: %v", err)
	}
	
	// Update settings
	updated := string(content)
	updated = cm.updateOrAddSetting(updated, "max_connections", fmt.Sprintf("%d", settings.MaxConnections))
	updated = cm.updateOrAddSetting(updated, "innodb_buffer_pool_size", settings.InnodbBufferPool)
	updated = cm.updateOrAddSetting(updated, "query_cache_size", settings.QueryCacheSize)
	updated = cm.updateOrAddSetting(updated, "max_allowed_packet", settings.MaxAllowedPacket)
	
	// Write back
	return os.WriteFile(myCnf, []byte(updated), 0644) //nosec G703 -- myCnf is a fixed constant path
}

// extractValue extracts value from "key = value" or "key value" format
func (cm *ConfigManager) extractValue(line string) string {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	
	// Space separated
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		return fields[1]
	}
	
	return ""
}

// updateOrAddSetting updates or adds a setting in config content
func (cm *ConfigManager) updateOrAddSetting(content, key, value string) string {
	// Try to update existing setting
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^(\s*;?\s*)%s(\s*=\s*.*)$`, regexp.QuoteMeta(key)))
	
	replacement := fmt.Sprintf("%s = %s", key, value)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, replacement)
	}
	
	// If not found, add at the end
	return content + "\n" + replacement + "\n"
}
