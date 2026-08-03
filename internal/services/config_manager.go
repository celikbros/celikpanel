package services

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ConfigManager manages service configuration files.
type ConfigManager struct{}

var mysqlServerConfigPath = "/etc/mysql/mariadb.conf.d/50-server.cnf"

func NewConfigManager() *ConfigManager { return &ConfigManager{} }

func phpINIPath(phpVersion string) string {
	return filepath.Join(phpEtcDir, phpVersion, "fpm", "php.ini")
}

type PHPSettings struct {
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxInputVars      int    `json:"max_input_vars"`
}

type MySQLSettings struct {
	MaxConnections   int    `json:"max_connections"`
	InnodbBufferPool string `json:"innodb_buffer_pool_size"`
	QueryCacheSize   string `json:"query_cache_size"`
	MaxAllowedPacket string `json:"max_allowed_packet"`
}

func configPair(line string) (key, value string, ok bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key, value = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	return key, value, key != "" && value != ""
}

func trimInlineComment(value string, marker byte) string {
	if index := strings.IndexByte(value, marker); index >= 0 {
		value = value[:index]
	}
	return strings.TrimSpace(value)
}

func (cm *ConfigManager) GetPHPSettings(phpVersion string) (*PHPSettings, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	content, err := readManagedConfig(phpINIPath(phpVersion))
	if err != nil {
		return nil, fmt.Errorf("read php.ini: %w", err)
	}
	settings := &PHPSettings{MemoryLimit: "128M", MaxExecutionTime: 30, UploadMaxFilesize: "2M", PostMaxSize: "8M", MaxInputVars: 1000}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := configPair(line)
		if !ok {
			continue
		}
		value = trimInlineComment(value, ';')
		switch key {
		case "memory_limit":
			settings.MemoryLimit = value
		case "max_execution_time":
			settings.MaxExecutionTime, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse max_execution_time: %w", err)
			}
		case "upload_max_filesize":
			settings.UploadMaxFilesize = value
		case "post_max_size":
			settings.PostMaxSize = value
		case "max_input_vars":
			settings.MaxInputVars, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse max_input_vars: %w", err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan php.ini: %w", err)
	}
	if err := validatePHPSettings(settings); err != nil {
		return nil, fmt.Errorf("invalid php.ini value: %w", err)
	}
	return settings, nil
}

func (cm *ConfigManager) UpdatePHPSettings(phpVersion string, settings *PHPSettings) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if err := validatePHPSettings(settings); err != nil {
		return err
	}
	path := phpINIPath(phpVersion)
	return mutateManagedConfig(path, 0o644, func(content []byte) ([]byte, error) {
		updated := string(content)
		updated = cm.updateOrAddSetting(updated, "memory_limit", settings.MemoryLimit)
		updated = cm.updateOrAddSetting(updated, "max_execution_time", strconv.Itoa(settings.MaxExecutionTime))
		updated = cm.updateOrAddSetting(updated, "upload_max_filesize", settings.UploadMaxFilesize)
		updated = cm.updateOrAddSetting(updated, "post_max_size", settings.PostMaxSize)
		updated = cm.updateOrAddSetting(updated, "max_input_vars", strconv.Itoa(settings.MaxInputVars))
		return []byte(updated), nil
	}, func() error { return phpFPMConfigTest(phpVersion) }, func() error { return reloadPHPFPM(phpVersion) })
}

func (cm *ConfigManager) GetMySQLSettings() (*MySQLSettings, error) {
	content, err := readManagedConfig(mysqlServerConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read database configuration: %w", err)
	}
	settings := &MySQLSettings{MaxConnections: 151, InnodbBufferPool: "128M", QueryCacheSize: "16M", MaxAllowedPacket: "16M"}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := configPair(line)
		if !ok {
			continue
		}
		value = trimInlineComment(value, '#')
		switch key {
		case "max_connections":
			settings.MaxConnections, err = strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("parse max_connections: %w", err)
			}
		case "innodb_buffer_pool_size":
			settings.InnodbBufferPool = value
		case "query_cache_size":
			settings.QueryCacheSize = value
		case "max_allowed_packet":
			settings.MaxAllowedPacket = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan database configuration: %w", err)
	}
	if err := validateMySQLSettings(settings); err != nil {
		return nil, fmt.Errorf("invalid database configuration value: %w", err)
	}
	return settings, nil
}

func (cm *ConfigManager) UpdateMySQLSettings(settings *MySQLSettings) error {
	if err := validateMySQLSettings(settings); err != nil {
		return err
	}
	return mutateManagedConfig(mysqlServerConfigPath, 0o644, func(content []byte) ([]byte, error) {
		updated := string(content)
		updated = cm.updateOrAddSetting(updated, "max_connections", strconv.Itoa(settings.MaxConnections))
		updated = cm.updateOrAddSetting(updated, "innodb_buffer_pool_size", settings.InnodbBufferPool)
		updated = cm.updateOrAddSetting(updated, "query_cache_size", settings.QueryCacheSize)
		updated = cm.updateOrAddSetting(updated, "max_allowed_packet", settings.MaxAllowedPacket)
		return []byte(updated), nil
	}, mysqlConfigTest, restartMariaDB)
}

func (cm *ConfigManager) updateOrAddSetting(content, key, value string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^([ \t]*[;#]?[ \t]*)%s([ \t]*=[ \t]*.*)$`, regexp.QuoteMeta(key)))
	replacement := fmt.Sprintf("%s = %s", key, value)
	if re.MatchString(content) {
		return re.ReplaceAllStringFunc(content, func(string) string { return replacement })
	}
	return strings.TrimRight(content, "\n") + "\n" + replacement + "\n"
}
