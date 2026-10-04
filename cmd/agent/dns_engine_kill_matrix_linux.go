//go:build linux && dns_kill_matrix

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"golang.org/x/sys/unix"
)

const (
	dnsKillMatrixMarkerSchema            = "celikpanel-dns-kill-matrix-boundary/v1"
	dnsKillMatrixRollbackPrecursorSchema = "celikpanel-dns-kill-matrix-rollback-precursor/v1"
	dnsKillMatrixRollbackPrecursorAction = "returned-injected-error"

	dnsKillMatrixEnvCellID            = "CELIKPANEL_DNS_KILL_MATRIX_CELL_ID"
	dnsKillMatrixEnvDriver            = "CELIKPANEL_DNS_KILL_MATRIX_DRIVER"
	dnsKillMatrixEnvPoint             = "CELIKPANEL_DNS_KILL_MATRIX_POINT"
	dnsKillMatrixEnvPhase             = "CELIKPANEL_DNS_KILL_MATRIX_PHASE"
	dnsKillMatrixEnvRequestID         = "CELIKPANEL_DNS_KILL_MATRIX_REQUEST_ID"
	dnsKillMatrixEnvNonce             = "CELIKPANEL_DNS_KILL_MATRIX_NONCE"
	dnsKillMatrixEnvMarker            = "CELIKPANEL_DNS_KILL_MATRIX_MARKER"
	dnsKillMatrixEnvReadyFD           = "CELIKPANEL_DNS_KILL_MATRIX_READY_FD"
	dnsKillMatrixEnvRollbackPrecursor = "CELIKPANEL_DNS_KILL_MATRIX_ROLLBACK_PRECURSOR"
	dnsKillMatrixBindHandoffCell      = "bind__rolling-back__after-write__standalone__peer-reachable"

	dnsKillMatrixPreIntentPhase = "pre-intent"
	dnsKillMatrixMaxCellID      = 192
	dnsKillMatrixMaxMarkerPath  = 4095
)

var (
	dnsKillMatrixEnvironment = [...]string{
		dnsKillMatrixEnvCellID,
		dnsKillMatrixEnvDriver,
		dnsKillMatrixEnvPoint,
		dnsKillMatrixEnvPhase,
		dnsKillMatrixEnvRequestID,
		dnsKillMatrixEnvNonce,
		dnsKillMatrixEnvMarker,
		dnsKillMatrixEnvReadyFD,
	}
	// Returned only when an injected park operation returns, which the default
	// runtime never does. It is kept as the fail-closed result of a test park.
	dnsKillMatrixResumedError = errors.New(
		"DNS kill-matrix child resumed after SIGSTOP instead of being killed",
	)
)

type dnsKillMatrixConfig struct {
	CellID            string
	Driver            string
	Point             string
	Phase             string
	RequestID         string
	Nonce             string
	Marker            string
	ReadyFD           int
	RollbackPrecursor string
}

type dnsKillMatrixObservedJournal struct {
	Path              string `json:"path,omitempty"`
	Schema            string `json:"schema"`
	Phase             string `json:"phase"`
	Mode              string `json:"mode"`
	MutationRequestID string `json:"mutation_request_id"`
	MutationOwnerID   string `json:"mutation_owner_id"`
	ManifestQualifier string `json:"manifest_qualifier"`
	SourceEngine      string `json:"source_engine,omitempty"`
	TargetEngine      string `json:"target_engine"`
	SourceEpoch       int64  `json:"source_epoch"`
	TargetEpoch       int64  `json:"target_epoch"`
	SourceRevision    int64  `json:"source_revision"`
	Topology          string `json:"topology"`
	PairRole          string `json:"pair_role,omitempty"`
}

type dnsKillMatrixRollbackPrecursorSpec struct {
	Point string
	Phase string
}

type dnsKillMatrixRollbackPrecursorEvidence struct {
	Schema          string                       `json:"schema"`
	Driver          string                       `json:"driver"`
	ObservedDriver  string                       `json:"observed_driver"`
	Point           string                       `json:"point"`
	Phase           string                       `json:"phase"`
	RequestID       string                       `json:"request_id"`
	Action          string                       `json:"action"`
	ObservedJournal dnsKillMatrixObservedJournal `json:"observed_journal"`
}

