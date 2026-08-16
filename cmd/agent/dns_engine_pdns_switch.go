package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsConfigMutation struct {
	before  []dnsFileSnapshot
	desired map[string][]byte
}

func managedPowerDNSStandaloneConfig() []byte {
	listen := publicListenAddresses()
	if listen == "" {
		listen = "0.0.0.0"
	}
	return []byte(fmt.Sprintf(`# Managed by CelikPanel; do not edit by hand.
launch=gsqlite3
gsqlite3-dnssec=yes
gsqlite3-database=%s
local-address=%s
zone-cache-refresh-interval=0
webserver=no
api=no
`, pdnsDBPath(), listen))
}

func preparePDNSConfigMutation() (pdnsConfigMutation, error) {
	mainBefore, err := captureDNSFileSnapshot(dnsMainConf, 0o644, false)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	managedBefore, err := captureDNSFileSnapshot(dnsManagedConf, 0o644, true)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	clusterBefore, err := captureDNSFileSnapshot(dnsClusterConf, 0o644, true)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	if clusterBefore.Exists {
		return pdnsConfigMutation{}, errors.New("paired PowerDNS topology must be disabled before switching DNS engines")
	}
	managedDir := filepath.Clean(filepath.Dir(dnsManagedConf))
	hasInclude, err := validateManagedPowerDNSMainConfig(string(mainBefore.Data), managedDir)
	if err != nil {
		return pdnsConfigMutation{}, err
	}
	mainDesired := append([]byte(nil), mainBefore.Data...)
	if !hasInclude {
		mainDesired = append(mainDesired, []byte("\n# Managed by CelikPanel.\ninclude-dir="+managedDir+"\n")...)
	}
	before := []dnsFileSnapshot{mainBefore, managedBefore, clusterBefore}
	sort.Slice(before, func(left, right int) bool { return before[left].Path < before[right].Path })
	return pdnsConfigMutation{
		before: before,
		desired: map[string][]byte{
			dnsMainConf:    mainDesired,
			dnsManagedConf: managedPowerDNSStandaloneConfig(),
		},
	}, nil
}

func ensurePDNSConfigDirectory() error {
	directory := filepath.Clean(filepath.Dir(dnsManagedConf))
	if !filepath.IsAbs(directory) || directory == string(os.PathSeparator) {
		return errors.New("PowerDNS managed config directory is unsafe")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		return err
	}
	return verifyDNSRootDirectory(directory, 0o755)
}

func (mutation pdnsConfigMutation) apply() error {
	if err := ensurePDNSConfigDirectory(); err != nil {
		return err
	}
	paths := make([]string, 0, len(mutation.desired))
	for path := range mutation.desired {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := secureWriteConfig(path, mutation.desired[path], 0o644); err != nil {
			return err
		}
	}
	if err := secureRemoveConfig(dnsClusterConf); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	effective, detail, err := effectiveManagedPowerDNSConfig()
	if err != nil {
		return err
	}
	if !effective {
		return fmt.Errorf("PowerDNS managed configuration is not effective: %s", detail)
	}
	return nil
}

func (mutation pdnsConfigMutation) restore() error {
	var restoreErr error
	for index := len(mutation.before) - 1; index >= 0; index-- {
		restoreErr = errors.Join(restoreErr, restoreDNSFileSnapshot(mutation.before[index]))
	}
	return restoreErr
}

func verifyStandaloneUnsignedPowerDNS(ctx context.Context) error {
	if _, err := os.Lstat(dnsClusterConf); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("paired PowerDNS topology must be disabled before switching DNS engines")
		}
		return err
	}
	if err := requireManagedDNSClusterReady(); err != nil {
		return err
	}
	return verifyUnsignedPowerDNSData(ctx)
}

func verifyUnsignedPowerDNSForManifest(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if manifest.Topology == transport.DNSTopologyStandalone {
		return verifyStandaloneUnsignedPowerDNS(ctx)
	}
	if manifest.Topology != transport.DNSTopologyPaired ||
		manifest.TargetEngine != transport.DNSEngineBIND {
		return errors.New("PowerDNS source topology is not supported for this switch")
	}
	commitment, err := mutationpayload.CanonicalDNSClusterConfig(
		manifest.Topology, manifest.PeerIP, manifest.PeerNS,
	)
	if err != nil {
		return err
	}
	if err := verifyDNSClusterConfig(
		commitment,
		dnsClusterConfig(&DNSClusterRequest{
			Role: commitment.Role, PeerIP: commitment.PeerIP, PeerNS: commitment.PeerNS,
		}),
	); err != nil {
		return fmt.Errorf("verify paired PowerDNS source: %w", err)
	}
	return verifyUnsignedPowerDNSData(ctx)
}

