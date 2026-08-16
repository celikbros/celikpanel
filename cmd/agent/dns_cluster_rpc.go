package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A paired PowerDNS node can be primary and secondary at the same time because
// those roles belong to zones, not whole machines. Locally managed zones are
// MASTER; zones announced by the configured peer arrive as SLAVE through
// autoprimary. Both machines therefore serve every zone without a panel-to-
// panel account or API channel.
//
// Eşlenmiş PowerDNS düğümü aynı anda birincil ve ikincil olabilir; çünkü bu
// roller makineye değil zone'a aittir. Yerelde yönetilen zone'lar MASTER olur,
// yapılandırılmış eşin bildirdikleri autoprimary üzerinden SLAVE gelir. Böylece
// panelden panele hesap ya da API kanalı olmadan iki makine de her zone'u sunar.

const (
	dnsRoleStandalone = "standalone"
	dnsRolePaired     = "paired"
	dnsRolePrimary    = "primary"
	dnsRoleSecondary  = "secondary"
)

var (
	dnsClusterConf = "/etc/powerdns/pdns.d/celikpanel-cluster.conf"
	dnsManagedConf = "/etc/powerdns/pdns.d/celikpanel.conf"
	dnsMainConf    = "/etc/powerdns/pdns.conf"

	dnsClusterLookPath = exec.LookPath
	dnsClusterReadFile = os.ReadFile
	dnsClusterStat     = os.Lstat
	dnsClusterReadDir  = os.ReadDir

	// Managed PowerDNS drop-ins are root-owned in production. Focused tests
	// replace this with the current euid because their temporary directories
	// cannot be root-owned.
	dnsClusterConfigRequiredOwnerUID = uint32(0)
	dnsClusterConfigOwnerUID         = platformRepoFileOwnerUID
	dnsClusterRestart                = func(ctx context.Context) ([]byte, error) {
		return serviceMutationCommand(ctx, "systemctl", "restart", "pdns").CombinedOutput()
	}
	dnsClusterRetrieve = func(ctx context.Context, zone string) ([]byte, error) {
		return serviceMutationCommand(ctx, "pdns_control", "retrieve", zone).CombinedOutput()
	}
	dnsClusterPurge = func(ctx context.Context, zone string) ([]byte, error) {
		return serviceMutationCommand(ctx, "pdns_control", "purge", zone+"$").CombinedOutput()
	}
	dnsClusterApplyAutoprimaryTx = applyAutoprimaryTx
	dnsClusterSetLocalZoneTypeTx = setLocalZoneTypeTx
)

type DNSClusterRequest = transport.DNSClusterRequest

type DNSClusterResponse = transport.DNSClusterResponse

type ConfigureDNSClusterV2Request = transport.ConfigureDNSClusterV2Request

type ConfigureDNSClusterV2Response = transport.ConfigureDNSClusterV2Response

type DNSClusterReadinessResponse = transport.DNSClusterReadinessResponse

// DNSClusterReadiness is a read-only preflight. It lets the panel explain the
// exact missing prerequisite before the operator reaches the save action.
func (a *Agent) DNSClusterReadiness(_ *transport.Empty, resp *DNSClusterReadinessResponse) error {
	return inspectDNSClusterReadiness(resp)
}

func inspectDNSClusterReadiness(resp *DNSClusterReadinessResponse) error {
	if resp == nil {
		return errors.New("DNS cluster readiness response is required")
	}
	*resp = DNSClusterReadinessResponse{}
	if !legacyPowerDNSReadinessAuthorized(resp) {
		return nil
	}
	if err := inspectManagedPowerDNSArtifacts(resp); err != nil || !resp.Ready {
		return err
	}
	if !legacyPowerDNSReadinessAuthorized(resp) {
		return nil
	}
	resp.Detail = "PowerDNS is configured and ready for CelikPanel DNS publication"
	return nil
}

