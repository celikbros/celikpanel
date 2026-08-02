package services

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

const (
	additionalPHPBegin = "; BEGIN CELIKPANEL ADDITIONAL DIRECTIVES"
	additionalPHPEnd   = "; END CELIKPANEL ADDITIONAL DIRECTIVES"
)

type PHPConfigManager struct{}

func NewPHPConfigManager() *PHPConfigManager { return &PHPConfigManager{} }

func (cm *PHPConfigManager) GetExtendedConfig(phpVersion string) (*core.ExtendedPHPConfig, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	content, err := readManagedConfig(phpINIPath(phpVersion))
	if err != nil {
		return nil, fmt.Errorf("read php.ini: %w", err)
	}
	config := &core.ExtendedPHPConfig{
		MemoryLimit: "128M", MaxExecutionTime: "30", MaxInputTime: "60",
		PostMaxSize: "8M", UploadMaxFilesize: "2M", OpcacheEnable: "1",
		RealpathCacheSize: "4096K", DisplayErrors: "Off", LogErrors: "On",
		AllowUrlFopen: "On", FileUploads: "On", ShortOpenTag: "Off",
	}
	inAdditional := false
	additionalSeen := false
	additional := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == additionalPHPBegin {
			if inAdditional || additionalSeen {
				return nil, fmt.Errorf("duplicate additional-directive begin marker")
			}
			additionalSeen = true
			inAdditional = true
			continue
		}
		if line == additionalPHPEnd {
			if !inAdditional {
				return nil, fmt.Errorf("additional-directive end marker without begin marker")
			}
			inAdditional = false
			continue
		}
		if inAdditional {
			additional = append(additional, raw)
			continue
		}
		if strings.HasPrefix(line, ";") || line == "" {
			continue
		}
		key, value, ok := configPair(line)
		if !ok {
			continue
		}
		value = trimInlineComment(value, ';')
		switch key {
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
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan php.ini: %w", err)
	}
	if inAdditional {
		return nil, fmt.Errorf("unterminated additional-directive block")
	}
	config.AdditionalDirectives = strings.TrimSpace(strings.Join(additional, "\n"))
	if err := validateExtendedPHPConfig(config); err != nil {
		return nil, fmt.Errorf("invalid php.ini value: %w", err)
	}
	return config, nil
}

func replaceAdditionalPHPBlock(content, directives string) (string, error) {
	beginCount := strings.Count(content, additionalPHPBegin)
	endCount := strings.Count(content, additionalPHPEnd)
	if beginCount > 1 || endCount > 1 {
		return "", fmt.Errorf("duplicate CelikPanel additional-directive marker")
	}
	if beginCount != endCount {
		return "", fmt.Errorf("malformed CelikPanel additional-directive block")
	}
	if beginCount == 1 {
		begin := strings.Index(content, additionalPHPBegin)
		end := strings.Index(content, additionalPHPEnd)
		if end < begin {
			return "", fmt.Errorf("malformed CelikPanel additional-directive block")
		}
		end += len(additionalPHPEnd)
		prefix := strings.TrimRight(content[:begin], "\r\n")
		suffix := strings.TrimLeft(content[end:], "\r\n")
		switch {
		case prefix != "" && suffix != "":
			content = prefix + "\n" + suffix
		case prefix != "":
			content = prefix
		default:
			content = suffix
		}
	}
	directives = strings.TrimSpace(directives)
	if directives == "" {
		return strings.TrimRight(content, "\r\n") + "\n", nil
	}
	return strings.TrimRight(content, "\r\n") + "\n\n" + additionalPHPBegin + "\n" + directives + "\n" + additionalPHPEnd + "\n", nil
}

type phpSettingValue struct {
	key   string
	value string
}

func (cm *PHPConfigManager) UpdateExtendedConfig(phpVersion string, config *core.ExtendedPHPConfig) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if err := validateExtendedPHPConfig(config); err != nil {
		return err
	}
	settings := []phpSettingValue{
		{"memory_limit", config.MemoryLimit},
		{"max_execution_time", config.MaxExecutionTime},
		{"max_input_time", config.MaxInputTime},
		{"post_max_size", config.PostMaxSize},
		{"upload_max_filesize", config.UploadMaxFilesize},
		{"opcache.enable", config.OpcacheEnable},
		{"disable_functions", config.DisableFunctions},
		{"include_path", config.IncludePath},
		{"session.save_path", config.SessionSavePath},
		{"realpath_cache_size", config.RealpathCacheSize},
		{"open_basedir", config.OpenBasedir},
		{"error_reporting", config.ErrorReporting},
		{"display_errors", config.DisplayErrors},
		{"log_errors", config.LogErrors},
		{"allow_url_fopen", config.AllowUrlFopen},
		{"file_uploads", config.FileUploads},
		{"short_open_tag", config.ShortOpenTag},
	}
	path := phpINIPath(phpVersion)
	return mutateManagedConfig(path, 0o644, func(content []byte) ([]byte, error) {
		updated := string(content)
		for _, setting := range settings {
			updated = updateOrAddSetting(updated, setting.key, setting.value)
		}
		updated, err := replaceAdditionalPHPBlock(updated, config.AdditionalDirectives)
		if err != nil {
			return nil, err
		}
		return []byte(updated), nil
	}, func() error { return phpFPMConfigTest(phpVersion) }, func() error { return reloadPHPFPM(phpVersion) })
}
