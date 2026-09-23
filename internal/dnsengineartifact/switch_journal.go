package dnsengineartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// SwitchJournalV1 retains the historical wire format and frozen before-images.
// Validation is a pure evidence check, not authorization to execute or restore.
// Consumers must separately prove the accepted ledger binding, host ownership,
// native target state and unchanged source under the mutation lock.
const (
	SwitchJournalSchemaV1     = "celikpanel-dns-engine-switch-journal/v1"
	SwitchJournalLimit        = 96 << 20
	SwitchPhaseIntent         = "intent"
	SwitchPhaseTargetStaged   = "target-staged"
	SwitchPhaseSourceStopped  = "source-stopped"
	SwitchPhaseTargetStarted  = "target-started"
	SwitchPhaseTargetVerified = "target-verified"
	SwitchPhaseCommitted      = "committed"
	SwitchPhaseRollingBack    = "rolling-back"
	SwitchPhaseRolledBack     = "rolled-back"
)

// JournalPolicy is supplied by a trusted host adapter, never by journal content.
// RequireOwner is true on supported Linux hosts. False retains the historical
// non-Linux test contract; it is not a production fallback for missing metadata.
type JournalPolicy struct {
	StatePath                                                        string
	StateUID, StateGID                                               uint32
	RequireOwner                                                     bool
	PDNSMainPath, PDNSManagedPath, PDNSClusterPath, PDNSDatabasePath string
}

func canonicalJournalPath(name string) bool {
	return (filepath.IsAbs(name) && filepath.Clean(name) == name) || (strings.HasPrefix(name, "/") && pathpkg.Clean(name) == name)
}

func (p JournalPolicy) Validate() error {
	seen := make(map[string]bool)
	for _, name := range []string{p.StatePath, p.PDNSMainPath, p.PDNSManagedPath, p.PDNSClusterPath, p.PDNSDatabasePath} {
		if name == "" || name == "/" || !canonicalJournalPath(name) || strings.ContainsAny(name, "\x00\r\n") || seen[name] {
			return errors.New("invalid DNS journal host policy path")
		}
		seen[name] = true
	}
	return nil
}
func (p JournalPolicy) pdnsConfigPaths() []string {
	paths := []string{p.PDNSMainPath, p.PDNSManagedPath, p.PDNSClusterPath}
	sort.Strings(paths)
	return paths
}
func (p JournalPolicy) pdnsCandidatePath(requestID string) string {
	return filepath.Join(filepath.Dir(p.PDNSDatabasePath), ".celikpanel-switch-"+requestID+".sqlite3")
}
func (p JournalPolicy) pdnsBackupPath(requestID string) string {
	return filepath.Join(filepath.Dir(p.PDNSDatabasePath), ".celikpanel-before-switch-"+requestID+".sqlite3")
}

