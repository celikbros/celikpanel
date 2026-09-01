package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	serviceMutationLedgerVersion = 1

	serviceMutationStatusRunning    = "running"
	serviceMutationStatusCancelling = "cancelling"
	serviceMutationStatusOrphaned   = "orphaned"
	serviceMutationStatusPending    = "pending"
	serviceMutationStatusSucceeded  = "succeeded"
	serviceMutationStatusFailed     = "failed"

	serviceMutationPhaseCancellingExpiredLease = "cancelling_expired_lease"
	serviceMutationErrorLeaseExpired           = "service_mutation_lease_expired"
	serviceMutationMessageLeaseExpired         = "The panel stopped heartbeating before the service mutation completed."

	serviceMutationLeaseDuration = 20 * time.Second
	serviceMutationOverallLimit  = 45 * time.Minute
	serviceMutationHistoryLimit  = 128
	serviceMutationLedgerMaxSize = 1 << 20
	serviceMutationStageLimit    = 16
)

var (
	errServiceMutationBusy                     = errors.New("another service mutation owns the host lease")
	errServiceMutationHostBusy                 = errors.New("the host package manager or mutation lock is busy")
	errServiceMutationLedgerAlreadyInitialized = errors.New("service mutation ledger is already initialized")
	errServiceMutationManagerPoisoned          = errors.New("service mutation manager is fail-closed after an ambiguous ledger write")

	globalServiceMutationMu      sync.Mutex
	globalServiceMutationManager *serviceMutationManager
	globalServiceMutationErr     error

	verifyServiceMutationSecurityPolicy = hostplatform.VerifyLiveSecurityPolicy
)

const (
	serviceMutationWriteFaultBeforeRename = "before_rename"
	serviceMutationWriteFaultAfterRename  = "after_rename_before_directory_sync"
	serviceMutationWriteFaultAfterSync    = "after_directory_sync"
)

type serviceMutationLedgerWriteState uint8

const (
	serviceMutationLedgerWriteNotPublished serviceMutationLedgerWriteState = iota
	serviceMutationLedgerWritePublished
	serviceMutationLedgerWriteAmbiguous
)

type serviceMutationLedgerWriteError struct {
	state serviceMutationLedgerWriteState
	err   error
}

func (e *serviceMutationLedgerWriteError) Error() string {
	return e.err.Error()
}

func (e *serviceMutationLedgerWriteError) Unwrap() error {
	return e.err
}

type ServiceMutationJob = transport.ServiceMutationJob

type serviceMutationLedger struct {
	Version         int                            `json:"version"`
	ActiveRequestID string                         `json:"active_request_id,omitempty"`
	Jobs            map[string]*ServiceMutationJob `json:"jobs"`
}

type ServiceMutationBeginRequest = transport.ServiceMutationBeginRequest

type ServiceMutationBinding = transport.ServiceMutationBinding

// ServiceMutationRequest is the common request envelope for privileged RPCs
// that do not otherwise need arguments.
// ServiceMutationRequest, başka argümana ihtiyaç duymayan ayrıcalıklı RPC'ler
// için ortak istek zarfıdır.
type ServiceMutationRequest = transport.ServiceMutationRequest

type ServiceMutationHeartbeatRequest = transport.ServiceMutationHeartbeatRequest

type ServiceMutationStatusRequest = transport.ServiceMutationStatusRequest

type ServiceMutationCancelRequest = transport.ServiceMutationCancelRequest

type ServiceMutationFinishRequest = transport.ServiceMutationFinishRequest

type ServiceMutationResponse = transport.ServiceMutationResponse

type serviceMutationRuntime struct {
	job                                 *ServiceMutationJob
	lock                                *serviceMutationFileLock
	ctx                                 context.Context
	cancel                              context.CancelFunc
	stepMu                              sync.Mutex
	steps                               int
	vpnPeerSyncPublishedPhase           string
	firewallApplyCommittedPhase         string
	mailTLSSyncCommittedPhase           string
	dnsClusterConfigCommittedPhase      string
	dnsZoneSyncAppliedPhase             string
	dnsZoneSyncPublishedPhase           string
	dnsEngineSwitchFinalizing           bool
	dnsZoneSyncV3AppliedPhase           string
	dnsZoneSyncV3Recovery               bool
	dnsZoneSyncV3PendingPhase           string
	panelCertificateIssuePublishedPhase string
}

type serviceMutationManager struct {
	mu sync.Mutex

	ledgerPath string
	lockPath   string
	ledger     serviceMutationLedger
	active     *serviceMutationRuntime
	poisoned   error
	poisonLock *serviceMutationFileLock
	writeFault func(string) error

	releaseTransactionPresent func() (bool, error)

	now             func() time.Time
	leaseDuration   time.Duration
	overallDuration time.Duration
}

func serviceMutationStateDirectory() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_AGENT_STATE_DIR")); value != "" {
		return value
	}
	return hostingpath.ServiceMutationStateRoot()
}

func serviceMutationLockFile() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_MUTATION_LOCK")); value != "" {
		return value
	}
	return "/run/celikpanel/service-mutation.lock"
}

func serviceMutationLedgerPublicationLockFile(hostLockPath string) string {
	return filepath.Clean(hostLockPath) + ".ledger-publication"
}

// acquireServiceMutationHostAndPublicationLocks is the only production entry
// to a service-mutation ledger publication lifetime. The fixed acquisition
// order is host -> publication. DNS terminal publication may release the host
// half first, but keeps publication until its durable v2 receipt is re-read.
func acquireServiceMutationHostAndPublicationLocks(
	hostLockPath string,
) (*serviceMutationFileLock, error) {
	hostLock, err := acquireServiceMutationFileLock(hostLockPath)
	if err != nil {
		return nil, err
	}
	publicationLock, publicationErr := acquireServiceMutationFileLock(
		serviceMutationLedgerPublicationLockFile(hostLockPath),
	)
	if publicationErr != nil {
		return nil, errors.Join(
			fmt.Errorf("acquire service mutation ledger publication lock: %w", publicationErr),
			hostLock.Close(),
		)
	}
	hostLock.publication = publicationLock
	return hostLock, nil
}

// initializeServiceMutationLedger publishes the canonical empty ledger exactly once while holding the host mutation lock.
// initializeServiceMutationLedger, ana makine mutation kilidini tutarken kanonik boş ledger'ı yalnızca bir kez yayımlar.
func initializeServiceMutationLedger(stateDir, lockPath string) (returnErr error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	stateDir = filepath.Clean(stateDir)
	lockPath = filepath.Clean(lockPath)
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(lockPath) {
		return errors.New("service mutation initialization paths must be absolute")
	}

	lock, err := acquireServiceMutationHostAndPublicationLocks(lockPath)
	if err != nil {
		return fmt.Errorf("acquire service mutation initialization lock: %w", err)
	}
	defer func() {
		if err := lock.Close(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("release service mutation initialization lock: %w", err)
		}
	}()

	if err := ensureSecureServiceMutationStateDirectory(stateDir); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
	if _, err := os.Lstat(ledgerPath); err == nil {
		return errServiceMutationLedgerAlreadyInitialized
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect initial service mutation ledger: %w", err)
	}
	raw, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		return fmt.Errorf("encode initial service mutation ledger: %w", err)
	}
	if err := cleanupAbandonedInitialServiceMutationStage(stateDir, raw); err != nil {
		return fmt.Errorf("clean abandoned initial service mutation stage: %w", err)
	}
	file, err := os.CreateTemp(stateDir, initialServiceMutationStagePattern)
	if err != nil {
		return fmt.Errorf("stage initial service mutation ledger: %w", err)
	}
	stagePath := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(stagePath)
	}()
	if err := file.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("set initial service mutation ledger owner: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure initial service mutation ledger: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		return fmt.Errorf("write initial service mutation ledger: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync initial service mutation ledger: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close initial service mutation ledger: %w", err)
	}
	// A same-directory no-replace rename publishes exactly one final link in one
	// atomic step; the platform helper also fsyncs the containing directory.
	// Aynı dizindeki üzerine-yazmayan rename, tek atomik adımda tam bir nihai bağlantı
	// yayımlar; platform yardımcısı ayrıca kapsayan dizini fsync eder.
	if err := publishInitialServiceMutationLedger(stagePath, ledgerPath); err != nil {
		if os.IsExist(err) {
			return errServiceMutationLedgerAlreadyInitialized
		}
		return fmt.Errorf("publish initial service mutation ledger: %w", err)
	}
	return nil
}

func newServiceMutationManager(stateDir, lockPath string) (*serviceMutationManager, error) {
	return newServiceMutationManagerWithWriteFault(stateDir, lockPath, nil)
}

func newServiceMutationManagerWithWriteFault(
	stateDir, lockPath string,
	writeFault func(string) error,
) (*serviceMutationManager, error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = serviceMutationStateDirectory()
	}
	if strings.TrimSpace(lockPath) == "" {
		lockPath = serviceMutationLockFile()
	}
	manager := &serviceMutationManager{
		ledgerPath:      filepath.Join(stateDir, "service-mutations.json"),
		lockPath:        lockPath,
		now:             func() time.Time { return time.Now().UTC() },
		leaseDuration:   serviceMutationLeaseDuration,
		overallDuration: serviceMutationOverallLimit,
		ledger: serviceMutationLedger{
			Version: serviceMutationLedgerVersion,
			Jobs:    map[string]*ServiceMutationJob{},
		},
		writeFault:                writeFault,
		releaseTransactionPresent: productionReleaseTransactionPresent,
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	if err := manager.reconcilePersistedActive(); err != nil {
		// Return a poisoned manager with its retained lock so the process-global
		// owner cannot lose fail-closed state through garbage collection.
		// Zehirlenmiş manager'ı tuttuğu kilitle birlikte döndür; böylece süreç-geneli
		// sahibi fail-closed durumunu çöp toplama nedeniyle kaybetmesin.
		if manager.poisoned != nil {
			return manager, err
		}
		return nil, err
	}
	return manager, nil
}

