package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEngineSwitchJournalSchema = dnsengineartifact.SwitchJournalSchemaV1
	dnsEngineSwitchJournalFile   = "dns-engine-switch-journal.json"
	dnsEngineSwitchRecoveryLimit = 45 * time.Second
	dnsEngineSwitchJournalLimit  = dnsengineartifact.SwitchJournalLimit

	dnsSwitchPhaseIntent         = dnsengineartifact.SwitchPhaseIntent
	dnsSwitchPhaseTargetStaged   = dnsengineartifact.SwitchPhaseTargetStaged
	dnsSwitchPhaseSourceStopped  = dnsengineartifact.SwitchPhaseSourceStopped
	dnsSwitchPhaseTargetStarted  = dnsengineartifact.SwitchPhaseTargetStarted
	dnsSwitchPhaseTargetVerified = dnsengineartifact.SwitchPhaseTargetVerified
	dnsSwitchPhaseCommitted      = dnsengineartifact.SwitchPhaseCommitted
	dnsSwitchPhaseRollingBack    = dnsengineartifact.SwitchPhaseRollingBack
	dnsSwitchPhaseRolledBack     = dnsengineartifact.SwitchPhaseRolledBack

	dnsEngineSwitchJournalFaultPreIntent   = "pre_intent"
	dnsEngineSwitchJournalFaultBeforeWrite = "before_write"
	dnsEngineSwitchJournalFaultAfterWrite  = "after_write"

	dnsEngineSwitchFaultDriverBIND                     = "bind"
	dnsEngineSwitchFaultDriverPDNSSwitch               = "pdns-switch"
	dnsEngineSwitchFaultDriverPDNSAdopt                = "pdns-adopt"
	dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure = "pdns-secondary-reconfigure"
	dnsEngineSwitchFaultDriverSignedUpdateFinalize     = "signed-update-finalize"
)

// Assigned only by focused crash-recovery tests or the linux && dns_kill_matrix
// tagged runtime. Standard untagged production builds never assign this hook,
// so their journal publication has no fault-injection behavior.
var dnsEngineSwitchJournalFaultHook func(string, string, dnsEngineSwitchJournal) error

// Returned only by the linux && dns_kill_matrix tagged runtime after a
// durably published forward journal has been selected as the deterministic
// precursor for a rollback cell. Ordinary builds leave the hook above nil and
// therefore cannot produce this sentinel.
var dnsEngineSwitchRollbackPrecursorError = errors.New(
	"DNS kill-matrix rollback precursor injected",
)

func runDNSEngineSwitchPreIntentFaultHook(
	driver string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	if dnsEngineSwitchJournalFaultHook == nil {
		return nil
	}
	point := dnsEngineSwitchJournal{
		Schema:            dnsEngineSwitchJournalSchema,
		Phase:             "pre-intent",
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		PairRole:          manifest.PairRole,
		LocalIP:           manifest.LocalIP,
		LocalNS:           manifest.LocalNS,
		PeerIP:            manifest.PeerIP,
		PeerNS:            manifest.PeerNS,
		SnapshotBytes:     manifest.SnapshotBytes,
		Zones:             manifest.Zones,
	}
	if err := dnsEngineSwitchJournalFaultHook(
		driver, dnsEngineSwitchJournalFaultPreIntent, point,
	); err != nil {
		return fmt.Errorf(
			"injected failure in DNS engine switch pre-intent window: %w",
			err,
		)
	}
	return nil
}

func newDNSEngineRollbackContext(
	parent context.Context,
) (context.Context, context.CancelFunc, error) {
	if parent == nil {
		return nil, nil, errors.New("DNS rollback requires a parent context")
	}
	tracker, _ := parent.Value(
		serviceMutationExecutionTrackerKey{},
	).(*serviceMutationExecutionTracker)
	if tracker != nil {
		return serviceMutationCancellingRecoveryContext(
			parent, dnsEngineSwitchRecoveryLimit,
		)
	}
	recoveryCtx, cancel := context.WithTimeout(
		context.WithoutCancel(parent), dnsEngineSwitchRecoveryLimit,
	)
	return recoveryCtx, cancel, nil
}

type dnsFileSnapshot = dnsengineartifact.FileSnapshot

type dnsUnitSnapshot = dnsengineartifact.UnitSnapshot

type dnsEngineSwitchJournal = dnsengineartifact.SwitchJournalV1

func dnsEngineSwitchJournalPath() string {
	return filepath.Join(serviceMutationStateDirectory(), dnsEngineSwitchJournalFile)
}

func validDNSSwitchPhase(value string) bool {
	return dnsengineartifact.ValidSwitchPhase(value)
}