type dnsKillMatrixMarker struct {
	Schema            string                                  `json:"schema"`
	CellID            string                                  `json:"cell_id"`
	Driver            string                                  `json:"driver"`
	ObservedDriver    string                                  `json:"observed_driver"`
	Point             string                                  `json:"point"`
	Phase             string                                  `json:"phase"`
	RequestID         string                                  `json:"request_id"`
	Nonce             string                                  `json:"nonce"`
	Marker            string                                  `json:"marker"`
	ReadyFD           int                                     `json:"ready_fd"`
	PID               int                                     `json:"pid"`
	ProcessStartTicks string                                  `json:"process_start_ticks"`
	RecordedAt        string                                  `json:"recorded_at"`
	ObservedJournal   dnsKillMatrixObservedJournal            `json:"observed_journal"`
	RollbackPrecursor *dnsKillMatrixRollbackPrecursorEvidence `json:"rollback_precursor,omitempty"`
}

type dnsKillMatrixRuntimeOps struct {
	pid         func() int
	startTicks  func(int) (string, error)
	writeMarker func(string, dnsKillMatrixMarker) error
	notifyReady func(int, string) error
	stopProcess func(int) error
	now         func() time.Time
	// park holds the calling goroutine after the boundary has been published.
	// The default never returns, so neither the rest of the journal writer nor
	// any error path or deferred cleanup of the mutating operation runs in this
	// process after the marker exists.
	park func(error)
}

type dnsKillMatrixRuntime struct {
	config dnsKillMatrixConfig
	ops    dnsKillMatrixRuntimeOps
	fired  atomic.Bool

	precursorMu       sync.Mutex
	precursorState    uint8
	precursorEvidence dnsKillMatrixRollbackPrecursorEvidence
}

const (
	dnsKillMatrixPrecursorAbsent uint8 = iota
	dnsKillMatrixPrecursorRecorded
	dnsKillMatrixPrecursorInvalid
)

func init() {
	if mailRenewalOnlyBuild {
		return
	}
	config, active, err := dnsKillMatrixConfigFromEnvironment(os.LookupEnv)
	if !active {
		return
	}
	if err == nil {
		err = dnsKillMatrixPrepareReadyFD(config.ReadyFD)
	}
	if err != nil {
		// A partial or malformed selector must never degrade into an inert hook:
		// that would let the controller report a false pass after timing out at
		// the boundary it believed it had armed.
		dnsEngineSwitchJournalFaultHook = func(string, string, dnsEngineSwitchJournal) error {
			return fmt.Errorf("invalid DNS kill-matrix selector: %w", err)
		}
		return
	}
	runtime := &dnsKillMatrixRuntime{
		config: config,
		ops:    dnsKillMatrixDefaultRuntimeOps(),
	}
	dnsEngineSwitchJournalFaultHook = runtime.hook
}

func dnsKillMatrixDefaultRuntimeOps() dnsKillMatrixRuntimeOps {
	return dnsKillMatrixRuntimeOps{
		pid:         os.Getpid,
		startTicks:  serviceMutationProcessStartIdentity,
		writeMarker: dnsKillMatrixWriteMarker,
		notifyReady: dnsKillMatrixNotifyReady,
		stopProcess: dnsKillMatrixStopCallingThread,
		now:         time.Now,
		park:        dnsKillMatrixParkForever,
	}
}

// dnsKillMatrixStopCallingThread stops the whole process from the thread that
// runs the hook. A process-directed kill(2) may be taken by another thread, so
// the caller could run for milliseconds before its own thread stops (cell c6,
// batch 4, 2026-09-29). tgkill(2) to the calling thread marks this thread's
// pending stop, and the kernel handles it before the syscall returns to user
// space; the group stop then stops every other thread. The OS thread stays
// locked because the caller parks afterwards and never unlocks it.
func dnsKillMatrixStopCallingThread(pid int) error {
	goruntime.LockOSThread()
	return unix.Tgkill(pid, unix.Gettid(), unix.SIGSTOP)
}