type FileSnapshot struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Mode       uint32 `json:"mode"`
	OwnerKnown bool   `json:"owner_known,omitempty"`
	UID        uint32 `json:"uid,omitempty"`
	GID        uint32 `json:"gid,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Data       []byte `json:"data,omitempty"`
}

type UnitSnapshot struct {
	Name          string `json:"name"`
	LoadState     string `json:"load_state"`
	ActiveState   string `json:"active_state"`
	UnitFileState string `json:"unit_file_state"`
}

type SwitchJournalV1 struct {
	Schema               string                                  `json:"schema"`
	Phase                string                                  `json:"phase"`
	Mode                 string                                  `json:"mode"`
	MutationRequestID    string                                  `json:"mutation_request_id"`
	MutationOwnerID      string                                  `json:"mutation_owner_id"`
	ManifestQualifier    string                                  `json:"manifest_qualifier"`
	SourceEngine         transport.DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine         transport.DNSEngine                     `json:"target_engine"`
	SourceEpoch          int64                                   `json:"source_epoch"`
	TargetEpoch          int64                                   `json:"target_epoch"`
	SourceRevision       int64                                   `json:"source_revision"`
	Topology             string                                  `json:"topology"`
	PairRole             string                                  `json:"pair_role,omitempty"`
	LocalIP              string                                  `json:"local_ip,omitempty"`
	LocalNS              string                                  `json:"local_ns,omitempty"`
	PeerIP               string                                  `json:"peer_ip,omitempty"`
	PeerNS               string                                  `json:"peer_ns,omitempty"`
	PrimaryCatalogSerial uint32                                  `json:"primary_catalog_serial,omitempty"`
	SnapshotBytes        int64                                   `json:"snapshot_bytes"`
	Zones                []transport.DNSEngineSwitchZoneSnapshot `json:"zones"`
	TargetGeneration     string                                  `json:"target_generation,omitempty"`
	PreviousGeneration   string                                  `json:"previous_generation,omitempty"`
	HadPrevious          bool                                    `json:"had_previous_generation"`
	StateBefore          FileSnapshot                            `json:"state_before"`
	ConfigBefore         []FileSnapshot                          `json:"config_before"`
	TargetUnitsBefore    []UnitSnapshot                          `json:"target_units_before"`
	SourceUnitsBefore    []UnitSnapshot                          `json:"source_units_before"`
	PDNSCandidatePath    string                                  `json:"pdns_candidate_path,omitempty"`
	PDNSBackupPath       string                                  `json:"pdns_backup_path,omitempty"`
	PDNSBackupSHA256     string                                  `json:"pdns_backup_sha256,omitempty"`
	PDNSBackupSize       int64                                   `json:"pdns_backup_size,omitempty"`
	PDNSLiveSHA256       string                                  `json:"pdns_live_sha256,omitempty"`
	PDNSLiveSize         int64                                   `json:"pdns_live_size,omitempty"`
}

func ValidSwitchPhase(value string) bool {
	switch value {
	case SwitchPhaseIntent, SwitchPhaseTargetStaged,
		SwitchPhaseSourceStopped, SwitchPhaseTargetStarted,
		SwitchPhaseTargetVerified, SwitchPhaseCommitted,
		SwitchPhaseRollingBack, SwitchPhaseRolledBack:
		return true
	default:
		return false
	}
}

func DigestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func ValidateFileSnapshotIntegrity(snapshot FileSnapshot) error {
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
	if snapshot.Mode == 0 || len(snapshot.Data) > SwitchJournalLimit ||
		snapshot.SHA256 != DigestBytes(snapshot.Data) {
		return errors.New("DNS switch file snapshot digest is invalid")
	}
	if !snapshot.OwnerKnown && (snapshot.UID != 0 || snapshot.GID != 0) {
		return errors.New("DNS switch file snapshot has hidden ownership metadata")
	}
	return nil
}

func (policy JournalPolicy) ValidateFileSnapshot(snapshot FileSnapshot) error {
	return policy.ValidateFileSnapshotForOwnerContract(
		snapshot, 0, 0,
		"DNS switch file snapshot is not root-owned",
	)
}

func (policy JournalPolicy) ValidateFileSnapshotForOwner(
	snapshot FileSnapshot,
	requiredUID, requiredGID uint32,
) error {
	return policy.ValidateFileSnapshotForOwnerContract(
		snapshot, requiredUID, requiredGID,
		"DNS switch file snapshot ownership differs from the managed contract",
	)
}

func (policy JournalPolicy) ValidateFileSnapshotForOwnerContract(
	snapshot FileSnapshot,
	requiredUID, requiredGID uint32,
	ownerError string,
) error {
	if err := ValidateFileSnapshotIntegrity(snapshot); err != nil {
		return err
	}
	if !snapshot.Exists {
		return nil
	}
	if policy.RequireOwner && !snapshot.OwnerKnown {
		return errors.New("DNS switch file snapshot is missing required ownership metadata")
	}
	if (snapshot.OwnerKnown &&
		(snapshot.UID != requiredUID || snapshot.GID != requiredGID)) ||
		(!snapshot.OwnerKnown && (snapshot.UID != 0 || snapshot.GID != 0)) {
		return errors.New(ownerError)
	}
	return nil
}

func (policy JournalPolicy) ValidateStateSnapshot(snapshot FileSnapshot) error {
	if err := policy.ValidateFileSnapshotForOwner(
		snapshot,
		policy.StateUID,
		policy.StateGID,
	); err != nil {
		return err
	}
	if snapshot.Path != filepath.Clean(policy.StatePath) ||
		(snapshot.Exists && snapshot.Mode != 0o600) {
		return errors.New("DNS engine switch journal state snapshot path is invalid")
	}
	return nil
}

func ValidateUnitSnapshot(snapshot UnitSnapshot) error {
	if strings.TrimSpace(snapshot.Name) != snapshot.Name || snapshot.Name == "" ||
		strings.ContainsAny(snapshot.Name, "/\\\x00\r\n") {
		return errors.New("DNS switch unit snapshot has an unsafe name")
	}
	if !validUnitLoadState(snapshot.LoadState) ||
		!validUnitActiveState(snapshot.ActiveState) ||
		!validUnitFileState(snapshot.LoadState, snapshot.UnitFileState) {
		return errors.New("DNS switch unit snapshot contains an unsupported systemd state")
	}
	if snapshot.ActiveState == "failed" || ((snapshot.LoadState == "masked") && (snapshot.ActiveState == "active")) {
		return errors.New("DNS switch unit snapshot cannot be restored deterministically")
	}
	if snapshot.LoadState == "not-found" && snapshot.UnitFileState == "" {
		return nil
	}
	switch snapshot.UnitFileState {
	case "enabled", "enabled-runtime", "disabled", "masked", "masked-runtime":
		return nil
	default:
		return errors.New("DNS switch unit-file state has no exact inverse")
	}
}

func (policy JournalPolicy) ValidateSwitchJournal(journal SwitchJournalV1) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if journal.Schema != SwitchJournalSchemaV1 || !ValidSwitchPhase(journal.Phase) ||
		(journal.Mode != transport.DNSEngineSwitchModeSwitch &&
			journal.Mode != transport.DNSEngineSwitchModeAdopt &&
			journal.Mode != transport.DNSEngineSwitchModeReinstall) ||
		!servicemutationledger.ValidIdentity(journal.MutationRequestID) ||
		!servicemutationledger.ValidIdentity(journal.MutationOwnerID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(journal.ManifestQualifier) {
		return errors.New("DNS engine switch journal identity is invalid")
	}
	commitment, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		journal.Mode,
		journal.SourceEngine, journal.TargetEngine,
		journal.SourceEpoch, journal.TargetEpoch, journal.SourceRevision,
		journal.Topology, journal.PairRole, journal.LocalIP, journal.LocalNS,
		journal.PeerIP, journal.PeerNS, journal.Zones,
	)
	if err != nil || commitment.Qualifier != journal.ManifestQualifier ||
		commitment.SnapshotBytes != journal.SnapshotBytes ||
		!reflect.DeepEqual(commitment.Zones, journal.Zones) {
		return errors.New("DNS engine switch journal manifest is not canonical")
	}
	if err := ValidatePrimaryCatalogSerial(commitment, journal.PrimaryCatalogSerial); err != nil {
		return err
	}
	if err := policy.ValidateStateSnapshot(journal.StateBefore); err != nil {
		return err
	}
	sourceState, sourceExists, err := SourceStateFromSwitchJournal(journal)
	if err != nil {
		return err
	}
	if RequiresPrimaryCatalogSerial(commitment) {
		if journal.SourceEngine == "" {
			if sourceExists || journal.PrimaryCatalogSerial != 1 {
				return errors.New("initial primary catalog journal is not fresh")
			}
		} else {
			legacySource := sourceExists &&
				sourceState.Mode == transport.DNSEngineSwitchModeSwitch &&
				sourceState.PairRole == "" &&
				sourceState.PrimaryCatalogSerial == 0
			boundSource := sourceExists &&
				sourceState.Mode == transport.DNSEngineSwitchModeSwitch &&
				sourceState.PairRole == transport.DNSPairRolePrimary &&
				sourceState.PrimaryCatalogSerial != 0 &&
				sourceState.PrimaryCatalogSerial <= journal.PrimaryCatalogSerial
			if !legacySource && !boundSource {
				return errors.New("primary catalog journal differs from its source receipt")
			}
		}
	}
	previous := ""
	for _, snapshot := range journal.ConfigBefore {
		validate := policy.ValidateFileSnapshot
		if ((journal.Mode == transport.DNSEngineSwitchModeSwitch ||
			journal.Mode == transport.DNSEngineSwitchModeReinstall) &&
			(journal.TargetEngine == transport.DNSEngineBIND ||
				journal.TargetEngine == transport.DNSEnginePowerDNS)) ||
			(journal.Mode == transport.DNSEngineSwitchModeAdopt &&
				journal.TargetEngine == transport.DNSEnginePowerDNS) {
			validate = ValidateFileSnapshotIntegrity
		}
		if err := validate(snapshot); err != nil {
			return err
		}
		if previous != "" && snapshot.Path <= previous {
			return errors.New("DNS switch file snapshots are unsorted or duplicated")
		}
		previous = snapshot.Path
	}
	for _, snapshots := range [][]UnitSnapshot{journal.TargetUnitsBefore, journal.SourceUnitsBefore} {
		previous := ""
		for _, snapshot := range snapshots {
			if err := ValidateUnitSnapshot(snapshot); err != nil {
				return err
			}
			if previous != "" && snapshot.Name <= previous {
				return errors.New("DNS switch unit snapshots are unsorted or duplicated")
			}
			previous = snapshot.Name
		}
	}
	if journal.Mode == transport.DNSEngineSwitchModeAdopt {
		if err := policy.ValidatePDNSAdoptionJournal(journal); err != nil {
			return err
		}
		return nil
	}
	if journal.TargetEngine == transport.DNSEngineBIND {
		if !ValidGeneration(journal.TargetGeneration) ||
			(journal.HadPrevious && !ValidGeneration(journal.PreviousGeneration)) ||
			(!journal.HadPrevious && journal.PreviousGeneration != "") ||
			journal.PDNSCandidatePath != "" || journal.PDNSBackupPath != "" ||
			journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
			journal.PDNSLiveSHA256 != "" || journal.PDNSLiveSize != 0 {
			return errors.New("BIND switch journal generation or PowerDNS fields are invalid")
		}
		if !policy.ValidBINDConfigSnapshotSet(journal.ConfigBefore) {
			return errors.New("BIND switch journal config snapshot set is incomplete")
		}
	} else {
		if journal.TargetGeneration != "" || journal.HadPrevious || journal.PreviousGeneration != "" {
			return errors.New("PowerDNS switch journal contains BIND generation state")
		}
		if journal.PDNSCandidatePath != filepath.Clean(policy.pdnsCandidatePath(journal.MutationRequestID)) ||
			journal.PDNSBackupPath != filepath.Clean(policy.pdnsBackupPath(journal.MutationRequestID)) {
			return errors.New("PowerDNS switch journal staging paths are invalid")
		}
		if (journal.PDNSBackupSHA256 == "" && journal.PDNSBackupSize != 0) ||
			(journal.PDNSBackupSHA256 != "" && (!ValidGeneration(journal.PDNSBackupSHA256) || journal.PDNSBackupSize <= 0)) {
			return errors.New("PowerDNS switch journal backup receipt is invalid")
		}
		if journal.PDNSLiveSHA256 != "" || journal.PDNSLiveSize != 0 {
			return errors.New("PowerDNS switch journal contains adoption-only live database state")
		}
		if err := policy.ValidatePDNSConfigSnapshotSet(
			journal.ConfigBefore,
		); err != nil {
			return err
		}
	}
	wantTarget := []string{"bind9.service", "named.service"}
	if journal.TargetEngine == transport.DNSEnginePowerDNS {
		wantTarget = []string{"pdns.service"}
	}
	wantSource := []string{}
	// A reinstall's source and target are one engine, so they are one unit set.
	// The target snapshot already froze it; demanding a second copy under the
	// source name asked the journal to record the same units twice and refused
	// the operation after its packages were already on the host.
	//
	// Yeniden kurulumun kaynağı ile hedefi tek motordur; dolayısıyla tek birim
	// kümesidir. Hedef anlık görüntüsü onu zaten dondurdu; kaynak adı altında
	// ikinci bir kopya istemek, günlükten aynı birimleri iki kez kaydetmesini
	// istiyor ve işlemi paketleri sunucuya çoktan indikten sonra reddediyordu.
	if journal.Mode != transport.DNSEngineSwitchModeReinstall {
		if journal.SourceEngine == transport.DNSEnginePowerDNS {
			wantSource = []string{"pdns.service"}
		} else if journal.SourceEngine == transport.DNSEngineBIND {
			wantSource = []string{"bind9.service", "named.service"}
		}
	}
	if !UnitSnapshotNamesEqual(journal.TargetUnitsBefore, wantTarget) ||
		!UnitSnapshotNamesEqual(journal.SourceUnitsBefore, wantSource) {
		return errors.New("DNS engine switch journal unit snapshot set is incomplete")
	}
	return nil
}

func (policy JournalPolicy) ValidatePDNSAdoptionJournal(journal SwitchJournalV1) error {
	if journal.SourceEngine != "" || journal.TargetEngine != transport.DNSEnginePowerDNS ||
		journal.TargetGeneration != "" || journal.PreviousGeneration != "" || journal.HadPrevious ||
		journal.PDNSCandidatePath != "" || journal.PDNSBackupPath != "" ||
		journal.PDNSBackupSHA256 != "" || journal.PDNSBackupSize != 0 ||
		!ValidGeneration(journal.PDNSLiveSHA256) || journal.PDNSLiveSize <= 0 {
		return errors.New("PowerDNS adoption journal contains switch mutation state")
	}
	if err := policy.ValidatePDNSConfigSnapshotSet(journal.ConfigBefore); err != nil {
		return err
	}
	for _, snapshot := range journal.ConfigBefore {
		switch snapshot.Path {
		case policy.PDNSMainPath, policy.PDNSManagedPath:
			if !snapshot.Exists {
				return errors.New("PowerDNS adoption journal is missing managed config evidence")
			}
		case policy.PDNSClusterPath:
			wantExists := journal.Topology == transport.DNSTopologyPaired
			if snapshot.Exists != wantExists {
				return errors.New("PowerDNS adoption journal topology evidence differs from its manifest")
			}
		}
	}
	if !UnitSnapshotNamesEqual(
		journal.TargetUnitsBefore,
		[]string{"bind9.service", "named.service", "pdns.service"},
	) || len(journal.SourceUnitsBefore) != 0 {
		return errors.New("PowerDNS adoption journal unit evidence is incomplete")
	}
	if err := ValidatePDNSAdoptionUnits(journal.TargetUnitsBefore); err != nil {
		return err
	}
	return nil
}

func (policy JournalPolicy) ValidBINDConfigSnapshotSet(snapshots []FileSnapshot) bool {
	apt := []string{"/etc/bind/named.conf.local", "/etc/bind/named.conf.options"}
	pacman := []string{"/etc/named.conf"}
	matches := func(want []string, aptLayout bool) bool {
		if len(snapshots) != len(want) {
			return false
		}
		// The vendor file mode follows the layout: Debian ships its BIND
		// configuration 0644 root:bind, Arch ships /etc/named.conf 0640
		// root:named (register R-018). The exact group is proven by the
		// owner policy at capture time; here the shape must merely be the
		// layout's, with one common bounded group across the set.
		// Satıcı dosya kipi yerleşimi izler: Debian BIND yapılandırmasını
		// 0644 root:bind, Arch /etc/named.conf'u 0640 root:named gönderir
		// (defter R-018). Tam grup, yakalama anında sahiplik politikasıyla
		// kanıtlanır; burada biçim yalnız yerleşimin biçimi olmalı ve küme
		// boyunca tek, sınırlı bir ortak grup taşımalıdır.
		wantMode := uint32(0o640)
		if aptLayout {
			wantMode = 0o644
		}
		var commonGID uint32
		for index, snapshot := range snapshots {
			if snapshot.Path != want[index] || !snapshot.Exists || snapshot.Mode != wantMode ||
				(policy.RequireOwner && !snapshot.OwnerKnown) ||
				(snapshot.OwnerKnown && (snapshot.UID != 0 || snapshot.GID > uint32(1<<31-1))) ||
				(index > 0 && snapshot.OwnerKnown != snapshots[0].OwnerKnown) {
				return false
			}
			if snapshot.OwnerKnown {
				if index == 0 {
					commonGID = snapshot.GID
				} else if snapshot.GID != commonGID {
					return false
				}
			}
		}
		return true
	}
	return matches(apt, true) || matches(pacman, false)
}

func UnitSnapshotNamesEqual(snapshots []UnitSnapshot, want []string) bool {
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

func (policy JournalPolicy) EncodeSwitchJournal(journal SwitchJournalV1) ([]byte, error) {
	if journal.Zones == nil {
		journal.Zones = []transport.DNSEngineSwitchZoneSnapshot{}
	}
	if journal.ConfigBefore == nil {
		journal.ConfigBefore = []FileSnapshot{}
	}
	if journal.TargetUnitsBefore == nil {
		journal.TargetUnitsBefore = []UnitSnapshot{}
	}
	if journal.SourceUnitsBefore == nil {
		journal.SourceUnitsBefore = []UnitSnapshot{}
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine switch journal: %w", err)
	}
	if len(encoded) > SwitchJournalLimit {
		return nil, errors.New("DNS engine switch journal exceeds the size limit")
	}
	return append(encoded, '\n'), nil
}

func (policy JournalPolicy) DecodeSwitchJournal(data []byte) (SwitchJournalV1, error) {
	if len(data) == 0 || len(data) > SwitchJournalLimit {
		return SwitchJournalV1{}, errors.New("DNS engine switch journal has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var journal SwitchJournalV1
	if err := decoder.Decode(&journal); err != nil {
		return SwitchJournalV1{}, fmt.Errorf("decode DNS engine switch journal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return SwitchJournalV1{}, errors.New("DNS engine switch journal contains trailing JSON")
	}
	canonical, err := policy.EncodeSwitchJournal(journal)
	if err != nil {
		return SwitchJournalV1{}, err
	}
	if !bytes.Equal(data, canonical) {
		return SwitchJournalV1{}, errors.New("DNS engine switch journal is not canonical JSON")
	}
	return journal, nil
}

func SourceStateFromSwitchJournal(
	journal SwitchJournalV1,
) (StateV1, bool, error) {
	if journal.SourceEngine == "" {
		if journal.StateBefore.Exists {
			return StateV1{}, false,
				errors.New("uninitialized DNS source journal unexpectedly snapshots active state")
		}
		return StateV1{}, false, nil
	}
	if !journal.StateBefore.Exists {
		return StateV1{}, false,
			errors.New("DNS switch journal is missing source engine state")
	}
	state, _, err := DecodeStateDocument(journal.StateBefore.Data)
	if err != nil {
		return StateV1{}, false,
			fmt.Errorf("decode DNS switch source state: %w", err)
	}
	if state.Engine != journal.SourceEngine ||
		state.EngineEpoch != journal.SourceEpoch {
		return StateV1{}, false,
			errors.New("DNS switch journal source state identity differs from its manifest")
	}
	return state, true, nil
}

func (policy JournalPolicy) ValidatePDNSConfigSnapshotSet(
	snapshots []FileSnapshot,
) error {
	paths := policy.pdnsConfigPaths()
	if len(snapshots) != len(paths) {
		return errors.New("PowerDNS config snapshot set is incomplete")
	}
	for index, snapshot := range snapshots {
		if err := ValidateFileSnapshotIntegrity(snapshot); err != nil {
			return err
		}
		if snapshot.Path != paths[index] {
			return errors.New("PowerDNS config snapshot set contains an unexpected path")
		}
		if err := policy.ValidatePDNSConfigSnapshot(snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (policy JournalPolicy) ValidatePDNSConfigSnapshot(snapshot FileSnapshot) error {
	if err := ValidateFileSnapshotIntegrity(snapshot); err != nil {
		return err
	}
	switch snapshot.Path {
	case policy.PDNSMainPath:
		if !snapshot.Exists || snapshot.Mode != 0o640 ||
			!snapshot.OwnerKnown || snapshot.UID != 0 ||
			snapshot.GID > uint32(1<<31-1) {
			return errors.New("PowerDNS main config snapshot differs from its installed-file contract")
		}
	case policy.PDNSManagedPath, policy.PDNSClusterPath:
		if snapshot.Exists && (snapshot.Mode != 0o644 ||
			!snapshot.OwnerKnown || snapshot.UID != 0 || snapshot.GID != 0) {
			return errors.New("PowerDNS managed config snapshot differs from its root-owned contract")
		}
	default:
		return errors.New("PowerDNS config snapshot path is unsupported")
	}
	return nil
}

func ValidatePDNSAdoptionUnits(units []UnitSnapshot) error {
	if !UnitSnapshotNamesEqual(
		units, []string{"bind9.service", "named.service", "pdns.service"},
	) {
		return errors.New("PowerDNS adoption unit evidence is incomplete")
	}
	for _, unit := range units {
		active := unit.ActiveState == "active"
		if unit.Name == "pdns.service" {
			if !active {
				return errors.New("PowerDNS adoption target is not running")
			}
			continue
		}
		if active {
			return errors.New("PowerDNS adoption found another DNS engine running")
		}
	}
	return nil
}

func RequiresPrimaryCatalogSerial(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary
}

func ValidatePrimaryCatalogSerial(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	serial uint32,
) error {
	if RequiresPrimaryCatalogSerial(manifest) {
		if serial == 0 {
			return errors.New("paired primary DNS engine state is missing its catalog serial")
		}
		return nil
	}
	if serial != 0 {
		return errors.New("non-primary DNS engine state unexpectedly binds a catalog serial")
	}
	return nil
}

func validUnitLoadState(state string) bool {
	switch state {
	case "loaded", "not-found", "masked":
		return true
	default:
		return false
	}
}

func validUnitActiveState(state string) bool {
	// Transitional or unknown states cannot be restored deterministically.
	switch state {
	case "active", "inactive", "failed":
		return true
	default:
		return false
	}
}

func validUnitFileState(loadState, state string) bool {
	if state == "" {
		return loadState == "not-found"
	}
	switch state {
	case "enabled", "enabled-runtime", "disabled":
		return loadState == "loaded"
	case "masked", "masked-runtime":
		return loadState == "masked"
	default:
		return false
	}
}
