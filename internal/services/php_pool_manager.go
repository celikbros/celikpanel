package services

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
)

// phpEtcDir is the root of the distro's PHP tree. It is a variable rather than a
// constant purely so tests can point it at a temp directory — the pool writer
// carries a security invariant (identity is never taken from the caller) and an
// invariant with no test is a promise, not a guarantee.
// phpEtcDir, dağıtımın PHP ağacının köküdür. Sabit değil değişken olmasının tek
// sebebi, testlerin onu geçici bir dizine yöneltebilmesidir — havuz yazıcısı bir
// güvenlik değişmezi taşır (kimlik asla çağırandan alınmaz) ve testi olmayan bir
// değişmez garanti değil, vaattir.
var phpEtcDir = "/etc/php"

func poolFilePath(phpVersion, poolName string) string {
	return fmt.Sprintf("%s/%s/fpm/pool.d/%s.conf", phpEtcDir, phpVersion, poolName)
}

func poolDirPath(phpVersion string) string {
	return fmt.Sprintf("%s/%s/fpm/pool.d", phpEtcDir, phpVersion)
}

// PHPPoolManager handles PHP-FPM pool operations
type PHPPoolManager struct{}

func NewPHPPoolManager() *PHPPoolManager {
	return &PHPPoolManager{}
}