// dnsKillMatrixParkForever is reached only if the boundary could not be
// published or stopped, or if something resumed the stopped process (SIGCONT)
// instead of killing it. The operation must not continue in either case: its
// error path would perform native effects outside the named boundary. The
// controller then sees a sleeping, not stopped, process and refuses the cell.
func dnsKillMatrixParkForever(reason error) {
	log.Printf(
		"DNS kill-matrix boundary holds the mutating operation instead of continuing it: %v",
		reason,
	)
	// A sleeping goroutine keeps a timer pending, so the runtime's deadlock
	// detector cannot turn the hold into a process exit when no other
	// goroutine is runnable (as in a bare test binary); select{} would.
	for {
		time.Sleep(time.Hour)
	}
}

func dnsKillMatrixConfigFromEnvironment(
	lookup func(string) (string, bool),
) (dnsKillMatrixConfig, bool, error) {
	if lookup == nil {
		return dnsKillMatrixConfig{}, false, errors.New("environment lookup is nil")
	}
	values := make(map[string]string, len(dnsKillMatrixEnvironment))
	present := make(map[string]bool, len(dnsKillMatrixEnvironment))
	active := false
	for _, name := range dnsKillMatrixEnvironment {
		value, ok := lookup(name)
		values[name] = value
		present[name] = ok
		active = active || ok
	}
	precursor, precursorPresent := lookup(dnsKillMatrixEnvRollbackPrecursor)
	active = active || precursorPresent
	if !active {
		return dnsKillMatrixConfig{}, false, nil
	}
	if precursorPresent && precursor == "" {
		return dnsKillMatrixConfig{}, true, fmt.Errorf("%s must not be empty", dnsKillMatrixEnvRollbackPrecursor)
	}
	missing := make([]string, 0, len(dnsKillMatrixEnvironment))
	for _, name := range dnsKillMatrixEnvironment {
		if !present[name] || values[name] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		return dnsKillMatrixConfig{}, true, fmt.Errorf(
			"selector requires every environment value; missing %s",
			strings.Join(missing, ", "),
		)
	}
	readyFD, err := dnsKillMatrixParseReadyFD(values[dnsKillMatrixEnvReadyFD])
	if err != nil {
		return dnsKillMatrixConfig{}, true, err
	}
	config := dnsKillMatrixConfig{
		CellID:            values[dnsKillMatrixEnvCellID],
		Driver:            values[dnsKillMatrixEnvDriver],
		Point:             values[dnsKillMatrixEnvPoint],
		Phase:             values[dnsKillMatrixEnvPhase],
		RequestID:         values[dnsKillMatrixEnvRequestID],
		Nonce:             values[dnsKillMatrixEnvNonce],
		Marker:            values[dnsKillMatrixEnvMarker],
		ReadyFD:           readyFD,
		RollbackPrecursor: precursor,
	}
	if err := dnsKillMatrixValidateConfig(config); err != nil {
		return dnsKillMatrixConfig{}, true, err
	}
	return config, true, nil
}

func dnsKillMatrixValidateConfig(config dnsKillMatrixConfig) error {
	if !dnsKillMatrixValidCellID(config.CellID) {
		return fmt.Errorf("%s is not a lowercase matrix-cell slug", dnsKillMatrixEnvCellID)
	}
	switch config.Driver {
	case dnsEngineSwitchFaultDriverBIND,
		dnsEngineSwitchFaultDriverPDNSSwitch,
		dnsEngineSwitchFaultDriverPDNSAdopt,
		dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure,
		dnsEngineSwitchFaultDriverSignedUpdateFinalize:
	default:
		return fmt.Errorf("%s is not a supported matrix driver", dnsKillMatrixEnvDriver)
	}
	switch config.Point {
	case dnsEngineSwitchJournalFaultPreIntent:
		if config.Phase != dnsKillMatrixPreIntentPhase {
			return fmt.Errorf("%s pre_intent requires phase pre-intent", dnsKillMatrixEnvPoint)
		}
	case dnsEngineSwitchJournalFaultBeforeWrite, dnsEngineSwitchJournalFaultAfterWrite:
		if !validDNSSwitchPhase(config.Phase) {
			return fmt.Errorf("%s is not a journal phase", dnsKillMatrixEnvPhase)
		}
	default:
		return fmt.Errorf("%s is not a supported hook point", dnsKillMatrixEnvPoint)
	}
	if !validMutationIdentity(config.RequestID) {
		return fmt.Errorf("%s must be exactly 32 lowercase hexadecimal characters", dnsKillMatrixEnvRequestID)
	}
	if !dnsKillMatrixValidNonce(config.Nonce) {
		return fmt.Errorf("%s must be 32 to 128 lowercase hexadecimal characters", dnsKillMatrixEnvNonce)
	}
	if err := dnsKillMatrixValidateMarkerPath(config.Marker); err != nil {
		return fmt.Errorf("%s: %w", dnsKillMatrixEnvMarker, err)
	}
	if config.ReadyFD < 3 {
		return fmt.Errorf("%s must not name a standard file descriptor", dnsKillMatrixEnvReadyFD)
	}
	if config.RollbackPrecursor != "" && !(config.RollbackPrecursor == dnsSwitchPhaseTargetStarted &&
		config.CellID == dnsKillMatrixBindHandoffCell &&
		config.Driver == dnsEngineSwitchFaultDriverBIND &&
		config.Point == dnsEngineSwitchJournalFaultAfterWrite &&
		config.Phase == dnsSwitchPhaseRollingBack) {
		return fmt.Errorf("%s is not valid for the exact BIND recovery handoff", dnsKillMatrixEnvRollbackPrecursor)
	}
	return nil
}