func digestDNSBytes(data []byte) string {
	return dnsengineartifact.DigestBytes(data)
}

func validateDNSFileSnapshotIntegrity(snapshot dnsFileSnapshot) error {
	return dnsengineartifact.ValidateFileSnapshotIntegrity(snapshot)
}

func validateDNSFileSnapshot(snapshot dnsFileSnapshot) error {
	return dnsJournalPolicy().ValidateFileSnapshot(snapshot)
}

func validateDNSFileSnapshotForOwner(
	snapshot dnsFileSnapshot,
	requiredUID, requiredGID uint32,
) error {
	return dnsJournalPolicy().ValidateFileSnapshotForOwner(snapshot, requiredUID, requiredGID)
}

func validateDNSFileSnapshotForOwnerContract(
	snapshot dnsFileSnapshot,
	requiredUID, requiredGID uint32,
	ownerError string,
) error {
	return dnsJournalPolicy().ValidateFileSnapshotForOwnerContract(snapshot, requiredUID, requiredGID, ownerError)
}

func validateDNSEngineStateSnapshot(snapshot dnsFileSnapshot) error {
	return dnsJournalPolicy().ValidateStateSnapshot(snapshot)
}

func validateDNSUnitSnapshot(snapshot dnsUnitSnapshot) error {
	return dnsengineartifact.ValidateUnitSnapshot(snapshot)
}

func validateDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) error {
	return dnsJournalPolicy().ValidateSwitchJournal(journal)
}

func validatePDNSAdoptionJournal(journal dnsEngineSwitchJournal) error {
	return dnsJournalPolicy().ValidatePDNSAdoptionJournal(journal)
}

func validBINDConfigSnapshotSet(snapshots []dnsFileSnapshot) bool {
	return dnsJournalPolicy().ValidBINDConfigSnapshotSet(snapshots)
}

func dnsUnitSnapshotNamesEqual(snapshots []dnsUnitSnapshot, want []string) bool {
	return dnsengineartifact.UnitSnapshotNamesEqual(snapshots, want)
}

func encodeDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) ([]byte, error) {
	return dnsJournalPolicy().EncodeSwitchJournal(journal)
}

func decodeDNSEngineSwitchJournal(data []byte) (dnsEngineSwitchJournal, error) {
	return dnsJournalPolicy().DecodeSwitchJournal(data)
}

func readDNSEngineSwitchJournal() (dnsEngineSwitchJournal, bool, error) {
	return readDNSEngineSwitchJournalAt(dnsEngineSwitchJournalPath())
}

func readDNSEngineSwitchJournalAt(path string) (dnsEngineSwitchJournal, bool, error) {
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	if err != nil {
		return dnsEngineSwitchJournal{}, false, err
	}
	journal, err := decodeDNSEngineSwitchJournal(data)
	return journal, err == nil, err
}

func writeDNSEngineSwitchJournalWithOps(
	journal dnsEngineSwitchJournal,
	persist func([]byte) error,
	read func() (dnsEngineSwitchJournal, bool, error),
	faultHook func(string, dnsEngineSwitchJournal) error,
) error {
	if persist == nil || read == nil {
		return errors.New("DNS engine switch journal writer is incomplete")
	}
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		return err
	}
	if faultHook != nil {
		if err := faultHook(dnsEngineSwitchJournalFaultBeforeWrite, journal); err != nil {
			return fmt.Errorf(
				"injected failure before DNS engine switch journal write for phase %q: %w",
				journal.Phase, err,
			)
		}
	}
	if err := persist(encoded); err != nil {
		verified, exists, readErr := read()
		if readErr == nil && exists && reflect.DeepEqual(verified, journal) {
			if faultHook != nil {
				if hookErr := faultHook(dnsEngineSwitchJournalFaultAfterWrite, journal); hookErr != nil {
					return fmt.Errorf(
						"injected failure after DNS engine switch journal write for phase %q: %w",
						journal.Phase, hookErr,
					)
				}
			}
			return nil
		}
		return errors.Join(err, readErr)
	}
	if faultHook != nil {
		if err := faultHook(dnsEngineSwitchJournalFaultAfterWrite, journal); err != nil {
			return fmt.Errorf(
				"injected failure after DNS engine switch journal write for phase %q: %w",
				journal.Phase, err,
			)
		}
	}
	verified, exists, err := read()
	if err != nil || !exists || !reflect.DeepEqual(verified, journal) {
		if err == nil {
			err = errors.New("DNS engine switch journal readback mismatch")
		}
		return err
	}
	return nil
}

func writeDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) error {
	return writeDNSEngineSwitchJournalForFaultDriver("", journal)
}

func writeDNSEngineSwitchJournalForFaultDriver(
	driver string,
	journal dnsEngineSwitchJournal,
) error {
	globalFaultHook := dnsEngineSwitchJournalFaultHook
	var faultHook func(string, dnsEngineSwitchJournal) error
	if globalFaultHook != nil {
		faultHook = func(point string, observed dnsEngineSwitchJournal) error {
			return globalFaultHook(driver, point, observed)
		}
	}
	return writeDNSEngineSwitchJournalWithOps(
		journal,
		func(encoded []byte) error {
			return secureWriteConfig(dnsEngineSwitchJournalPath(), encoded, 0o600)
		},
		readDNSEngineSwitchJournal,
		faultHook,
	)
}

func removeDNSEngineSwitchJournal() error {
	if err := secureRemoveConfig(dnsEngineSwitchJournalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		if _, exists, readErr := readDNSEngineSwitchJournal(); readErr == nil && !exists {
			return nil
		} else {
			return errors.Join(err, readErr)
		}
	}
	if _, exists, err := readDNSEngineSwitchJournal(); err != nil || exists {
		if err == nil {
			err = errors.New("DNS engine switch journal still exists after removal")
		}
		return err
	}
	return nil
}

func captureDNSFileSnapshot(path string, mode os.FileMode, allowAbsent bool) (dnsFileSnapshot, error) {
	return captureDNSFileSnapshotForOwnerContract(
		path, mode, allowAbsent, 0, 0,
		"DNS switch snapshot file is not root-owned",
	)
}

func captureDNSFileSnapshotForOwner(
	path string,
	mode os.FileMode,
	allowAbsent bool,
	requiredUID, requiredGID uint32,
) (dnsFileSnapshot, error) {
	return captureDNSFileSnapshotForOwnerContract(
		path, mode, allowAbsent, requiredUID, requiredGID,
		"DNS switch snapshot file ownership differs from the managed contract",
	)
}

func captureDNSEngineStateSnapshot(allowAbsent bool) (dnsFileSnapshot, error) {
	return captureDNSFileSnapshotForOwner(
		dnsEngineStatePath(), 0o600, allowAbsent,
		serviceMutationRequiredOwnerUID,
		serviceMutationRequiredOwnerGID,
	)
}

func captureDNSFileSnapshotForOwnerContract(
	path string,
	mode os.FileMode,
	allowAbsent bool,
	requiredUID, requiredGID uint32,
	ownerError string,
) (dnsFileSnapshot, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) || mode.Perm() == 0 || mode.Perm() != mode {
		return dnsFileSnapshot{}, errors.New("invalid DNS switch snapshot path or mode")
	}
	data, metadata, err := readDNSFileForSnapshot(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return dnsFileSnapshot{Path: path}, nil
	}
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	if metadata.Mode.Perm() != mode.Perm() {
		return dnsFileSnapshot{}, errors.New("DNS switch snapshot file mode differs from the managed contract")
	}
	if metadata.OwnerKnown && (metadata.UID != requiredUID || metadata.GID != requiredGID) {
		return dnsFileSnapshot{}, errors.New(ownerError)
	}
	if dnsSnapshotOwnerRequired() && !metadata.OwnerKnown {
		return dnsFileSnapshot{}, errors.New("DNS switch snapshot ownership cannot be verified")
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(metadata.Mode.Perm()),
		OwnerKnown: metadata.OwnerKnown, UID: metadata.UID, GID: metadata.GID,
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}, nil
}

// captureDNSFileSnapshotPreserve records an existing root-owned configuration
// exactly without normalizing its safe permission bits. Adoption is a
// read-only operation, so even a harmless chmod would violate its contract.
func captureDNSFileSnapshotPreserve(path string, allowAbsent bool) (dnsFileSnapshot, error) {
	return captureDNSFileSnapshotPreserveForOwner(path, allowAbsent, 0, 0)
}

func captureDNSFileSnapshotPreserveForOwner(
	path string, allowAbsent bool, requiredUID, requiredGID uint32,
) (dnsFileSnapshot, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return dnsFileSnapshot{}, errors.New("invalid DNS adoption snapshot path")
	}
	data, metadata, err := readDNSFileForSnapshot(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return dnsFileSnapshot{Path: path}, nil
	}
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	mode := metadata.Mode.Perm()
	if mode == 0 || (dnsSnapshotOwnerRequired() && mode&0o022 != 0) {
		return dnsFileSnapshot{}, errors.New("DNS adoption config is group/other writable")
	}
	if metadata.OwnerKnown && (metadata.UID != requiredUID || metadata.GID != requiredGID) {
		return dnsFileSnapshot{}, errors.New("DNS adoption config is not root-owned")
	}
	if dnsSnapshotOwnerRequired() && !metadata.OwnerKnown {
		return dnsFileSnapshot{}, errors.New("DNS adoption config ownership cannot be verified")
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(mode),
		OwnerKnown: metadata.OwnerKnown, UID: metadata.UID, GID: metadata.GID,
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}, nil
}