func agentServiceMutationManager() (*serviceMutationManager, error) {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	if globalServiceMutationManager == nil && globalServiceMutationErr == nil {
		manager, err := newServiceMutationManager("", "")
		if errors.Is(err, errServiceMutationHostBusy) && manager == nil {
			// Another process holding the host lock is normal, transient startup
			// contention. Do not cache it for the lifetime of this agent: the next
			// RPC can retry after that mutation releases the lock.
			// Host kilidini başka bir sürecin tutması normal, geçici bir başlangıç
			// yarışıdır. Bunu agent ömrü boyunca önbellekleme; sonraki RPC, mutation
			// kilidi bıraktıktan sonra yeniden deneyebilsin.
			return nil, err
		}
		globalServiceMutationManager, globalServiceMutationErr = manager, err
	}
	return globalServiceMutationManager, globalServiceMutationErr
}

func loadedAgentServiceMutationManager() *serviceMutationManager {
	globalServiceMutationMu.Lock()
	defer globalServiceMutationMu.Unlock()
	return globalServiceMutationManager
}

func (m *serviceMutationManager) load() error {
	ledger, err := m.loadLedgerFromDisk()
	if err != nil {
		return err
	}
	m.ledger = ledger
	return nil
}

func (m *serviceMutationManager) loadLedgerFromDisk() (serviceMutationLedger, error) {
	stateDir := filepath.Dir(m.ledgerPath)
	if !filepath.IsAbs(stateDir) {
		return serviceMutationLedger{}, errors.New("service mutation state directory must be absolute")
	}
	info, err := os.Lstat(stateDir)
	if err != nil {
		return serviceMutationLedger{}, fmt.Errorf("inspect service mutation state directory: %w", err)
	}
	if err := secureServiceMutationStateDirectoryStat(stateDir, info); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("validate service mutation state directory: %w", err)
	}
	raw, exists, err := readSecureServiceMutationLedger(
		m.ledgerPath,
		serviceMutationLedgerMaxSize,
	)
	if err != nil {
		return serviceMutationLedger{}, err
	}
	if !exists {
		return serviceMutationLedger{}, errors.New("service mutation ledger is not initialized; run --initialize-service-mutation-ledger")
	}
	ledger, err := decodeServiceMutationLedger(raw)
	if err != nil {
		return serviceMutationLedger{}, err
	}
	return ledger, nil
}

// reloadLedgerUnderHostLockLocked must be called only after this manager has
// acquired the common host flock. It closes the read-before-lock race between
// independent agent instances: every writer starts from the latest committed
// history instead of overwriting jobs published by the previous lock holder.
// reloadLedgerUnderHostLockLocked yalnızca bu manager ortak host flock'unu
// aldıktan sonra çağrılmalıdır. Bağımsız agent örnekleri arasındaki kilitten
// önce okuma yarışını kapatır: her yazıcı önceki kilit sahibinin yayımladığı
// işleri ezmek yerine en son kaydedilmiş geçmişten başlar.
func (m *serviceMutationManager) reloadLedgerUnderHostLockLocked() error {
	if m.active != nil {
		return errors.New("cannot reload service mutation ledger while this manager owns an active runtime")
	}
	ledger, err := m.loadLedgerFromDisk()
	if err != nil {
		return err
	}
	m.ledger = ledger
	return nil
}

func decodeServiceMutationLedger(raw []byte) (serviceMutationLedger, error) {
	var ledger serviceMutationLedger
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return serviceMutationLedger{}, errors.New("service mutation ledger contains more than one JSON value")
		}
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger trailer: %w", err)
	}
	if ledger.Version != serviceMutationLedgerVersion || ledger.Jobs == nil {
		return serviceMutationLedger{}, errors.New("service mutation ledger has an unsupported schema")
	}
	canonical, err := json.Marshal(&ledger)
	if err != nil {
		return serviceMutationLedger{}, fmt.Errorf("canonicalize service mutation ledger: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return serviceMutationLedger{}, errors.New("service mutation ledger is not canonical")
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return serviceMutationLedger{}, err
	}
	return ledger, nil
}

