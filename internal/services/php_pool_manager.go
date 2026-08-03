package services

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

var phpEtcDir = "/etc/php"

func poolFilePath(phpVersion, poolName string) string {
	return filepath.Join(phpEtcDir, phpVersion, "fpm", "pool.d", poolName+".conf")
}

func poolDirPath(phpVersion string) string {
	return filepath.Join(phpEtcDir, phpVersion, "fpm", "pool.d")
}

type PHPPoolManager struct{}

func NewPHPPoolManager() *PHPPoolManager { return &PHPPoolManager{} }

var (
	validPoolName   = regexp.MustCompile(`^site[0-9]+$`)
	validListenMode = regexp.MustCompile(`^0[0-7]{3}$`)
	validPMModes    = map[string]bool{"dynamic": true, "ondemand": true, "static": true}
)

func parsePoolInteger(key, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid %s value %q", key, value)
	}
	return n, nil
}

func (pm *PHPPoolManager) GetPoolConfig(phpVersion, poolName string) (*core.PHPPoolConfig, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	if !validPoolName.MatchString(poolName) {
		return nil, fmt.Errorf("invalid pool name %q", poolName)
	}
	content, err := readManagedConfig(poolFilePath(phpVersion, poolName))
	if err != nil {
		return nil, fmt.Errorf("read pool file: %w", err)
	}
	config := &core.PHPPoolConfig{Name: poolName, PM: "dynamic", PMMaxChildren: 5, ListenMode: "0660"}
	sectionSeen := false
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if sectionSeen || strings.TrimSuffix(strings.TrimPrefix(line, "["), "]") != poolName {
				return nil, fmt.Errorf("pool file contains an unexpected section %q", line)
			}
			sectionSeen = true
			continue
		}
		key, value, ok := configPair(line)
		if !ok {
			return nil, fmt.Errorf("malformed pool directive %q", line)
		}
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
			config.PMMaxChildren, err = parsePoolInteger(key, value)
		case "pm.start_servers":
			config.PMStartServers, err = parsePoolInteger(key, value)
		case "pm.min_spare_servers":
			config.PMMinSpareServers, err = parsePoolInteger(key, value)
		case "pm.max_spare_servers":
			config.PMMaxSpareServers, err = parsePoolInteger(key, value)
		case "pm.max_requests":
			config.PMMaxRequests, err = parsePoolInteger(key, value)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pool file: %w", err)
	}
	if !sectionSeen {
		return nil, fmt.Errorf("pool section %q is missing", poolName)
	}
	for key, value := range map[string]string{"user": config.User, "group": config.Group, "listen.owner": config.ListenOwner, "listen.group": config.ListenGroup} {
		if err := validatePoolIdentity(key, value); err != nil {
			return nil, err
		}
	}
	expectedListen := fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", phpVersion, poolName)
	if config.Listen != expectedListen {
		return nil, fmt.Errorf("pool listen path %q does not match managed socket %q", config.Listen, expectedListen)
	}
	if !validListenMode.MatchString(config.ListenMode) {
		return nil, fmt.Errorf("invalid pool listen mode %q", config.ListenMode)
	}
	if !validPMModes[config.PM] || config.PMMaxChildren < 1 {
		return nil, fmt.Errorf("invalid process-manager configuration")
	}
	return config, nil
}

func clamp(v, min, max, def int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (pm *PHPPoolManager) UpdatePoolConfig(phpVersion string, config *core.PHPPoolConfig) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if config == nil || !validPoolName.MatchString(config.Name) {
		return fmt.Errorf("invalid pool name")
	}
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()

	current, err := pm.GetPoolConfig(phpVersion, config.Name)
	if err != nil {
		return fmt.Errorf("pool %s does not exist for PHP %s: %w", config.Name, phpVersion, err)
	}
	pmMode := config.PM
	if pmMode == "" {
		pmMode = current.PM
	}
	if !validPMModes[pmMode] {
		return fmt.Errorf("invalid process-manager mode %q", config.PM)
	}
	maxChildren := clamp(config.PMMaxChildren, 1, 200, 5)
	startServers := clamp(config.PMStartServers, 1, maxChildren, 2)
	minSpare := clamp(config.PMMinSpareServers, 1, maxChildren, 1)
	maxSpare := clamp(config.PMMaxSpareServers, minSpare, maxChildren, 3)
	maxRequests := clamp(config.PMMaxRequests, 1, 100000, 500)
	content := renderPool(config.Name, current.User, current.Group, current.Listen,
		current.ListenOwner, current.ListenGroup, current.ListenMode,
		pmMode, maxChildren, startServers, minSpare, maxSpare, maxRequests)
	return applyManagedConfigLocked(poolFilePath(phpVersion, config.Name), []byte(content), 0o644,
		func() error { return phpFPMConfigTest(phpVersion) },
		func() error { return reloadPHPFPM(phpVersion) })
}