func verifyDNSFileSnapshotsExact(snapshots []dnsFileSnapshot) error {
	return verifyDNSFileSnapshotsExactForOwner(snapshots, 0, 0)
}

func verifyDNSFileSnapshotsExactForOwner(
	snapshots []dnsFileSnapshot, requiredUID, requiredGID uint32,
) error {
	for _, expected := range snapshots {
		actual, err := captureDNSFileSnapshotPreserveForOwner(
			expected.Path, true, requiredUID, requiredGID,
		)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, expected) {
			return errors.New("DNS adoption configuration changed during verification")
		}
	}
	return nil
}

func verifyDNSEngineStateSnapshotExact(expected dnsFileSnapshot) error {
	if err := validateDNSEngineStateSnapshot(expected); err != nil {
		return err
	}
	actual, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("DNS engine state changed during exact verification")
	}
	return nil
}

func restoreDNSEngineStateSnapshot(snapshot dnsFileSnapshot) error {
	if err := validateDNSEngineStateSnapshot(snapshot); err != nil {
		return err
	}
	current, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return err
	}
	switch {
	case snapshot.Exists:
		err = secureWriteConfigReplacingSnapshotWithOwner(
			snapshot.Path,
			snapshot.Data,
			0o600,
			&current,
			serviceMutationRequiredOwnerUID,
			serviceMutationRequiredOwnerGID,
		)
	case current.Exists:
		err = secureRemoveConfig(snapshot.Path)
	}
	if err != nil {
		return err
	}
	return verifyDNSEngineStateSnapshotExact(snapshot)
}

func restoreDNSFileSnapshot(snapshot dnsFileSnapshot) error {
	if err := validateDNSFileSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.Exists {
		if err := secureWriteConfig(snapshot.Path, snapshot.Data, os.FileMode(snapshot.Mode)); err != nil {
			return err
		}
	} else if err := secureRemoveConfig(snapshot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	after, err := captureDNSFileSnapshot(snapshot.Path, os.FileMode(max(snapshot.Mode, 0o600)), true)
	if err != nil {
		return err
	}
	if after.Exists != snapshot.Exists ||
		(snapshot.Exists && (after.Mode != snapshot.Mode ||
			after.OwnerKnown != snapshot.OwnerKnown || after.UID != snapshot.UID ||
			after.GID != snapshot.GID || after.SHA256 != snapshot.SHA256 ||
			!bytes.Equal(after.Data, snapshot.Data))) {
		return errors.New("DNS switch file snapshot restore readback mismatch")
	}
	return nil
}

func captureDNSUnitSnapshots(
	ctx context.Context,
	systemctl string,
	units []string,
) ([]dnsUnitSnapshot, error) {
	units = append([]string(nil), units...)
	sort.Strings(units)
	guard := dnsSystemdStateGuard(systemctl)
	snapshots := make([]dnsUnitSnapshot, len(units))
	for index, unit := range units {
		if index > 0 && unit == units[index-1] {
			return nil, errors.New("DNS switch unit list contains a duplicate")
		}
		state, err := guard.inspect(ctx, unit)
		if err != nil {
			return nil, err
		}
		snapshot := dnsUnitSnapshot{
			Name: state.name, LoadState: state.loadState,
			ActiveState: state.activeState, UnitFileState: state.unitFileState,
		}
		if err := validateDNSUnitSnapshot(snapshot); err != nil {
			return nil, fmt.Errorf("capture %s: %w", unit, err)
		}
		snapshots[index] = snapshot
	}
	return snapshots, nil
}

func verifyDNSUnitSnapshotsExact(
	ctx context.Context,
	systemctl string,
	expected []dnsUnitSnapshot,
) error {
	units := make([]string, len(expected))
	for index := range expected {
		units[index] = expected[index].Name
	}
	actual, err := captureDNSUnitSnapshots(ctx, systemctl, units)
	if err != nil {
		return err
	}
	if !exactDNSUnitSnapshotSet(actual, expected) {
		return errors.New("DNS adoption service state changed during verification")
	}
	return nil
}

func exactDNSUnitSnapshotSet(left, right []dnsUnitSnapshot) bool {
	return reflect.DeepEqual(left, right)
}

func dnsSystemdStateGuard(systemctl string) *bindPackageInstallGuard {
	return &bindPackageInstallGuard{
		systemctl: systemctl,
		ops: bindInstallGuardOps{
			verifyMaskParent: verifyBINDMaskParentMetadata,
			runSystemd: func(ctx context.Context, executable string, args ...string) ([]byte, error) {
				if executable != systemctl {
					return nil, errors.New("DNS switch systemctl executable changed")
				}
				return runDNSSystemctl(ctx, executable, args...)
			},
			recoveryContext: func(parent context.Context) (context.Context, context.CancelFunc, error) {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), dnsEngineSwitchRecoveryLimit)
				return ctx, cancel, nil
			},
		},
		ownedMask: make(map[string]bool),
	}
}