func payloadBoundDirectMutationPublishedPhase(
	job *ServiceMutationJob,
) (string, bool, error) {
	if job == nil {
		return "", false, errors.New("payload-bound mutation job is required")
	}
	var phase string
	var err error
	switch job.Kind {
	case "vpn_peer_sync":
		if job.Target != "wireguard" ||
			!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
			return "", true, errors.New("invalid VPN peer sync publication identity")
		}
		phase, err = formatVPNPeerSyncCommitPhase(
			vpnPeerSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "firewall_apply", "firewall_sync":
		if job.Target != "nftables" ||
			!mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
			return "", true, errors.New("invalid firewall publication identity")
		}
		phase, err = formatFirewallApplyCommitPhase(
			firewallApplyCommitPublished, job.RequestID, job.PackageName,
		)
	case "mail_tls_sync":
		if job.Target != "mail-tls" ||
			!mutationpayload.ValidMailTLSSyncQualifier(job.PackageName) {
			return "", true, errors.New("invalid mail TLS publication identity")
		}
		phase, err = formatMailTLSSyncCommitPhase(
			mailTLSSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_cluster_configure":
		if job.Target != "pdns" ||
			!mutationpayload.ValidDNSClusterConfigQualifier(job.PackageName) {
			return "", true, errors.New("invalid DNS cluster publication identity")
		}
		phase, err = formatDNSClusterConfigCommitPhase(
			dnsClusterConfigCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_zone_sync":
		if !serviceMutationCanonicalFQDN(job.Target) {
			return "", true, errors.New("invalid DNS zone publication identity")
		}
		switch {
		case mutationpayload.ValidDNSZoneSyncQualifier(job.PackageName):
			phase, err = formatDNSZoneSyncCommitPhase(
				dnsZoneSyncCommitPublished, job.RequestID, job.Target, job.PackageName,
			)
		case mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName):
			phase, err = formatDNSZoneSyncV3PublishedPhase(
				job.RequestID, job.Target, job.PackageName,
			)
		default:
			return "", true, errors.New("invalid DNS zone publication identity")
		}
	case "panel_certificate_issue":
		if !serviceMutationCanonicalFQDN(job.Target) ||
			!mutationpayload.ValidPanelCertificateIssueQualifier(job.PackageName) {
			return "", true, errors.New("invalid panel certificate publication identity")
		}
		phase, err = formatPanelCertificateIssueCommitPhase(
			panelCertificateIssueCommitPublished,
			job.RequestID,
			job.Target,
			job.PackageName,
		)
	default:
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return phase, true, nil
}

func validatePayloadBoundDirectMutationSuccess(job *ServiceMutationJob) error {
	if job == nil || job.Status != serviceMutationStatusSucceeded {
		return nil
	}
	expected, direct, err := payloadBoundDirectMutationPublishedPhase(job)
	if err != nil {
		return err
	}
	if direct && job.Phase != expected {
		return errors.New(
			"payload-bound direct mutation success lacks its exact canonical published receipt",
		)
	}
	return nil
}

// validateServiceMutationLedger enforces identity and bidirectional active-pointer invariants for the complete ledger.
// validateServiceMutationLedger, ledger'ın tamamı için kimlik ve çift yönlü aktif işaretçi değişmezlerini uygular.
func validateServiceMutationLedger(ledger *serviceMutationLedger) error {
	activeRequestID := ""
	for requestID, job := range ledger.Jobs {
		if job == nil || job.RequestID != requestID {
			return errors.New("service mutation ledger job identity is inconsistent")
		}
		if !validMutationIdentity(job.RequestID) || !validMutationIdentity(job.OwnerID) {
			return errors.New("service mutation ledger job identity is invalid")
		}
		if strings.TrimSpace(job.Kind) == "" ||
			strings.TrimSpace(job.Target) == "" ||
			strings.TrimSpace(job.Phase) == "" ||
			job.Attempt <= 0 {
			return errors.New("service mutation ledger job metadata is incomplete")
		}
		if err := validatePayloadBoundDirectMutationSuccess(job); err != nil {
			return fmt.Errorf("service mutation ledger job %s: %w", requestID, err)
		}
		if strings.HasPrefix(job.Phase, vpnPeerSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := parseVPNPeerSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				job.Kind != "vpn_peer_sync" || job.Target != "wireguard" {
				return errors.New("service mutation ledger has an invalid VPN peer commit receipt")
			}
			if (state == vpnPeerSyncCommitIntent &&
				job.Status != serviceMutationStatusRunning &&
				job.Status != serviceMutationStatusCancelling) ||
				(state == vpnPeerSyncCommitPublished && job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger VPN peer commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
			state, requestID, qualifier, err := parseFirewallApplyCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				(job.Kind != "firewall_apply" && job.Kind != "firewall_sync") ||
				job.Target != "nftables" {
				return errors.New("service mutation ledger has an invalid firewall commit receipt")
			}
			if (state == firewallApplyCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == firewallApplyCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger firewall commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := parseMailTLSSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "mail_tls_sync" || job.Target != "mail-tls" {
				return errors.New("service mutation ledger has an invalid mail TLS commit receipt")
			}
			if (state == mailTLSSyncCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == mailTLSSyncCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger mail TLS commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsClusterConfigCommitPhasePrefix) {
			state, requestID, qualifier, err :=
				parseDNSClusterConfigCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "dns_cluster_configure" || job.Target != "pdns" {
				return errors.New("service mutation ledger has an invalid DNS cluster commit receipt")
			}
			if (state == dnsClusterConfigCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == dnsClusterConfigCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger DNS cluster commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsZoneSyncCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parseDNSZoneSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "dns_zone_sync" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone commit receipt")
			}
			if ((state == dnsZoneSyncCommitIntent ||
				state == dnsZoneSyncCommitApplied) &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == dnsZoneSyncCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger DNS zone commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parseDNSZoneSyncV3Phase(job.Phase)
			if err != nil || requestID != job.RequestID || domain != job.Target ||
				qualifier != job.PackageName || job.Kind != "dns_zone_sync" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone V3 receipt")
			}
			validStatus := false
			switch state {
			case dnsZoneSyncV3Applied:
				validStatus = serviceMutationStatusActive(job.Status)
			case dnsZoneSyncV3PropagationPending:
				validStatus = job.Status == serviceMutationStatusPending
			case dnsZoneSyncV3Recovering:
				validStatus = serviceMutationStatusActive(job.Status)
			case dnsZoneSyncV3Published:
				validStatus = job.Status == serviceMutationStatusSucceeded
			}
			if !validStatus {
				return errors.New("service mutation ledger DNS zone V3 receipt conflicts with job status")
			}
		}
		if job.Status == serviceMutationStatusPending &&
			!strings.HasPrefix(job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
			return errors.New("pending service mutation lacks an exact DNS zone V3 receipt")
		}
		if strings.HasPrefix(job.Phase, panelCertificateIssueCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parsePanelCertificateIssueCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "panel_certificate_issue" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid panel certificate commit receipt")
			}
			if (state == panelCertificateIssueCommitIntent &&
				job.Status != serviceMutationStatusRunning &&
				job.Status != serviceMutationStatusCancelling) ||
				(state == panelCertificateIssueCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger panel certificate commit receipt conflicts with job status")
			}
		}
		hasWorkerPID := job.WorkerPID > 0
		hasWorkerStarted := strings.TrimSpace(job.WorkerStarted) != ""
		hasWorkerCommand := strings.TrimSpace(job.WorkerCommand) != ""
		if job.WorkerPID < 0 ||
			hasWorkerPID != hasWorkerStarted ||
			hasWorkerPID != hasWorkerCommand {
			return errors.New("service mutation ledger worker identity is inconsistent")
		}

		if job.StartedAt.IsZero() || job.UpdatedAt.IsZero() || job.DeadlineAt.IsZero() {
			return errors.New("service mutation ledger lifecycle timestamps are incomplete")
		}
		if job.UpdatedAt.Before(job.StartedAt) || job.DeadlineAt.Before(job.StartedAt) {
			return errors.New("service mutation ledger lifecycle timestamps are out of order")
		}
		if !job.LeaseExpiresAt.IsZero() &&
			(job.LeaseExpiresAt.Before(job.StartedAt) ||
				job.LeaseExpiresAt.After(job.DeadlineAt)) {
			return errors.New("service mutation ledger lease timestamp is out of range")
		}
		switch job.Status {
		case serviceMutationStatusRunning,
			serviceMutationStatusCancelling,
			serviceMutationStatusOrphaned:
			if job.LeaseExpiresAt.IsZero() {
				return errors.New("active service mutation ledger job has no lease timestamp")
			}
			if !job.FinishedAt.IsZero() {
				return errors.New("active service mutation ledger job has a finish timestamp")
			}
			if activeRequestID != "" {
				return errors.New("service mutation ledger contains multiple active jobs")
			}
			activeRequestID = requestID
		case serviceMutationStatusPending,
			serviceMutationStatusSucceeded, serviceMutationStatusFailed:
			if hasWorkerPID {
				return errors.New("terminal service mutation ledger job retains a worker")
			}
			if !job.LeaseExpiresAt.IsZero() {
				return errors.New("terminal service mutation ledger job retains a lease")
			}
			if job.FinishedAt.IsZero() ||
				job.FinishedAt.Before(job.StartedAt) ||
				job.UpdatedAt.After(job.FinishedAt) {
				return errors.New("terminal service mutation ledger timestamps are inconsistent")
			}
			// Terminal jobs remain as history and must not be selected by the active pointer.
			// Sonlandırılmış işler geçmiş olarak kalır ve aktif işaretçi tarafından seçilmemelidir.
		default:
			return errors.New("service mutation ledger job has an unsupported status")
		}
	}
	if ledger.ActiveRequestID != activeRequestID {
		return errors.New("service mutation ledger active pointer is inconsistent")
	}
	return nil
}

func cloneServiceMutationLedger(ledger serviceMutationLedger) serviceMutationLedger {
	copy := serviceMutationLedger{
		Version:         ledger.Version,
		ActiveRequestID: ledger.ActiveRequestID,
		Jobs:            make(map[string]*ServiceMutationJob, len(ledger.Jobs)),
	}
	for requestID, job := range ledger.Jobs {
		copy.Jobs[requestID] = cloneServiceMutationJob(job)
	}
	return copy
}

func (m *serviceMutationManager) restoreLedgerLocked(ledger serviceMutationLedger) {
	m.ledger = ledger
	if m.active != nil && m.active.job != nil {
		m.active.job = m.ledger.Jobs[m.active.job.RequestID]
	}
}

func (m *serviceMutationManager) healthErrorLocked() error {
	if m.poisoned == nil {
		return nil
	}
	return errors.Join(errServiceMutationManagerPoisoned, m.poisoned)
}

// agentMutationHold reports, as a stable code, why durable mutations are being
// refused right now — or "" when they are accepted. It is the read-only view a
// status probe needs, and it deliberately returns a code rather than the
// underlying error: the cause names files, request identities and host state,
// and none of that belongs on a wire the panel renders.
//
// A manager that could not be constructed at all and one that was poisoned mid
// flight are different diagnoses. The first means nothing was ever recorded;
// the second means something may have been published and cannot be proven, which
// is the state a half-finished handover leaves.
//
// agentMutationHold, kalıcı mutasyonların şu anda neden reddedildiğini kararlı
// bir kodla bildirir; kabul ediliyorsa "" döner. Bir durum yoklamasının ihtiyaç
// duyduğu salt-okuma görünümüdür ve bilerek altta yatan hatayı değil bir kod
// döndürür: sebep dosya adları, istek kimlikleri ve host durumu içerir; bunların
// hiçbiri panelin gösterdiği bir telde yeri olmayan şeylerdir.
//
// Hiç kurulamamış bir yönetici ile uçuş sırasında zehirlenmiş bir yönetici
// farklı teşhislerdir. Birincisi hiçbir şeyin kaydedilmediği, ikincisi bir şeyin
// yayımlanmış olabileceği ve kanıtlanamadığı anlamına gelir — yarım kalmış bir
// devralmanın bıraktığı durum budur.
func agentMutationHold() string {
	manager := loadedAgentServiceMutationManager()
	if manager == nil {
		return transport.MutationHoldLedgerUnavailable
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.healthErrorLocked() != nil {
		return transport.MutationHoldLedgerAmbiguous
	}
	return ""
}

// Once publication may have happened, memory can no longer be rolled back to
// a provably matching state. Cancel execution but deliberately retain the host
// lock and active runtime so no second mutation can begin in this process.
// Yayım gerçekleşmiş olabilirse bellek artık kanıtlanabilir biçimde eşleşen bir
// duruma geri alınamaz. Yürütmeyi iptal et; ancak bu süreçte ikinci mutation
// başlayamasın diye host kilidini ve aktif runtime'ı bilinçli olarak koru.
func (m *serviceMutationManager) poisonLocked(cause error) error {
	if m.poisoned == nil {
		m.poisoned = cause
		if m.active != nil {
			m.active.cancel()
		}
	}
	return errors.Join(errServiceMutationManagerPoisoned, cause)
}

func serviceMutationWriteMayHavePublished(err error) bool {
	var writeErr *serviceMutationLedgerWriteError
	return errors.As(err, &writeErr) &&
		writeErr.state != serviceMutationLedgerWriteNotPublished
}

func (m *serviceMutationManager) persistLedgerMutationLocked(
	before serviceMutationLedger,
) error {
	return m.persistLedgerMutationProtectedLocked(before, "")
}

func (m *serviceMutationManager) persistLedgerMutationProtectedLocked(
	before serviceMutationLedger,
	protectedRequestID string,
) error {
	err := m.writeProtectedLocked(protectedRequestID)
	if err != nil && m.poisoned == nil {
		m.restoreLedgerLocked(before)
	}
	return err
}

func (m *serviceMutationManager) handleLedgerWriteErrorLocked(err error) error {
	if serviceMutationWriteMayHavePublished(err) {
		return m.poisonLocked(err)
	}
	return err
}

func (m *serviceMutationManager) reconcilePersistedActive() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	// No startup reconciliation result is published until this process proves
	// exclusive ownership of the same host mutation lock used by workers.
	// Bu süreç, worker'ların kullandığı aynı host mutation kilidinin münhasır
	// sahipliğini kanıtlamadan hiçbir başlangıç uzlaştırma sonucunu yayımlamaz.
	lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
	if err != nil {
		return fmt.Errorf("acquire service mutation reconciliation lock: %w", err)
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		closeErr := lock.Close()
		if closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return fmt.Errorf("reload service mutation ledger under reconciliation lock: %w", err)
	}
	if err := cleanupAbandonedServiceMutationWriteStages(filepath.Dir(m.ledgerPath)); err != nil {
		closeErr := lock.Close()
		if closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	if err := cleanupAbandonedFirewallApplyJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate firewall journal stages during startup: %w",
			err,
		))
	}
	if err := cleanupAbandonedMailTLSSyncJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate mail TLS journal stages during startup: %w", err,
		))
	}
	if err := cleanupAbandonedDNSClusterConfigJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate DNS cluster journal stages during startup: %w", err,
		))
	}
	if _, _, err := readMailTLSSyncJournal(mailTLSSyncJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate mail TLS journal during startup: %w", err,
		))
	}
	if _, _, err := readFirewallApplyJournal(firewallApplyJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate firewall journal during startup: %w",
			err,
		))
	}
	if _, _, err := readDNSClusterConfigJournal(dnsClusterConfigJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate DNS cluster journal during startup: %w", err,
		))
	}
	if m.ledger.ActiveRequestID == "" {
		if handled, recoveryErr :=
			m.recoverPersistedCommittedDNSEngineSwitchLocked(lock); handled {
			return recoveryErr
		}
		return lock.Close()
	}
	job := m.ledger.Jobs[m.ledger.ActiveRequestID]
	if job == nil {
		_ = lock.Close()
		return errors.New("service mutation ledger lost its active job")
	}

	if handled, recoveryErr := m.recoverPersistedFirewallApplyLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedMailTLSSyncLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSClusterConfigLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSEngineSwitchLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSZoneSyncV3Locked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSZoneSyncLocked(job, lock); handled {
		return recoveryErr
	}
	workerAlive := serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted)
	if !workerAlive {
		handled, recoveryErr := m.recoverPersistedPanelCertificateIssueLocked(job, lock)
		if handled {
			return recoveryErr
		}
		handled, recoveryErr = m.recoverPersistedVPNPeerSyncLocked(job, lock)
		if handled {
			return recoveryErr
		}
	}

	var writeErr error
	switch {
	case workerAlive:
		before := cloneServiceMutationLedger(m.ledger)
		job.Status = serviceMutationStatusOrphaned
		job.Phase = "waiting_for_orphaned_process"
		job.ErrorCode = "agent_restart_worker_alive"
		job.ErrorMessage = "The previous privileged worker is still alive with the recorded process identity."
		job.UpdatedAt = m.now()
		writeErr = m.persistLedgerMutationLocked(before)
	default:
		busy, probeErr := packageManagerMutationBusy()
		switch {
		case probeErr != nil:
			before := cloneServiceMutationLedger(m.ledger)
			job.Status = serviceMutationStatusOrphaned
			job.Phase = "host_state_unverified"
			job.ErrorCode = "package_manager_probe_failed"
			job.ErrorMessage = "The agent could not prove that the previous host mutation stopped."
			job.UpdatedAt = m.now()
			writeErr = m.persistLedgerMutationLocked(before)
		case busy:
			before := cloneServiceMutationLedger(m.ledger)
			job.Status = serviceMutationStatusOrphaned
			job.Phase = "waiting_for_orphaned_process"
			job.ErrorCode = "agent_restart_host_busy"
			job.ErrorMessage = "The previous agent exited while a trusted host mutation may still be running."
			job.UpdatedAt = m.now()
			writeErr = m.persistLedgerMutationLocked(before)
		default:
			writeErr = m.finishPersistedOrphanLocked(
				job,
				"agent_restarted_before_completion",
				"The privileged agent restarted before the mutation reached a verified terminal state.",
			)
		}
	}
	if m.poisoned != nil {
		m.poisonLock = lock
		return writeErr
	}
	closeErr := lock.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (m *serviceMutationManager) tryResolvePersistedOrphan() error {
	m.mu.Lock()
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.active != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return errors.Join(err, lock.Close())
	}
	if m.active != nil {
		return lock.Close()
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return errors.Join(fmt.Errorf("reload service mutation ledger under orphan lock: %w", err), lock.Close())
	}
	if err := cleanupAbandonedFirewallApplyJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate firewall journal stages during orphan recovery: %w",
			err,
		))
	}
	if err := cleanupAbandonedMailTLSSyncJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate mail TLS journal stages during orphan recovery: %w", err,
		))
	}
	if err := cleanupAbandonedDNSClusterConfigJournalStages(filepath.Dir(m.ledgerPath)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate DNS cluster journal stages during orphan recovery: %w", err,
		))
	}
	if _, _, err := readMailTLSSyncJournal(mailTLSSyncJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate mail TLS journal during orphan recovery: %w", err,
		))
	}
	if _, _, err := readFirewallApplyJournal(firewallApplyJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate firewall journal during orphan recovery: %w",
			err,
		))
	}
	if _, _, err := readDNSClusterConfigJournal(dnsClusterConfigJournalPath(m)); err != nil {
		m.poisonLock = lock
		return m.poisonLocked(fmt.Errorf(
			"validate DNS cluster journal during orphan recovery: %w", err,
		))
	}
	requestID := m.ledger.ActiveRequestID
	job := m.ledger.Jobs[requestID]
	if requestID == "" || job == nil || job.Status != serviceMutationStatusOrphaned {
		return lock.Close()
	}
	if handled, recoveryErr := m.recoverPersistedFirewallApplyLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedMailTLSSyncLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSClusterConfigLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSEngineSwitchLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSZoneSyncV3Locked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedDNSZoneSyncLocked(job, lock); handled {
		return recoveryErr
	}
	if serviceMutationWorkerMatches(job.WorkerPID, job.WorkerStarted) {
		return errors.Join(errServiceMutationHostBusy, lock.Close())
	}
	if handled, recoveryErr := m.recoverPersistedPanelCertificateIssueLocked(job, lock); handled {
		return recoveryErr
	}
	if handled, recoveryErr := m.recoverPersistedVPNPeerSyncLocked(job, lock); handled {
		return recoveryErr
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return errors.Join(fmt.Errorf("verify orphaned service mutation: %w", err), lock.Close())
	}
	if busy {
		return errors.Join(errServiceMutationHostBusy, lock.Close())
	}
	writeErr := m.finishPersistedOrphanLocked(
		job,
		"agent_restarted_before_completion",
		"The previous privileged process is no longer running; the interrupted mutation may now be resumed.",
	)
	if m.poisoned != nil {
		m.poisonLock = lock
		return writeErr
	}
	closeErr := lock.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (m *serviceMutationManager) finishPersistedOrphanLocked(
	job *ServiceMutationJob,
	code, message string,
) error {
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	job.Status = serviceMutationStatusFailed
	job.Phase = "interrupted"
	job.ErrorCode = code
	job.ErrorMessage = message
	job.UpdatedAt = now
	job.FinishedAt = now
	job.LeaseExpiresAt = time.Time{}
	job.WorkerPID = 0
	job.WorkerStarted = ""
	job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	return m.persistLedgerMutationLocked(before)
}

