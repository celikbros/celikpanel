package services

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

var phpExtensionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,127}$`)

type PHPFPMManager struct {
	tmpl          *template.Template
	PoolManager   *PHPPoolManager
	ConfigManager *PHPConfigManager
}

func NewPHPFPMManager() (*PHPFPMManager, error) {
	tmpl, err := template.New("pool").Parse(phpPoolTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse PHP pool template: %w", err)
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

// CreatePool creates and activates a PHP-FPM pool as one rollback-safe change.
func (pm *PHPFPMManager) CreatePool(siteID int, username, phpVersion string) (string, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return "", err
	}
	if siteID < 1 {
		return "", fmt.Errorf("site ID must be positive")
	}
	if err := validatePoolIdentity("user", username); err != nil {
		return "", err
	}

	poolName := fmt.Sprintf("site%d", siteID)
	path := poolFilePath(phpVersion, poolName)
	socket := fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", phpVersion, poolName)
	var body bytes.Buffer
	if err := pm.tmpl.Execute(&body, PoolData{SiteID: siteID, Username: username, Socket: socket}); err != nil {
		return "", fmt.Errorf("render PHP pool %s: %w", poolName, err)
	}
	if err := createManagedConfig(path, body.Bytes(), 0o644,
		func() error { return phpFPMConfigTest(phpVersion) },
		func() error { return reloadPHPFPM(phpVersion) }); err != nil {
		return "", fmt.Errorf("create PHP pool %s: %w", poolName, err)
	}
	return socket, nil
}

func (pm *PHPFPMManager) DeletePool(siteID int, phpVersion string) error {
	if siteID < 1 {
		return fmt.Errorf("site ID must be positive")
	}
	return pm.PoolManager.DeletePoolByName(phpVersion, fmt.Sprintf("site%d", siteID))
}

func (pm *PHPFPMManager) ListPools(phpVersion string) ([]core.PHPPool, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(poolDirPath(phpVersion))
	if err != nil {
		return nil, fmt.Errorf("read PHP pool directory: %w", err)
	}

	pools := make([]core.PHPPool, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".conf") {
			continue
		}
		pool, err := pm.parsePoolFile(filepath.Join(poolDirPath(phpVersion), file.Name()))
		if err != nil {
			return nil, fmt.Errorf("parse PHP pool %s: %w", file.Name(), err)
		}
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools, nil
}

func (pm *PHPFPMManager) parsePoolFile(path string) (core.PHPPool, error) {
	content, err := readManagedConfig(path)
	if err != nil {
		return core.PHPPool{}, err
	}
	pool := core.PHPPool{PM: "dynamic"}
	sectionSeen := false
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if sectionSeen {
				return core.PHPPool{}, fmt.Errorf("multiple pool sections are not supported")
			}
			pool.Name = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			if !configNamePattern.MatchString(pool.Name) {
				return core.PHPPool{}, fmt.Errorf("invalid pool section %q", pool.Name)
			}
			sectionSeen = true
			continue
		}
		key, value, ok := configPair(line)
		if !ok {
			return core.PHPPool{}, fmt.Errorf("malformed pool directive %q", line)
		}
		switch key {
		case "user":
			if err := validatePoolIdentity("user", value); err != nil {
				return core.PHPPool{}, err
			}
			pool.User = value
		case "listen":
			if !strings.HasPrefix(value, "/") && strings.Contains(value, ":") {
				_, portText, err := net.SplitHostPort(value)
				if err != nil {
					return core.PHPPool{}, fmt.Errorf("invalid TCP listen address %q: %w", value, err)
				}
				pool.Port, err = strconv.Atoi(portText)
				if err != nil || pool.Port < 1 || pool.Port > 65535 {
					return core.PHPPool{}, fmt.Errorf("invalid PHP pool port %q", portText)
				}
			}
		case "pm":
			if !validPMModes[value] {
				return core.PHPPool{}, fmt.Errorf("invalid process-manager mode %q", value)
			}
			pool.PM = value
		}
	}
	if err := scanner.Err(); err != nil {
		return core.PHPPool{}, fmt.Errorf("scan PHP pool: %w", err)
	}
	if !sectionSeen || pool.User == "" {
		return core.PHPPool{}, fmt.Errorf("pool section or user is missing")
	}
	return pool, nil
}

func (pm *PHPFPMManager) ListExtensions(phpVersion string) ([]core.PHPExtension, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	availableDir := filepath.Join(phpEtcDir, phpVersion, "mods-available")
	enabledDir := filepath.Join(phpEtcDir, phpVersion, "fpm", "conf.d")
	availableFiles, err := os.ReadDir(availableDir)
	if err != nil {
		return nil, fmt.Errorf("read available PHP modules: %w", err)
	}
	enabledFiles, err := os.ReadDir(enabledDir)
	if err != nil {
		return nil, fmt.Errorf("read enabled PHP modules: %w", err)
	}

	enabled := make(map[string]bool, len(enabledFiles))
	for _, file := range enabledFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".ini") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".ini")
		if index := strings.IndexByte(name, '-'); index >= 0 {
			name = name[index+1:]
		}
		enabled[name] = true
	}

	extensions := make([]core.PHPExtension, 0, len(availableFiles))
	for _, file := range availableFiles {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".ini") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".ini")
		if !phpExtensionPattern.MatchString(name) {
			return nil, fmt.Errorf("invalid PHP module filename %q", file.Name())
		}
		extensions = append(extensions, core.PHPExtension{Name: name, Enabled: enabled[name]})
	}
	sort.Slice(extensions, func(i, j int) bool { return extensions[i].Name < extensions[j].Name })
	return extensions, nil
}

func runPHPModuleCommand(command, phpVersion, extension string) error {
	path := filepath.Join("/usr/sbin", command)
	output, err := exec.Command(path, "-v", phpVersion, "-s", "fpm", extension).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s PHP extension %s: %s: %w", command, extension, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (pm *PHPFPMManager) mutateExtension(phpVersion, extension string, enable bool) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if !phpExtensionPattern.MatchString(extension) {
		return fmt.Errorf("invalid PHP extension %q", extension)
	}

	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()
	extensions, err := pm.ListExtensions(phpVersion)
	if err != nil {
		return fmt.Errorf("inspect PHP extension %s: %w", extension, err)
	}
	available := false
	currentlyEnabled := false
	for _, candidate := range extensions {
		if candidate.Name == extension {
			available = true
			currentlyEnabled = candidate.Enabled
			break
		}
	}
	if !available {
		return fmt.Errorf("PHP extension %q is not available for PHP %s", extension, phpVersion)
	}
	if currentlyEnabled == enable {
		return nil
	}
	command, rollbackCommand := "phpdismod", "phpenmod"
	if enable {
		command, rollbackCommand = "phpenmod", "phpdismod"
	}
	if err := runPHPModuleCommand(command, phpVersion, extension); err != nil {
		return err
	}
	if err := reloadPHPFPM(phpVersion); err != nil {
		rollbackErr := runPHPModuleCommand(rollbackCommand, phpVersion, extension)
		reloadRollbackErr := reloadPHPFPM(phpVersion)
		if rollbackErr != nil {
			rollbackErr = fmt.Errorf("restore prior extension state: %w", rollbackErr)
		}
		if reloadRollbackErr != nil {
			reloadRollbackErr = fmt.Errorf("reload restored extension state: %w", reloadRollbackErr)
		}
		return errors.Join(fmt.Errorf("reload PHP-FPM after %s: %w", command, err), rollbackErr, reloadRollbackErr)
	}
	return nil
}

func (pm *PHPFPMManager) EnableExtension(phpVersion, extension string) error {
	return pm.mutateExtension(phpVersion, extension, true)
}

func (pm *PHPFPMManager) DisableExtension(phpVersion, extension string) error {
	return pm.mutateExtension(phpVersion, extension, false)
}

func (pm *PHPFPMManager) GetConfig(phpVersion string) (*core.PHPConfig, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	content, err := readManagedConfig(phpINIPath(phpVersion))
	if err != nil {
		return nil, fmt.Errorf("read php.ini: %w", err)
	}
	config := &core.PHPConfig{
		MemoryLimit: "128M", UploadMaxFilesize: "2M", PostMaxSize: "8M",
		MaxExecutionTime: "30", DisplayErrors: "Off",
	}
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
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan php.ini: %w", err)
	}
	if err := validatePHPConfig(config); err != nil {
		return nil, fmt.Errorf("invalid php.ini value: %w", err)
	}
	return config, nil
}

func (pm *PHPFPMManager) UpdateConfig(phpVersion string, config *core.PHPConfig) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if err := validatePHPConfig(config); err != nil {
		return err
	}
	path := phpINIPath(phpVersion)
	return mutateManagedConfig(path, 0o644, func(content []byte) ([]byte, error) {
		updated := string(content)
		updated = updateOrAddSetting(updated, "memory_limit", config.MemoryLimit)
		updated = updateOrAddSetting(updated, "max_execution_time", config.MaxExecutionTime)
		updated = updateOrAddSetting(updated, "upload_max_filesize", config.UploadMaxFilesize)
		updated = updateOrAddSetting(updated, "post_max_size", config.PostMaxSize)
		updated = updateOrAddSetting(updated, "display_errors", config.DisplayErrors)
		return []byte(updated), nil
	}, func() error { return phpFPMConfigTest(phpVersion) }, func() error { return reloadPHPFPM(phpVersion) })
}

func (pm *PHPFPMManager) ReloadPHPFPM(version string) error {
	if err := ValidatePHPVersion(version); err != nil {
		return err
	}
	return reloadPHPFPM(version)
}

func (pm *PHPFPMManager) CheckPHPVersion(version string) bool {
	if ValidatePHPVersion(version) != nil {
		return false
	}
	return exec.Command("php"+version, "--version").Run() == nil
}
