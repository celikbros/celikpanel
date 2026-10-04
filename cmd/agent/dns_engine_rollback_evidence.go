package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const dnsEngineRollbackEvidenceLimit = 15 * time.Second

var (
	readRollbackEvidenceJournal          = readDNSEngineSwitchJournal
	readRollbackEvidenceState            = readDNSEngineState
	readRollbackEvidenceOwnership        = readDNSEngineOwnership
	readRollbackEvidenceInstallOwnership = readDNSEngineInstallOwnership
	readRollbackEvidenceTargetHost       = verifiedDNSEngineRollbackTargetHost
	verifyRollbackEvidenceTargetSeal     = verifyDNSEngineRollbackTargetSeal
)

type dnsEngineRollbackTargetHost struct {
	PackageManager hostplatform.PackageManager
	Packages       []string
	Systemctl      string
	// RefuseRestoredTakeover is set for a paired-secondary first install:
	// no takeover shares its manifest, so a running unmanaged BIND is never
	// its rolled-back state.
	RefuseRestoredTakeover bool
}

func canonicalDNSEngineRollbackEvidence(
	request *transport.DNSEngineRollbackEvidenceRequest,
) (mutationpayload.DNSEngineSwitchManifestCommitment, error) {
	if request == nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine rollback evidence request is required")
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		request.Mode,
		request.SourceEngine,
		request.TargetEngine,
		request.SourceEpoch,
		request.TargetEpoch,
		request.SourceRevision,
		request.Topology,
		request.PairRole,
		request.LocalIP,
		request.LocalNS,
		request.PeerIP,
		request.PeerNS,
		request.Zones,
	)
	if err != nil {
		return mutationpayload.DNSEngineSwitchManifestCommitment{}, err
	}
	if request.ManifestQualifier != manifest.Qualifier ||
		request.SnapshotBytes != manifest.SnapshotBytes ||
		!equalDNSEngineSwitchWireZones(request.Zones, manifest.Zones) {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine rollback evidence manifest is not canonical")
	}
	if !initialDNSEngineInstallRollbackEvidenceScope(manifest) {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine rollback evidence is outside the supported initial DNS engine install scope")
	}
	return manifest, nil
}

func initialBINDInstallRollbackEvidenceScope(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	frozenTopology := manifest.Topology == transport.DNSTopologyStandalone &&
		manifest.PairRole == "" && manifest.LocalIP == "" &&
		manifest.LocalNS == "" && manifest.PeerIP == "" &&
		manifest.PeerNS == ""
	if manifest.Topology == transport.DNSTopologyPaired {
		frozenTopology = manifest.PairRole == transport.DNSPairRolePrimary &&
			manifest.LocalIP != "" && manifest.LocalNS != "" &&
			manifest.PeerIP != "" && manifest.PeerNS != ""
	}
	return manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.SourceEngine == "" &&
		manifest.SourceEpoch == 0 &&
		manifest.TargetEngine == transport.DNSEngineBIND &&
		manifest.TargetEpoch == 1 &&
		frozenTopology
}

// initialDNSEngineInstallRollbackEvidenceScope widens the initial BIND scope
// (Decision B, 2026-09-30) to every first install the setup wizard and the
// engine card can start whose rollback ends with the target stopped as
// rollback standby: BIND as paired secondary, and PowerDNS standalone or as
// paired secondary. The fresh paired PowerDNS primary (V3) keeps its own
// recovery and is inside only while its product gate is open
// (freshPairedPDNSPrimaryRollbackEvidenceScope); with the gate closed, as
// shipped, it stays outside.
//
// İlk BIND kapsamını, geri alınması hedefi durmuş yedek olarak bırakan her ilk
// kuruluma genişletir: eşli ikincil BIND, tek başına ya da eşli ikincil
// PowerDNS. Eşli PowerDNS birincili (V3) yalnız ürün kapısı açıkken kapsama
// girer; kapı kapalıyken dışarıda kalır.
func initialDNSEngineInstallRollbackEvidenceScope(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	if initialBINDInstallRollbackEvidenceScope(manifest) {
		return true
	}
	if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
		manifest.SourceEngine != "" || manifest.SourceEpoch != 0 ||
		manifest.TargetEpoch != 1 {
		return false
	}
	standalone := manifest.Topology == transport.DNSTopologyStandalone &&
		manifest.PairRole == "" && manifest.LocalIP == "" &&
		manifest.LocalNS == "" && manifest.PeerIP == "" &&
		manifest.PeerNS == ""
	secondary := manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary &&
		manifest.LocalIP != "" && manifest.LocalNS != "" &&
		manifest.PeerIP != "" && manifest.PeerNS != ""
	switch manifest.TargetEngine {
	case transport.DNSEngineBIND:
		return secondary
	case transport.DNSEnginePowerDNS:
		return standalone || secondary ||
			freshPairedPDNSPrimaryRollbackEvidenceScope(manifest, pdnsFreshPairedPrimaryGateOpen)
	}
	return false
}

