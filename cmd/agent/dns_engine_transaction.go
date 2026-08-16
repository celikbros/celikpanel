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
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256,omitempty"`
	Data   []byte `json:"data,omitempty"`
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
	MutationRequestID  string                                  `json:"mutation_request_id"`
	MutationOwnerID    string                                  `json:"mutation_owner_id"`
	ManifestQualifier  string                                  `json:"manifest_qualifier"`
	SourceEngine       transport.DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine       transport.DNSEngine                     `json:"target_engine"`
	SourceEpoch        int64                                   `json:"source_epoch"`
	TargetEpoch        int64                                   `json:"target_epoch"`
	SourceRevision     int64                                   `json:"source_revision"`
	Topology           string                                  `json:"topology"`
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
	if snapshot.Path == "" || !filepath.IsAbs(clean) || clean != snapshot.Path ||
		snapshot.Mode&^0o777 != 0 {
		return errors.New("DNS switch file snapshot has an unsafe path or mode")
	}
	if !snapshot.Exists {
		if snapshot.Mode != 0 || snapshot.SHA256 != "" || len(snapshot.Data) != 0 {
			return errors.New("absent DNS switch file snapshot contains hidden state")
		}
		return nil
	}
	if snapshot.Mode == 0 || len(snapshot.Data) > dnsEngineSwitchJournalLimit ||
		snapshot.SHA256 != digestDNSBytes(snapshot.Data) {
		return errors.New("DNS switch file snapshot digest is invalid")
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
		!validMutationIdentity(journal.MutationRequestID) ||
		!validMutationIdentity(journal.MutationOwnerID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(journal.ManifestQualifier) {
		return errors.New("DNS engine switch journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.Zones,
	)
	if err != nil || commitment.Qualifier != journal.ManifestQualifier ||
		commitment.SnapshotBytes != journal.SnapshotBytes ||
		!reflect.DeepEqual(commitment.Zones, journal.Zones) {
		return errors.New("DNS engine switch journal manifest is not canonical")
	}
	if err := validateDNSFileSnapshot(journal.StateBefore); err != nil {
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
	if journal.TargetEngine == transport.DNSEngineBIND {
		if !validDNSGeneration(journal.TargetGeneration) ||
			(journal.HadPrevious && !validDNSGeneration(journal.PreviousGeneration)) ||
			(!journal.HadPrevious && journal.PreviousGeneration != "") ||
			journal.PDNSCandidatePath != "" || journal.PDNSBackupPath != "" ||
			journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 {
			return errors.New("BIND switch journal generation or PowerDNS fields are invalid")
		}
	} else if journal.TargetGeneration != "" || journal.HadPrevious ||
		journal.PreviousGeneration != "" {
		return errors.New("PowerDNS switch journal contains BIND generation state")
	}
	return nil
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
		return err
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
		return err
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
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) && allowAbsent {
		return dnsFileSnapshot{Path: path}, nil
	}
	if err != nil {
		return dnsFileSnapshot{}, err
	}
	return dnsFileSnapshot{
		Path: path, Exists: true, Mode: uint32(mode.Perm()),
		SHA256: digestDNSBytes(data), Data: append([]byte(nil), data...),
	}, nil
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
		(snapshot.Exists && (after.SHA256 != snapshot.SHA256 || !bytes.Equal(after.Data, snapshot.Data))) {
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