// inspectManagedPowerDNSArtifacts proves only CelikPanel ownership of the
// configuration and database. It deliberately does not grant active DNS
// authority; callers that mutate or publish must separately verify the durable
// engine state and live port-53 owner.
func inspectManagedPowerDNSArtifacts(resp *DNSClusterReadinessResponse) error {
	if resp == nil {
		return errors.New("PowerDNS artifact readiness response is required")
	}
	*resp = DNSClusterReadinessResponse{}
	for _, binary := range []string{"pdns_server", "pdnsutil", "pdns_control"} {
		if _, err := dnsClusterLookPath(binary); err != nil {
			resp.Detail = "PowerDNS tooling is not installed on this server"
			return nil
		}
	}
	info, err := dnsClusterStat(dnsManagedConf)
	if errors.Is(err, os.ErrNotExist) {
		resp.Detail = "PowerDNS is installed but has not been configured by CelikPanel"
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect CelikPanel PowerDNS configuration: %w", err)
	}
	if !info.Mode().IsRegular() ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0) ||
		!dnsClusterConfigOwnerTrusted(info) {
		return errors.New("CelikPanel PowerDNS configuration has unsafe file permissions")
	}
	data, err := dnsClusterReadFile(dnsManagedConf)
	if errors.Is(err, os.ErrNotExist) {
		resp.Detail = "PowerDNS is installed but has not been configured by CelikPanel"
		return nil
	}
	if err != nil {
		return fmt.Errorf("read CelikPanel PowerDNS configuration: %w", err)
	}
	if !validManagedPowerDNSConfig(string(data), pdnsDBPath()) {
		return errors.New("CelikPanel PowerDNS configuration is incomplete or ambiguous")
	}
	effective, detail, err := effectiveManagedPowerDNSConfig()
	if err != nil {
		return err
	}
	if !effective {
		resp.Detail = detail
		return nil
	}
	dbInfo, err := dnsClusterStat(pdnsDBPath())
	if errors.Is(err, os.ErrNotExist) {
		resp.Detail = "PowerDNS is configured but its managed database is missing"
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect managed PowerDNS database: %w", err)
	}
	if !dbInfo.Mode().IsRegular() {
		return errors.New("managed PowerDNS database path is not a regular file")
	}
	resp.Ready = true
	resp.Detail = "PowerDNS configuration and database are managed by CelikPanel"
	return nil
}

func legacyPowerDNSReadinessAuthorized(resp *DNSClusterReadinessResponse) bool {
	if err := legacyPowerDNSDurableAuthorityCheck(false); err != nil {
		log.Printf("PowerDNS readiness blocked by durable DNS engine authority: %v", err)
		resp.Ready = false
		resp.Detail = "PowerDNS is not the active DNS engine on this server"
		return false
	}
	return true
}

func requireManagedDNSClusterReady() error {
	var readiness DNSClusterReadinessResponse
	if err := inspectDNSClusterReadiness(&readiness); err != nil {
		return err
	}
	if !readiness.Ready {
		detail := strings.TrimSpace(readiness.Detail)
		if detail == "" {
			detail = "PowerDNS is not ready for CelikPanel cluster convergence"
		}
		return errors.New(detail)
	}
	return nil
}

func requireManagedPowerDNSArtifacts() error {
	var readiness DNSClusterReadinessResponse
	if err := inspectManagedPowerDNSArtifacts(&readiness); err != nil {
		return err
	}
	if !readiness.Ready {
		detail := strings.TrimSpace(readiness.Detail)
		if detail == "" {
			detail = "PowerDNS managed artifacts could not be verified"
		}
		return errors.New(detail)
	}
	return nil
}

func dnsClusterConfigOwnerTrusted(info os.FileInfo) bool {
	ownerUID, enforceOwner := dnsClusterConfigOwnerUID(info)
	return !enforceOwner || ownerUID == dnsClusterConfigRequiredOwnerUID
}

func powerDNSConfigDirective(line string) (key, value string, found bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, found = strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value), true
}