func renderPool(name, user, group, listen, listenOwner, listenGroup, listenMode, pmMode string,
	maxChildren, startServers, minSpare, maxSpare, maxRequests int) string {
	return fmt.Sprintf(`[%s]
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
`, name, user, group, listen, listenOwner, listenGroup, listenMode,
		pmMode, maxChildren, startServers, minSpare, maxSpare, maxRequests)
}

func (pm *PHPPoolManager) DeletePoolByName(phpVersion, poolName string) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if !validPoolName.MatchString(poolName) {
		return fmt.Errorf("invalid pool name %q", poolName)
	}
	err := deleteManagedConfig(poolFilePath(phpVersion, poolName), 0o644,
		func() error { return phpFPMConfigTest(phpVersion) },
		func() error { return reloadPHPFPM(phpVersion) })
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (pm *PHPPoolManager) ListPoolNames(phpVersion string) ([]string, error) {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(poolDirPath(phpVersion))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".conf") {
			continue
		}
		name := strings.TrimSuffix(file.Name(), ".conf")
		if validPoolName.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (pm *PHPPoolManager) MigratePool(oldVersion, newVersion, poolName string) error {
	if err := ValidatePHPVersion(oldVersion); err != nil {
		return err
	}
	if err := ValidatePHPVersion(newVersion); err != nil {
		return err
	}
	if oldVersion == newVersion {
		return fmt.Errorf("source and target PHP versions are identical")
	}
	if !validPoolName.MatchString(poolName) {
		return fmt.Errorf("invalid pool name %q", poolName)
	}
	managedConfigMutationMu.Lock()
	defer managedConfigMutationMu.Unlock()

	oldConfig, err := pm.GetPoolConfig(oldVersion, poolName)
	if err != nil {
		return fmt.Errorf("get pool config from PHP %s: %w", oldVersion, err)
	}
	newPath := poolFilePath(newVersion, poolName)
	pmMode := oldConfig.PM
	if !validPMModes[pmMode] {
		return fmt.Errorf("invalid source process-manager mode %q", pmMode)
	}
	maxChildren := clamp(oldConfig.PMMaxChildren, 1, 200, 5)
	startServers := clamp(oldConfig.PMStartServers, 1, maxChildren, 2)
	minSpare := clamp(oldConfig.PMMinSpareServers, 1, maxChildren, 1)
	maxSpare := clamp(oldConfig.PMMaxSpareServers, minSpare, maxChildren, 3)
	maxRequests := clamp(oldConfig.PMMaxRequests, 1, 100000, 500)
	content := renderPool(poolName, oldConfig.User, oldConfig.Group,
		fmt.Sprintf("/var/run/php/php%s-fpm-%s.sock", newVersion, poolName),
		oldConfig.ListenOwner, oldConfig.ListenGroup, oldConfig.ListenMode,
		pmMode, maxChildren, startServers, minSpare, maxSpare, maxRequests)
	if err := createManagedConfigLocked(newPath, []byte(content), 0o644,
		func() error { return phpFPMConfigTest(newVersion) },
		func() error { return reloadPHPFPM(newVersion) }); err != nil {
		return fmt.Errorf("activate target PHP %s pool: %w", newVersion, err)
	}
	oldPath := poolFilePath(oldVersion, poolName)
	if err := deleteManagedConfigLocked(oldPath, 0o644,
		func() error { return phpFPMConfigTest(oldVersion) },
		func() error { return reloadPHPFPM(oldVersion) }); err != nil {
		cleanupErr := deleteManagedConfigLocked(newPath, 0o644,
			func() error { return phpFPMConfigTest(newVersion) },
			func() error { return reloadPHPFPM(newVersion) })
		return errors.Join(fmt.Errorf("remove source PHP %s pool: %w", oldVersion, err),
			func() error {
				if cleanupErr != nil {
					return fmt.Errorf("rollback target pool: %w", cleanupErr)
				}
				return nil
			}())
	}
	return nil
}