func serviceMutationStatusActive(status string) bool {
	return status == serviceMutationStatusRunning ||
		status == serviceMutationStatusCancelling ||
		status == serviceMutationStatusOrphaned
}

func validMutationIdentity(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newMutationOwnerID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func serviceMutationIdentityMatches(job *ServiceMutationJob, request *ServiceMutationBeginRequest) bool {
	return job != nil &&
		job.RequestID == request.RequestID &&
		job.Kind == request.Kind &&
		job.Target == request.Target &&
		job.PackageName == request.PackageName
}

func serviceMutationDNSKind(kind string) bool {
	switch kind {
	case "dns_zone_sync", "dns_engine_switch", "dns_cluster_configure",
		"dnssec_secure", "pdns_configure":
		return true
	default:
		return false
	}
}

func (m *serviceMutationManager) releaseTransactionBlocksMutations() (bool, error) {
	check := m.releaseTransactionPresent
	if check == nil {
		check = productionReleaseTransactionPresent
	}
	return check()
}

func (m *serviceMutationManager) begin(request *ServiceMutationBeginRequest) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) ||
		strings.TrimSpace(request.Kind) == "" ||
		strings.TrimSpace(request.Target) == "" {
		return nil, errors.New("invalid service mutation identity")
	}
	// The existing PackageName identity slot is the durable payload qualifier
	// for a direct peer-set publication. Validate it before orphan resolution,
	// the host flock, or any ledger write so an old or confused panel fails
	// closed without occupying the machine mutation lease.
	if request.Kind == "vpn_peer_sync" &&
		(request.Target != "wireguard" ||
			!mutationpayload.ValidVPNPeerSyncQualifier(request.PackageName)) {
		return nil, errors.New("invalid VPN peer mutation payload qualifier")
	}
	if (request.Kind == "firewall_apply" || request.Kind == "firewall_sync") &&
		(request.Target != "nftables" ||
			!mutationpayload.ValidFirewallApplyQualifier(request.PackageName)) {
		return nil, errors.New("invalid firewall mutation payload qualifier")
	}
	if request.Kind == "panel_certificate_issue" &&
		(!serviceMutationCanonicalFQDN(request.Target) ||
			!mutationpayload.ValidPanelCertificateIssueQualifier(request.PackageName)) {
		return nil, errors.New("invalid panel certificate mutation payload qualifier")
	}
	if request.Kind == "mail_tls_sync" &&
		(request.Target != "mail-tls" ||
			!mutationpayload.ValidMailTLSSyncQualifier(request.PackageName)) {
		return nil, errors.New("invalid mail TLS mutation payload qualifier")
	}
	if request.Kind == "dns_zone_sync" &&
		(!serviceMutationCanonicalFQDN(request.Target) ||
			(!mutationpayload.ValidDNSZoneSyncQualifier(request.PackageName) &&
				!mutationpayload.ValidDNSZoneSyncV3Qualifier(request.PackageName))) {
		return nil, errors.New("invalid DNS zone mutation payload qualifier")
	}
	if request.Kind == "dnssec_secure" &&
		(!serviceMutationCanonicalFQDN(request.Target) || request.PackageName != "") {
		return nil, errors.New("invalid DNSSEC secure mutation identity")
	}
	if request.Kind == "dns_cluster_configure" &&
		(request.Target != "pdns" ||
			!mutationpayload.ValidDNSClusterConfigQualifier(request.PackageName)) {
		return nil, errors.New("invalid DNS cluster mutation payload qualifier")
	}
	blocked, err := m.releaseTransactionBlocksMutations()
	if err != nil {
		return nil, errors.Join(
			errServiceMutationHostBusy,
			fmt.Errorf("verify persistent release transaction gate: %w", err),
		)
	}
	if blocked {
		return nil, errServiceMutationHostBusy
	}
	if err := m.tryResolvePersistedOrphan(); err != nil &&
		!errors.Is(err, errServiceMutationHostBusy) {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	if m.active != nil {
		job := m.active.job
		if serviceMutationIdentityMatches(job, request) && job.OwnerID == request.OwnerID &&
			job.Status == serviceMutationStatusRunning {
			before := cloneServiceMutationLedger(m.ledger)
			now := m.now()
			job.UpdatedAt = now
			job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), job.DeadlineAt)
			if err := m.persistLedgerMutationLocked(before); err != nil {
				return nil, err
			}
			return cloneServiceMutationJob(job), nil
		}
		return cloneServiceMutationJob(job), errServiceMutationBusy
	}
	lock, err := acquireServiceMutationHostAndPublicationLocks(m.lockPath)
	if err != nil {
		return nil, err
	}
	closeLock := func(job *ServiceMutationJob, resultErr error) (*ServiceMutationJob, error) {
		if closeErr := lock.Close(); closeErr != nil {
			if resultErr == nil {
				resultErr = closeErr
			} else {
				resultErr = errors.Join(resultErr, closeErr)
			}
		}
		return cloneServiceMutationJob(job), resultErr
	}
	if err := m.reloadLedgerUnderHostLockLocked(); err != nil {
		return closeLock(nil, fmt.Errorf("reload service mutation ledger under begin lock: %w", err))
	}
	if m.ledger.ActiveRequestID != "" {
		return closeLock(m.ledger.Jobs[m.ledger.ActiveRequestID], errServiceMutationBusy)
	}
	previous := m.ledger.Jobs[request.RequestID]
	pendingRecovery := false
	pendingPhase := ""
	recoveringPhase := ""
	if previous != nil {
		if !serviceMutationIdentityMatches(previous, request) {
			return closeLock(previous, errors.New("request_id belongs to another service mutation"))
		}
		if !request.Resume {
			return closeLock(previous, nil)
		}
		switch previous.Status {
		case serviceMutationStatusFailed:
		case serviceMutationStatusPending:
			state, requestID, domain, qualifier, parseErr :=
				parseDNSZoneSyncV3Phase(previous.Phase)
			if parseErr != nil || state != dnsZoneSyncV3PropagationPending ||
				requestID != previous.RequestID || previous.OwnerID != request.OwnerID ||
				domain != previous.Target || qualifier != previous.PackageName ||
				previous.Kind != "dns_zone_sync" {
				return closeLock(previous, errors.New(
					"only the exact pending DNS zone V3 mutation owner may resume recovery",
				))
			}
			pendingRecovery = true
			pendingPhase = previous.Phase
			recoveringPhase, parseErr = formatDNSZoneSyncV3Phase(
				dnsZoneSyncV3Recovering,
				previous.RequestID,
				previous.Target,
				previous.PackageName,
			)
			if parseErr != nil {
				return closeLock(previous, parseErr)
			}
		default:
			return closeLock(previous, errors.New(
				"only an interrupted failed or exact pending mutation can be resumed",
			))
		}
	}
	if !pendingRecovery && serviceMutationDNSKind(request.Kind) {
		pending := false
		for _, retained := range m.ledger.Jobs {
			if retained != nil && retained.Status == serviceMutationStatusPending {
				pending = true
				break
			}
		}
		if pending {
			return closeLock(nil, errors.New(
				"an exact DNS zone V3 publication is pending recovery",
			))
		}
	}
	busy, err := packageManagerMutationBusy()
	if err != nil {
		return closeLock(nil, fmt.Errorf("verify package manager lease: %w", err))
	}
	if busy {
		return closeLock(nil, errServiceMutationHostBusy)
	}
	now := m.now()
	attempt := 1
	startedAt := now
	if previous != nil {
		attempt = previous.Attempt + 1
		if !pendingRecovery {
			startedAt = previous.StartedAt
		}
	}
	deadline := now.Add(m.overallDuration)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	phase := "leased"
	if pendingRecovery {
		phase = recoveringPhase
	}
	job := &ServiceMutationJob{
		RequestID:      request.RequestID,
		OwnerID:        request.OwnerID,
		Kind:           request.Kind,
		Target:         request.Target,
		PackageName:    request.PackageName,
		Status:         serviceMutationStatusRunning,
		Phase:          phase,
		Attempt:        attempt,
		StartedAt:      startedAt,
		UpdatedAt:      now,
		LeaseExpiresAt: minMutationTime(now.Add(m.leaseDuration), deadline),
		DeadlineAt:     deadline,
	}
	runtime := &serviceMutationRuntime{
		job:                       job,
		lock:                      lock,
		ctx:                       ctx,
		cancel:                    cancel,
		dnsZoneSyncV3Recovery:     pendingRecovery,
		dnsZoneSyncV3PendingPhase: pendingPhase,
	}
	before := cloneServiceMutationLedger(m.ledger)
	m.ledger.ActiveRequestID = job.RequestID
	m.ledger.Jobs[job.RequestID] = job
	m.active = runtime
	if err := m.persistLedgerMutationLocked(before); err != nil {
		if m.poisoned != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		m.active = nil
		cancel()
		_ = lock.Close()
		return nil, err
	}
	go m.watch(runtime)
	return cloneServiceMutationJob(job), nil
}