func effectiveManagedPowerDNSConfig() (bool, string, error) {
	mainInfo, err := dnsClusterStat(dnsMainConf)
	if errors.Is(err, os.ErrNotExist) {
		return false, "PowerDNS is installed but its main configuration is missing", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("inspect PowerDNS main configuration: %w", err)
	}
	if !mainInfo.Mode().IsRegular() ||
		(runtime.GOOS != "windows" && mainInfo.Mode().Perm()&0o022 != 0) ||
		!dnsClusterConfigOwnerTrusted(mainInfo) {
		return false, "", errors.New("PowerDNS main configuration has unsafe file permissions")
	}
	mainData, err := dnsClusterReadFile(dnsMainConf)
	if err != nil {
		return false, "", fmt.Errorf("read PowerDNS main configuration: %w", err)
	}
	wantDir := filepath.Clean(filepath.Dir(dnsManagedConf))
	includeCount := 0
	for _, line := range strings.Split(string(mainData), "\n") {
		key, value, found := powerDNSConfigDirective(line)
		if !found {
			continue
		}
		switch key {
		case "include-dir":
			includeCount++
			if filepath.Clean(value) != wantDir {
				return false, "", errors.New("PowerDNS loads an unexpected include directory")
			}
		case "launch", "gsqlite3-database", "gsqlite3-dnssec",
			"local-address", "zone-cache-refresh-interval", "webserver", "api",
			"primary", "secondary", "autosecondary",
			"allow-axfr-ips", "also-notify",
			"master", "slave", "supermaster", "autoprimary":
			return false, "", errors.New("PowerDNS main configuration overrides managed DNS state")
		}
	}
	if includeCount == 0 {
		return false, "PowerDNS is installed but does not load the CelikPanel configuration directory", nil
	}
	if includeCount != 1 {
		return false, "", errors.New("PowerDNS include directory is configured ambiguously")
	}
	includeInfo, err := dnsClusterStat(wantDir)
	if err != nil {
		return false, "", fmt.Errorf("inspect PowerDNS include directory: %w", err)
	}
	if !includeInfo.IsDir() || includeInfo.Mode()&os.ModeSymlink != 0 {
		return false, "", errors.New("PowerDNS include path is not a trusted directory")
	}
	if ownerUID, enforceOwner := dnsClusterConfigOwnerUID(includeInfo); enforceOwner {
		if includeInfo.Mode().Perm()&0o022 != 0 {
			return false, "", errors.New("PowerDNS include directory is group/other writable")
		}
		if ownerUID != dnsClusterConfigRequiredOwnerUID {
			return false, "", errors.New("PowerDNS include directory has an unexpected owner")
		}
	}
	entries, err := dnsClusterReadDir(wantDir)
	if err != nil {
		return false, "", fmt.Errorf("inspect PowerDNS include directory: %w", err)
	}
	managedBase := filepath.Base(dnsManagedConf)
	clusterBase := filepath.Base(dnsClusterConf)
	for _, entry := range entries {
		name := entry.Name()
		entryPath := filepath.Join(wantDir, name)
		if filepath.Ext(name) != ".conf" || name == managedBase ||
			filepath.Clean(entryPath) == filepath.Clean(dnsMainConf) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() ||
			(runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0) ||
			!dnsClusterConfigOwnerTrusted(info) {
			return false, "", errors.New("PowerDNS include directory contains an unsafe configuration entry")
		}
		other, err := dnsClusterReadFile(entryPath)
		if err != nil {
			return false, "", fmt.Errorf("read PowerDNS include %s: %w", name, err)
		}
		if name == clusterBase {
			if !validDNSClusterPowerDNSConfig(string(other)) {
				return false, "", errors.New("PowerDNS cluster configuration is malformed or ambiguous")
			}
			continue
		}
		for _, line := range strings.Split(string(other), "\n") {
			key, _, found := powerDNSConfigDirective(line)
			if !found {
				continue
			}
			switch key {
			case "launch", "gsqlite3-database", "gsqlite3-dnssec", "include-dir",
				"local-address", "zone-cache-refresh-interval", "webserver", "api",
				"primary", "secondary", "autosecondary",
				"allow-axfr-ips", "also-notify",
				"master", "slave", "supermaster", "autoprimary":
				return false, "", fmt.Errorf("PowerDNS include %s conflicts with the managed backend", name)
			}
		}
	}
	return true, "", nil
}

func validManagedPowerDNSConfig(config, databasePath string) bool {
	want := map[string]string{
		"launch":                      "gsqlite3",
		"gsqlite3-dnssec":             "yes",
		"gsqlite3-database":           filepath.Clean(databasePath),
		"local-address":               "",
		"zone-cache-refresh-interval": "0",
		"webserver":                   "no",
		"api":                         "no",
	}
	seen := make(map[string]string, len(want))
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found {
			continue
		}
		if _, required := want[key]; !required {
			return false
		}
		if _, duplicate := seen[key]; duplicate || value == "" {
			return false
		}
		seen[key] = value
	}
	for key, expected := range want {
		actual, ok := seen[key]
		if !ok {
			return false
		}
		if key == "gsqlite3-database" {
			actual = filepath.Clean(actual)
		}
		if expected == "" {
			if actual == "" {
				return false
			}
		} else if actual != expected {
			return false
		}
	}
	return true
}