func dnsKillMatrixValidCellID(value string) bool {
	if len(value) == 0 || len(value) > dnsKillMatrixMaxCellID ||
		!dnsKillMatrixCellAlphaNumeric(value[0]) {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == ':' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func dnsKillMatrixCellAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9'
}

func dnsKillMatrixValidNonce(value string) bool {
	if len(value) < 32 || len(value) > 128 || len(value)%2 != 0 ||
		strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func dnsKillMatrixValidateMarkerPath(path string) error {
	if path == "" || len(path) > dnsKillMatrixMaxMarkerPath ||
		!filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("marker path must be a clean absolute path")
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return errors.New("marker path must name a file")
	}
	return nil
}

func dnsKillMatrixParseReadyFD(value string) (int, error) {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 3 || strconv.FormatInt(parsed, 10) != value {
		return 0, fmt.Errorf(
			"%s must be a canonical decimal file descriptor greater than 2",
			dnsKillMatrixEnvReadyFD,
		)
	}
	return int(parsed), nil
}

func dnsKillMatrixPrepareReadyFD(fd int) error {
	if err := dnsKillMatrixValidateOpenReadyFD(fd); err != nil {
		return err
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		return fmt.Errorf("inspect DNS kill-matrix ready descriptor: %w", err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, flags|unix.FD_CLOEXEC); err != nil {
		return fmt.Errorf("make DNS kill-matrix ready descriptor close-on-exec: %w", err)
	}
	return nil
}

func dnsKillMatrixValidateOpenReadyFD(fd int) error {
	if fd < 3 {
		return errors.New("DNS kill-matrix ready descriptor overlaps standard I/O")
	}
	var status unix.Stat_t
	if err := unix.Fstat(fd, &status); err != nil {
		return fmt.Errorf("inspect DNS kill-matrix ready descriptor: %w", err)
	}
	typeBits := status.Mode & unix.S_IFMT
	if typeBits != unix.S_IFIFO && typeBits != unix.S_IFSOCK {
		return errors.New("DNS kill-matrix ready descriptor is not a pipe or socket")
	}
	return nil
}

func dnsKillMatrixRollbackPrecursorFor(
	config dnsKillMatrixConfig,
) (dnsKillMatrixRollbackPrecursorSpec, bool) {
	if config.Phase != dnsSwitchPhaseRollingBack &&
		config.Phase != dnsSwitchPhaseRolledBack {
		return dnsKillMatrixRollbackPrecursorSpec{}, false
	}
	if config.RollbackPrecursor == dnsSwitchPhaseTargetStarted {
		return dnsKillMatrixRollbackPrecursorSpec{
			Point: dnsEngineSwitchJournalFaultAfterWrite,
			Phase: dnsSwitchPhaseTargetStarted,
		}, true
	}
	switch config.Driver {
	case dnsEngineSwitchFaultDriverBIND,
		dnsEngineSwitchFaultDriverPDNSSwitch,
		dnsEngineSwitchFaultDriverPDNSSecondaryReconfigure:
		return dnsKillMatrixRollbackPrecursorSpec{
			Point: dnsEngineSwitchJournalFaultAfterWrite,
			Phase: dnsSwitchPhaseTargetStaged,
		}, true
	case dnsEngineSwitchFaultDriverPDNSAdopt:
		return dnsKillMatrixRollbackPrecursorSpec{
			Point: dnsEngineSwitchJournalFaultAfterWrite,
			Phase: dnsSwitchPhaseIntent,
		}, true
	default:
		return dnsKillMatrixRollbackPrecursorSpec{}, false
	}
}

func dnsKillMatrixObservedJournalFor(
	point string,
	journal dnsEngineSwitchJournal,
) dnsKillMatrixObservedJournal {
	journalPath := ""
	if point != dnsEngineSwitchJournalFaultPreIntent {
		journalPath = dnsEngineSwitchJournalPath()
	}
	return dnsKillMatrixObservedJournal{
		Path:              journalPath,
		Schema:            journal.Schema,
		Phase:             journal.Phase,
		Mode:              journal.Mode,
		MutationRequestID: journal.MutationRequestID,
		MutationOwnerID:   journal.MutationOwnerID,
		ManifestQualifier: journal.ManifestQualifier,
		SourceEngine:      string(journal.SourceEngine),
		TargetEngine:      string(journal.TargetEngine),
		SourceEpoch:       journal.SourceEpoch,
		TargetEpoch:       journal.TargetEpoch,
		SourceRevision:    journal.SourceRevision,
		Topology:          journal.Topology,
		PairRole:          journal.PairRole,
	}
}

// The late BIND fault only arms after a complete V2 inverse-source envelope.
// Full journal validation below binds every source byte and identity before the
// fault is published; this gate preserves the earlier, specific refusal.
func dnsKillMatrixLaterBINDFrozenSourceProof(journal dnsEngineSwitchJournal) bool {
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV2 ||
		journal.InversePlan == nil ||
		len(journal.InversePlan.BINDUnchangedConfig) != 2 {
		return false
	}
	plan := journal.InversePlan
	if journal.SourceEngine == "pdns" {
		return plan.SourcePDNS != nil && plan.SourceBIND == nil &&
			len(plan.SourcePDNS.ConfigBefore) == 3
	}
	return journal.SourceEngine == "" && plan.SourcePDNS == nil &&
		plan.SourceBIND != nil && len(plan.SourceBIND.Files) > 0 &&
		len(plan.SourceBIND.Zones) > 0 && len(journal.ConfigBefore) == 2 &&
		journal.Mode == "switch" && journal.TargetEngine == "bind" &&
		journal.SourceEpoch == 0 && journal.TargetEpoch == 1 &&
		journal.SourceRevision == 0 && journal.Topology == "standalone" &&
		journal.PairRole == "" && journal.LocalIP == "" &&
		journal.LocalNS == "" && journal.PeerIP == "" &&
		journal.PeerNS == "" && !journal.StateBefore.Exists
}

// dnsKillMatrixFreshPairedPrimaryV3 reports the one V3 shape the hook admits:
// the fresh paired PowerDNS primary produced by the pdns-switch driver (empty
// source, target PowerDNS at epoch 1, paired topology, primary role, a V3
// fresh-primary plan). Every other V3 journal, and any V3 journal under
// another driver, keeps the schema refusal.
//
// Hook'un kabul ettiği tek V3 biçimi: pdns-switch sürücüsünün ürettiği boş
// kaynaklı, eşli, birincil PowerDNS ilk kurulumu. Diğer her V3 günlüğü
// reddedilmeye devam eder.
func dnsKillMatrixFreshPairedPrimaryV3(driver string, journal dnsEngineSwitchJournal) bool {
	return driver == dnsEngineSwitchFaultDriverPDNSSwitch &&
		journal.Schema == dnsengineartifact.SwitchJournalSchemaV3 &&
		journal.PDNSFreshPlan != nil &&
		journal.Mode == "switch" &&
		journal.SourceEngine == "" && journal.SourceEpoch == 0 &&
		journal.TargetEngine == "pdns" && journal.TargetEpoch == 1 &&
		journal.Topology == "paired" && journal.PairRole == "primary"
}

// dnsKillMatrixSelectsPhase decides whether journal is the selected write.
//
// V1/V2 journals are selected by their exact phase, unchanged.
//
// For the fresh paired PowerDNS primary (V3) two rules are added:
//   - V3 has no source-stopped phase. The harness names the manifest
//     coordinate source-stopped for the boundary where the V3 producer
//     records target-enable-intent (the target unit is about to be enabled
//     and started); only for this shape does a configured source-stopped
//     select the target-enable-intent write. A configured target-enable-intent
//     selects it directly.
//   - target-started is written twice: once right after the start, and again
//     with the native observation attached. The selected write is always the
//     FIRST one (no native observation). The second write is never selected,
//     so the cut is deterministic whether the point is before- or after-write.
//
// V3 fresh-primary günlüğünde source-stopped yoktur; yalnız bu biçim için
// yapılandırılmış source-stopped, target-enable-intent yazımını seçer.
// target-started iki kez yazılır; seçilen her zaman ilk yazımdır.
func (runtime *dnsKillMatrixRuntime) selectsPhase(driver string, journal dnsEngineSwitchJournal) bool {
	if !dnsKillMatrixFreshPairedPrimaryV3(driver, journal) {
		return journal.Phase == runtime.config.Phase
	}
	switch {
	case journal.Phase == dnsSwitchPhaseTargetStarted:
		return runtime.config.Phase == dnsSwitchPhaseTargetStarted &&
			journal.PDNSFreshPlan.Native == nil
	case journal.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent:
		return runtime.config.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent ||
			runtime.config.Phase == dnsSwitchPhaseSourceStopped
	default:
		return journal.Phase == runtime.config.Phase
	}
}

func (runtime *dnsKillMatrixRuntime) validateObservation(
	driver string,
	point string,
	journal dnsEngineSwitchJournal,
	label string,
) error {
	if journal.MutationRequestID != runtime.config.RequestID {
		return fmt.Errorf(
			"DNS kill-matrix request mismatch at %s: observed %q",
			label, journal.MutationRequestID,
		)
	}
	if driver != runtime.config.Driver {
		return fmt.Errorf(
			"DNS kill-matrix driver mismatch at %s: observed %q",
			label, driver,
		)
	}
	if journal.Schema != dnsEngineSwitchJournalSchema &&
		!(driver == dnsEngineSwitchFaultDriverBIND && journal.Schema == dnsengineartifact.SwitchJournalSchemaV2) &&
		!dnsKillMatrixFreshPairedPrimaryV3(driver, journal) {
		return fmt.Errorf(
			"DNS kill-matrix journal schema mismatch at %s: observed %q",
			label, journal.Schema,
		)
	}
	if runtime.config.RollbackPrecursor == dnsSwitchPhaseTargetStarted &&
		!dnsKillMatrixLaterBINDFrozenSourceProof(journal) {
		return fmt.Errorf("DNS kill-matrix later BIND rollback requires V2 frozen PowerDNS or owner BIND source proof at %s", label)
	}
	if point == dnsEngineSwitchJournalFaultPreIntent {
		if !validMutationIdentity(journal.MutationOwnerID) {
			return errors.New(`DNS kill-matrix pre-intent owner identity is invalid`)
		}
		if _, err := switchJournalManifest(journal); err != nil {
			return fmt.Errorf(`validate DNS kill-matrix pre-intent manifest: %w`, err)
		}
	} else if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return fmt.Errorf(`validate DNS kill-matrix observed journal: %w`, err)
	}
	return nil
}

func (runtime *dnsKillMatrixRuntime) injectRollbackPrecursor(
	driver string,
	point string,
	journal dnsEngineSwitchJournal,
	spec dnsKillMatrixRollbackPrecursorSpec,
) error {
	if err := runtime.validateObservation(
		driver, point, journal, "rollback precursor",
	); err != nil {
		return err
	}
	evidence := dnsKillMatrixRollbackPrecursorEvidence{
		Schema:          dnsKillMatrixRollbackPrecursorSchema,
		Driver:          runtime.config.Driver,
		ObservedDriver:  driver,
		Point:           point,
		Phase:           journal.Phase,
		RequestID:       runtime.config.RequestID,
		Action:          dnsKillMatrixRollbackPrecursorAction,
		ObservedJournal: dnsKillMatrixObservedJournalFor(point, journal),
	}
	runtime.precursorMu.Lock()
	if runtime.precursorState != dnsKillMatrixPrecursorAbsent {
		runtime.precursorState = dnsKillMatrixPrecursorInvalid
		runtime.precursorEvidence = dnsKillMatrixRollbackPrecursorEvidence{}
		runtime.precursorMu.Unlock()
		return errors.New("DNS kill-matrix rollback precursor fired more than once")
	}
	runtime.precursorEvidence = evidence
	runtime.precursorState = dnsKillMatrixPrecursorRecorded
	runtime.precursorMu.Unlock()
	return fmt.Errorf(
		"inject DNS kill-matrix rollback precursor at %s/%s: %w",
		spec.Phase, spec.Point, dnsEngineSwitchRollbackPrecursorError,
	)
}

func (runtime *dnsKillMatrixRuntime) selectedRollbackPrecursor() (*dnsKillMatrixRollbackPrecursorEvidence, error) {
	spec, required := dnsKillMatrixRollbackPrecursorFor(runtime.config)
	runtime.precursorMu.Lock()
	defer runtime.precursorMu.Unlock()
	if !required {
		if runtime.precursorState != dnsKillMatrixPrecursorAbsent {
			return nil, errors.New(
				"DNS kill-matrix forward boundary observed unexpected rollback precursor",
			)
		}
		return nil, nil
	}
	if runtime.precursorState != dnsKillMatrixPrecursorRecorded {
		return nil, errors.New(
			"DNS kill-matrix rollback boundary reached without one exact precursor",
		)
	}
	evidence := runtime.precursorEvidence
	if evidence.Schema != dnsKillMatrixRollbackPrecursorSchema ||
		evidence.Driver != runtime.config.Driver ||
		evidence.ObservedDriver != runtime.config.Driver ||
		evidence.Point != spec.Point || evidence.Phase != spec.Phase ||
		evidence.RequestID != runtime.config.RequestID ||
		evidence.Action != dnsKillMatrixRollbackPrecursorAction {
		return nil, errors.New("DNS kill-matrix rollback precursor evidence is inconsistent")
	}
	return &evidence, nil
}

func (runtime *dnsKillMatrixRuntime) hook(
	driver string,
	point string,
	journal dnsEngineSwitchJournal,
) error {
	if runtime == nil {
		return errors.New("DNS kill-matrix runtime is nil")
	}
	precursorSpec, precursorRequired := dnsKillMatrixRollbackPrecursorFor(
		runtime.config,
	)
	if precursorRequired && point == precursorSpec.Point &&
		journal.Phase == precursorSpec.Phase {
		return runtime.injectRollbackPrecursor(
			driver, point, journal, precursorSpec,
		)
	}
	if point != runtime.config.Point || !runtime.selectsPhase(driver, journal) {
		return nil
	}
	if err := runtime.validateObservation(
		driver, point, journal, "selected boundary",
	); err != nil {
		return err
	}
	precursor, err := runtime.selectedRollbackPrecursor()
	if err != nil {
		return err
	}
	if !runtime.fired.CompareAndSwap(false, true) {
		return errors.New("DNS kill-matrix boundary fired more than once")
	}
	return runtime.stopAtBoundary(driver, point, journal, precursor)
}

func (runtime *dnsKillMatrixRuntime) stopAtBoundary(
	driver string,
	point string,
	journal dnsEngineSwitchJournal,
	precursor *dnsKillMatrixRollbackPrecursorEvidence,
) error {
	if runtime.ops.pid == nil || runtime.ops.startTicks == nil ||
		runtime.ops.writeMarker == nil || runtime.ops.notifyReady == nil ||
		runtime.ops.stopProcess == nil || runtime.ops.now == nil ||
		runtime.ops.park == nil {
		return errors.New("DNS kill-matrix runtime operations are incomplete")
	}
	pid := runtime.ops.pid()
	if pid <= 1 {
		return fmt.Errorf("DNS kill-matrix refused unsafe process ID %d", pid)
	}
	startTicks, err := runtime.ops.startTicks(pid)
	if err != nil {
		return fmt.Errorf("read DNS kill-matrix child process start ticks: %w", err)
	}
	if parsed, parseErr := strconv.ParseUint(startTicks, 10, 64); parseErr != nil || parsed == 0 {
		return errors.New("DNS kill-matrix child process start ticks are invalid")
	}
	marker := dnsKillMatrixMarker{
		Schema:            dnsKillMatrixMarkerSchema,
		CellID:            runtime.config.CellID,
		Driver:            runtime.config.Driver,
		ObservedDriver:    driver,
		Point:             runtime.config.Point,
		Phase:             runtime.config.Phase,
		RequestID:         runtime.config.RequestID,
		Nonce:             runtime.config.Nonce,
		Marker:            runtime.config.Marker,
		ReadyFD:           runtime.config.ReadyFD,
		PID:               pid,
		ProcessStartTicks: startTicks,
		RecordedAt:        runtime.ops.now().UTC().Format(time.RFC3339Nano),
		ObservedJournal:   dnsKillMatrixObservedJournalFor(point, journal),
		RollbackPrecursor: precursor,
	}
	// From the first marker write on, this call never returns into the
	// operation. The marker can be visible even when its publication reports a
	// late error, so every outcome below ends in park: the caller's error path
	// (for example a BIND pointer restore or unit rollback) and its deferred
	// cleanups must not run after the boundary the controller will record.
	reason := dnsKillMatrixResumedError
	if err := runtime.ops.writeMarker(runtime.config.Marker, marker); err != nil {
		reason = fmt.Errorf("publish DNS kill-matrix boundary marker: %w", err)
	} else if err := runtime.ops.notifyReady(runtime.config.ReadyFD, runtime.config.Nonce); err != nil {
		reason = fmt.Errorf("notify DNS kill-matrix controller: %w", err)
	} else if err := runtime.ops.stopProcess(pid); err != nil {
		reason = fmt.Errorf("stop DNS kill-matrix child process: %w", err)
	}
	runtime.ops.park(reason)
	return reason
}

func dnsKillMatrixWriteMarker(path string, marker dnsKillMatrixMarker) error {
	if err := dnsKillMatrixValidateMarkerPath(path); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode DNS kill-matrix marker: %w", err)
	}
	encoded = append(encoded, '\n')
	parent := filepath.Dir(path)
	parentStatus, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("inspect DNS kill-matrix marker parent: %w", err)
	}
	if parentStatus.Mode()&os.ModeSymlink != 0 || !parentStatus.IsDir() {
		return errors.New("DNS kill-matrix marker parent is not a real directory")
	}
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open DNS kill-matrix marker parent: %w", err)
	}
	defer directory.Close()
	openedStatus, err := directory.Stat()
	if err != nil || !os.SameFile(parentStatus, openedStatus) {
		return errors.New("DNS kill-matrix marker parent changed while opening it")
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("DNS kill-matrix marker already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect DNS kill-matrix marker target: %w", err)
	}
	stage, err := os.CreateTemp(parent, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create DNS kill-matrix marker staging file: %w", err)
	}
	stagePath := stage.Name()
	stageClosed := false
	stagePublished := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
		if !stagePublished {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("set DNS kill-matrix marker mode: %w", err)
	}
	if _, err := stage.Write(encoded); err != nil {
		return fmt.Errorf("write DNS kill-matrix marker: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync DNS kill-matrix marker: %w", err)
	}
	if err := stage.Close(); err != nil {
		stageClosed = true
		return fmt.Errorf("close DNS kill-matrix marker: %w", err)
	}
	stageClosed = true
	if err := unix.Renameat2(
		unix.AT_FDCWD, stagePath, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE,
	); err != nil {
		return fmt.Errorf("publish DNS kill-matrix marker without replacement: %w", err)
	}
	stagePublished = true
	currentParent, err := os.Lstat(parent)
	if err != nil || !os.SameFile(parentStatus, currentParent) {
		return errors.New("DNS kill-matrix marker parent changed during publication")
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync DNS kill-matrix marker parent: %w", err)
	}
	return nil
}

func dnsKillMatrixNotifyReady(fd int, nonce string) (returnErr error) {
	if err := dnsKillMatrixValidateOpenReadyFD(fd); err != nil {
		return err
	}
	defer func() {
		if err := unix.Close(fd); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("close DNS kill-matrix ready descriptor: %w", err)
		}
	}()
	payload := []byte(nonce + "\n")
	for len(payload) != 0 {
		written, err := unix.Write(fd, payload)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("write DNS kill-matrix ready descriptor: %w", err)
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}