// GetPoolConfig reads detailed pool configuration
func (pm *PHPPoolManager) GetPoolConfig(phpVersion, poolName string) (*core.PHPPoolConfig, error) {
	poolFile := poolFilePath(phpVersion, poolName)
	
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

// validPoolName bounds a pool name to the panel's own naming scheme, since it
// selects the file this writes under /etc/php/<ver>/fpm/pool.d/.
// validPoolName, bir havuz adını panelin kendi adlandırma şemasına sınırlar;
// çünkü /etc/php/<ver>/fpm/pool.d/ altında yazılacak dosyayı o seçer.
var validPoolName = regexp.MustCompile(`^site[0-9]+$`)

// validPMModes are the only process-manager modes php-fpm accepts. Anything
// else makes the master refuse to start the pool.
// validPMModes, php-fpm'in kabul ettiği tek süreç-yöneticisi kipleridir.
// Başka bir şey, master'ın havuzu başlatmayı reddetmesine yol açar.
var validPMModes = map[string]bool{"dynamic": true, "ondemand": true, "static": true}

// clamp bounds a tunable, and substitutes the default when the caller sent
// nothing (0) — a pool with pm.max_children = 0 does not start.
// clamp bir ayarı sınırlar ve çağıran hiçbir şey göndermediyse (0) varsayılanı
// koyar — pm.max_children = 0 olan bir havuz başlamaz.
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

// UpdatePoolConfig rewrites a pool's PERFORMANCE settings, and only those.
//
// The pool's identity — user, group, listen socket and the socket's ownership
// and mode — is never taken from the caller. It is read back from the pool file
// the panel itself wrote at site creation and carried over unchanged. That is
// not defensive style, it closes a live privilege escalation: this function is
// reached from POST /api/v1/domains/{id}/php/pool, which is authorised by
// DOMAIN OWNERSHIP, not by admin. The handler pinned only Version and Name, so
// a customer could send {"pool_config":{"user":"root","group":"root"}} and the
// next FPM reload would run their PHP as root. The socket fields are equally
// load-bearing: `listen` is the path nginx talks to (repoint it and you answer
// another tenant's requests), and listen.owner/mode decide who may speak to the
// pool at all.
//
// The agent enforces this rather than the handler because the agent is the
// layer that cannot be bypassed — a future handler that forgets to pin a field
// must not be able to reopen the hole.
//
// UpdatePoolConfig bir havuzun yalnızca PERFORMANS ayarlarını yeniden yazar.
//
// Havuzun kimliği — kullanıcı, grup, dinlenen soket ve soketin sahipliği ile
// kipi — asla çağırandan alınmaz. Panelin site oluşturulurken kendi yazdığı
// havuz dosyasından geri okunur ve olduğu gibi taşınır. Bu savunmacı bir üslup
// değil, canlı bir yetki yükseltmeyi kapatır: bu fonksiyona
// POST /api/v1/domains/{id}/php/pool üzerinden gelinir ve o rota admin ile
// değil ALAN ADI SAHİPLİĞİ ile yetkilendirilir. Handler yalnız Version ve
// Name'i sabitliyordu; yani bir müşteri {"pool_config":{"user":"root"}}
// gönderip bir sonraki FPM yeniden yüklemesinde PHP'sini root olarak
// koşturabiliyordu. Soket alanları da aynı ölçüde taşıyıcıdır: `listen`,
// nginx'in konuştuğu yoldur (başka yere çevirirsen başka kiracının isteklerini
// yanıtlarsın) ve listen.owner/mode havuzla kimin konuşabileceğine karar verir.
//
// Bunu handler değil agent uygular; çünkü atlatılamayan katman agent'tır — bir
// alanı sabitlemeyi unutan gelecekteki bir handler deliği yeniden açamamalıdır.
func (pm *PHPPoolManager) UpdatePoolConfig(phpVersion string, config *core.PHPPoolConfig) error {
	if err := ValidatePHPVersion(phpVersion); err != nil {
		return err
	}
	if !validPoolName.MatchString(config.Name) {
		return fmt.Errorf("invalid pool name %q", config.Name)
	}

	// The existing pool is the only source of identity. If it is not there,
	// this is not an update — refuse rather than inventing one, because a pool
	// whose user cannot be resolved makes the FPM master refuse to start and
	// takes down every site on this version, not just this one.
	// Kimliğin tek kaynağı mevcut havuzdur. Yoksa bu bir güncelleme değildir —
	// uydurmak yerine reddet; çünkü kullanıcısı çözülemeyen bir havuz, FPM
	// master'ının başlamayı reddetmesine ve bu sürümdeki yalnız bu sitenin
	// değil TÜM sitelerin düşmesine yol açar.
	current, err := pm.GetPoolConfig(phpVersion, config.Name)
	if err != nil {
		return fmt.Errorf("pool %s does not exist for PHP %s: %v", config.Name, phpVersion, err)
	}

	poolFile := poolFilePath(phpVersion, config.Name)

	pmMode := config.PM
	if !validPMModes[pmMode] {
		pmMode = current.PM
		if !validPMModes[pmMode] {
			pmMode = "dynamic"
		}
	}

	// Upper bounds are not comfort limits: pm.max_children multiplies this
	// tenant's memory across the whole host, so an unclamped value from a
	// customer is a one-request denial of service against every other site.
	// Üst sınırlar konfor sınırı değildir: pm.max_children bu kiracının
	// belleğini tüm makine boyunca çarpar; müşteriden gelen sınırsız bir değer,
	// diğer tüm sitelere karşı tek istekle hizmet reddidir.
	maxChildren := clamp(config.PMMaxChildren, 1, 200, 5)
	startServers := clamp(config.PMStartServers, 1, maxChildren, 2)
	minSpare := clamp(config.PMMinSpareServers, 1, maxChildren, 1)
	maxSpare := clamp(config.PMMaxSpareServers, minSpare, maxChildren, 3)
	maxRequests := clamp(config.PMMaxRequests, 0, 100000, 500)

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
		current.User,
		current.Group,
		current.Listen,
		current.ListenOwner,
		current.ListenGroup,
		current.ListenMode,
		pmMode,
		maxChildren,
		startServers,
		minSpare,
		maxSpare,
		maxRequests,
	)

	if err := os.WriteFile(poolFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write pool file: %v", err)
	}

	return reloadPHPFPM(phpVersion)
}

// DeletePoolByName deletes a pool by name
func (pm *PHPPoolManager) DeletePoolByName(phpVersion, poolName string) error {
	poolFile := poolFilePath(phpVersion, poolName)
	
	if err := os.Remove(poolFile); err != nil {
		return fmt.Errorf("failed to delete pool: %v", err)
	}

	return reloadPHPFPM(phpVersion)
}

// ListPoolNames returns list of pool names
func (pm *PHPPoolManager) ListPoolNames(phpVersion string) ([]string, error) {
	poolDir := poolDirPath(phpVersion)
	
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