func validDNSClusterPowerDNSConfig(config string) bool {
	want := map[string]string{
		"primary": "yes", "secondary": "yes", "autosecondary": "yes",
		"allow-axfr-ips": "", "also-notify": "",
	}
	seen := make(map[string]string, len(want))
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := powerDNSConfigDirective(line)
		if !found {
			return false
		}
		expected, allowed := want[key]
		if !allowed || value == "" {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		if expected != "" && value != expected {
			return false
		}
		seen[key] = value
	}
	for key := range want {
		if _, ok := seen[key]; !ok {
			return false
		}
	}
	parsed := net.ParseIP(seen["allow-axfr-ips"])
	if parsed == nil || parsed.To4() == nil {
		return false
	}
	peer := parsed.To4().String()
	return seen["allow-axfr-ips"] == peer && seen["also-notify"] == peer
}

func normalizeAgentDNSRole(role string) string {
	switch role {
	case dnsRolePaired, dnsRolePrimary, dnsRoleSecondary:
		return dnsRolePaired
	case dnsRoleStandalone:
		return dnsRoleStandalone
	default:
		return ""
	}
}

func dnsClusterConfig(req *DNSClusterRequest) string {
	if req.Role != dnsRolePaired {
		return ""
	}
	return fmt.Sprintf(`# Managed by CelikPanel - do not edit by hand / elle duzenlemeyin
# DNS pair: this server owns local zones and keeps secondary copies from %s.
# DNS cifti: bu sunucu yerel zonelarin sahibi, %s uzerinden gelenlerin ikincilidir.
primary=yes
secondary=yes
autosecondary=yes
allow-axfr-ips=%s
also-notify=%s
`, req.PeerIP, req.PeerIP, req.PeerIP, req.PeerIP)
}

const configureDNSClusterLegacyUnsupportedError = "legacy DNS cluster configuration is unsupported; use Agent.ConfigureDNSClusterV2"

func (a *Agent) ConfigureDNSCluster(_ *DNSClusterRequest, resp *DNSClusterResponse) error {
	*resp = DNSClusterResponse{Error: configureDNSClusterLegacyUnsupportedError}
	return nil
}

func (a *Agent) ConfigureDNSClusterV2(
	req *ConfigureDNSClusterV2Request,
	resp *ConfigureDNSClusterV2Response,
) error {
	*resp = ConfigureDNSClusterV2Response{}
	if req == nil {
		resp.Error = "DNS cluster V2 request is required"
		return nil
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		req.Role, req.PeerIP, req.PeerNS,
	)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	ctx, finish, err := a.requiredServiceMutationStep(
		req.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepConfigureDNSCluster,
			"pdns",
			commitment.Qualifier,
			"configure",
		),
	)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	defer finish()
	if err := requireLegacyPowerDNSMutationSafe(ctx, true); err != nil {
		log.Printf("legacy PowerDNS cluster configuration blocked by durable DNS engine guard: %v", err)
		resp.Error = "PowerDNS cluster configuration is blocked because PowerDNS is not the sole active DNS engine"
		return nil
	}
	return configureDNSClusterV2(ctx, commitment, resp)
}

