package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/crypto/curve25519"
)

// The panel owns the WireGuard peer ledger and pushes the complete desired
// set. The agent performs only privileged host work and never stores a client
// private key.
// WireGuard peer defterini panel tutar ve istenen kümenin tamamını gönderir.
// Agent yalnız ayrıcalıklı makine işlerini yapar ve istemci özel anahtarını
// hiçbir zaman saklamaz.

const (
	wgIface     = "wg0"
	wgSubnet    = "10.8.0.0/24"
	wgServerIP  = "10.8.0.1"
	wgDefaultPt = 51820

	wgNFTMarker = "celikpanel-vpn-managed"
)

// These production paths are replaceable only by focused RPC rollback tests;
// release code always leaves them at the fixed host locations below.
// Bu üretim yolları yalnız odaklı RPC geri alma testlerinde değiştirilir;
// yayın kodu onları daima aşağıdaki sabit makine yollarında bırakır.
var (
	wgConfDir    = "/etc/wireguard"
	wgSysctlPath = "/etc/sysctl.d/99-celikpanel-vpn.conf"
)

func wgConfPath() string { return filepath.Join(wgConfDir, wgIface+".conf") }

// stageAtomicFile writes and fsyncs a same-directory temporary file. The
// caller either renames it into place or removes it, so readers never observe
// a partially written WireGuard configuration.
// stageAtomicFile aynı dizinde geçici bir dosya yazıp fsync eder. Çağıran
// dosyayı yerine taşır ya da siler; okuyucular yarım WireGuard yapılandırması
// görmez.
func stageAtomicFile(target string, content []byte, mode os.FileMode) (string, error) {
	dir := filepath.Dir(target)
	if err := secureMkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := rejectUnsafeAtomicTarget(target); err != nil {
		return "", err
	}
	staged, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*.conf")
	if err != nil {
		return "", err
	}
	name := staged.Name()
	ok := false
	defer func() {
		if !ok {
			staged.Close()
			os.Remove(name)
		}
	}()
	if err := staged.Chmod(mode); err != nil {
		return "", err
	}
	if _, err := staged.Write(content); err != nil {
		return "", err
	}
	if err := staged.Sync(); err != nil {
		return "", err
	}
	if err := staged.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

// syncAtomicParentDirectory is replaceable only by focused durability tests.
// Production always fsyncs and closes the real parent directory.
// syncAtomicParentDirectory yalnız odaklı dayanıklılık testlerinde değiştirilir.
// Üretim daima gerçek üst dizini fsync edip kapatır.
var syncAtomicParentDirectory = func(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open parent directory for sync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("sync parent directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close parent directory: %w", err)
	}
	return nil
}

func commitAtomicFile(staged, target string) error {
	if err := rejectUnsafeAtomicTarget(target); err != nil {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(target))
}

func rejectUnsafeAtomicTarget(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("refusing to replace a non-regular or symbolic-link target")
	}
	file, _, err := secureOpenRegular(target)
	if err != nil {
		return err
	}
	return file.Close()
}

func writeAtomicFile(target string, content []byte, mode os.FileMode) error {
	staged, err := stageAtomicFile(target, content, mode)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	return commitAtomicFile(staged, target)
}