// freshPairedPDNSPrimaryRollbackEvidenceScope admits the fresh paired
// PowerDNS primary (V3) only while its product gate is open, so a build that
// cannot start that install never proves its rollback either. Its evidence
// is the same as every other first install: no switch journal (the V3
// pre-start inverse retires it only after the rolled-back checkpoint; a
// started target keeps it, forward only), no engine state, the exact
// install-ownership receipt, and the target stopped as rollback standby.
//
// Eşli PowerDNS birincilinin ilk kurulumu, yalnız ürün kapısı açıkken geri
// alma kanıtı kapsamındadır; kanıt diğer ilk kurulumlarla aynıdır.
func freshPairedPDNSPrimaryRollbackEvidenceScope(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	gateOpen bool,
) bool {
	return gateOpen &&
		manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRolePrimary &&
		manifest.LocalIP != "" && manifest.LocalNS != "" &&
		manifest.PeerIP != "" && manifest.PeerNS != "" &&
		!pdnsPairedPrimarySwitchPausedWithGate(manifest, gateOpen) &&
		!bindSourcePDNSSwitchUnsupported(manifest)
}

func exactFailedDNSEngineEvidenceJob(
	job *ServiceMutationJob,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return job != nil &&
		job.RequestID == request.MutationRequestID &&
		job.OwnerID == request.MutationOwnerID &&
		job.Kind == "dns_engine_switch" &&
		job.Target == string(manifest.TargetEngine) &&
		job.PackageName == manifest.Qualifier &&
		job.Status == serviceMutationStatusFailed &&
		job.Attempt > 0 &&
		!job.StartedAt.IsZero() &&
		!job.UpdatedAt.IsZero() &&
		!job.DeadlineAt.IsZero() &&
		!job.FinishedAt.IsZero() &&
		!job.UpdatedAt.Before(job.StartedAt) &&
		!job.DeadlineAt.Before(job.StartedAt) &&
		!job.FinishedAt.Before(job.StartedAt) &&
		job.UpdatedAt.Equal(job.FinishedAt) &&
		job.LeaseExpiresAt.IsZero() &&
		job.WorkerPID == 0 &&
		strings.TrimSpace(job.WorkerStarted) == "" &&
		strings.TrimSpace(job.WorkerCommand) == "" &&
		strings.TrimSpace(job.Phase) != "" &&
		!strings.HasPrefix(job.Phase, dnsEngineSwitchPublishedPhasePrefix)
}