func restoreDNSUnitSnapshots(ctx context.Context, systemctl string, snapshots []dnsUnitSnapshot) error {
	return restoreDNSUnitSnapshotsWithGuard(ctx, dnsSystemdStateGuard(systemctl), snapshots)
}

// dnsUnitRestoreRank orders a restore so that a distro alias comes after the
// unit it aliases. On APT hosts bind9.service is only an Alias= of
// named.service: the symlink exists while named.service is enabled and is
// removed by "systemctl disable named.service". Journals record unit
// snapshots sorted by name, and "bind9.service" sorts before "named.service",
// so a rollback that restored them in journal order ran
// "systemctl enable bind9.service" while the alias did not exist and failed
// with "Unit bind9.service does not exist" (S-8 T5, register R-031). Restoring
// the real unit first recreates the alias; enabling the alias afterwards is
// then an exact no-op readback.
//
// dnsUnitRestoreRank, geri yüklemeyi dağıtım takma adı, takma adı olduğu
// birimden sonra gelecek şekilde sıralar. APT sunucularında bind9.service
// yalnız named.service'in bir Alias='ıdır: sembolik bağ named.service
// etkinleştirilmişken var olur ve "systemctl disable named.service" onu
// kaldırır. Günlükler birim anlık görüntülerini ada göre sıralı kaydeder ve
// "bind9.service" "named.service"ten önce gelir; dolayısıyla onları günlük
// sırasıyla geri yükleyen bir geri alma, takma ad yokken "systemctl enable
// bind9.service" koşturup "Unit bind9.service does not exist" ile düştü (S-8
// T5, defter R-031). Önce gerçek birimi geri yüklemek takma adı yeniden
// yaratır; sonra takma adı etkinleştirmek tam bir no-op okumadır.
func dnsUnitRestoreRank(name string) int {
	if name == "bind9.service" {
		return 1
	}
	return 0
}

func orderDNSUnitSnapshotsForRestore(snapshots []dnsUnitSnapshot) []dnsUnitSnapshot {
	ordered := append([]dnsUnitSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return dnsUnitRestoreRank(ordered[i].Name) < dnsUnitRestoreRank(ordered[j].Name)
	})
	return ordered
}

func restoreDNSUnitSnapshotsWithGuard(
	ctx context.Context,
	guard *bindPackageInstallGuard,
	snapshots []dnsUnitSnapshot,
) error {
	if guard == nil {
		return errors.New("DNS unit snapshot restore requires a systemd guard")
	}
	for _, snapshot := range orderDNSUnitSnapshotsForRestore(snapshots) {
		if err := validateDNSUnitSnapshot(snapshot); err != nil {
			return err
		}
		state := bindInstallUnitState{
			name: snapshot.Name, loadState: snapshot.LoadState,
			activeState: snapshot.ActiveState, unitFileState: snapshot.UnitFileState,
		}
		guard.before = append(guard.before, state)
		if !state.masked() {
			guard.ownedMask[state.name] = true
		}
	}
	return guard.restore(ctx)
}

// Host paths and ownership come from the installed adapter, never from the
// incoming journal. The shared package does not read the host or grant authority.
func dnsJournalPolicy() dnsengineartifact.JournalPolicy {
	return dnsengineartifact.JournalPolicy{
		StatePath: filepath.Clean(dnsEngineStatePath()),
		StateUID:  serviceMutationRequiredOwnerUID, StateGID: serviceMutationRequiredOwnerGID,
		RequireOwner: dnsSnapshotOwnerRequired(),
		PDNSMainPath: filepath.Clean(dnsMainConf), PDNSManagedPath: filepath.Clean(dnsManagedConf),
		PDNSClusterPath: filepath.Clean(dnsClusterConf), PDNSDatabasePath: filepath.Clean(pdnsDBPath()),
	}
}
