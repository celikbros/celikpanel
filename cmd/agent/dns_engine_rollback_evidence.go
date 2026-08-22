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
	if !initialBINDInstallRollbackEvidenceScope(manifest) {
		return mutationpayload.DNSEngineSwitchManifestCommitment{},
			errors.New("DNS engine rollback evidence is outside the supported initial BIND install scope")
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
	if target != transport.DNSEngineBIND {
		return dnsEngineRollbackTargetHost{},
			errors.New("rollback evidence is supported only for sealed BIND targets")
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

func verifyDNSEngineRollbackTargetSeal(
	ctx context.Context,
	target transport.DNSEngine,
	host dnsEngineRollbackTargetHost,
) error {
	if target != transport.DNSEngineBIND ||
		host.Systemctl == "" || len(host.Packages) == 0 {
		return errors.New("rollback evidence target host identity is invalid")
	}
	return verifyBINDSealedTargetNotServingWithoutManagedAuthority(
		ctx, host.Systemctl,
	)
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