func verifyUnsignedPowerDNSData(ctx context.Context) error {
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return err
	}
	defer db.Close()
	var keys, signingMetadata int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cryptokeys`).Scan(&keys); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM domainmetadata
		WHERE UPPER(kind) IN ('PRESIGNED', 'NSEC3PARAM', 'NSEC3NARROW')
	`).Scan(&signingMetadata); err != nil {
		return err
	}
	if keys != 0 || signingMetadata != 0 {
		return errors.New("DNSSEC must be disabled for every zone before switching DNS engines")
	}
	return nil
}

func pdnsSwitchCandidatePath(requestID string) string {
	return filepath.Join(filepath.Dir(pdnsDBPath()), ".celikpanel-switch-"+requestID+".sqlite3")
}

func pdnsSwitchBackupPath(requestID string) string {
	return filepath.Join(filepath.Dir(pdnsDBPath()), ".celikpanel-before-switch-"+requestID+".sqlite3")
}

func inspectPDNSDatabaseFile(path string, allowAbsent bool) (bool, int64, string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return false, 0, "", nil
	}
	if err != nil {
		return false, 0, "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
		return false, 0, "", errors.New("PowerDNS database path is not a safe regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, 0, "", err
	}
	defer file.Close()
	digest := sha256.New()
	written, err := io.Copy(digest, file)
	if err != nil || written != info.Size() {
		if err == nil {
			err = errors.New("PowerDNS database changed while it was hashed")
		}
		return false, 0, "", err
	}
	return true, written, hex.EncodeToString(digest.Sum(nil)), nil
}

func setPDNSDatabaseOwnership(path string) error {
	account, err := user.Lookup(pdnsUser())
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}

func activatePDNSCandidate(journal dnsEngineSwitchJournal) error {
	live := filepath.Clean(pdnsDBPath())
	if journal.PDNSCandidatePath != pdnsSwitchCandidatePath(journal.MutationRequestID) ||
		journal.PDNSBackupPath != pdnsSwitchBackupPath(journal.MutationRequestID) {
		return errors.New("PowerDNS switch database paths changed")
	}
	backupExists, _, _, err := inspectPDNSDatabaseFile(journal.PDNSBackupPath, true)
	if err != nil || backupExists {
		if err == nil {
			err = errors.New("PowerDNS switch backup path already exists")
		}
		return err
	}
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(live, true)
	if err != nil {
		return err
	}
	if liveExists {
		if liveSize != journal.PDNSBackupSize || liveHash != journal.PDNSBackupSHA256 {
			return errors.New("PowerDNS live database changed after switch staging")
		}
		if err := os.Rename(live, journal.PDNSBackupPath); err != nil {
			return err
		}
		if err := syncAtomicParentDirectory(filepath.Dir(live)); err != nil {
			return err
		}
	}
	if err := os.Rename(journal.PDNSCandidatePath, live); err != nil {
		return err
	}
	if err := syncAtomicParentDirectory(filepath.Dir(live)); err != nil {
		return err
	}
	return setPDNSDatabaseOwnership(live)
}

