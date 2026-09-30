//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"

	"github.com/alicelik/celikpanel/internal/dnspeerjournal"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

// verifyEnrolledBINDPeerDeletion is an optional, owner-enrolled observation.
// Every failure leaves the existing V3 operation pending. An untracked startup
// orphan pass has no active attempt and cannot mint a network challenge.
func verifyEnrolledBINDPeerDeletion(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) error {
	if !plan.Changed.Delete || plan.Legacy || ctx.Err() != nil {
		return errors.New("native BIND deletion proof is unavailable")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("native BIND proof lacks an active mutation attempt")
	}
	m, runtime := tracker.manager, tracker.runtime
	attempt, err := currentBINDPeerLedgerAttempt(m, runtime, plan)
	if err != nil {
		return err
	}
	// Select the post-inspection catalog probes before any challenge exists.
	// An unusable plan is the Agent's own precondition, never an owner edit.
	localCatalogProbe, peerCatalogProbe, err := nativePeerProofCatalogProbes(plan)
	if err != nil {
		return err
	}
	enrollment, err := dnspeerenrollment.Read()
	if err != nil {
		if dnspeerenrollment.IsCode(err, dnspeerenrollment.Disabled) {
			return pendingBINDPeer(transport.DNSPeerPendingEnrollmentRequired)
		}
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	if enrollment.Record.PrimaryIP != authority.sourceIP ||
		enrollment.Record.PeerIP != authority.peerIP ||
		enrollment.Record.CatalogName != authority.catalog ||
		enrollment.Record.View != dnspeerproof.DefaultView {
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	verifyCurrent := func() error {
		return peerCurrentPendingCodeAt(
			func() error {
				current, currentErr := currentBINDPeerLedgerAttempt(m, runtime, plan)
				if currentErr != nil || current != attempt {
					return errors.New("native BIND proof lost its active operation attempt")
				}
				return nil
			},
			func() error {
				if dnspeerenrollment.Recheck(enrollment) != nil || !pdnsPeerEnrollmentAbsent() {
					return errors.New("native BIND peer enrollment changed")
				}
				return nil
			},
			func() error { return recheckNativePeerLocalEvidence(ctx, plan) },
		)
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	if err := reconcileHistoricalBINDPeerChallenge(m, plan, verifyCurrent); err != nil {
		return pendingBINDPeer(transport.DNSPeerPendingJournalUnknown)
	}
	next := uint64(1)
	previous, err := dnspeerjournal.Read()
	if err == nil {
		if previous.Request.Attempt == ^uint64(0) {
			return pendingBINDPeer(transport.DNSPeerPendingJournalUnknown)
		}
		next = previous.Request.Attempt + 1
	} else if !dnspeerjournal.IsCode(err, dnspeerjournal.Missing) {
		return pendingBINDPeer(transport.DNSPeerPendingJournalUnknown)
	}
	request, err := mintBINDPeerDeletionRequest(plan, authority,
		enrollment.Record.HostKeySHA256, next, time.Now(), rand.Reader)
	if err != nil {
		return err
	}
	if err := dnspeerjournal.Publish(request, enrollment.RecordSHA256, attempt, verifyCurrent); err != nil {
		return pendingBINDPeer(transport.DNSPeerPendingJournalUnknown)
	}
	if err := dnspeerenrollment.Recheck(enrollment); err != nil {
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	response, authenticated, err := dnspeertransport.Inspect(ctx, enrollment.Transport, request, dnspeertransport.SSH{})
	if err != nil {
		return pendingBINDPeer(inspectionPendingCode(dnspeertransport.InspectorReason(err)))
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	// The source-bound catalog and no-transfer evidence must still match after
	// the network round trip. A contradictory positive DNS answer stays fatal.
	fresh, err := verifyDNSPrimaryPairReadyAuthorityAt(ctx, plan.Evidence,
		probeDNSZoneSOA, localCatalogProbe, peerCatalogProbe)
	if err != nil || fresh != authority {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	if verifyPeerZoneNoTransferAt(ctx, fresh, plan.Changed.Domain, probeDNSBoundZoneAXFR) != nil {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	observation, err := observeDeletedDNSZoneAt(ctx, fresh.sourceIP, fresh.peerIP,
		plan.Changed.Domain, probeDNSZoneSOA)
	if err != nil || observation != dnsDeletedZoneEmptyRefused {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	var consumeErr error
	_, err = dnspeerproof.Verify(request, response, authenticated, time.Now(),
		func(digest string) bool {
			consumeErr = dnspeerjournal.ConsumeOnce(request, enrollment.RecordSHA256,
				digest, attempt, verifyCurrent)
			return consumeErr == nil
		})
	if err != nil || consumeErr != nil {
		if consumeErr != nil {
			return pendingBINDPeer(transport.DNSPeerPendingJournalUnknown)
		}
		return pendingBINDPeer(transport.DNSPeerPendingNativeUnknown)
	}
	if err := verifyCurrent(); err != nil {
		return err
	}
	return nil
}

// A crash after terminal ledger publication but before challenge retirement
// must not block a later independent deletion. The current operation holds the
// host and publication locks. Only an exact historical published result may
// retire old evidence; anything else remains unknown and cannot mint a request.
func reconcileHistoricalBINDPeerChallenge(
	m *serviceMutationManager, plan dnsV3PrimaryPropagationPlan, verifyCurrent func() error,
) error {
	return reconcileHistoricalBINDPeerChallengeAt(plan, verifyCurrent,
		m.loadLedgerFromDisk, dnspeerjournal.Read, dnspeerjournal.Retire)
}

func reconcileHistoricalBINDPeerChallengeAt(
	plan dnsV3PrimaryPropagationPlan, verifyCurrent func() error,
	loadLedger func() (serviceMutationLedger, error),
	read func() (dnspeerjournal.RecordV1, error),
	retire func(dnspeerproof.RequestV1, string, string, uint64, dnspeerjournal.VerifyCurrent) error,
) error {
	if verifyCurrent == nil || loadLedger == nil || read == nil || retire == nil {
		return errors.New("native BIND challenge reconciliation is unavailable")
	}
	previous, err := read()
	if dnspeerjournal.IsCode(err, dnspeerjournal.Missing) {
		return nil
	}
	if err != nil {
		return errors.New("native BIND challenge journal is unreadable")
	}
	if previous.Request.MutationRequestID == plan.Operation.RequestID &&
		previous.Request.MutationOwnerID == plan.Operation.OwnerID &&
		previous.Request.DeletedZone == plan.Changed.Domain &&
		previous.Request.DeletionQualifier == plan.Operation.Qualifier {
		return nil
	}
	verifyTerminal := func() error {
		if err := verifyCurrent(); err != nil {
			return err
		}
		ledger, err := loadLedger()
		if err != nil || !historicalBINDPeerChallengeMatchesLedger(previous, &ledger) {
			return errors.New("previous native BIND challenge is not terminal in the durable ledger")
		}
		return nil
	}
	if err := verifyTerminal(); err != nil {
		return err
	}
	if err := retire(previous.Request, previous.EnrollmentSHA256,
		previous.RequestSHA256, previous.LedgerAttempt, verifyTerminal); err != nil {
		return errors.New("previous native BIND challenge could not be retired")
	}
	if _, err := read(); !dnspeerjournal.IsCode(err, dnspeerjournal.Missing) {
		return errors.New("previous native BIND challenge retirement is unverified")
	}
	return verifyCurrent()
}

func historicalBINDPeerChallengeMatchesLedger(previous dnspeerjournal.RecordV1, ledger *serviceMutationLedger) bool {
	if previous.Validate() != nil || previous.LedgerAttempt == 0 ||
		previous.LedgerAttempt > uint64(^uint(0)>>1) {
		return false
	}
	request := previous.Request
	return servicemutationledger.ClassifyDNSZoneV3PeerOperation(ledger,
		request.MutationRequestID, request.MutationOwnerID,
		request.DeletedZone, request.DeletionQualifier,
		int(previous.LedgerAttempt)) ==
		servicemutationledger.DNSZoneV3PeerOperationHistoricalPublished
}

// The manager mutex protects the in-memory active pointer. The two exact
// external flock descriptions protect competing processes; the durable ledger
// is reread under both before the captured attempt can authorize any challenge.
func currentBINDPeerLedgerAttempt(m *serviceMutationManager, runtime *serviceMutationRuntime, plan dnsV3PrimaryPropagationPlan) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.healthErrorLocked() != nil || m.active != runtime || runtime.steps != 1 ||
		runtime.lock == nil || runtime.lock.file == nil ||
		runtime.lock.publication == nil || runtime.lock.publication.file == nil ||
		runtime.job == nil || runtime.job.WorkerPID != 0 ||
		runtime.job.RequestID != plan.Operation.RequestID ||
		runtime.job.OwnerID != plan.Operation.OwnerID ||
		runtime.job.Target != plan.Changed.Domain ||
		runtime.job.PackageName != plan.Operation.Qualifier ||
		runtime.job.Attempt < 1 {
		return 0, errors.New("native BIND proof lacks an exact active runtime")
	}
	owner := serviceMutationLockOwner()
	if hostmutationlock.VerifyInherited(m.lockPath, int(runtime.lock.file.Fd()), owner) != nil ||
		hostmutationlock.VerifyInherited(serviceMutationLedgerPublicationLockFile(m.lockPath),
			int(runtime.lock.publication.file.Fd()), owner) != nil {
		return 0, errors.New("native BIND proof lost its host or publication lock")
	}
	durable, err := m.loadLedgerFromDisk()
	if err != nil || !reflect.DeepEqual(durable, m.ledger) {
		return 0, errors.New("native BIND proof cannot reconcile the durable ledger")
	}
	state := servicemutationledger.ClassifyDNSZoneV3PeerOperation(&durable,
		plan.Operation.RequestID, plan.Operation.OwnerID,
		plan.Changed.Domain, plan.Operation.Qualifier, runtime.job.Attempt)
	if state != servicemutationledger.DNSZoneV3PeerOperationActiveApplied &&
		state != servicemutationledger.DNSZoneV3PeerOperationActiveRecovering {
		return 0, fmt.Errorf("native BIND proof operation is %s", state)
	}
	if runtime.dnsZoneSyncV3Recovery != (state == servicemutationledger.DNSZoneV3PeerOperationActiveRecovering) {
		return 0, errors.New("native BIND proof runtime phase disagrees with the ledger")
	}
	return uint64(runtime.job.Attempt), nil
}

// The primary engine is bound to the accepted operation before any peer
// challenge. An unknown or changed source cannot authorize a native answer.
// Every plan carries its source receipt (newDNSV3PrimaryPropagationPlan); an
// empty or unknown engine is the Agent's own precondition failure.
func recheckNativePeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan) error {
	switch plan.SourceState.Engine {
	case transport.DNSEnginePowerDNS:
		return recheckPDNSPeerLocalEvidence(ctx, plan)
	case transport.DNSEngineBIND:
		return recheckBINDPeerLocalEvidence(ctx, plan)
	default:
		return dnsPeerProofInternal(fmt.Errorf(
			"native peer proof plan source engine %q is unknown", plan.SourceState.Engine))
	}
}

// The PowerDNS producer, exact V3 deletion receipt and native daemon must
// continue to describe the same state captured before peer inspection. This
// check runs before minting, after the SSH round trip, during consume-once and
// once more before reporting success.
func recheckPDNSPeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan) error {
	if !plan.Changed.Delete || plan.Legacy ||
		plan.SourceState.Engine != transport.DNSEnginePowerDNS ||
		plan.SourceState.PairRole != transport.DNSPairRolePrimary ||
		plan.SourceState.Mode != transport.DNSEngineSwitchModeSwitch {
		return dnsPeerProofInternal(errors.New("native PowerDNS proof plan source state is not a managed primary"))
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	return recheckPDNSNativeBindingAt(
		func() (pdnsPrimaryNativeBinding, error) {
			return readPDNSPrimaryNativeBinding(ctx, systemctl)
		},
		func() error {
			state, found, err := readDNSEngineState()
			if err != nil || !found || !reflect.DeepEqual(state, plan.SourceState) {
				return errors.New("native PowerDNS proof engine state changed")
			}
			if err := verifyOnlyPDNSActive(ctx, systemctl); err != nil {
				return err
			}
			binding := transport.ServiceMutationBinding{
				MutationRequestID: plan.Operation.RequestID,
				MutationOwnerID:   plan.Operation.OwnerID,
			}
			zone, exact, err := readPDNSV3ZoneSnapshot(ctx, pdnsDBPath(), state,
				plan.Changed.Domain, plan.Operation.Qualifier, binding)
			if err != nil || !exact || !zone.Delete ||
				zone.DesiredGeneration != plan.Operation.Generation ||
				zone.ZoneQualifier != plan.Operation.Qualifier {
				return errors.New("native PowerDNS proof deletion receipt changed")
			}
			current, primary, err := managedPDNSPrimaryCatalogEvidenceForState(ctx, state)
			if err != nil || !primary || !reflect.DeepEqual(current, plan.Evidence) {
				return errors.New("native PowerDNS proof producer catalog changed")
			}
			last, found, err := readDNSEngineState()
			if err != nil || !found || !reflect.DeepEqual(last, state) {
				return errors.New("native PowerDNS proof engine state changed during inspection")
			}
			return nil
		},
	)
}
func recheckBINDPeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan) error {
	state, found, err := readDNSEngineState()
	if err != nil || !found || state.Engine != transport.DNSEngineBIND {
		return errors.New("native BIND proof requires an active BIND receipt")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return err
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return err
	}
	tree, err := publisher.LoadCurrent()
	if err != nil {
		return err
	}
	receipt := tree.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch || receipt.Generation != state.Generation {
		return errors.New("native BIND proof current generation changed")
	}
	legacy, err := bindStateTreePairContract(layout.GenerationRoot, state, tree, false, false, false)
	if err != nil || legacy {
		return errors.New("native BIND proof local pair receipt changed")
	}
	if err := verifyManagedBINDRuntimeConfigExact(ctx, layout, receipt, false); err != nil {
		return err
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return err
	}
	if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
		return err
	}
	// The plan carries the state receipt it was built from; the one read now
	// must be identical, as must everything derived from the current tree.
	current, primary, err := bindV3PrimaryPropagationPlan(tree, plan.Changed.Domain, state)
	if err != nil || !primary || !reflect.DeepEqual(current, plan) {
		return errors.New("native BIND proof local deletion receipt changed")
	}
	return nil
}

// The terminal ledger receipt is committed before retiring the consumed (or
// superseded) challenge. Retirement is best effort: failure cannot revoke a
// durable successful operation, but a later proof will fail closed on residue.
// The caller has released m.mu. This cleanup does not acquire it, so it
// cannot invert the manager's m.mu -> host -> publication lock ordering.
func retireTerminalBINDPeerChallenge(ledgerPath, lockPath string, job *ServiceMutationJob) error {
	if job == nil || job.Kind != "dns_zone_sync" {
		return nil
	}
	record, err := dnspeerjournal.Read()
	if dnspeerjournal.IsCode(err, dnspeerjournal.Missing) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Request.MutationRequestID != job.RequestID ||
		record.Request.MutationOwnerID != job.OwnerID ||
		record.Request.DeletedZone != job.Target ||
		record.Request.DeletionQualifier != job.PackageName ||
		int(record.LedgerAttempt) != job.Attempt {
		return nil
	}
	lock, err := acquireServiceMutationHostAndPublicationLocks(lockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	verify := func() error {
		if lock.file == nil || lock.publication == nil || lock.publication.file == nil ||
			hostmutationlock.VerifyInherited(lockPath, int(lock.file.Fd()), serviceMutationLockOwner()) != nil ||
			hostmutationlock.VerifyInherited(serviceMutationLedgerPublicationLockFile(lockPath),
				int(lock.publication.file.Fd()), serviceMutationLockOwner()) != nil {
			return errors.New("terminal native BIND challenge lost its external locks")
		}
		ledger, err := loadExactTerminalBINDPeerLedger(ledgerPath)
		if err != nil ||
			servicemutationledger.ClassifyDNSZoneV3PeerOperation(&ledger,
				job.RequestID, job.OwnerID, job.Target, job.PackageName, job.Attempt) !=
				servicemutationledger.DNSZoneV3PeerOperationHistoricalPublished {
			return errors.New("terminal native BIND challenge lost its exact ledger result")
		}
		return nil
	}
	return dnspeerjournal.Retire(record.Request, record.EnrollmentSHA256,
		record.RequestSHA256, record.LedgerAttempt, verify)
}

func loadExactTerminalBINDPeerLedger(path string) (serviceMutationLedger, error) {
	reader := &serviceMutationManager{ledgerPath: path}
	return reader.loadLedgerFromDisk()
}
