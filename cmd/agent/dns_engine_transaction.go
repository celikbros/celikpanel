package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	dnsEngineSwitchJournalSchema = "celikpanel-dns-engine-switch-journal/v1"
	dnsEngineSwitchJournalFile   = "dns-engine-switch-journal.json"
	dnsEngineSwitchRecoveryLimit = 45 * time.Second
	dnsEngineSwitchJournalLimit  = 96 << 20

	dnsSwitchPhaseIntent         = "intent"
	dnsSwitchPhaseTargetStaged   = "target-staged"
	dnsSwitchPhaseSourceStopped  = "source-stopped"
	dnsSwitchPhaseTargetStarted  = "target-started"
	dnsSwitchPhaseTargetVerified = "target-verified"
	dnsSwitchPhaseCommitted      = "committed"
	dnsSwitchPhaseRollingBack    = "rolling-back"
	dnsSwitchPhaseRolledBack     = "rolled-back"
)

type dnsFileSnapshot struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Mode       uint32 `json:"mode"`
	OwnerKnown bool   `json:"owner_known,omitempty"`
	UID        uint32 `json:"uid,omitempty"`
	GID        uint32 `json:"gid,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Data       []byte `json:"data,omitempty"`
}

type dnsUnitSnapshot struct {
	Name          string `json:"name"`
	LoadState     string `json:"load_state"`
	ActiveState   string `json:"active_state"`
	UnitFileState string `json:"unit_file_state"`
}

type dnsEngineSwitchJournal struct {
	Schema             string                                  `json:"schema"`
	Phase              string                                  `json:"phase"`
	Mode               string                                  `json:"mode"`
	MutationRequestID  string                                  `json:"mutation_request_id"`
	MutationOwnerID    string                                  `json:"mutation_owner_id"`
	ManifestQualifier  string                                  `json:"manifest_qualifier"`
	SourceEngine       transport.DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine       transport.DNSEngine                     `json:"target_engine"`
	SourceEpoch        int64                                   `json:"source_epoch"`
	TargetEpoch        int64                                   `json:"target_epoch"`
	SourceRevision     int64                                   `json:"source_revision"`
	Topology           string                                  `json:"topology"`
	PeerIP             string                                  `json:"peer_ip,omitempty"`
	PeerNS             string                                  `json:"peer_ns,omitempty"`
	SnapshotBytes      int64                                   `json:"snapshot_bytes"`
	Zones              []transport.DNSEngineSwitchZoneSnapshot `json:"zones"`
	TargetGeneration   string                                  `json:"target_generation,omitempty"`
	PreviousGeneration string                                  `json:"previous_generation,omitempty"`
	HadPrevious        bool                                    `json:"had_previous_generation"`
	StateBefore        dnsFileSnapshot                         `json:"state_before"`
	ConfigBefore       []dnsFileSnapshot                       `json:"config_before"`
	TargetUnitsBefore  []dnsUnitSnapshot                       `json:"target_units_before"`
	SourceUnitsBefore  []dnsUnitSnapshot                       `json:"source_units_before"`
	PDNSCandidatePath  string                                  `json:"pdns_candidate_path,omitempty"`
	PDNSBackupPath     string                                  `json:"pdns_backup_path,omitempty"`
	PDNSBackupSHA256   string                                  `json:"pdns_backup_sha256,omitempty"`
	PDNSBackupSize     int64                                   `json:"pdns_backup_size,omitempty"`
	PDNSLiveSHA256     string                                  `json:"pdns_live_sha256,omitempty"`
	PDNSLiveSize       int64                                   `json:"pdns_live_size,omitempty"`
}

func dnsEngineSwitchJournalPath() string {
	return filepath.Join(serviceMutationStateDirectory(), dnsEngineSwitchJournalFile)
}

func validDNSSwitchPhase(value string) bool {
	switch value {
	case dnsSwitchPhaseIntent, dnsSwitchPhaseTargetStaged,
		dnsSwitchPhaseSourceStopped, dnsSwitchPhaseTargetStarted,
		dnsSwitchPhaseTargetVerified, dnsSwitchPhaseCommitted,
		dnsSwitchPhaseRollingBack, dnsSwitchPhaseRolledBack:
		return true
	default:
		return false
	}
}

func digestDNSBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validateDNSFileSnapshot(snapshot dnsFileSnapshot) error {
	clean := filepath.Clean(snapshot.Path)
	posixClean := pathpkg.Clean(snapshot.Path)
	canonicalAbsolute := filepath.IsAbs(clean) && clean == snapshot.Path
	if strings.HasPrefix(snapshot.Path, "/") && posixClean == snapshot.Path && snapshot.Path != "/" {
		canonicalAbsolute = true
	}
	if snapshot.Path == "" || !canonicalAbsolute ||
		snapshot.Mode&^0o777 != 0 {
		return errors.New("DNS switch file snapshot has an unsafe path or mode")
	}
	if !snapshot.Exists {
		if snapshot.Mode != 0 || snapshot.OwnerKnown || snapshot.UID != 0 || snapshot.GID != 0 ||
			snapshot.SHA256 != "" || len(snapshot.Data) != 0 {
			return errors.New("absent DNS switch file snapshot contains hidden state")
		}
		return nil
	}
	if snapshot.Mode == 0 || len(snapshot.Data) > dnsEngineSwitchJournalLimit ||
		snapshot.SHA256 != digestDNSBytes(snapshot.Data) {
		return errors.New("DNS switch file snapshot digest is invalid")
	}
	if dnsSnapshotOwnerRequired() && !snapshot.OwnerKnown {
		return errors.New("DNS switch file snapshot is missing required ownership metadata")
	}
	if (snapshot.OwnerKnown && (snapshot.UID != 0 || snapshot.GID != 0)) ||
		(!snapshot.OwnerKnown && (snapshot.UID != 0 || snapshot.GID != 0)) {
		return errors.New("DNS switch file snapshot is not root-owned")
	}
	return nil
}

func validateDNSUnitSnapshot(snapshot dnsUnitSnapshot) error {
	if strings.TrimSpace(snapshot.Name) != snapshot.Name || snapshot.Name == "" ||
		strings.ContainsAny(snapshot.Name, "/\\\x00\r\n") {
		return errors.New("DNS switch unit snapshot has an unsafe name")
	}
	state := bindInstallUnitState{
		name: snapshot.Name, loadState: snapshot.LoadState,
		activeState: snapshot.ActiveState, unitFileState: snapshot.UnitFileState,
	}
	if !validBINDInstallLoadState(state.loadState) ||
		!validBINDInstallActiveState(state.activeState) ||
		!validBINDInstallUnitFileState(state.loadState, state.unitFileState) {
		return errors.New("DNS switch unit snapshot contains an unsupported systemd state")
	}
	if state.activeState == "failed" || (state.masked() && state.active()) {
		return errors.New("DNS switch unit snapshot cannot be restored deterministically")
	}
	if state.loadState == "not-found" && state.unitFileState == "" {
		return nil
	}
	switch state.unitFileState {
	case "enabled", "enabled-runtime", "disabled", "masked", "masked-runtime":
		return nil
	default:
		return errors.New("DNS switch unit-file state has no exact inverse")
	}
}

func validateDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) error {
	if journal.Schema != dnsEngineSwitchJournalSchema || !validDNSSwitchPhase(journal.Phase) ||
		(journal.Mode != transport.DNSEngineSwitchModeSwitch &&
			journal.Mode != transport.DNSEngineSwitchModeAdopt) ||
		!validMutationIdentity(journal.MutationRequestID) ||
		!validMutationIdentity(journal.MutationOwnerID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(journal.ManifestQualifier) {
		return errors.New("DNS engine switch journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPeer(
		journal.Mode,
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.PeerIP, journal.PeerNS, journal.Zones,
	)
	if err != nil || commitment.Qualifier != journal.ManifestQualifier ||
		commitment.SnapshotBytes != journal.SnapshotBytes ||
		!reflect.DeepEqual(commitment.Zones, journal.Zones) {
		return errors.New("DNS engine switch journal manifest is not canonical")
	}
	if err := validateDNSFileSnapshot(journal.StateBefore); err != nil {
		return err
	}
	if journal.StateBefore.Path != filepath.Clean(dnsEngineStatePath()) ||
		(journal.StateBefore.Exists && journal.StateBefore.Mode != 0o600) {
		return errors.New("DNS engine switch journal state snapshot path is invalid")
	}
	if _, _, err := sourceStateFromDNSSwitchJournal(journal); err != nil {
		return err
	}
	for _, snapshots := range [][]dnsFileSnapshot{journal.ConfigBefore} {
		previous := ""
		for _, snapshot := range snapshots {
			if err := validateDNSFileSnapshot(snapshot); err != nil {
				return err
			}
			if previous != "" && snapshot.Path <= previous {
				return errors.New("DNS switch file snapshots are unsorted or duplicated")
			}
			previous = snapshot.Path
		}
	}
	for _, snapshots := range [][]dnsUnitSnapshot{journal.TargetUnitsBefore, journal.SourceUnitsBefore} {
		previous := ""
		for _, snapshot := range snapshots {
			if err := validateDNSUnitSnapshot(snapshot); err != nil {
				return err
			}
			if previous != "" && snapshot.Name <= previous {
				return errors.New("DNS switch unit snapshots are unsorted or duplicated")
			}
			previous = snapshot.Name
		}
	}
	if journal.Mode == transport.DNSEngineSwitchModeAdopt {
		if err := validatePDNSAdoptionJournal(journal); err != nil {
			return err
		}
		return nil
	}
	if journal.TargetEngine == transport.DNSEngineBIND {
		if !validDNSGeneration(journal.TargetGeneration) ||
			(journal.HadPrevious && !validDNSGeneration(journal.PreviousGeneration)) ||
			(!journal.HadPrevious && journal.PreviousGeneration != "") ||
			journal.PDNSCandidatePath != "" || journal.PDNSBackupPath != "" ||
			journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
			journal.PDNSLiveSHA256 != "" || journal.PDNSLiveSize != 0 {
			return errors.New("BIND switch journal generation or PowerDNS fields are invalid")
		}
		if !validBINDConfigSnapshotSet(journal.ConfigBefore) {
			return errors.New("BIND switch journal config snapshot set is incomplete")
		}
	} else {
		if journal.TargetGeneration != "" || journal.HadPrevious || journal.PreviousGeneration != "" {
			return errors.New("PowerDNS switch journal contains BIND generation state")
		}
		if journal.PDNSCandidatePath != filepath.Clean(pdnsSwitchCandidatePath(journal.MutationRequestID)) ||
			journal.PDNSBackupPath != filepath.Clean(pdnsSwitchBackupPath(journal.MutationRequestID)) {
			return errors.New("PowerDNS switch journal staging paths are invalid")
		}
		if (journal.PDNSBackupSHA256 == "" && journal.PDNSBackupSize != 0) ||
			(journal.PDNSBackupSHA256 != "" && (!validDNSGeneration(journal.PDNSBackupSHA256) || journal.PDNSBackupSize <= 0)) {
			return errors.New("PowerDNS switch journal backup receipt is invalid")
		}
		if journal.PDNSLiveSHA256 != "" || journal.PDNSLiveSize != 0 {
			return errors.New("PowerDNS switch journal contains adoption-only live database state")
		}
		wantConfig := []string{
			filepath.Clean(dnsMainConf), filepath.Clean(dnsManagedConf), filepath.Clean(dnsClusterConf),
		}
		sort.Strings(wantConfig)
		if len(journal.ConfigBefore) != len(wantConfig) {
			return errors.New("PowerDNS switch journal config snapshot set is incomplete")
		}
		for index, snapshot := range journal.ConfigBefore {
			if snapshot.Path != wantConfig[index] || (snapshot.Exists && snapshot.Mode != 0o644) {
				return errors.New("PowerDNS switch journal contains an unexpected config snapshot")
			}
		}
	}
	wantTarget := []string{"bind9.service", "named.service"}
	if journal.TargetEngine == transport.DNSEnginePowerDNS {
		wantTarget = []string{"pdns.service"}
	}
	wantSource := []string{}
	if journal.SourceEngine == transport.DNSEnginePowerDNS {
		wantSource = []string{"pdns.service"}
	} else if journal.SourceEngine == transport.DNSEngineBIND {
		wantSource = []string{"bind9.service", "named.service"}
	}
	if !dnsUnitSnapshotNamesEqual(journal.TargetUnitsBefore, wantTarget) ||
		!dnsUnitSnapshotNamesEqual(journal.SourceUnitsBefore, wantSource) {
		return errors.New("DNS engine switch journal unit snapshot set is incomplete")
	}
	return nil
}

func validatePDNSAdoptionJournal(journal dnsEngineSwitchJournal) error {
	if journal.SourceEngine != "" || journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.TargetGeneration != "" || journal.PreviousGeneration != "" || journal.HadPrevious ||
		journal.PDNSCandidatePath != "" || journal.PDNSBackupPath != "" ||
		journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
		!validDNSGeneration(journal.PDNSLiveSHA256) || journal.PDNSLiveSize <= 0 {
		return errors.New("PowerDNS adoption journal contains switch mutation state")
	}
	wantConfig := []string{
		filepath.Clean(dnsMainConf), filepath.Clean(dnsManagedConf), filepath.Clean(dnsClusterConf),
	}
	sort.Strings(wantConfig)
	if len(journal.ConfigBefore) != len(wantConfig) {
		return errors.New("PowerDNS adoption journal config evidence is incomplete")
	}
	for index, snapshot := range journal.ConfigBefore {
		if snapshot.Path != wantConfig[index] ||
			(snapshot.Exists && dnsSnapshotOwnerRequired() && snapshot.Mode&0o022 != 0) {
			return errors.New("PowerDNS adoption journal contains unsafe config evidence")
		}
		switch snapshot.Path {
		case filepath.Clean(dnsMainConf), filepath.Clean(dnsManagedConf):
			if !snapshot.Exists {
				return errors.New("PowerDNS adoption journal is missing managed config evidence")
			}
		case filepath.Clean(dnsClusterConf):
			wantExists := journal.Topology == transport.DNSTopologyPaired
			if snapshot.Exists != wantExists {
				return errors.New("PowerDNS adoption journal topology evidence differs from its manifest")
			}
		}
	}
	if !dnsUnitSnapshotNamesEqual(
		journal.TargetUnitsBefore,
		[]string{"bind9.service", "named.service", "pdns.service"},
	) || len(journal.SourceUnitsBefore) != 0 {
		return errors.New("PowerDNS adoption journal unit evidence is incomplete")
	}
	if err := validatePDNSAdoptionUnitEvidence(journal.TargetUnitsBefore); err != nil {
		return err
	}
	return nil
}

func validBINDConfigSnapshotSet(snapshots []dnsFileSnapshot) bool {
	apt := []string{"/etc/bind/named.conf.local", "/etc/bind/named.conf.options"}
	pacman := []string{"/etc/named.conf"}
	matches := func(want []string) bool {
		if len(snapshots) != len(want) {
			return false
		}
		for index, snapshot := range snapshots {
			if snapshot.Path != want[index] || !snapshot.Exists || snapshot.Mode != 0o644 {
				return false
			}
		}
		return true
	}
	return matches(apt) || matches(pacman)
}

func dnsUnitSnapshotNamesEqual(snapshots []dnsUnitSnapshot, want []string) bool {
	if len(snapshots) != len(want) {
		return false
	}
	for index := range snapshots {
		if snapshots[index].Name != want[index] {
			return false
		}
	}
	return true
}

func encodeDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) ([]byte, error) {
	if journal.Zones == nil {
		journal.Zones = []transport.DNSEngineSwitchZoneSnapshot{}
	}
	if journal.ConfigBefore == nil {
		journal.ConfigBefore = []dnsFileSnapshot{}
	}
	if journal.TargetUnitsBefore == nil {
		journal.TargetUnitsBefore = []dnsUnitSnapshot{}
	}
	if journal.SourceUnitsBefore == nil {
		journal.SourceUnitsBefore = []dnsUnitSnapshot{}
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine switch journal: %w", err)
	}
	if len(encoded) > dnsEngineSwitchJournalLimit {
		return nil, errors.New("DNS engine switch journal exceeds the size limit")
	}
	return append(encoded, '\n'), nil
}

func decodeDNSEngineSwitchJournal(data []byte) (dnsEngineSwitchJournal, error) {
	if len(data) == 0 || len(data) > dnsEngineSwitchJournalLimit {
		return dnsEngineSwitchJournal{}, errors.New("DNS engine switch journal has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var journal dnsEngineSwitchJournal
	if err := decoder.Decode(&journal); err != nil {
		return dnsEngineSwitchJournal{}, fmt.Errorf("decode DNS engine switch journal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return dnsEngineSwitchJournal{}, errors.New("DNS engine switch journal contains trailing JSON")
	}
	canonical, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		return dnsEngineSwitchJournal{}, err
	}
	if !bytes.Equal(data, canonical) {
		return dnsEngineSwitchJournal{}, errors.New("DNS engine switch journal is not canonical JSON")
	}
	return journal, nil
}

func readDNSEngineSwitchJournal() (dnsEngineSwitchJournal, bool, error) {
	data, err := secureReadConfig(dnsEngineSwitchJournalPath())
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineSwitchJournal{}, false, nil
	}
	if err != nil {
		return dnsEngineSwitchJournal{}, false, err
	}
	journal, err := decodeDNSEngineSwitchJournal(data)
	return journal, err == nil, err
}

func writeDNSEngineSwitchJournal(journal dnsEngineSwitchJournal) error {
	encoded, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(dnsEngineSwitchJournalPath(), encoded, 0o600); err != nil {
		verified, exists, readErr := readDNSEngineSwitchJournal()
		if readErr == nil && exists && reflect.DeepEqual(verified, journal) {
			return nil
		}
		return errors.Join(err, readErr)
	}
	verified, exists, err := readDNSEngineSwitchJournal()
	if err != nil || !exists || !reflect.DeepEqual(verified, journal) {
		if err == nil {
			err = errors.New("DNS engine switch journal readback mismatch")
		}
		return err
	}
	return nil
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
	if metadata.OwnerKnown && (metadata.UID != 0 || metadata.GID != 0) {
		return dnsFileSnapshot{}, errors.New("DNS switch snapshot file is not root-owned")
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
	if metadata.OwnerKnown && (metadata.UID != 0 || metadata.GID != 0) {
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
	for _, expected := range snapshots {
		actual, err := captureDNSFileSnapshotPreserve(expected.Path, true)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(actual, expected) {
			return errors.New("DNS adoption configuration changed during verification")
		}
	}
	return nil
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
	guard := dnsSystemdStateGuard(systemctl)
	for _, snapshot := range snapshots {
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