func minMutationTime(left, right time.Time) time.Time {
	if right.IsZero() || left.Before(right) {
		return left
	}
	return right
}

func (m *serviceMutationManager) watch(runtime *serviceMutationRuntime) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	ctxDone := runtime.ctx.Done()
	for {
		select {
		case <-ctxDone:
			if !m.expire(runtime) {
				return
			}
			// A pre-commit DNS guard can outlive the runtime context. Disable
			// the permanently-ready channel and let the periodic watchdog
			// retry expiry without spinning.
			ctxDone = nil
		case <-timer.C:
			m.mu.Lock()
			active := m.poisoned == nil && m.active == runtime &&
				runtime.job.Status == serviceMutationStatusRunning
			expired := active && !m.now().Before(runtime.job.LeaseExpiresAt)
			m.mu.Unlock()
			if expired {
				if !m.expire(runtime) {
					return
				}
			}
			if !active {
				return
			}
			timer.Reset(time.Second)
		}
	}
}

// expire returns true only when the exact guarded DNS runtime remains active
// and its watchdog must retry. Every ordinary or terminal outcome returns
// false, preserving the existing one-shot behavior for other mutations.
func (m *serviceMutationManager) expire(runtime *serviceMutationRuntime) bool {
	m.mu.Lock()
	if m.poisoned != nil || m.active != runtime ||
		runtime.job.Status != serviceMutationStatusRunning {
		m.mu.Unlock()
		return false
	}
	if protected, err := m.protectCommittedDNSEngineSwitchFinalizationLocked(runtime); err != nil {
		m.poisonLock = runtime.lock
		_ = m.poisonLocked(fmt.Errorf(
			"protect committed DNS engine switch from lease expiry: %w", err,
		))
		m.mu.Unlock()
		return false
	} else if protected {
		m.mu.Unlock()
		return true
	}
	if runtime.vpnPeerSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.vpnPeerSyncPublishedPhase, "", "",
		)
		if err != nil && m.poisoned == nil {
			_ = m.poisonLocked(err)
		}
		m.mu.Unlock()
		return false
	}
	if runtime.panelCertificateIssuePublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.panelCertificateIssuePublishedPhase, "", "",
		)
		if err != nil && m.poisoned == nil {
			_ = m.poisonLocked(err)
		}
		m.mu.Unlock()
		return false
	}
	if runtime.firewallApplyCommittedPhase != "" {
		m.mu.Unlock()
		return false
	}
	if runtime.mailTLSSyncCommittedPhase != "" {
		m.mu.Unlock()
		return false
	}
	if runtime.dnsClusterConfigCommittedPhase != "" {
		m.mu.Unlock()
		return false
	}
	if runtime.dnsZoneSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.dnsZoneSyncPublishedPhase, "", "",
		)
		if err != nil && m.poisoned == nil {
			_ = m.poisonLocked(err)
		}
		m.mu.Unlock()
		return false
	}
	if runtime.dnsZoneSyncAppliedPhase != "" {
		m.mu.Unlock()
		return false
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	runtime.job.Status = serviceMutationStatusCancelling
	if !strings.HasPrefix(runtime.job.Phase, panelCertificateIssueCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
		runtime.job.Phase = serviceMutationPhaseCancellingExpiredLease
	}
	runtime.job.ErrorCode = serviceMutationErrorLeaseExpired
	runtime.job.ErrorMessage = serviceMutationMessageLeaseExpired
	runtime.job.UpdatedAt = now
	if err := m.persistLedgerMutationLocked(before); err != nil {
		if m.poisoned == nil {
			_ = m.poisonLocked(err)
		}
		m.mu.Unlock()
		return false
	}
	runtime.cancel()
	steps := runtime.steps
	m.mu.Unlock()
	if steps == 0 {
		m.finishExpired(runtime)
	}
	return false
}