func configureDNSClusterV2(
	ctx context.Context,
	commitment mutationpayload.DNSClusterConfigCommitment,
	resp *ConfigureDNSClusterV2Response,
) error {
	// Readiness is checked before the durable intent/journal and before the
	// cluster config or database can change. Once intent exists recovery is
	// forward-only, so an unconfigured/ambiguous backend must fail zero-touch.
	if err := requireManagedDNSClusterReady(); err != nil {
		resp.Error = fmt.Sprintf("PowerDNS is not ready for DNS cluster convergence: %v", err)
		return nil
	}
	if err := validateDNSClusterConfigTarget(); err != nil {
		resp.Error = fmt.Sprintf("inspect existing DNS cluster configuration: %v", err)
		return nil
	}
	journal, err := commitDNSClusterConfigIntent(ctx, commitment)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	commitCtx, cancelCommit := context.WithTimeout(
		context.WithoutCancel(ctx), dnsClusterConfigRecoveryTimeout,
	)
	defer cancelCommit()
	peerZones, err := convergeDNSClusterConfig(commitCtx, commitment)
	if err != nil {
		resp.Error = poisonDNSClusterConfigConvergence(ctx, err).Error()
		return nil
	}
	var refreshFailures []string
	for _, zone := range peerZones {
		var out []byte
		var refreshErr error
		if commitment.Role == dnsRoleStandalone {
			out, refreshErr = dnsClusterPurge(commitCtx, zone)
		} else {
			out, refreshErr = dnsClusterRetrieve(commitCtx, zone)
		}
		if refreshErr != nil {
			detail := firstLine(string(out))
			if detail == "" {
				detail = refreshErr.Error()
			}
			refreshFailures = append(refreshFailures, zone+": "+detail)
		}
	}
	if err := publishDNSClusterConfig(commitCtx, journal); err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Applied = true
	if commitment.Role == dnsRolePaired {
		resp.Detail = "DNS pairing enabled; local zones are copied to " + commitment.PeerIP + " and peer zones are copied here"
	} else {
		resp.Detail = "this server serves DNS on its own"
	}
	if len(refreshFailures) > 0 {
		resp.Detail += "; DNS cache/peer refresh needs attention for " + strings.Join(refreshFailures, ", ")
	}
	return nil
}

const dnsClusterConfigMaxSize = 64 << 10

var (
	dnsClusterConfigLstat    = os.Lstat
	dnsClusterConfigReadFile = os.ReadFile
)

func validateDNSClusterConfigTarget() error {
	path := filepath.Clean(dnsClusterConf)
	if !filepath.IsAbs(path) || path != dnsClusterConf {
		return errors.New("DNS cluster configuration path must be canonical and absolute")
	}
	info, err := dnsClusterConfigLstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 0 || info.Size() > dnsClusterConfigMaxSize {
		return errors.New("existing DNS cluster configuration is not a safe regular file")
	}
	data, err := dnsClusterConfigReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > dnsClusterConfigMaxSize {
		return errors.New("existing DNS cluster configuration exceeds the size limit")
	}
	return nil
}

func publishDNSClusterConfigFile(config string) error {
	if err := validateDNSClusterConfigTarget(); err != nil {
		return err
	}
	path := filepath.Clean(dnsClusterConf)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Reject an untrusted include directory before cleanup, staging, removal,
	// or rename can alter anything PowerDNS may load. The final directory fsync
	// repeats the same proof to close replacement races around publication.
	if err := validateDNSClusterConfigDirectory(dir); err != nil {
		return err
	}
	if err := cleanupAbandonedDNSClusterConfigStages(dir); err != nil {
		return err
	}
	if config == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDNSClusterConfigDirectory(path)
	}
	// Never stage with a .conf suffix inside pdns.d. PowerDNS may load every
	// matching drop-in after a crash, before recovery can rename or remove it.
	stage, err := os.CreateTemp(dir, ".celikpanel-cluster-stage-*.tmp")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	published := false
	defer func() {
		_ = stage.Close()
		if !published {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chmod(0o644); err != nil {
		return err
	}
	if _, err := stage.WriteString(config); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, path); err != nil {
		return err
	}
	published = true
	return syncDNSClusterConfigDirectory(path)
}

func cleanupAbandonedDNSClusterConfigStages(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var stages []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".celikpanel-cluster-stage-") &&
			strings.HasSuffix(name, ".tmp") {
			stages = append(stages, filepath.Join(dir, name))
		}
	}
	if len(stages) > dnsClusterConfigJournalStageLimit {
		return errors.New("abandoned DNS cluster config stage count exceeds the limit")
	}
	for _, stage := range stages {
		info, err := os.Lstat(stage)
		if err != nil || !info.Mode().IsRegular() ||
			info.Mode()&os.ModeSymlink != 0 || info.Size() > dnsClusterConfigMaxSize {
			return errors.New("abandoned DNS cluster config stage is unsafe")
		}
	}
	for _, stage := range stages {
		if err := os.Remove(stage); err != nil {
			return err
		}
	}
	if len(stages) != 0 {
		return syncDNSClusterConfigDirectory(dnsClusterConf)
	}
	return nil
}