func writeSecureRootFile(target string, content []byte, mode os.FileMode) error {
	file, info, err := secureOpenRegular(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := validateRepoFileMetadata(info, mode); err != nil {
			file.Close()
			return errors.New("managed file failed security validation")
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return writeAtomicFile(target, content, mode)
}

type vpnManagedFileSnapshot struct {
	exists bool
	data   []byte
	mode   os.FileMode
}

func snapshotVPNManagedFile(
	target string, mode os.FileMode,
) (vpnManagedFileSnapshot, error) {
	file, info, err := secureOpenRegular(target)
	if errors.Is(err, os.ErrNotExist) {
		return vpnManagedFileSnapshot{mode: mode}, nil
	}
	if err != nil {
		return vpnManagedFileSnapshot{}, err
	}
	defer file.Close()
	if err := validateRepoFileMetadata(info, mode); err != nil {
		return vpnManagedFileSnapshot{}, errors.New("managed file failed security validation")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return vpnManagedFileSnapshot{}, err
	}
	after, err := file.Stat()
	if err != nil {
		return vpnManagedFileSnapshot{}, err
	}
	if !os.SameFile(info, after) || after.Mode() != info.Mode() {
		return vpnManagedFileSnapshot{}, errors.New("managed file changed while it was read")
	}
	return vpnManagedFileSnapshot{
		exists: true,
		data:   data,
		mode:   mode,
	}, nil
}

func restoreVPNManagedFile(target string, snapshot vpnManagedFileSnapshot) error {
	if snapshot.exists {
		return writeSecureRootFile(target, snapshot.data, snapshot.mode)
	}
	file, info, err := secureOpenRegular(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateRepoFileMetadata(info, snapshot.mode); err != nil {
		file.Close()
		return errors.New("managed rollback target failed security validation")
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := secureRemoveRegular(target); err != nil {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(target))
}

func validateRootOwnedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o022 != 0 {
		return errors.New("managed directory failed security validation")
	}
	if uid, unixMetadata := repoFileOwnerUID(info); unixMetadata && uid != 0 {
		return errors.New("managed directory is not root-owned")
	}
	return nil
}

func validateVPNDirectory(path string) error {
	if err := rejectSymlinkPath(path); err != nil {
		return errors.New("VPN configuration directory failed security validation")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("VPN configuration directory is not a secure directory")
	}
	if info.Mode().Perm() != 0o700 {
		return errors.New("VPN configuration directory permissions must be 0700")
	}
	if err := validateRootOwnedDirectory(path); err != nil {
		return err
	}
	return nil
}

func readSecureVPNConfig() ([]byte, error) {
	if err := validateVPNDirectory(wgConfDir); err != nil {
		return nil, err
	}
	file, info, err := secureOpenRegular(wgConfPath())
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := validateRepoFileMetadata(info, 0o600); err != nil {
		return nil, errors.New("VPN configuration file failed security validation")
	}
	data, err := io.ReadAll(io.LimitReader(file, vpnPeerSyncConfigMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > vpnPeerSyncConfigMaxSize {
		return nil, errors.New("VPN configuration file exceeds the size limit")
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) || after.Mode() != info.Mode() {
		return nil, errors.New("VPN configuration changed while it was read")
	}
	return data, nil
}

func removeSecureVPNConfig() error {
	file, info, err := secureOpenRegular(wgConfPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := validateRepoFileMetadata(info, 0o600); err != nil {
		file.Close()
		return errors.New("VPN configuration file failed security validation")
	}
	if err := file.Close(); err != nil {
		return err
	}
	return secureRemoveRegular(wgConfPath())
}

// wgKeyPair produces WireGuard-compatible base64 Curve25519 keys.
// wgKeyPair WireGuard ile uyumlu base64 Curve25519 anahtarları üretir.
func wgKeyPair() (privB64, pubB64 string, err error) {
	var private [32]byte
	if _, err = rand.Read(private[:]); err != nil {
		return "", "", err
	}
	private[0] &= 248
	private[31] &= 127
	private[31] |= 64
	public, err := curve25519.X25519(private[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(private[:]),
		base64.StdEncoding.EncodeToString(public), nil
}

type VPNKeysResponse = transport.VPNKeysResponse

// GenerateVPNKeys returns a fresh client key pair and preshared key. Only the
// public and encrypted preshared keys are kept by the panel.
// GenerateVPNKeys yeni istemci anahtar çiftini ve ön paylaşımlı anahtarı
// döndürür. Panel yalnız genel anahtarı ve şifreli ön paylaşımlı anahtarı tutar.
func (a *Agent) GenerateVPNKeys(_ *transport.Empty, response *VPNKeysResponse) error {
	private, public, err := wgKeyPair()
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	psk := make([]byte, 32)
	if _, err := rand.Read(psk); err != nil {
		response.Error = err.Error()
		return nil
	}
	response.PrivateKey = private
	response.PublicKey = public
	response.PresharedKey = base64.StdEncoding.EncodeToString(psk)
	return nil
}

type SetupVPNRequest = transport.SetupVPNRequest

type SetupVPNResponse = transport.SetupVPNResponse

// SetupVPN writes the server configuration once, enables forwarding and
// starts wg-quick@wg0. The release-managed port is always UDP/51820.
// SetupVPN sunucu yapılandırmasını bir kez yazar, yönlendirmeyi açar ve
// wg-quick@wg0 hizmetini başlatır. Sürümce yönetilen port daima UDP/51820'dir.
func validateVPNSetupPort(port int) error {
	if port != 0 && port != wgDefaultPt {
		return errors.New("custom VPN ports are not supported; use UDP/51820")
	}
	return nil
}

func preflightVPNHost() error {
	for _, command := range []string{"wg", "wg-quick", "nft", "ip", "sysctl"} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("required VPN tool %s is not installed", command)
		}
	}
	return nil
}

func validLinuxInterfaceName(value string) bool {
	if len(value) == 0 || len(value) > 15 || value == "." || value == ".." {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9':
		case index > 0 && (char == '_' || char == '-' || char == '.'):
		default:
			return false
		}
	}
	return true
}

func defaultRouteIface(ctx context.Context) (string, error) {
	output, err := serviceMutationCommand(
		ctx, "ip", "-o", "route", "show", "default",
	).Output()
	if err != nil {
		return "", errors.New("could not inspect the default network route")
	}
	fields := strings.Fields(string(output))
	for index := range fields {
		if fields[index] == "dev" && index+1 < len(fields) {
			iface := fields[index+1]
			if !validLinuxInterfaceName(iface) {
				return "", errors.New("the default route has an unsafe interface name")
			}
			return iface, nil
		}
	}
	return "", errors.New("no default network route is available")
}

type nftListDocument struct {
	NFTables []struct {
		Table *struct {
			Family string `json:"family"`
			Name   string `json:"name"`
		} `json:"table,omitempty"`
	} `json:"nftables"`
}

func inspectVPNNFTTable(ctx context.Context) (bool, string, error) {
	output, err := serviceMutationCommand(ctx, "nft", "-j", "list", "tables").Output()
	if err != nil {
		return false, "", errors.New("could not inspect nftables policies")
	}
	var document nftListDocument
	if err := json.Unmarshal(output, &document); err != nil {
		return false, "", errors.New("nftables returned an invalid policy inventory")
	}
	exists := false
	for _, object := range document.NFTables {
		if object.Table != nil &&
			object.Table.Family == "inet" &&
			object.Table.Name == "celikpanel_vpn" {
			exists = true
			break
		}
	}
	if !exists {
		return false, "", nil
	}
	table, err := serviceMutationCommand(
		ctx, "nft", "list", "table", "inet", "celikpanel_vpn",
	).Output()
	if err != nil {
		return true, "", errors.New("could not inspect the CelikPanel VPN nftables policy")
	}
	return true, string(table), nil
}

func managedVPNConfig(config []byte) bool {
	value := string(config)
	return strings.Contains(value, "# CelikPanel VPN server - managed by the panel.") &&
		strings.Contains(value, wgNFTMarker) &&
		strings.Contains(value, "PostDown = nft delete table inet celikpanel_vpn")
}

func managedVPNNFTTable(table string) bool {
	return strings.Contains(table, wgNFTMarker) &&
		strings.Contains(table, "chain postrouting") &&
		strings.Contains(table, "hook postrouting") &&
		strings.Contains(table, wgSubnet) &&
		strings.Contains(table, "masquerade")
}

func reconcileVPNNFTTable(
	ctx context.Context, config []byte, interfaceRunning bool,
) error {
	exists, table, err := inspectVPNNFTTable(ctx)
	if err != nil || !exists {
		return err
	}
	if !managedVPNConfig(config) || !managedVPNNFTTable(table) {
		return errors.New("an unmanaged nftables policy conflicts with CelikPanel VPN")
	}
	if interfaceRunning {
		return nil
	}
	if output, err := serviceMutationCommand(
		ctx, "nft", "delete", "table", "inet", "celikpanel_vpn",
	).CombinedOutput(); err != nil {
		_ = output
		return errors.New("could not remove a verified stale CelikPanel VPN policy")
	}
	return nil
}

func (a *Agent) SetupVPN(request *SetupVPNRequest, response *SetupVPNResponse) error {
	*response = SetupVPNResponse{}
	if request == nil {
		return errors.New("VPN setup request is required")
	}
	port := request.Port
	if err := validateVPNSetupPort(port); err != nil {
		*response = SetupVPNResponse{Error: err.Error()}
		return nil
	}
	ctx, finishStep, err := a.requiredServiceMutationStep(
		request.ServiceMutationBinding,
		newServiceMutationStepClaim(serviceMutationStepSetupVPN, "wireguard", "", "setup"),
	)
	if err != nil {
		*response = SetupVPNResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()
	if err := preflightVPNHost(); err != nil {
		response.Error = err.Error()
		return nil
	}
	systemctl, err := serviceMutationSystemctlResolver()
	if err != nil {
		response.Error = "systemd client failed security validation"
		return nil
	}
	if err := secureMkdirAll(wgConfDir, 0o700); err != nil {
		response.Error = "could not prepare the VPN configuration directory"
		return nil
	}
	if err := validateVPNDirectory(wgConfDir); err != nil {
		response.Error = err.Error()
		return nil
	}

	current, configErr := readSecureVPNConfig()
	newConfig := false
	switch {
	case errors.Is(configErr, os.ErrNotExist):
		private, _, err := wgKeyPair()
		if err != nil {
			response.Error = "could not generate the VPN server key"
			return nil
		}
		wanInterface, err := defaultRouteIface(ctx)
		if err != nil {
			response.Error = err.Error()
			return nil
		}
		// A dedicated nftables table provides client NAT and clean teardown.
		// Ayrı nftables tablosu istemci NAT'ını ve temiz kapanışı sağlar.
		postUp := fmt.Sprintf(
			`nft add table inet celikpanel_vpn '{ comment "%s"; }'; nft add chain inet celikpanel_vpn postrouting '{ type nat hook postrouting priority 100 ; }'; nft add rule inet celikpanel_vpn postrouting ip saddr %s oifname "%s" masquerade comment "%s"`,
			wgNFTMarker, wgSubnet, wanInterface, wgNFTMarker,
		)
		postDown := `nft delete table inet celikpanel_vpn`
		current = []byte(fmt.Sprintf(`[Interface]
# CelikPanel VPN server - managed by the panel.
Address = %s/24
ListenPort = %d
PrivateKey = %s
PostUp = %s
PostDown = %s
`, wgServerIP, wgDefaultPt, private, postUp, postDown))
		newConfig = true
	case configErr != nil:
		response.Error = "VPN configuration failed security validation"
		return nil
	default:
		if !managedVPNConfig(current) {
			response.Error = "the existing VPN configuration is not managed by CelikPanel"
			return nil
		}
		_, port := wgConfIdentityFrom(current)
		if port != wgDefaultPt {
			response.Error = fmt.Sprintf(
				"existing VPN configuration uses UDP/%d; CelikPanel requires UDP/51820",
				port,
			)
			return nil
		}
	}

	interfaceRunning := serviceMutationCommand(ctx, "wg", "show", wgIface).Run() == nil
	if err := validateRootOwnedDirectory(filepath.Dir(wgSysctlPath)); err != nil {
		response.Error = "the sysctl policy directory failed security validation"
		return nil
	}
	sysctlBefore, err := snapshotVPNManagedFile(wgSysctlPath, 0o644)
	if err != nil {
		response.Error = "the sysctl policy file failed security validation"
		return nil
	}
	forwardingOutput, err := serviceMutationCommand(
		ctx, "sysctl", "-n", "net.ipv4.ip_forward",
	).Output()
	if err != nil {
		response.Error = "could not read the IPv4 forwarding state"
		return nil
	}
	forwardingBefore := strings.TrimSpace(string(forwardingOutput))
	if forwardingBefore != "0" && forwardingBefore != "1" {
		response.Error = "the IPv4 forwarding state is invalid"
		return nil
	}
	unitName := "wg-quick@" + wgIface
	enabledOutput, _ := serviceMutationCommand(
		ctx, systemctl, "is-enabled", unitName,
	).Output()
	unitWasEnabled := strings.HasPrefix(
		strings.TrimSpace(string(enabledOutput)), "enabled",
	)
	configBefore := vpnManagedFileSnapshot{
		exists: !newConfig,
		data:   append([]byte(nil), current...),
		mode:   0o600,
	}
	sysctlAttempted := false
	runtimeForwardingAttempted := false
	configAttempted := false
	serviceAttempted := false

	// Rollback is verified against host state. A command error is tolerated only
	// when a fresh probe proves that the requested before-state was restored.
	// Geri alma makine durumuna karşı doğrulanır. Komut hatası yalnız yeni bir
	// kontrol istenen önceki durumun geri geldiğini kanıtlarsa kabul edilir.
	rollback := func() error {
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()

		var rollbackErrors []error
		if serviceAttempted && !interfaceRunning {
			_, _ = serviceMutationCommand(recoveryCtx, systemctl, "stop", unitName).CombinedOutput()
			if serviceMutationCommand(recoveryCtx, "wg", "show", wgIface).Run() == nil {
				rollbackErrors = append(
					rollbackErrors, errors.New("VPN interface remained active"),
				)
			}
		}
		if serviceAttempted && !unitWasEnabled {
			_, _ = serviceMutationCommand(recoveryCtx, systemctl, "disable", unitName).CombinedOutput()
			probe, _ := serviceMutationCommand(
				recoveryCtx, systemctl, "is-enabled", unitName,
			).Output()
			if strings.HasPrefix(strings.TrimSpace(string(probe)), "enabled") {
				rollbackErrors = append(
					rollbackErrors, errors.New("VPN unit remained enabled"),
				)
			}
		}
		if newConfig && serviceAttempted {
			if err := reconcileVPNNFTTable(recoveryCtx, current, false); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("VPN policy: %w", err))
			}
		}
		if configAttempted {
			if err := restoreVPNManagedFile(wgConfPath(), configBefore); err != nil {
				rollbackErrors = append(
					rollbackErrors, fmt.Errorf("VPN configuration: %w", err),
				)
			}
			response.Created = false
		}
		if sysctlAttempted {
			if err := restoreVPNManagedFile(wgSysctlPath, sysctlBefore); err != nil {
				rollbackErrors = append(
					rollbackErrors, fmt.Errorf("forwarding policy: %w", err),
				)
			}
		}
		if runtimeForwardingAttempted {
			value := "net.ipv4.ip_forward=" + forwardingBefore
			_, _ = serviceMutationCommand(recoveryCtx, "sysctl", "-w", value).CombinedOutput()
			probe, err := serviceMutationCommand(
				recoveryCtx, "sysctl", "-n", "net.ipv4.ip_forward",
			).Output()
			if err != nil || strings.TrimSpace(string(probe)) != forwardingBefore {
				rollbackErrors = append(
					rollbackErrors, errors.New("IPv4 forwarding was not restored"),
				)
			}
		}
		return errors.Join(rollbackErrors...)
	}
	failSetup := func(message string) {
		if rollbackErr := rollback(); rollbackErr != nil {
			log.Printf("VPN setup rollback failed: %v", rollbackErr)
			response.Error = "VPN setup failed and automatic recovery is required"
			return
		}
		response.Error = message
	}

	if err := reconcileVPNNFTTable(ctx, current, interfaceRunning); err != nil {
		response.Error = err.Error()
		return nil
	}

	// Persist and apply IPv4 forwarding for full-tunnel clients.
	// Tam tünel istemcileri için IPv4 yönlendirmesini kalıcılaştırıp uygula.
	sysctlAttempted = true
	if err := writeSecureRootFile(
		wgSysctlPath,
		[]byte("net.ipv4.ip_forward = 1\n"),
		0o644,
	); err != nil {
		failSetup("could not persist IPv4 forwarding policy")
		return nil
	}
	runtimeForwardingAttempted = true
	if output, err := serviceMutationCommand(
		ctx, "sysctl", "-w", "net.ipv4.ip_forward=1",
	).CombinedOutput(); err != nil {
		_ = output
		failSetup("could not enable IPv4 forwarding")
		return nil
	}

	if newConfig {
		configAttempted = true
		if err := writeSecureRootFile(wgConfPath(), current, 0o600); err != nil {
			failSetup("could not persist the VPN server configuration")
			return nil
		}
		response.Created = true
	}
	serviceAttempted = true
	if err := enableServiceForMutationWithExecutable(ctx, systemctl, unitName, true); err != nil {
		failSetup("wg-quick failed to start the VPN server")
		return nil
	}
	if err := serviceMutationCommand(ctx, "wg", "show", wgIface).Run(); err != nil {
		failSetup("the VPN interface did not become ready")
		return nil
	}
	response.Detail = "VPN server is up on udp/" + strconv.Itoa(wgDefaultPt)
	return nil
}

type VPNPeerSpec = transport.VPNPeerSpec

type SyncVPNPeersRequest = transport.SyncVPNPeersRequest

type SyncVPNPeersResponse = transport.SyncVPNPeersResponse

const syncVPNPeersLegacyUnsupportedError = "Agent.SyncVPNPeers is unsupported; use Agent.SyncVPNPeersV2"

// SyncVPNPeers is retained only as a fail-closed compatibility endpoint. It
// must not inspect the request, acquire a lease, or touch host state.
func (a *Agent) SyncVPNPeers(
	_ *SyncVPNPeersRequest,
	response *SyncVPNPeersResponse,
) error {
	*response = SyncVPNPeersResponse{Error: syncVPNPeersLegacyUnsupportedError}
	return nil
}

func probeWireGuardInterface(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	output, err := serviceMutationCommand(ctx, "wg", "show", "interfaces").Output()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, errors.New("WireGuard interface probe failed")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	interfaceUp := false
	seen := make(map[string]struct{})
	for _, name := range strings.Fields(string(output)) {
		// Linux IFNAMSIZ includes the terminating NUL, so a real name is at
		// most 15 bytes and cannot contain a path separator or embedded NUL.
		if len(name) > 15 || name == "." || name == ".." ||
			strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\x00') {
			return false, errors.New("WireGuard interface probe returned invalid output")
		}
		if _, duplicate := seen[name]; duplicate {
			return false, errors.New("WireGuard interface probe returned invalid output")
		}
		seen[name] = struct{}{}
		if name == wgIface {
			interfaceUp = true
		}
	}
	return interfaceUp, nil
}

// SyncVPNPeersV2 validates and stages the full peer set, applies it live when
// wg0 is proven up, and only then atomically commits wg0.conf.
func (a *Agent) SyncVPNPeersV2(
	request *SyncVPNPeersRequest,
	response *SyncVPNPeersResponse,
) error {
	*response = SyncVPNPeersResponse{}
	if request == nil {
		return errors.New("VPN peer sync request is required")
	}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(
		request.DesiredGeneration,
		request.Peers,
	)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	authorizedRequest := *request
	authorizedRequest.DesiredGeneration = commitment.DesiredGeneration
	authorizedRequest.Peers = commitment.Peers
	request = &authorizedRequest
	ctx, finishStep, err := a.requiredServiceMutationStep(
		request.ServiceMutationBinding,
		newServiceMutationStepClaim(
			serviceMutationStepSyncVPNPeers,
			"wireguard",
			commitment.Qualifier,
			"sync",
		),
	)
	if err != nil {
		*response = SyncVPNPeersResponse{Error: err.Error()}
		return nil
	}
	defer finishStep()

	current, err := readSecureVPNConfig()
	if err != nil {
		response.Error = "VPN server is not set up"
		return nil
	}
	requestID, err := vpnPeerSyncCommitIdentity(ctx, commitment.Qualifier)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	interfaceConfig := string(current)
	if index := strings.Index(interfaceConfig, "[Peer]"); index >= 0 {
		interfaceConfig = interfaceConfig[:index]
	}
	interfaceConfig, err = replaceVPNPeerSyncReceiptMarker(interfaceConfig, requestID, commitment.Qualifier)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	interfaceConfig = strings.TrimRight(interfaceConfig, "\n") + "\n"
	var desired strings.Builder
	desired.WriteString(interfaceConfig)
	for _, peer := range request.Peers {
		fmt.Fprintf(
			&desired,
			"\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32\n",
			peer.PublicKey, peer.PresharedKey, peer.IP,
		)
	}
	interfaceUp, err := probeWireGuardInterface(ctx)
	if err != nil {
		response.Error = "could not determine VPN interface state"
		return nil
	}
	if err := ctx.Err(); err != nil {
		response.Error = "service mutation lease ended before VPN peers were committed"
		return nil
	}

	staged, err := stageAtomicFile(wgConfPath(), []byte(desired.String()), 0o600)
	if err != nil {
		response.Error = err.Error()
		return nil
	}
	defer os.Remove(staged)

	if err := ctx.Err(); err != nil {
		response.Error = "service mutation lease ended before VPN peers were committed"
		return nil
	}
	if interfaceUp {
		if err := applyWireGuardConfig(ctx, staged); err != nil {
			recoveryCtx, cancel, recoveryContextErr := serviceMutationCancellingRecoveryContext(ctx, 30*time.Second)
			if recoveryContextErr != nil {
				poisonErr := poisonVPNPeerSyncRollback(ctx, errors.Join(err, recoveryContextErr))
				log.Printf("VPN peer recovery context failed after sync error %v: %v; poison: %v", err, recoveryContextErr, poisonErr)
				response.Error = "VPN peer synchronization failed and automatic recovery is required"
				return nil
			}
			rollbackErr := applyWireGuardBytes(recoveryCtx, current)
			cancel()
			if rollbackErr != nil {
				poisonErr := poisonVPNPeerSyncRollback(ctx, errors.Join(err, rollbackErr))
				log.Printf("VPN peer live rollback failed after sync error %v: %v; poison: %v", err, rollbackErr, poisonErr)
				response.Error = "VPN peer synchronization failed and automatic recovery is required"
				return nil
			}
			response.Error = err.Error()
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		if interfaceUp {
			recoveryCtx, cancel, recoveryContextErr := serviceMutationCancellingRecoveryContext(ctx, 30*time.Second)
			if recoveryContextErr != nil {
				poisonErr := poisonVPNPeerSyncRollback(ctx, recoveryContextErr)
				log.Printf("VPN peer recovery context failed after lease cancellation: %v; poison: %v", recoveryContextErr, poisonErr)
				response.Error = "VPN peer synchronization was canceled and automatic recovery is required"
				return nil
			}
			rollbackErr := applyWireGuardBytes(recoveryCtx, current)
			cancel()
			if rollbackErr != nil {
				poisonErr := poisonVPNPeerSyncRollback(ctx, rollbackErr)
				log.Printf("VPN peer live rollback failed after lease cancellation: %v; poison: %v", rollbackErr, poisonErr)
				response.Error = "VPN peer synchronization was canceled and automatic recovery is required"
				return nil
			}
		}
		response.Error = "service mutation lease ended before VPN peers were committed"
		return nil
	}
	hostPublished, commitErr := commitStandaloneVPNPeerSyncStep(ctx, func() error {
		return commitAtomicFile(staged, wgConfPath())
	})
	if commitErr != nil {
		if hostPublished {
			// Publication won the commit race. The manager either persisted
			// terminal success or retained the host lock for startup recovery.
			log.Printf("VPN peer host publication completed with receipt error: %v", commitErr)
			response.Applied = true
			response.AppliedGeneration = request.DesiredGeneration
			return nil
		}
		// The rename may have happened before parent-directory fsync failed.
		// Always attempt both live and durable rollback; one failure must not
		// prevent the other recovery path from running.
		// Yeniden adlandırma üst dizin fsync hatasından önce gerçekleşmiş olabilir.
		// Canlı ve kalıcı geri almayı daima dene; birinin hatası diğer kurtarma
		// yolunun çalışmasını engellememelidir.
		recoveryCtx, cancel, recoveryContextErr := serviceMutationCancellingRecoveryContext(ctx, 30*time.Second)
		if recoveryContextErr != nil {
			poisonErr := poisonVPNPeerSyncRollback(ctx, errors.Join(commitErr, recoveryContextErr))
			log.Printf("VPN peer recovery context failed after commit error %v: %v; poison: %v", commitErr, recoveryContextErr, poisonErr)
			response.Error = "persist VPN config failed and automatic recovery is required"
			return nil
		}
		rollbackErr := runVPNCommitRollback(
			interfaceUp,
			func() error { return applyWireGuardBytes(recoveryCtx, current) },
			func() error { return writeSecureRootFile(wgConfPath(), current, 0o600) },
		)
		cancel()
		if rollbackErr != nil {
			poisonErr := poisonVPNPeerSyncRollback(ctx, errors.Join(commitErr, rollbackErr))
			log.Printf("VPN peer commit rollback failed after %v: %v; poison: %v", commitErr, rollbackErr, poisonErr)
			response.Error = "persist VPN config failed and automatic recovery is required"
			return nil
		}
		response.Error = "persist VPN config failed; previous state restored"
		return nil
	}
	response.Applied = true
	response.AppliedGeneration = request.DesiredGeneration
	return nil
}

// runVPNCommitRollback keeps the two recovery paths independent so a failed
// live restore cannot suppress the durable restore attempt.
// runVPNCommitRollback iki kurtarma yolunu bağımsız tutar; canlı geri alma
// hatası kalıcı geri alma denemesini engelleyemez.
func runVPNCommitRollback(
	interfaceUp bool,
	restoreLive func() error,
	restoreDisk func() error,
) error {
	var rollbackErrors []error
	if interfaceUp {
		if err := restoreLive(); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("live state: %w", err))
		}
	}
	if err := restoreDisk(); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("durable state: %w", err))
	}
	return errors.Join(rollbackErrors...)
}