func restorePDNSDatabase(journal dnsEngineSwitchJournal) error {
	live := filepath.Clean(pdnsDBPath())
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(live, true)
	if err != nil {
		return err
	}
	backupExists, backupSize, backupHash, err := inspectPDNSDatabaseFile(journal.PDNSBackupPath, true)
	if err != nil {
		return err
	}
	if journal.PDNSBackupSHA256 != "" {
		if !backupExists {
			if !liveExists || liveSize != journal.PDNSBackupSize || liveHash != journal.PDNSBackupSHA256 {
				return errors.New("PowerDNS rollback database backup is missing or changed")
			}
			if err := os.Remove(journal.PDNSCandidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return syncAtomicParentDirectory(filepath.Dir(live))
		}
		if backupSize != journal.PDNSBackupSize || backupHash != journal.PDNSBackupSHA256 {
			return errors.New("PowerDNS rollback database backup changed")
		}
	} else if backupExists {
		return errors.New("PowerDNS rollback found an unexpected database backup")
	}
	if liveExists {
		manifest, canonicalErr := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
			journal.Mode,
			journal.SourceEngine, journal.TargetEngine, journal.SourceEpoch,
			journal.TargetEpoch, journal.SourceRevision, journal.Topology,
			journal.PairRole, journal.LocalIP, journal.LocalNS,
			journal.PeerIP, journal.PeerNS, journal.Zones,
		)
		binding := transport.ServiceMutationBinding{
			MutationRequestID: journal.MutationRequestID, MutationOwnerID: journal.MutationOwnerID,
		}
		if canonicalErr != nil || verifyPDNSSwitchDatabase(context.Background(), live, manifest, binding) != nil {
			return errors.New("PowerDNS rollback live database is not the staged target")
		}
	}
	if err := os.Remove(live); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if journal.PDNSBackupSHA256 != "" {
		if err := os.Rename(journal.PDNSBackupPath, live); err != nil {
			return err
		}
		if err := setPDNSDatabaseOwnership(live); err != nil {
			return err
		}
	}
	if err := os.Remove(journal.PDNSCandidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncAtomicParentDirectory(filepath.Dir(live))
}

func stopDNSSourceForPDNSTarget(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	switch manifest.SourceEngine {
	case transport.DNSEngineBIND:
		output, err := runDNSSystemctl(ctx, systemctl, "disable", "--now", "named.service")
		if err != nil {
			return fmt.Errorf("stop BIND source: %w: %s", err, firstLine(string(output)))
		}
	case "":
		output, err := runDNSSystemctl(ctx, systemctl, "stop", "pdns.service")
		if err != nil {
			return fmt.Errorf("stop adopted PowerDNS source: %w: %s", err, firstLine(string(output)))
		}
	default:
		return errors.New("PowerDNS target received an unsupported source engine")
	}
	return nil
}

func startPDNSTarget(ctx context.Context, systemctl string) error {
	guard := dnsSystemdStateGuard(systemctl)
	if err := guard.ensureUnmasked(ctx, "pdns.service"); err != nil {
		return err
	}
	return enableServiceForMutationWithExecutable(ctx, systemctl, "pdns.service", true)
}

func rollbackPDNSSwitch(
	ctx context.Context,
	systemctl string,
	journal dnsEngineSwitchJournal,
	configs pdnsConfigMutation,
) error {
	recoveryCtx := context.WithoutCancel(ctx)
	_, stopErr := runDNSSystemctl(recoveryCtx, systemctl, "stop", "pdns.service")
	return errors.Join(
		stopErr,
		restorePDNSDatabase(journal),
		configs.restore(),
		restoreDNSFileSnapshot(journal.StateBefore),
		restoreDNSUnitSnapshots(recoveryCtx, systemctl, journal.TargetUnitsBefore),
		restoreDNSUnitSnapshots(recoveryCtx, systemctl, journal.SourceUnitsBefore),
	)
}

func switchToPDNS(
	ctx context.Context,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	if manifest.Mode == transport.DNSEngineSwitchModeAdopt {
		return adoptPDNS(ctx, manifest, binding)
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS engine operation mode is unsupported")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if stateExists && state.Engine == transport.DNSEnginePowerDNS &&
		state.EngineEpoch == manifest.TargetEpoch && state.ManifestQualifier == manifest.Qualifier &&
		state.MutationRequestID == binding.MutationRequestID && state.MutationOwnerID == binding.MutationOwnerID {
		if err := verifyPDNSSwitchDatabase(ctx, pdnsDBPath(), manifest, binding); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
		return transport.SwitchDNSEngineV1Response{Applied: true, ActiveEngine: transport.DNSEnginePowerDNS, ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones), Detail: "the exact PowerDNS engine switch was already completed and verified"}, nil
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		if err == nil {
			err = errors.New("a DNS engine switch recovery journal requires reconciliation")
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyDNSEngineSwitchSource(ctx, profile, manifest, state, stateExists); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := publishDNSEngineSourceOwnership(
		manifest, state, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if manifest.SourceEngine == transport.DNSEnginePowerDNS ||
		(manifest.SourceEngine == "" && capturePDNSActive(ctx, systemctl)) {
		if err := verifyStandaloneUnsignedPowerDNS(ctx); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	service := core.GetManagedServiceByID("pdns")
	if service == nil {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS service definition is unavailable")
	}
	packages := service.Packages[string(profile.PackageManager)]
	if len(packages) == 0 {
		return transport.SwitchDNSEngineV1Response{}, errors.New("PowerDNS packages are unavailable for this host")
	}
	missing := make([]string, 0, len(packages))
	for _, packageName := range packages {
		if !packageInstalledForProfile(profile, packageName) {
			missing = append(missing, packageName)
		}
	}
	if len(missing) != 0 {
		installReceipt, receiptErr := newDNSEngineInstallOwnership(
			transport.DNSEnginePowerDNS, profile.PackageManager,
			packages, missing, manifest, binding,
		)
		if receiptErr != nil {
			return transport.SwitchDNSEngineV1Response{}, receiptErr
		}
		if err := runDNSPort53PreMutationGuard(
			ctx, !stateExists && manifest.SourceEngine == "",
			func() error {
				return installOwnedDNSEnginePackages(installReceipt, func() error {
					_, installErr := installPDNSPackagesWithGuard(ctx, systemctl, func() (string, error) {
						return installPackagesWithCandidateContext(
							ctx, string(profile.PackageManager), missing, "",
						)
					})
					return installErr
				})
			},
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	configs, err := preparePDNSConfigMutation()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSFileSnapshot(dnsEngineStatePath(), 0o600, true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	targetBefore, err := captureDNSUnitSnapshots(ctx, systemctl, []string{"pdns.service"})
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	sourceUnits := []string{}
	if manifest.SourceEngine == transport.DNSEngineBIND {
		sourceUnits = []string{"bind9.service", "named.service"}
	}
	sourceBefore, err := captureDNSUnitSnapshots(ctx, systemctl, sourceUnits)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	candidate := pdnsSwitchCandidatePath(binding.MutationRequestID)
	backup := pdnsSwitchBackupPath(binding.MutationRequestID)
	for _, path := range []string{candidate, backup} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				err = errors.New("PowerDNS switch staging path already exists")
			}
			return transport.SwitchDNSEngineV1Response{}, err
		}
	}
	liveExists, liveSize, liveHash, err := inspectPDNSDatabaseFile(pdnsDBPath(), true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID, MutationOwnerID: binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology, PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		SnapshotBytes: manifest.SnapshotBytes, Zones: manifest.Zones,
		StateBefore: stateBefore, ConfigBefore: configs.before,
		TargetUnitsBefore: targetBefore, SourceUnitsBefore: sourceBefore,
		PDNSCandidatePath: candidate, PDNSBackupPath: backup,
	}
	if liveExists {
		journal.PDNSBackupSHA256, journal.PDNSBackupSize = liveHash, liveSize
	}
	writeIntent := func() error { return writeDNSEngineSwitchJournal(journal) }
	if len(missing) == 0 {
		if err := runDNSPort53PreMutationGuard(
			ctx, !stateExists && manifest.SourceEngine == "", writeIntent,
		); err != nil {
			return transport.SwitchDNSEngineV1Response{}, err
		}
	} else if err := writeIntent(); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollback := func(cause error) (transport.SwitchDNSEngineV1Response, error) {
		journal.Phase = dnsSwitchPhaseRollingBack
		journalErr := writeDNSEngineSwitchJournal(journal)
		rollbackErr := rollbackPDNSSwitch(ctx, systemctl, journal, configs)
		if rollbackErr == nil {
			journal.Phase = dnsSwitchPhaseRolledBack
			journalErr = errors.Join(journalErr, writeDNSEngineSwitchJournal(journal), removeDNSEngineSwitchJournal())
		}
		return transport.SwitchDNSEngineV1Response{}, errors.Join(cause, journalErr, rollbackErr)
	}
	if err := buildPDNSSwitchCandidate(ctx, candidate, manifest, binding); err != nil {
		return rollback(err)
	}
	if err := verifyPDNSSwitchDatabase(ctx, candidate, manifest, binding); err != nil {
		return rollback(err)
	}
	if err := configs.apply(); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseTargetStaged
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := stopDNSSourceForPDNSTarget(ctx, systemctl, manifest); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseSourceStopped
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := activatePDNSCandidate(journal); err != nil {
		return rollback(err)
	}
	if err := startPDNSTarget(ctx, systemctl); err != nil {
		return rollback(err)
	}
	journal.Phase = dnsSwitchPhaseTargetStarted
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return rollback(err)
	}
	if err := verifyPDNSSwitchDatabase(ctx, pdnsDBPath(), manifest, binding); err != nil {
		return rollback(err)
	}
	if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
		return rollback(err)
	}
	if err := verifyDNSZoneManifestAuthority(ctx, manifest.Zones); err != nil {
		return rollback(err)
	}
	nextState := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: manifest.Mode,
		Engine:      transport.DNSEnginePowerDNS,
		EngineEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		ManifestQualifier: manifest.Qualifier, MutationRequestID: binding.MutationRequestID,
		MutationOwnerID: binding.MutationOwnerID,
	}
	if err := writeDNSEngineState(nextState); err != nil {
		if actual, exists, readErr := readDNSEngineState(); readErr != nil || !exists || actual != nextState {
			return rollback(errors.Join(err, readErr))
		}
	}
	journal.Phase = dnsSwitchPhaseTargetVerified
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := writeDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEnginePowerDNS,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "PowerDNS is the verified active authoritative DNS engine",
	}, nil
}

func capturePDNSActive(ctx context.Context, systemctl string) bool {
	state, err := dnsSystemdStateGuard(systemctl).inspect(ctx, "pdns.service")
	return err == nil && state.active()
}
