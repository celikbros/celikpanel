package services

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

var (
	configSizePattern       = regexp.MustCompile(`(?i)^(?:0|[1-9][0-9]{0,12})(?:[KMGTPE])?$`)
	configNamePattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)
	additionalKeyPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)
	disableFunctionsPattern = regexp.MustCompile(`^[A-Za-z0-9_,[:space:]]*$`)
)

func rejectConfigLineBreak(name, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%s contains a forbidden line break or NUL byte", name)
	}
	return nil
}

func validateSize(name, value string, allowUnlimited bool) error {
	value = strings.TrimSpace(value)
	if allowUnlimited && value == "-1" {
		return nil
	}
	if !configSizePattern.MatchString(value) {
		return fmt.Errorf("%s must be a non-negative integer with an optional K/M/G/T/P/E suffix", name)
	}
	return nil
}

func validateInteger(name, value string, min, max int) error {
	if err := rejectConfigLineBreak(name, value); err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < min || n > max {
		return fmt.Errorf("%s must be an integer between %d and %d", name, min, max)
	}
	return nil
}

func validateToggle(name, value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "off", "1", "0":
		return nil
	default:
		return fmt.Errorf("%s must be On, Off, 1 or 0", name)
	}
}

func validatePHPSettings(settings *PHPSettings) error {
	if settings == nil {
		return fmt.Errorf("PHP settings are required")
	}
	if err := validateSize("memory_limit", settings.MemoryLimit, true); err != nil {
		return err
	}
	if settings.MaxExecutionTime < 0 || settings.MaxExecutionTime > 86400 {
		return fmt.Errorf("max_execution_time must be between 0 and 86400")
	}
	if err := validateSize("upload_max_filesize", settings.UploadMaxFilesize, false); err != nil {
		return err
	}
	if err := validateSize("post_max_size", settings.PostMaxSize, false); err != nil {
		return err
	}
	if settings.MaxInputVars < 1 || settings.MaxInputVars > 10_000_000 {
		return fmt.Errorf("max_input_vars must be between 1 and 10000000")
	}
	return nil
}

func validateMySQLSettings(settings *MySQLSettings) error {
	if settings == nil {
		return fmt.Errorf("MySQL settings are required")
	}
	if settings.MaxConnections < 1 || settings.MaxConnections > 1_000_000 {
		return fmt.Errorf("max_connections must be between 1 and 1000000")
	}
	for name, value := range map[string]string{
		"innodb_buffer_pool_size": settings.InnodbBufferPool,
		"query_cache_size":        settings.QueryCacheSize,
		"max_allowed_packet":      settings.MaxAllowedPacket,
	} {
		if err := validateSize(name, value, false); err != nil {
			return err
		}
	}
	return nil
}

func validatePHPConfig(config *core.PHPConfig) error {
	if config == nil {
		return fmt.Errorf("PHP configuration is required")
	}
	if err := validateSize("memory_limit", config.MemoryLimit, true); err != nil {
		return err
	}
	if err := validateSize("upload_max_filesize", config.UploadMaxFilesize, false); err != nil {
		return err
	}
	if err := validateSize("post_max_size", config.PostMaxSize, false); err != nil {
		return err
	}
	if err := validateInteger("max_execution_time", config.MaxExecutionTime, 0, 86400); err != nil {
		return err
	}
	return validateToggle("display_errors", config.DisplayErrors)
}

func validateExtendedPHPConfig(config *core.ExtendedPHPConfig) error {
	if config == nil {
		return fmt.Errorf("extended PHP configuration is required")
	}
	if err := validateSize("memory_limit", config.MemoryLimit, true); err != nil {
		return err
	}
	if err := validateInteger("max_execution_time", config.MaxExecutionTime, 0, 86400); err != nil {
		return err
	}
	if err := validateInteger("max_input_time", config.MaxInputTime, -1, 86400); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"post_max_size":       config.PostMaxSize,
		"upload_max_filesize": config.UploadMaxFilesize,
		"realpath_cache_size": config.RealpathCacheSize,
	} {
		if err := validateSize(name, value, false); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{
		"opcache.enable":  config.OpcacheEnable,
		"display_errors":  config.DisplayErrors,
		"log_errors":      config.LogErrors,
		"allow_url_fopen": config.AllowUrlFopen,
		"file_uploads":    config.FileUploads,
		"short_open_tag":  config.ShortOpenTag,
	} {
		if err := validateToggle(name, value); err != nil {
			return err
		}
	}
	if !disableFunctionsPattern.MatchString(config.DisableFunctions) {
		return fmt.Errorf("disable_functions contains an invalid character")
	}
	for name, value := range map[string]string{
		"include_path":      config.IncludePath,
		"session.save_path": config.SessionSavePath,
		"open_basedir":      config.OpenBasedir,
		"error_reporting":   config.ErrorReporting,
	} {
		if err := rejectConfigLineBreak(name, value); err != nil {
			return err
		}
	}
	return validateAdditionalPHPDirectives(config.AdditionalDirectives)
}

var managedExtendedPHPKeys = map[string]bool{
	"memory_limit": true, "max_execution_time": true, "max_input_time": true,
	"post_max_size": true, "upload_max_filesize": true, "opcache.enable": true,
	"disable_functions": true, "include_path": true, "session.save_path": true,
	"realpath_cache_size": true, "open_basedir": true, "error_reporting": true,
	"display_errors": true, "log_errors": true, "allow_url_fopen": true,
	"file_uploads": true, "short_open_tag": true,
}

func validateAdditionalPHPDirectives(directives string) error {
	if strings.ContainsRune(directives, '\x00') || strings.Contains(directives, "\r") {
		return fmt.Errorf("additional directives contain a forbidden CR or NUL byte")
	}
	for number, raw := range strings.Split(directives, "\n") {
		line := strings.TrimSpace(raw)
		if line == additionalPHPBegin || line == additionalPHPEnd {
			return fmt.Errorf("additional directive line %d contains a reserved CelikPanel marker", number+1)
		}
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return fmt.Errorf("additional directive line %d cannot open a new section", number+1)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("additional directive line %d must use key = value", number+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if !additionalKeyPattern.MatchString(key) || value == "" {
			return fmt.Errorf("additional directive line %d is invalid", number+1)
		}
		if managedExtendedPHPKeys[strings.ToLower(key)] {
			return fmt.Errorf("additional directive %q duplicates a managed setting", key)
		}
	}
	return nil
}

func validatePoolIdentity(name, value string) error {
	if !configNamePattern.MatchString(value) {
		return fmt.Errorf("pool %s is invalid", name)
	}
	return nil
}

var phpFPMConfigTest = func(version string) error {
	binary, err := exec.LookPath("php-fpm" + version)
	if err != nil {
		// Some supported package layouts do not expose an FPM binary in PATH.
		// Validation is therefore best-effort here; activation remains mandatory.
		return nil
	}
	output, err := exec.Command(binary, "-t").CombinedOutput()
	if err != nil {
		return fmt.Errorf("php-fpm%s -t: %s: %w", version, strings.TrimSpace(string(output)), err)
	}
	return nil
}

var mysqlConfigTest = func() error {
	var binary string
	for _, candidate := range []string{"mariadbd", "mysqld"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			binary = path
			break
		}
	}
	if binary == "" {
		return nil
	}
	output, err := exec.Command(binary, "--verbose", "--help").CombinedOutput()
	if err != nil {
		return fmt.Errorf("database configuration test: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

var restartMariaDB = func() error {
	output, err := exec.Command("systemctl", "restart", "mariadb").CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart mariadb: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