func (m *serviceMutationManager) finishExpired(runtime *serviceMutationRuntime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.poisoned != nil || m.active != runtime || runtime.steps != 0 ||
		runtime.job.Status != serviceMutationStatusCancelling {
		return
	}
	if err := m.finishRuntimeAfterFailureLocked(
		runtime,
		runtime.job.ErrorCode,
		runtime.job.ErrorMessage,
	); err != nil && m.poisoned == nil {
		_ = m.poisonLocked(err)
	}
}

func (m *serviceMutationManager) heartbeat(
	request *ServiceMutationHeartbeatRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) {
		return nil, errors.New("invalid service mutation heartbeat")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != request.RequestID ||
		runtime.job.OwnerID != request.OwnerID ||
		runtime.job.Status != serviceMutationStatusRunning {
		return m.jobLocked(request.RequestID), errors.New("service mutation lease is not owned by this panel")
	}
	if protected, err := m.protectCommittedDNSEngineSwitchFinalizationLocked(runtime); err != nil {
		m.poisonLock = runtime.lock
		return cloneServiceMutationJob(runtime.job), m.poisonLocked(fmt.Errorf(
			"protect committed DNS engine switch from heartbeat mutation: %w", err,
		))
	} else if protected {
		// A protected heartbeat still renews the lease. The heartbeat exists to
		// prove the panel is alive, and the finalizing interval does not change
		// that; what it must not do is touch the phase or race the journal, and
		// it does neither here. Returning without renewal let any package
		// install longer than the 20-second lease expire mid-switch, after
		// which the worker-clear write advanced UpdatedAt past LeaseExpiresAt
		// and the next strict proof poisoned the manager (risk R-017).
		// Korumalı kalp atışı da kiralamayı yeniler. Kalp atışı panelin canlı
		// olduğunu kanıtlamak için vardır ve sonlanma aralığı bunu değiştirmez;
		// yapmaması gereken, faza dokunmak ya da günlükle yarışmaktır — burada
		// ikisini de yapmaz. Yenilemeden dönmek, 20 saniyelik kiralamadan uzun
		// süren her paket kurulumunun geçişin ortasında kiralamayı düşürmesine
		// izin veriyordu; ardından işçi temizliği UpdatedAt değerini
		// LeaseExpiresAt ötesine taşıyor ve bir sonraki katı kanıt yöneticiyi
		// zehirliyordu (risk R-017).
		before := cloneServiceMutationLedger(m.ledger)
		now := m.now()
		if !now.Before(runtime.job.DeadlineAt) {
			return cloneServiceMutationJob(runtime.job), errors.New("service mutation deadline has expired")
		}
		runtime.job.UpdatedAt = now
		runtime.job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), runtime.job.DeadlineAt)
		if err := m.persistLedgerMutationLocked(before); err != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		return cloneServiceMutationJob(runtime.job), nil
	}
	if runtime.vpnPeerSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.vpnPeerSyncPublishedPhase, "", "",
		)
		return m.jobLocked(request.RequestID), err
	}
	if runtime.panelCertificateIssuePublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.panelCertificateIssuePublishedPhase, "", "",
		)
		return m.jobLocked(request.RequestID), err
	}
	if runtime.firewallApplyCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), nil
	}
	if runtime.mailTLSSyncCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), nil
	}
	if runtime.dnsClusterConfigCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), nil
	}
	if runtime.dnsZoneSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.dnsZoneSyncPublishedPhase, "", "",
		)
		return m.jobLocked(request.RequestID), err
	}
	if runtime.dnsZoneSyncAppliedPhase != "" {
		return cloneServiceMutationJob(runtime.job), nil
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	if !now.Before(runtime.job.DeadlineAt) {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation deadline has expired")
	}
	if phase := strings.TrimSpace(request.Phase); phase != "" &&
		runtime.job.Kind != "dns_engine_switch" &&
		!strings.HasPrefix(runtime.job.Phase, vpnPeerSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, firewallApplyCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, mailTLSSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsClusterConfigCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsZoneSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsZoneSyncV3CommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, panelCertificateIssueCommitPhasePrefix) {
		runtime.job.Phase = phase
	}
	runtime.job.UpdatedAt = now
	runtime.job.LeaseExpiresAt = minMutationTime(now.Add(m.leaseDuration), runtime.job.DeadlineAt)
	if err := m.persistLedgerMutationLocked(before); err != nil {
		return cloneServiceMutationJob(runtime.job), err
	}
	return cloneServiceMutationJob(runtime.job), nil
}

func (m *serviceMutationManager) status(requestID string) *ServiceMutationJob {
	_ = m.tryResolvePersistedOrphan()
	m.mu.Lock()
	defer m.mu.Unlock()
	if requestID == "" {
		requestID = m.ledger.ActiveRequestID
	}
	return m.jobLocked(requestID)
}

func (m *serviceMutationManager) jobLocked(requestID string) *ServiceMutationJob {
	return cloneServiceMutationJob(m.ledger.Jobs[requestID])
}

func cloneServiceMutationJob(job *ServiceMutationJob) *ServiceMutationJob {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func (m *serviceMutationManager) cancelJob(
	request *ServiceMutationCancelRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.ExpectedOwner) {
		return nil, errors.New("invalid service mutation cancellation")
	}
	m.mu.Lock()
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != request.RequestID ||
		runtime.job.OwnerID != request.ExpectedOwner {
		job := m.jobLocked(request.RequestID)
		m.mu.Unlock()
		if job != nil && job.OwnerID == request.ExpectedOwner &&
			!serviceMutationStatusActive(job.Status) {
			return job, nil
		}
		return job, errors.New("active service mutation identity changed")
	}
	if protected, err := m.protectCommittedDNSEngineSwitchFinalizationLocked(runtime); err != nil {
		m.poisonLock = runtime.lock
		job := cloneServiceMutationJob(runtime.job)
		poisonErr := m.poisonLocked(fmt.Errorf(
			"protect committed DNS engine switch from cancellation: %w", err,
		))
		m.mu.Unlock()
		return job, poisonErr
	} else if protected {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	if runtime.vpnPeerSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.vpnPeerSyncPublishedPhase, "", "",
		)
		job := m.jobLocked(request.RequestID)
		m.mu.Unlock()
		return job, err
	}
	if runtime.panelCertificateIssuePublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.panelCertificateIssuePublishedPhase, "", "",
		)
		job := m.jobLocked(request.RequestID)
		m.mu.Unlock()
		return job, err
	}
	if runtime.firewallApplyCommittedPhase != "" {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	if runtime.mailTLSSyncCommittedPhase != "" {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	if runtime.dnsClusterConfigCommittedPhase != "" {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	if runtime.dnsZoneSyncPublishedPhase != "" {
		err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.dnsZoneSyncPublishedPhase, "", "",
		)
		job := m.jobLocked(request.RequestID)
		m.mu.Unlock()
		return job, err
	}
	if runtime.dnsZoneSyncAppliedPhase != "" {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	if runtime.job.Status != serviceMutationStatusRunning {
		job := cloneServiceMutationJob(runtime.job)
		m.mu.Unlock()
		return job, nil
	}
	code := strings.TrimSpace(request.FailureCode)
	if code == "" {
		code = "panel_restarted_during_mutation"
	}
	message := strings.TrimSpace(request.FailureMessage)
	if message == "" {
		message = "The panel restarted while the agent still owned the service mutation."
	}
	before := cloneServiceMutationLedger(m.ledger)
	runtime.job.Status = serviceMutationStatusCancelling
	if !strings.HasPrefix(runtime.job.Phase, vpnPeerSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, firewallApplyCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, mailTLSSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsClusterConfigCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsZoneSyncCommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, dnsZoneSyncV3CommitPhasePrefix) &&
		!strings.HasPrefix(runtime.job.Phase, panelCertificateIssueCommitPhasePrefix) {
		runtime.job.Phase = "cancelling"
		if reason := strings.TrimSpace(request.Reason); reason != "" {
			runtime.job.Phase = reason
		}
	}
	runtime.job.ErrorCode = code
	runtime.job.ErrorMessage = message
	runtime.job.UpdatedAt = m.now()
	err := m.persistLedgerMutationLocked(before)
	steps := runtime.steps
	job := cloneServiceMutationJob(runtime.job)
	if err == nil {
		runtime.cancel()
	}
	m.mu.Unlock()
	if err != nil {
		return job, err
	}
	if steps == 0 {
		m.finishExpired(runtime)
		job = m.status(request.RequestID)
	}
	return job, nil
}

func (m *serviceMutationManager) finish(
	request *ServiceMutationFinishRequest,
) (*ServiceMutationJob, error) {
	if request == nil || !validMutationIdentity(request.RequestID) ||
		!validMutationIdentity(request.OwnerID) {
		return nil, errors.New("invalid service mutation completion")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.healthErrorLocked(); err != nil {
		return nil, err
	}
	runtime := m.active
	if runtime == nil {
		job := m.ledger.Jobs[request.RequestID]
		if job != nil && !serviceMutationStatusActive(job.Status) {
			return cloneServiceMutationJob(job), nil
		}
		return cloneServiceMutationJob(job), errors.New("service mutation is not active")
	}
	if runtime.job.RequestID != request.RequestID || runtime.job.OwnerID != request.OwnerID {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation lease is owned by another request")
	}
	if protected, err := m.protectCommittedDNSEngineSwitchFinalizationLocked(runtime); err != nil {
		m.poisonLock = runtime.lock
		return cloneServiceMutationJob(runtime.job), m.poisonLocked(fmt.Errorf(
			"protect committed DNS engine switch from premature completion: %w", err,
		))
	} else if protected {
		return cloneServiceMutationJob(runtime.job), errors.New(
			"committed DNS engine switch is still finalizing",
		)
	}
	if runtime.vpnPeerSyncPublishedPhase != "" {
		if err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.vpnPeerSyncPublishedPhase, "", "",
		); err != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		return m.jobLocked(request.RequestID), nil
	}
	if runtime.panelCertificateIssuePublishedPhase != "" {
		if err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.panelCertificateIssuePublishedPhase, "", "",
		); err != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		return m.jobLocked(request.RequestID), nil
	}
	if runtime.firewallApplyCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), errors.New(
			"committed firewall mutation is still converging",
		)
	}
	if runtime.mailTLSSyncCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), errors.New(
			"committed mail TLS mutation is still converging",
		)
	}
	if runtime.dnsClusterConfigCommittedPhase != "" {
		return cloneServiceMutationJob(runtime.job), errors.New(
			"committed DNS cluster mutation is still converging",
		)
	}
	if runtime.dnsZoneSyncPublishedPhase != "" {
		if err := m.finishRuntimeTerminalLocked(
			runtime, true, runtime.dnsZoneSyncPublishedPhase, "", "",
		); err != nil {
			return cloneServiceMutationJob(runtime.job), err
		}
		return m.jobLocked(request.RequestID), nil
	}
	if runtime.dnsZoneSyncAppliedPhase != "" {
		return cloneServiceMutationJob(runtime.job), errors.New(
			"applied DNS zone mutation is still finalizing",
		)
	}
	if runtime.job.Status != serviceMutationStatusRunning {
		return cloneServiceMutationJob(runtime.job), errors.New("only a running service mutation may be finished")
	}
	if runtime.steps != 0 {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation still has an active privileged step")
	}
	if runtime.job.WorkerPID != 0 {
		return cloneServiceMutationJob(runtime.job), errors.New("service mutation still has a recorded privileged worker")
	}
	if request.Success {
		if _, direct, err := payloadBoundDirectMutationPublishedPhase(runtime.job); err != nil {
			return cloneServiceMutationJob(runtime.job), err
		} else if direct {
			return cloneServiceMutationJob(runtime.job), errors.New(
				"payload-bound direct mutation cannot succeed without its exact canonical published receipt",
			)
		}
	}
	var finishErr error
	if request.Success {
		finishErr = m.finishRuntimeLocked(
			runtime, true, request.FailureCode, request.Message,
		)
	} else {
		finishErr = m.finishRuntimeAfterFailureLocked(
			runtime, request.FailureCode, request.Message,
		)
	}
	if finishErr != nil {
		return cloneServiceMutationJob(runtime.job), finishErr
	}
	return cloneServiceMutationJob(runtime.job), nil
}