func failedDNSEngineReceiptCommitment(job *ServiceMutationJob) (string, error) {
	encoded, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func exactDNSEngineInstallEvidence(
	receipt dnsEngineInstallOwnershipReceipt,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return receipt.Engine == manifest.TargetEngine &&
		receipt.ManifestQualifier == manifest.Qualifier &&
		receipt.MutationRequestID == request.MutationRequestID &&
		receipt.MutationOwnerID == request.MutationOwnerID
}

func conflictingDNSEngineRollbackTargetOwnership(
	ownership dnsEngineStateReceipt,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return ownership.EngineEpoch >= manifest.SourceEpoch ||
		ownership.EngineEpoch == manifest.TargetEpoch ||
		ownership.ManifestQualifier == manifest.Qualifier ||
		ownership.MutationRequestID == request.MutationRequestID ||
		ownership.MutationOwnerID == request.MutationOwnerID
}

func classifyDNSEngineRollbackHostEvidence(
	ctx context.Context,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (string, error) {
	if _, exists, err := readRollbackEvidenceJournal(); err != nil {
		return transport.DNSEngineRollbackUnverified, err
	} else if exists {
		return transport.DNSEngineRollbackJournalPresent, nil
	}

	state, stateExists, err := readRollbackEvidenceState()
	if err != nil {
		return transport.DNSEngineRollbackUnverified, err
	}
	if manifest.SourceEngine == "" {
		if stateExists {
			return transport.DNSEngineRollbackCommittedEvidence, nil
		}
	} else if !stateExists ||
		state.Engine != manifest.SourceEngine ||
		state.EngineEpoch != manifest.SourceEpoch {
		return transport.DNSEngineRollbackCommittedEvidence, nil
	}

	ownership, ownershipExists, err := readRollbackEvidenceOwnership(
		manifest.TargetEngine,
	)
	if err != nil {
		return transport.DNSEngineRollbackUnverified, err
	}
	if ownershipExists && conflictingDNSEngineRollbackTargetOwnership(
		ownership, request, manifest,
	) {
		return transport.DNSEngineRollbackCommittedEvidence, nil
	}

	install, installExists, err := readRollbackEvidenceInstallOwnership(
		manifest.TargetEngine,
	)
	if err != nil {
		return transport.DNSEngineRollbackUnverified, err
	}
	host, err := readRollbackEvidenceTargetHost(manifest.TargetEngine)
	if err != nil {
		return transport.DNSEngineRollbackUnverified, err
	}
	host.RefuseRestoredTakeover = manifest.PairRole == transport.DNSPairRoleSecondary
	if installExists &&
		(validateDNSEngineInstallOwnership(install) != nil ||
			!exactDNSEngineInstallEvidence(install, request, manifest) ||
			!exactDNSEngineInstallOwnership(
				install, true, manifest.TargetEngine,
				host.PackageManager, host.Packages,
			)) {
		return transport.DNSEngineRollbackInstallOwnershipMismatch, nil
	}
	if err := verifyRollbackEvidenceTargetSeal(
		ctx, manifest.TargetEngine, host,
	); err != nil {
		return transport.DNSEngineRollbackRuntimeUnsealed, nil
	}
	return transport.DNSEngineRollbackSafe, nil
}

func classifyDNSEngineRollbackHostEvidenceWithin(
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	limit time.Duration,
) string {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	outcome, err := classifyDNSEngineRollbackHostEvidence(
		ctx, request, manifest,
	)
	if err != nil || ctx.Err() != nil {
		return transport.DNSEngineRollbackUnverified
	}
	return outcome
}

func verifiedDNSEngineRollbackTargetHost(
	target transport.DNSEngine,
) (dnsEngineRollbackTargetHost, error) {
	if target == transport.DNSEnginePowerDNS {
		return verifiedPowerDNSRollbackTargetHost()
	}
	if target != transport.DNSEngineBIND {
		return dnsEngineRollbackTargetHost{},
			errors.New("rollback evidence is supported only for sealed BIND or stopped PowerDNS targets")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	if profile.PackageManager != hostplatform.PackageManagerAPT &&
		profile.PackageManager != hostplatform.PackageManagerPacman {
		return dnsEngineRollbackTargetHost{},
			errors.New("BIND rollback evidence host profile is unsupported")
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	systemctl, err := executableForProfile(
		profile, string(profile.PackageManager), "systemctl",
	)
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	return dnsEngineRollbackTargetHost{
		PackageManager: profile.PackageManager,
		Packages:       append([]string(nil), layout.Packages...),
		Systemctl:      systemctl,
	}, nil
}

type bindRollbackTargetProofOps struct {
	sealed   func() error
	restored func() error
}

// verifyDNSEngineRollbackTargetSealWithOps decides whether the host is in a
// state the panel may record as rolled back, and it accepts two shapes because
// the two operations that share this manifest end in opposite places.
//
// A first install's rollback ends with the target SEALED AND NOT SERVING: the
// panel started that engine, so anything still up would be the panel's own
// half-live BIND, and refusing is right.
//
// A takeover's rollback ends with the target SERVING, on purpose. It is the
// operator's own server, it never stopped, and it is answering its own
// configuration again. The sealed proof can never pass there, so a takeover
// crash left the operator with a healthy host and an operation the panel would
// not close - the wedge in the register's own R-019 family (register R-043).
// The second proof is the same claim from the other side and is not weaker:
// CelikPanel owns nothing that is live.
//
// If neither shape can be proved, both refusals are returned together, so the
// message names what was actually found instead of a guess.
//
// verifyDNSEngineRollbackTargetSealWithOps, sunucunun panelin "geri alındı"
// diye kaydedebileceği bir durumda olup olmadığına karar verir ve iki biçimi de
// kabul eder; çünkü bu bildirgeyi paylaşan iki işlem zıt yerlerde biter.
//
// Bir ilk kurulumun geri alması, hedef MÜHÜRLÜ VE HİZMET VERMEZ hâlde biter:
// o motoru panel başlatmıştı, dolayısıyla hâlâ ayakta olan bir şey panelin
// kendi yarı-canlı BIND'i olurdu ve reddetmek doğrudur.
//
// Bir devralmanın geri alması ise hedef HİZMET VERİR hâlde, bilerek biter. O,
// operatörün kendi sunucusudur, hiç durmadı ve yeniden kendi yapılandırmasını
// yanıtlıyor. Mühürlü kanıt orada asla geçemez; bu yüzden bir devralma çökmesi
// operatöre sağlıklı bir sunucu ve panelin kapatmadığı bir işlem bırakırdı -
// defterin kendi R-019 ailesindeki çıkmaz (defter R-043). İkinci kanıt aynı
// iddianın öbür yüzüdür ve daha zayıf değildir: CelikPanel'in canlı hiçbir
// şeyi yoktur.
//
// Hiçbir biçim kanıtlanamazsa iki ret birlikte döner; böylece mesaj bir tahmini
// değil, gerçekten bulunanı adlandırır.
func verifyDNSEngineRollbackTargetSealWithOps(
	ops bindRollbackTargetProofOps,
) error {
	if ops.sealed == nil || ops.restored == nil {
		return errors.New("rollback evidence target proof operations are incomplete")
	}
	sealErr := ops.sealed()
	if sealErr == nil {
		return nil
	}
	if restoredErr := ops.restored(); restoredErr != nil {
		return errors.Join(sealErr, restoredErr)
	}
	return nil
}

func verifiedPowerDNSRollbackTargetHost() (dnsEngineRollbackTargetHost, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	if profile.PackageManager != hostplatform.PackageManagerAPT {
		return dnsEngineRollbackTargetHost{},
			errors.New("PowerDNS rollback evidence host profile is unsupported")
	}
	packages, err := managedDNSEnginePackagesForProfile(profile, transport.DNSEnginePowerDNS)
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	systemctl, err := executableForProfile(
		profile, string(profile.PackageManager), "systemctl",
	)
	if err != nil {
		return dnsEngineRollbackTargetHost{}, err
	}
	return dnsEngineRollbackTargetHost{
		PackageManager: profile.PackageManager,
		Packages:       packages,
		Systemctl:      systemctl,
	}, nil
}

// verifyPowerDNSRollbackTargetStopped is the first-install stopped-target
// proof the PowerDNS rollback itself runs before it restores the database:
// pdns.service absent, under the package guard's persistent mask, or loaded;
// inactive/dead with zero main and control PIDs and an empty cgroup; no public
// port-53 listener and only the resolver stub locally; read twice.
func verifyPowerDNSRollbackTargetStopped(ctx context.Context, systemctl string) error {
	proof := hostPDNSRollbackStoppedProofOps(systemctl)
	proof.freshSource = true
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	return verifyPDNSStoppedBeforeDatabaseRestoreWithOps(proofCtx, proof)
}

func verifyDNSEngineRollbackTargetSeal(
	ctx context.Context,
	target transport.DNSEngine,
	host dnsEngineRollbackTargetHost,
) error {
	if target == transport.DNSEnginePowerDNS && host.Systemctl != "" && len(host.Packages) != 0 {
		return verifyPowerDNSRollbackTargetStopped(ctx, host.Systemctl)
	}
	if target != transport.DNSEngineBIND ||
		host.Systemctl == "" || len(host.Packages) == 0 {
		return errors.New("rollback evidence target host identity is invalid")
	}
	return verifyDNSEngineRollbackTargetSealWithOps(bindRollbackTargetProofOps{
		sealed: func() error {
			return verifyBINDSealedTargetNotServingWithoutManagedAuthority(
				ctx, host.Systemctl,
			)
		},
		restored: func() error {
			if host.RefuseRestoredTakeover {
				return errors.New("a paired-secondary first install has no running restored state")
			}
			return verifyRestoredUnmanagedRunningBINDTarget(ctx, host.Systemctl)
		},
	})
}

func lockedDNSEngineRollbackEvidence(
	manager *serviceMutationManager,
	request *transport.DNSEngineRollbackEvidenceRequest,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) (string, string) {
	if manager == nil {
		return transport.DNSEngineRollbackUnverified, ""
	}
	manager.mu.Lock()
	if manager.healthErrorLocked() != nil || manager.active != nil {
		manager.mu.Unlock()
		return transport.DNSEngineRollbackUnverified, ""
	}
	ledgerPath, lockPath := manager.ledgerPath, manager.lockPath
	manager.mu.Unlock()

	lock, err := acquireExistingServiceMutationFileLock(lockPath)
	if err != nil {
		return transport.DNSEngineRollbackUnverified, ""
	}
	closeLock := func(outcome, commitment string) (string, string) {
		if lock == nil {
			return transport.DNSEngineRollbackUnverified, ""
		}
		if err := lock.Close(); err != nil {
			lock = nil
			return transport.DNSEngineRollbackUnverified, ""
		}
		lock = nil
		if outcome != transport.DNSEngineRollbackSafe {
			commitment = ""
		}
		return outcome, commitment
	}
	defer func() {
		if lock != nil {
			_ = lock.Close()
		}
	}()

	manager.mu.Lock()
	managerUnchanged := manager.healthErrorLocked() == nil &&
		manager.active == nil &&
		manager.ledgerPath == ledgerPath &&
		manager.lockPath == lockPath
	manager.mu.Unlock()
	if !managerUnchanged {
		return closeLock(transport.DNSEngineRollbackUnverified, "")
	}

	firstLedger, err := manager.loadLedgerFromDisk()
	if err != nil {
		return closeLock(transport.DNSEngineRollbackUnverified, "")
	}
	if firstLedger.ActiveRequestID != "" {
		return closeLock(transport.DNSEngineRollbackActiveOperation, "")
	}
	first := firstLedger.Jobs[request.MutationRequestID]
	if !exactFailedDNSEngineEvidenceJob(first, request, manifest) {
		return closeLock(transport.DNSEngineRollbackIdentityMismatch, "")
	}
	commitment, err := failedDNSEngineReceiptCommitment(first)
	if err != nil {
		return closeLock(transport.DNSEngineRollbackUnverified, "")
	}
	outcome := classifyDNSEngineRollbackHostEvidenceWithin(
		request, manifest, dnsEngineRollbackEvidenceLimit,
	)
	secondLedger, err := manager.loadLedgerFromDisk()
	if err != nil {
		return closeLock(transport.DNSEngineRollbackUnverified, "")
	}
	if secondLedger.ActiveRequestID != "" {
		return closeLock(transport.DNSEngineRollbackActiveOperation, "")
	}
	second := secondLedger.Jobs[request.MutationRequestID]
	if !exactFailedDNSEngineEvidenceJob(second, request, manifest) ||
		!reflect.DeepEqual(firstLedger, secondLedger) {
		return closeLock(transport.DNSEngineRollbackIdentityMismatch, "")
	}
	manager.mu.Lock()
	managerUnchanged = manager.healthErrorLocked() == nil &&
		manager.active == nil &&
		manager.ledgerPath == ledgerPath &&
		manager.lockPath == lockPath
	manager.mu.Unlock()
	if !managerUnchanged {
		return closeLock(transport.DNSEngineRollbackUnverified, "")
	}
	return closeLock(outcome, commitment)
}

// DNSEngineRollbackEvidenceV1 is comparison-only. The complete frozen
// manifest is independently canonicalized, the terminal ledger identity is
// checked twice around host evidence reads, and only a bounded enum plus a
// fixed-size receipt commitment cross the RPC boundary.
func (a *Agent) DNSEngineRollbackEvidenceV1(
	request *transport.DNSEngineRollbackEvidenceRequest,
	response *transport.DNSEngineRollbackEvidenceResponse,
) error {
	if response == nil {
		return errors.New("DNS engine rollback evidence response is required")
	}
	response.Outcome = transport.DNSEngineRollbackUnverified
	response.ReceiptCommitment = ""
	manifest, err := canonicalDNSEngineRollbackEvidence(request)
	if err != nil {
		response.Outcome = transport.DNSEngineRollbackIdentityMismatch
		return nil
	}
	response.Outcome, response.ReceiptCommitment = lockedDNSEngineRollbackEvidence(
		loadedAgentServiceMutationManager(), request, manifest,
	)
	return nil
}