func applyWireGuardBytes(ctx context.Context, config []byte) error {
	staged, err := stageAtomicFile(wgConfPath(), config, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	return applyWireGuardConfig(ctx, staged)
}

func applyWireGuardConfig(ctx context.Context, configPath string) error {
	stripped, err := serviceMutationCommand(
		ctx, "wg-quick", "strip", configPath,
	).Output()
	if err != nil {
		return errors.New("wg-quick strip failed")
	}
	temporary, err := os.CreateTemp("", "celikpanel-wg-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(stripped); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if output, err := serviceMutationCommand(
		ctx, "wg", "syncconf", wgIface, temporary.Name(),
	).CombinedOutput(); err != nil {
		return fmt.Errorf("wg syncconf failed: %s", firstLine(string(output)))
	}
	return nil
}

type VPNPeerStat = transport.VPNPeerStat

type VPNStatusResponse = transport.VPNStatusResponse

// VPNStatus reports tool, config, interface and live peer-counter state.
// VPNStatus araç, yapılandırma, arayüz ve canlı peer sayaç durumunu bildirir.
func (a *Agent) VPNStatus(_ *transport.Empty, response *VPNStatusResponse) error {
	if err := preflightVPNHost(); err != nil {
		return nil
	}
	response.Installed = true
	config, err := readSecureVPNConfig()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			response.Error = "VPN configuration failed security validation"
		}
		return nil
	}
	if !managedVPNConfig(config) {
		response.Error = "the VPN configuration is not managed by CelikPanel"
		return nil
	}
	response.Configured = true
	response.Endpoint = detectPublicIP()

	dump, err := exec.Command("wg", "show", wgIface, "dump").Output()
	if err != nil {
		response.ServerPublicKey, response.Port = wgConfIdentityFrom(config)
		return nil
	}
	response.Running = true
	for index, line := range strings.Split(strings.TrimSpace(string(dump)), "\n") {
		fields := strings.Split(line, "\t")
		if index == 0 {
			if len(fields) >= 3 {
				response.ServerPublicKey = fields[1]
				response.Port, _ = strconv.Atoi(fields[2])
			}
			continue
		}
		if len(fields) >= 7 {
			handshake, _ := strconv.ParseInt(fields[4], 10, 64)
			received, _ := strconv.ParseInt(fields[5], 10, 64)
			transmitted, _ := strconv.ParseInt(fields[6], 10, 64)
			response.Peers = append(response.Peers, VPNPeerStat{
				PublicKey: fields[0], LastHandshake: handshake,
				RxBytes: received, TxBytes: transmitted,
			})
		}
	}
	return nil
}

// wgConfIdentityFrom derives the public key and port from validated bytes.
// wgConfIdentityFrom doğrulanmış baytlardan genel anahtarı ve portu türetir.
func wgConfIdentityFrom(config []byte) (public string, port int) {
	for _, line := range strings.Split(string(config), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "PrivateKey":
			raw, err := base64.StdEncoding.DecodeString(value)
			if err == nil && len(raw) == 32 {
				derived, err := curve25519.X25519(raw, curve25519.Basepoint)
				if err == nil {
					public = base64.StdEncoding.EncodeToString(derived)
				}
			}
		case "ListenPort":
			port, _ = strconv.Atoi(value)
		}
	}
	return public, port
}

// detectPublicIP returns the source address selected for the public internet.
// detectPublicIP internet için seçilen kaynak adresini döndürür.
func detectPublicIP() string {
	output, err := exec.Command("ip", "-o", "route", "get", "1.1.1.1").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	for index := range fields {
		if fields[index] == "src" && index+1 < len(fields) {
			return fields[index+1]
		}
	}
	return ""
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}