func (m *serviceMutationManager) finishRuntimeLocked(
	runtime *serviceMutationRuntime,
	success bool,
	code, message string,
) error {
	phase := "completed"
	if !success {
		phase = "failed"
	}
	return m.finishRuntimeTerminalLocked(runtime, success, phase, code, message)
}

func (m *serviceMutationManager) finishRuntimeAfterFailureLocked(
	runtime *serviceMutationRuntime,
	code, message string,
) error {
	if runtime != nil && (runtime.dnsZoneSyncV3Recovery ||
		runtime.dnsZoneSyncV3AppliedPhase != "") {
		phase := runtime.dnsZoneSyncV3PendingPhase
		if phase == "" && runtime.job != nil {
			var err error
			phase, err = formatDNSZoneSyncV3Phase(
				dnsZoneSyncV3PropagationPending,
				runtime.job.RequestID,
				runtime.job.Target,
				runtime.job.PackageName,
			)
			if err != nil {
				return err
			}
		}
		return m.finishRuntimeDNSZoneV3PendingLocked(
			runtime, phase,
		)
	}
	return m.finishRuntimeLocked(runtime, false, code, message)
}

func (m *serviceMutationManager) finishRuntimeDNSZoneV3PendingLocked(
	runtime *serviceMutationRuntime,
	phase string,
) error {
	if runtime == nil || runtime.job == nil {
		return errors.New("DNS zone V3 pending runtime is required")
	}
	if m.active != runtime {
		if runtime.job.Status == serviceMutationStatusPending &&
			runtime.job.Phase == phase {
			return nil
		}
		return errors.New("service mutation runtime changed")
	}
	state, requestID, domain, qualifier, err := parseDNSZoneSyncV3Phase(phase)
	if err != nil || state != dnsZoneSyncV3PropagationPending ||
		requestID != runtime.job.RequestID || domain != runtime.job.Target ||
		qualifier != runtime.job.PackageName ||
		runtime.job.Kind != "dns_zone_sync" ||
		(runtime.job.Status != serviceMutationStatusRunning &&
			runtime.job.Status != serviceMutationStatusCancelling) ||
		runtime.steps > 1 || runtime.job.WorkerPID != 0 {
		return errors.New("DNS zone V3 pending receipt lost its exact runtime identity")
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	runtime.job.Status = serviceMutationStatusPending
	runtime.job.Phase = phase
	runtime.job.ErrorCode = "dns_zone_v3_propagation_pending"
	runtime.job.ErrorMessage =
		"The exact local DNS publication is waiting for paired propagation recovery."
	runtime.job.UpdatedAt = now
	runtime.job.FinishedAt = now
	runtime.job.LeaseExpiresAt = time.Time{}
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	runtime.dnsZoneSyncV3PendingPhase = phase
	m.ledger.ActiveRequestID = ""
	if err := m.persistLedgerMutationProtectedLocked(
		before, runtime.job.RequestID,
	); err != nil {
		return err
	}
	runtime.cancel()
	lockErr := runtime.lock.Close()
	m.active = nil
	m.trimHistoryLocked(runtime.job.RequestID)
	if lockErr != nil {
		return fmt.Errorf("release service mutation host lock: %w", lockErr)
	}
	return nil
}

func (m *serviceMutationManager) finishRuntimeTerminalLocked(
	runtime *serviceMutationRuntime,
	success bool,
	phase, code, message string,
) error {
	if m.active != runtime {
		return errors.New("service mutation runtime changed")
	}
	if strings.TrimSpace(phase) == "" {
		return errors.New("service mutation terminal phase is required")
	}
	before := cloneServiceMutationLedger(m.ledger)
	now := m.now()
	if success {
		runtime.job.Status = serviceMutationStatusSucceeded
		runtime.job.Phase = phase
		runtime.job.ErrorCode = ""
		runtime.job.ErrorMessage = ""
	} else {
		runtime.job.Status = serviceMutationStatusFailed
		runtime.job.Phase = phase
		if strings.TrimSpace(code) == "" {
			code = "service_mutation_failed"
		}
		if strings.TrimSpace(message) == "" {
			message = "The service mutation did not complete."
		}
		runtime.job.ErrorCode = code
		runtime.job.ErrorMessage = message
	}
	runtime.job.UpdatedAt = now
	runtime.job.FinishedAt = now
	runtime.job.LeaseExpiresAt = time.Time{}
	runtime.job.WorkerPID = 0
	runtime.job.WorkerStarted = ""
	runtime.job.WorkerCommand = ""
	m.ledger.ActiveRequestID = ""
	if err := m.persistLedgerMutationProtectedLocked(
		before, runtime.job.RequestID,
	); err != nil {
		return err
	}
	runtime.cancel()
	lockErr := runtime.lock.Close()
	m.active = nil
	m.trimHistoryLocked(runtime.job.RequestID)
	if lockErr != nil {
		return fmt.Errorf("release service mutation host lock: %w", lockErr)
	}
	return nil
}

func (m *serviceMutationManager) acquireStep(
	binding ServiceMutationBinding,
	claim serviceMutationStepClaim,
) (context.Context, func(), error) {
	if !validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return nil, nil, errors.New("a valid durable service mutation lease is required")
	}
	m.mu.Lock()
	if err := m.healthErrorLocked(); err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}
	runtime := m.active
	if runtime == nil || runtime.job.RequestID != binding.MutationRequestID ||
		runtime.job.OwnerID != binding.MutationOwnerID ||
		runtime.job.Status != serviceMutationStatusRunning {
		m.mu.Unlock()
		return nil, nil, errors.New("service mutation step does not own the active lease")
	}
	if err := authorizeServiceMutationStep(runtime.job, claim); err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}
	m.mu.Unlock()

	runtime.stepMu.Lock()
	if err := serviceMutationSecurityPolicyPreflight(); err != nil {
		runtime.stepMu.Unlock()
		return nil, nil, err
	}
	m.mu.Lock()
	if m.poisoned != nil || m.active != runtime ||
		runtime.job.Status != serviceMutationStatusRunning {
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		return nil, nil, errors.New("service mutation lease expired before the step started")
	}
	if err := authorizeServiceMutationStep(runtime.job, claim); err != nil {
		m.mu.Unlock()
		runtime.stepMu.Unlock()
		return nil, nil, err
	}
	runtime.steps++
	ctx := context.WithValue(
		runtime.ctx,
		serviceMutationExecutionTrackerKey{},
		&serviceMutationExecutionTracker{manager: m, runtime: runtime},
	)
	m.mu.Unlock()

	var once sync.Once
	done := func() {
		once.Do(func() {
			m.mu.Lock()
			runtime.steps--
			shouldFinish := m.poisoned == nil && m.active == runtime && runtime.steps == 0 &&
				runtime.job.Status == serviceMutationStatusCancelling
			m.mu.Unlock()
			runtime.stepMu.Unlock()
			if shouldFinish {
				m.finishExpired(runtime)
			}
		})
	}
	return ctx, done, nil
}