func convergeDNSClusterConfig(
	ctx context.Context,
	commitment mutationpayload.DNSClusterConfigCommitment,
) ([]string, error) {
	recomputed, err := mutationpayload.CanonicalDNSClusterConfig(
		commitment.Role, commitment.PeerIP, commitment.PeerNS,
	)
	if err != nil || recomputed.Qualifier != commitment.Qualifier {
		return nil, errors.New("DNS cluster convergence payload is not canonical")
	}
	req := &DNSClusterRequest{
		Role: commitment.Role, PeerIP: commitment.PeerIP, PeerNS: commitment.PeerNS,
	}
	config := dnsClusterConfig(req)
	if err := publishDNSClusterConfigFile(config); err != nil {
		return nil, fmt.Errorf("publish DNS cluster configuration: %w", err)
	}
	db, err := openPdnsDB()
	if err != nil {
		return nil, fmt.Errorf("open PowerDNS database: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin PowerDNS cluster transaction: %w", err)
	}
	defer tx.Rollback()
	if err := dnsClusterApplyAutoprimaryTx(tx, req); err != nil {
		_ = db.Close()
		return nil, err
	}
	peerZones, err := dnsClusterSetLocalZoneTypeTx(tx, req)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("commit PowerDNS cluster transaction: %w", err)
	}
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close PowerDNS cluster database: %w", err)
	}
	if out, err := dnsClusterRestart(ctx); err != nil {
		return nil, fmt.Errorf("restart PowerDNS after cluster convergence: %v: %s", err, firstLine(string(out)))
	}
	if err := verifyDNSClusterConfig(commitment, config); err != nil {
		return nil, err
	}
	return peerZones, nil
}

func verifyDNSClusterConfig(
	commitment mutationpayload.DNSClusterConfigCommitment,
	expectedConfig string,
) error {
	if expectedConfig == "" {
		if _, err := dnsClusterConfigLstat(dnsClusterConf); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return errors.New("standalone DNS cluster configuration file still exists")
			}
			return fmt.Errorf("verify absent DNS cluster configuration: %w", err)
		}
	} else {
		if err := validateDNSClusterConfigTarget(); err != nil {
			return err
		}
		actual, err := dnsClusterConfigReadFile(dnsClusterConf)
		if err != nil || string(actual) != expectedConfig {
			return errors.New("DNS cluster configuration readback does not match the committed plan")
		}
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return fmt.Errorf("verify effective managed PowerDNS state: %w", err)
	}
	db, err := openRawDNSZoneReceiptDB()
	if err != nil {
		return fmt.Errorf("open PowerDNS cluster readback: %w", err)
	}
	defer db.Close()
	var bad int
	if commitment.Role == dnsRolePaired {
		if err := db.QueryRow(`
			SELECT
			 (SELECT COUNT(*) FROM domains
			   WHERE UPPER(type) IN ('NATIVE','MASTER')
			     AND (UPPER(type) <> 'MASTER' OR master IS NOT NULL)) +
			 (SELECT COUNT(*) FROM domains WHERE account = 'celikpanel'
			   AND UPPER(type) IN ('SLAVE','SECONDARY')
			   AND COALESCE(master, '') <> ?)
		`, commitment.PeerIP).Scan(&bad); err != nil {
			return err
		}
		var managed, exact int
		if err := db.QueryRow(`SELECT COUNT(*) FROM supermasters WHERE account = 'celikpanel'`).Scan(&managed); err != nil {
			return err
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM supermasters WHERE account = 'celikpanel' AND ip = ? AND nameserver = ?`, commitment.PeerIP, commitment.PeerNS).Scan(&exact); err != nil {
			return err
		}
		if managed != 1 || exact != 1 || bad != 0 {
			return errors.New("PowerDNS paired-state readback does not match the committed plan")
		}
		return nil
	}
	if err := db.QueryRow(`
		SELECT
		 (SELECT COUNT(*) FROM supermasters WHERE account = 'celikpanel') +
		 (SELECT COUNT(*) FROM domains
		   WHERE UPPER(type) IN ('NATIVE','MASTER')
		     AND (UPPER(type) <> 'NATIVE' OR master IS NOT NULL)) +
		 (SELECT COUNT(*) FROM domains WHERE account = 'celikpanel'
		   AND UPPER(type) IN ('SLAVE','SECONDARY'))
	`).Scan(&bad); err != nil {
		return err
	}
	if bad != 0 {
		return errors.New("PowerDNS standalone-state readback does not match the committed plan")
	}
	return nil
}

// applyAutoprimary maintains the secondary's list of trusted primaries. The
// table is PowerDNS's own (`supermasters`), so nothing here invents a schema.
// applyAutoprimary, ikincilin güvendiği birincillerin listesini tutar. Tablo
// PowerDNS'in kendisinindir (`supermasters`); burada şema uydurulmaz.
func applyAutoprimary(req *DNSClusterRequest) error {
	db, err := openPdnsDB()
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	defer tx.Rollback()
	if err := applyAutoprimaryTx(tx, req); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}

func applyAutoprimaryTx(tx *sql.Tx, req *DNSClusterRequest) error {
	if _, err := tx.Exec(`DELETE FROM supermasters WHERE account = 'celikpanel'`); err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	if req.Role != dnsRolePaired && req.Role != dnsRoleSecondary {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO supermasters (ip, nameserver, account) VALUES (?, ?, 'celikpanel')`,
		req.PeerIP, strings.TrimSuffix(req.PeerNS, "."))
	if err != nil {
		return fmt.Errorf("powerdns database: %w", err)
	}
	return nil
}