func (m *serviceMutationManager) trimHistoryLocked(
	protectedRequestIDs ...string,
) {
	if len(m.ledger.Jobs) <= serviceMutationHistoryLimit {
		return
	}
	protectedRequestID := ""
	if len(protectedRequestIDs) > 0 {
		protectedRequestID = protectedRequestIDs[0]
	}
	type finishedJob struct {
		id   string
		when time.Time
	}
	var terminal []finishedJob
	for id, job := range m.ledger.Jobs {
		if id == m.ledger.ActiveRequestID ||
			id == protectedRequestID ||
			serviceMutationStatusActive(job.Status) ||
			job.Status == serviceMutationStatusPending {
			continue
		}
		terminal = append(terminal, finishedJob{id: id, when: job.FinishedAt})
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].when.Before(terminal[j].when) })
	for len(m.ledger.Jobs) > serviceMutationHistoryLimit && len(terminal) > 0 {
		delete(m.ledger.Jobs, terminal[0].id)
		terminal = terminal[1:]
	}
}

func (m *serviceMutationManager) writeLocked() error {
	return m.writeProtectedLocked("")
}

func (m *serviceMutationManager) writeProtectedLocked(
	protectedRequestID string,
) error {
	if err := m.healthErrorLocked(); err != nil {
		return err
	}
	m.trimHistoryLocked(protectedRequestID)
	if err := validateServiceMutationLedger(&m.ledger); err != nil {
		return fmt.Errorf("validate service mutation ledger before write: %w", err)
	}
	if err := ensureSecureServiceMutationStateDirectory(filepath.Dir(m.ledgerPath)); err != nil {
		return fmt.Errorf("secure service mutation state directory: %w", err)
	}
	if err := cleanupAbandonedServiceMutationWriteStages(filepath.Dir(m.ledgerPath)); err != nil {
		return err
	}
	raw, err := json.Marshal(&m.ledger)
	if err != nil {
		return fmt.Errorf("encode service mutation ledger: %w", err)
	}
	dir := filepath.Dir(m.ledgerPath)
	stage, err := os.CreateTemp(dir, ".service-mutations-*.json")
	if err != nil {
		return fmt.Errorf("stage service mutation ledger: %w", err)
	}
	stagePath := stage.Name()
	ok := false
	defer func() {
		_ = stage.Close()
		if !ok {
			_ = os.Remove(stagePath)
		}
	}()
	if err := stage.Chown(int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		return fmt.Errorf("set service mutation ledger owner: %w", err)
	}
	if err := stage.Chmod(0o600); err != nil {
		return err
	}
	if _, err := stage.Write(raw); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultBeforeRename); err != nil {
			return &serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWriteNotPublished,
				err:   fmt.Errorf("injected failure before service mutation ledger rename: %w", err),
			}
		}
	}
	if err := os.Rename(stagePath, m.ledgerPath); err != nil {
		return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
			state: serviceMutationLedgerWriteAmbiguous,
			err:   fmt.Errorf("publish service mutation ledger: %w", err),
		})
	}
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultAfterRename); err != nil {
			return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWritePublished,
				err:   fmt.Errorf("injected failure after service mutation ledger rename: %w", err),
			})
		}
	}
	if err := syncServiceMutationDirectory(m.ledgerPath); err != nil {
		return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
			state: serviceMutationLedgerWritePublished,
			err:   fmt.Errorf("sync service mutation ledger directory: %w", err),
		})
	}
	if m.writeFault != nil {
		if err := m.writeFault(serviceMutationWriteFaultAfterSync); err != nil {
			return m.handleLedgerWriteErrorLocked(&serviceMutationLedgerWriteError{
				state: serviceMutationLedgerWritePublished,
				err:   fmt.Errorf("injected failure after service mutation ledger directory sync: %w", err),
			})
		}
	}
	ok = true
	return nil
}

// cleanupAbandonedServiceMutationWriteStages removes only complete canonical
// writer stages with strict ledger metadata. The caller must hold the common
// host mutation flock, and any ambiguous artifact fails closed.
// cleanupAbandonedServiceMutationWriteStages yalnızca katı ledger meta verisine
// sahip eksiksiz kanonik yazıcı stage'lerini kaldırır. Çağıran ortak host
// mutation flock kilidini tutmalıdır; belirsiz her kalıntı fail-closed sonuçlanır.
func cleanupAbandonedServiceMutationWriteStages(stateDir string) error {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return fmt.Errorf("inspect abandoned service mutation write stages: %w", err)
	}
	stagePaths := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if isInitialServiceMutationStageName(name) {
			return errors.New("initialized service mutation state retains an initializer stage")
		}
		if strings.HasPrefix(name, ".service-mutations-") &&
			strings.HasSuffix(name, ".json") &&
			!strings.HasPrefix(name, initialServiceMutationStagePrefix) {
			stagePaths = append(stagePaths, filepath.Join(stateDir, name))
		}
	}
	if len(stagePaths) > serviceMutationStageLimit {
		return fmt.Errorf(
			"abandoned service mutation write stage count %d exceeds limit %d",
			len(stagePaths),
			serviceMutationStageLimit,
		)
	}
	sort.Strings(stagePaths)
	for _, stagePath := range stagePaths {
		raw, exists, err := readSecureServiceMutationLedger(
			stagePath,
			serviceMutationLedgerMaxSize,
		)
		if err != nil {
			return fmt.Errorf("validate abandoned service mutation write stage: %w", err)
		}
		if !exists {
			return errors.New("abandoned service mutation write stage disappeared during validation")
		}
		if _, err := decodeServiceMutationLedger(raw); err != nil {
			return fmt.Errorf("validate abandoned service mutation write stage content: %w", err)
		}
	}
	for _, stagePath := range stagePaths {
		if err := os.Remove(stagePath); err != nil {
			return fmt.Errorf("remove abandoned service mutation write stage: %w", err)
		}
	}
	if len(stagePaths) != 0 {
		if err := syncServiceMutationDirectory(
			filepath.Join(stateDir, serviceMutationLedgerFileName),
		); err != nil {
			return fmt.Errorf("sync abandoned service mutation write stage cleanup: %w", err)
		}
	}
	return nil
}

const hostMutationBusyMessage = "another server change or package-manager task is still running"

func setHostMutationBusyResponse(response *ServiceMutationResponse, err error) bool {
	if !errors.Is(err, errServiceMutationBusy) &&
		!errors.Is(err, errServiceMutationHostBusy) {
		return false
	}
	response.ErrorCode = transport.HostMutationBusy
	response.Error = hostMutationBusyMessage
	return true
}

func (a *Agent) BeginServiceMutation(
	request *ServiceMutationBeginRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		if setHostMutationBusyResponse(response, managerErr) {
			return nil
		}
		return managerErr
	}
	job, err := manager.begin(request)
	response.Job = job
	if err != nil {
		if !setHostMutationBusyResponse(response, err) {
			response.Error = err.Error()
		}
	}
	return nil
}

func (a *Agent) HeartbeatServiceMutation(
	request *ServiceMutationHeartbeatRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.heartbeat(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) ServiceMutationStatus(
	request *ServiceMutationStatusRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		if setHostMutationBusyResponse(response, managerErr) {
			return nil
		}
		return managerErr
	}
	response.Job = manager.status(strings.TrimSpace(request.RequestID))
	return nil
}

func (a *Agent) CancelServiceMutation(
	request *ServiceMutationCancelRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.cancelJob(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func (a *Agent) FinishServiceMutation(
	request *ServiceMutationFinishRequest,
	response *ServiceMutationResponse,
) error {
	manager, managerErr := agentServiceMutationManager()
	if managerErr != nil {
		return managerErr
	}
	job, err := manager.finish(request)
	response.Job = job
	if err != nil {
		response.Error = err.Error()
	}
	return nil
}

func serviceMutationSecurityPolicyPreflight() error {
	if err := verifyServiceMutationSecurityPolicy(); err != nil {
		return fmt.Errorf("service mutation security-policy preflight: %w", err)
	}
	return nil
}

func (a *Agent) requiredServiceMutationStep(
	binding ServiceMutationBinding,
	claim serviceMutationStepClaim,
) (context.Context, func(), error) {
	if !validMutationIdentity(binding.MutationRequestID) ||
		!validMutationIdentity(binding.MutationOwnerID) {
		return nil, nil, errors.New("a valid durable service mutation lease is required")
	}
	manager := loadedAgentServiceMutationManager()
	if manager == nil {
		return nil, nil, errors.New("service mutation manager is unavailable")
	}
	return manager.acquireStep(binding, claim)
}