// setLocalZoneType switches the zones this server already holds between NATIVE
// (nobody replicates) and MASTER (this server notifies a secondary). A
// secondary's zones are created by PowerDNS itself as SLAVE. Only zones marked
// with the CelikPanel account are retargeted or removed as pairing changes;
// manually managed secondaries are left alone.
// setLocalZoneType, bu sunucunun hâlihazırda tuttuğu zone'ları NATIVE (kimse
// çoğaltmıyor) ile MASTER (bu sunucu bir ikincile haber veriyor) arasında
// değiştirir. İkincilin zone'larını PowerDNS kendisi SLAVE olarak oluşturur ve
// onlara dokunulmaz.
func setLocalZoneTypeTx(tx *sql.Tx, req *DNSClusterRequest) ([]string, error) {
	switch req.Role {
	case dnsRolePaired, dnsRolePrimary:
		if _, err := tx.Exec(`
			UPDATE domains
			SET type = 'MASTER', master = NULL, last_check = NULL
			WHERE UPPER(type) IN ('NATIVE', 'MASTER')`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		rows, err := tx.Query(`
			SELECT name
			FROM domains
			WHERE account = 'celikpanel'
			  AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			  AND COALESCE(master, '') <> ?
			ORDER BY name`, req.PeerIP)
		if err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		var changed []string
		for rows.Next() {
			var zone string
			if err := rows.Scan(&zone); err != nil {
				rows.Close()
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
			changed = append(changed, zone)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if _, err := tx.Exec(`
			UPDATE domains
			SET master = ?, last_check = NULL
			WHERE account = 'celikpanel'
			  AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			  AND COALESCE(master, '') <> ?`, req.PeerIP, req.PeerIP); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		return changed, nil
	case dnsRoleStandalone:
		if _, err := tx.Exec(`
			UPDATE domains
			SET type = 'NATIVE', master = NULL, last_check = NULL
			WHERE UPPER(type) IN ('NATIVE', 'MASTER')`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		rows, err := tx.Query(`
			SELECT name
			FROM domains
			WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			ORDER BY name`)
		if err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		var removed []string
		for rows.Next() {
			var zone string
			if err := rows.Scan(&zone); err != nil {
				rows.Close()
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
			removed = append(removed, zone)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		for _, table := range []string{"records", "comments", "domainmetadata", "cryptokeys"} {
			if _, err := tx.Exec(`DELETE FROM ` + table + ` WHERE domain_id IN (
				SELECT id FROM domains
				WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')
			)`); err != nil {
				return nil, fmt.Errorf("powerdns database: %w", err)
			}
		}
		if _, err := tx.Exec(`
			DELETE FROM domains
			WHERE account = 'celikpanel' AND UPPER(type) IN ('SLAVE', 'SECONDARY')`); err != nil {
			return nil, fmt.Errorf("powerdns database: %w", err)
		}
		return removed, nil
	default:
		return nil, nil
	}
}
