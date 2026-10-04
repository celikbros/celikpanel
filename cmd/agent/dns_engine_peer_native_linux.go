//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
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
//
// ctx is the request context, never the completion wave's DNS probe context:
// each step runs under its own bound (dnsPeerProofSteps), and consume-once
// runs under a fresh context once the peer's answer was accepted.
func verifyEnrolledBINDPeerDeletion(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) (result error) {
	if !plan.Changed.Delete || plan.Legacy || ctx.Err() != nil {
		return errors.New("native BIND deletion proof is unavailable")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("native BIND proof lacks an active mutation attempt")
	}
	m, runtime := tracker.manager, tracker.runtime
	run := newDNSPeerProofRun(ctx, dnsPeerProofSteps, plan.Changed.Domain, "BIND")
	defer func() { run.finish(result) }()
	var (
		attempt           uint64
		localCatalogProbe dnsCatalogAXFRProbe
		peerCatalogProbe  dnsBoundCatalogAXFRProbe
		record            *dnsRecordedProducerCatalog
		enrollment        dnspeerenrollment.Snapshot
		checks            *nativePeerCurrentChecks
		request           dnspeerproof.RequestV1
		response          dnspeerproof.ResponseV1
		authenticated     dnspeerproof.PeerAuthentication
	)
	if err := run.step(dnsPeerProofStepPrepare, func(ctx context.Context) error {
		var err error
		if attempt, err = currentBINDPeerLedgerAttempt(m, runtime, plan); err != nil {
			return err
		}
		// Select the post-inspection catalog probes before any challenge
		// exists. An unusable plan is the Agent's own precondition, never an
		// owner edit.
		if localCatalogProbe, peerCatalogProbe, err = nativePeerProofCatalogProbes(plan); err != nil {
			return err
		}
		// The attempt's recorded producer catalog (shared with the wave).
		record = recordedProducerCatalogFor(plan)
		if enrollment, err = dnspeerenrollment.Read(); err != nil {
			if dnspeerenrollment.IsCode(err, dnspeerenrollment.Disabled) {
				return pendingBINDPeer(transport.DNSPeerPendingEnrollmentRequired)
			}
			return pendingDNSPeerCause(transport.DNSPeerPendingEnrollmentChanged, err)
		}
		if enrollment.Record.PrimaryIP != authority.sourceIP ||
			enrollment.Record.PeerIP != authority.peerIP ||
			enrollment.Record.CatalogName != authority.catalog ||
			enrollment.Record.View != dnspeerproof.DefaultView {
			return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
		}
		checks = &nativePeerCurrentChecks{
			attempt: func() error {
				current, currentErr := currentBINDPeerLedgerAttempt(m, runtime, plan)
				if currentErr != nil || current != attempt {
					return errors.New("native BIND proof lost its active operation attempt")
				}
				return nil
			},
			enrollment: func() error {
				if dnspeerenrollment.Recheck(enrollment) != nil || !pdnsPeerEnrollmentAbsent() {
					return errors.New("native BIND peer enrollment changed")
				}
				return nil
			},
			local: func(ctx context.Context) error { return recheckNativePeerLocalEvidence(ctx, plan, record) },
		}
		if err := checks.at(ctx)(); err != nil {
			return err
		}
		if err := checks.journalOp(ctx, "previous challenge reconciliation", func(verify func() error) error {
			return reconcileHistoricalBINDPeerChallenge(m, plan, verify)
		}); err != nil {
			return err
		}
		next := uint64(1)
		previous, err := dnspeerjournal.Read()
		if err == nil {
			if previous.Request.Attempt == ^uint64(0) {
				return pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown,
					errors.New("challenge journal attempt counter is exhausted"))
			}
			next = previous.Request.Attempt + 1
		} else if !dnspeerjournal.IsCode(err, dnspeerjournal.Missing) {
			return pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown,
				fmt.Errorf("challenge journal read: %w", err))
		}
		// A daemon re-stamp admitted by the rechecks above moved the recorded
		// catalog past the serial the wave proved: no challenge is minted;
		// the wave proves the re-stamped pair first.
		if plan, err = nativePeerChallengePlan(plan, record, authority); err != nil {
			return err
		}
		if request, err = mintBINDPeerDeletionRequest(plan, authority,
			enrollment.Record.HostKeySHA256, next, time.Now(), rand.Reader); err != nil {
			return err
		}
		run.challengeMinted(time.Unix(request.ExpiresAtUnix, 0))
		return nil
	}); err != nil {
		return err
	}
	if err := run.step(dnsPeerProofStepChallengeWrite, func(ctx context.Context) error {
		if err := checks.journalOp(ctx, "challenge publish", func(verify func() error) error {
			return dnspeerjournal.Publish(request, enrollment.RecordSHA256, attempt, verify)
		}); err != nil {
			return err
		}
		if err := dnspeerenrollment.Recheck(enrollment); err != nil {
			return pendingDNSPeerCause(transport.DNSPeerPendingEnrollmentChanged, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := run.stepWithin(dnsPeerProofStepExchange, enrollment.Transport.Timeout, func(ctx context.Context) error {
		var err error
		response, authenticated, err = dnspeertransport.Inspect(ctx, enrollment.Transport, request, dnspeertransport.SSH{})
		if err != nil {
			return pendingDNSPeerCause(inspectionPendingCode(dnspeertransport.InspectorReason(err)), err)
		}
		return nil
	}); err != nil {
		return err
	}
	// The source-bound catalog and no-transfer evidence must still match after
	// the network round trip. A contradictory positive DNS answer stays fatal.
	if err := run.step(dnsPeerProofStepPostInspection, func(ctx context.Context) error {
		verifyCurrent := checks.at(ctx)
		if err := verifyCurrent(); err != nil {
			return err
		}
		if err := verifyNativePeerAfterInspectionAt(ctx, record, authority, plan.Changed.Domain,
			nativePeerAfterInspectionProbes{
				soa: probeDNSZoneSOA, localCatalog: localCatalogProbe,
				peerCatalog: peerCatalogProbe, peerZone: probeDNSBoundZoneAXFR,
			}, verifyCurrent); err != nil {
			return err
		}
		return verifyCurrent()
	}); err != nil {
		return err
	}
	return run.consume(func() error {
		accepted := false
		var consumeErr error
		_, err := dnspeerproof.Verify(request, response, authenticated, time.Now(),
			func(digest string) bool {
				// The answer is accepted: consume-once runs under the fresh
				// consume context, never under an earlier step's clock.
				accepted = true
				consumeErr = checks.journalOp(run.accepted(), "consume-once", func(verify func() error) error {
					return dnspeerjournal.ConsumeOnce(request, enrollment.RecordSHA256,
						digest, attempt, verify)
				})
				return consumeErr == nil
			})
		run.logAnswer(response.CatalogState, response.MemberState, response.NativeState, accepted, err)
		if consumeErr != nil {
			return consumeErr
		}
		if err != nil {
			return pendingDNSPeerCause(transport.DNSPeerPendingNativeUnknown, err)
		}
		return checks.at(run.accepted())()
	})
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
// empty or unknown engine is the Agent's own precondition failure. record is
// the attempt's recorded producer catalog; nil records the plan's evidence.
func recheckNativePeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan, record *dnsRecordedProducerCatalog) error {
	if record == nil {
		record = recordedProducerCatalogFor(plan)
	}
	switch plan.SourceState.Engine {
	case transport.DNSEnginePowerDNS:
		return recheckPDNSPeerLocalEvidence(ctx, plan, record)
	case transport.DNSEngineBIND:
		return recheckBINDPeerLocalEvidence(ctx, plan, record)
	default:
		return dnsPeerProofInternal(fmt.Errorf(
			"native peer proof plan source engine %q is unknown", plan.SourceState.Engine))
	}
}

// pdnsPeerLocalEvidenceReaders are the reads of one PowerDNS local recheck.
type pdnsPeerLocalEvidenceReaders struct {
	binding  func() (pdnsPrimaryNativeBinding, error)
	state    func() (dnsEngineStateReceipt, bool, error)
	onlyPDNS func() error
	receipt  func(dnsEngineStateReceipt) (transport.DNSEngineSwitchZoneSnapshot, bool, error)
	catalog  func(dnsEngineStateReceipt) (dnsPrimaryCatalogEvidence, bool, error)
}

// pdnsPeerLocalRecheckReads bounds how often one local recheck reads again
// after a difference the local daemon's own write can cause for an instant
// (its database file changed during the bracket, or a half-written re-stamp).
const pdnsPeerLocalRecheckReads = 3

const pdnsPeerLocalRecheckDelay = 250 * time.Millisecond

// The PowerDNS producer, exact V3 deletion receipt and native daemon must
// continue to describe the same state captured before peer inspection. This
// check runs before minting, after the SSH round trip, during consume-once and
// once more before reporting success. The producer catalog is judged against
// the attempt's recorded evidence by classifyProducerCatalogEvidence; an
// admitted daemon re-stamp re-stamps the record once the whole recheck held.
func recheckPDNSPeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan, record *dnsRecordedProducerCatalog) error {
	if !plan.Changed.Delete || plan.Legacy ||
		plan.SourceState.Engine != transport.DNSEnginePowerDNS ||
		plan.SourceState.PairRole != transport.DNSPairRolePrimary ||
		plan.SourceState.Mode != transport.DNSEngineSwitchModeSwitch {
		return dnsPeerProofInternal(errors.New("native PowerDNS proof plan source state is not a managed primary"))
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: plan.Operation.RequestID,
		MutationOwnerID:   plan.Operation.OwnerID,
	}
	return recheckPDNSPeerLocalEvidenceAt(ctx, pdnsPeerLocalEvidenceReaders{
		binding: func() (pdnsPrimaryNativeBinding, error) {
			return readPDNSPrimaryNativeBinding(ctx, systemctl)
		},
		state:    readDNSEngineState,
		onlyPDNS: func() error { return verifyOnlyPDNSActive(ctx, systemctl) },
		receipt: func(state dnsEngineStateReceipt) (transport.DNSEngineSwitchZoneSnapshot, bool, error) {
			return readPDNSV3ZoneSnapshot(ctx, pdnsDBPath(), state,
				plan.Changed.Domain, plan.Operation.Qualifier, binding)
		},
		catalog: func(state dnsEngineStateReceipt) (dnsPrimaryCatalogEvidence, bool, error) {
			return managedPDNSPrimaryCatalogEvidenceForState(ctx, state)
		},
	}, plan, record)
}

func recheckPDNSPeerLocalEvidenceAt(
	ctx context.Context,
	readers pdnsPeerLocalEvidenceReaders,
	plan dnsV3PrimaryPropagationPlan,
	record *dnsRecordedProducerCatalog,
) error {
	if record == nil || readers.binding == nil || readers.state == nil ||
		readers.onlyPDNS == nil || readers.receipt == nil || readers.catalog == nil {
		return dnsPeerProofInternal(errors.New("native PowerDNS local recheck is incomplete"))
	}
	var err error
	for read := 1; ; read++ {
		var observed dnsPrimaryCatalogEvidence
		observed, err = recheckPDNSPeerLocalEvidenceOnce(readers, plan, record)
		if err == nil {
			// The whole bracket held: re-stamp the record before the
			// proof continues (an unchanged catalog leaves it as is).
			_, err = record.Admit(observed)
			return err
		}
		if !retryableDNSPeerOwnerEdit(err) || read >= pdnsPeerLocalRecheckReads || ctx.Err() != nil {
			return err
		}
		log.Printf("DNS peer proof reads its local PowerDNS evidence again (%d of %d) after a momentary difference: %s",
			read+1, pdnsPeerLocalRecheckReads, boundedDNSPeerProofLogText(err))
		select {
		case <-ctx.Done():
			return err
		case <-time.After(pdnsPeerLocalRecheckDelay):
		}
	}
}

func recheckPDNSPeerLocalEvidenceOnce(
	readers pdnsPeerLocalEvidenceReaders,
	plan dnsV3PrimaryPropagationPlan,
	record *dnsRecordedProducerCatalog,
) (dnsPrimaryCatalogEvidence, error) {
	var observed dnsPrimaryCatalogEvidence
	err := recheckPDNSNativeBindingAt(readers.binding, func() error {
		state, found, err := readers.state()
		if err != nil || !found || !reflect.DeepEqual(state, plan.SourceState) {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckEngineState,
				"native PowerDNS proof engine state changed: %s",
				describeDNSEngineStateDifference(plan.SourceState, state, found, err))
		}
		if err := readers.onlyPDNS(); err != nil {
			return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
		}
		zone, exact, err := readers.receipt(state)
		if err != nil || !exact || !zone.Delete ||
			zone.DesiredGeneration != plan.Operation.Generation ||
			zone.ZoneQualifier != plan.Operation.Qualifier {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckDeletionReceipt,
				"native PowerDNS proof deletion receipt changed: recorded generation=%v delete=true; observed exact=%t delete=%t generation=%v qualifier-matches=%t (%s)",
				plan.Operation.Generation, exact, zone.Delete, zone.DesiredGeneration,
				zone.ZoneQualifier == plan.Operation.Qualifier, errorTextOrNone(err))
		}
		current, primary, err := readers.catalog(state)
		if err != nil || !primary {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckProducerCatalog,
				"native PowerDNS producer catalog is not readable as a managed primary (primary=%t): %s",
				primary, errorTextOrNone(err))
		}
		if _, err := record.Classify(current); err != nil {
			return err
		}
		last, found, err := readers.state()
		if err != nil || !found || !reflect.DeepEqual(last, state) {
			return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckEngineState,
				"native PowerDNS proof engine state changed during inspection: %s",
				describeDNSEngineStateDifference(state, last, found, err))
		}
		observed = current
		return nil
	})
	return observed, err
}

// describeDNSEngineStateDifference names the recorded and observed engine
// receipt fields a recheck compares (no key material is in the receipt).
func describeDNSEngineStateDifference(recorded, observed dnsEngineStateReceipt, found bool, err error) string {
	if err != nil || !found {
		return fmt.Sprintf("recorded engine=%s epoch=%d generation=%v; observed found=%t (%s)",
			recorded.Engine, recorded.EngineEpoch, recorded.Generation, found, errorTextOrNone(err))
	}
	return fmt.Sprintf("recorded engine=%s epoch=%d generation=%v mode=%s role=%s catalog-serial=%d; observed engine=%s epoch=%d generation=%v mode=%s role=%s catalog-serial=%d",
		recorded.Engine, recorded.EngineEpoch, recorded.Generation, recorded.Mode, recorded.PairRole, recorded.PrimaryCatalogSerial,
		observed.Engine, observed.EngineEpoch, observed.Generation, observed.Mode, observed.PairRole, observed.PrimaryCatalogSerial)
}

// BIND has no daemon re-stamp: the plan rebuilt from the current tree must
// equal the recorded plan, and its catalog evidence is compared through the
// same rule (classifyProducerCatalogEvidence), which for BIND admits nothing.
func recheckBINDPeerLocalEvidence(ctx context.Context, plan dnsV3PrimaryPropagationPlan, record *dnsRecordedProducerCatalog) error {
	state, found, err := readDNSEngineState()
	if err != nil || !found || state.Engine != transport.DNSEngineBIND {
		return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckEngineState,
			"native BIND proof requires an active BIND receipt: %s",
			describeDNSEngineStateDifference(plan.SourceState, state, found, err))
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckNativeBinding, err)
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckNativeBinding, err)
	}
	tree, err := publisher.LoadCurrent()
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckDeletionReceipt, err)
	}
	receipt := tree.CurrentReceipt()
	if receipt.EngineEpoch != state.EngineEpoch || receipt.Generation != state.Generation {
		return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckEngineState,
			"native BIND proof current generation changed: state epoch=%d generation=%v; tree epoch=%d generation=%v",
			state.EngineEpoch, state.Generation, receipt.EngineEpoch, receipt.Generation)
	}
	legacy, err := bindStateTreePairContract(layout.GenerationRoot, state, tree, false, false, false)
	if err != nil || legacy {
		return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckEngineState,
			"native BIND proof local pair receipt changed (legacy=%t): %s", legacy, errorTextOrNone(err))
	}
	if err := verifyManagedBINDRuntimeConfigExact(ctx, layout, receipt, false); err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckNativeBinding, err)
	}
	systemctl, err := executableForProfile(profile, string(profile.PackageManager), "systemctl")
	if err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
	}
	if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
		return dnsPeerOwnerEditAt(transport.DNSPeerOwnerEditCheckActiveEngine, err)
	}
	// The plan carries the state receipt it was built from; the one read now
	// must be identical, as must everything derived from the current tree.
	current, primary, err := bindV3PrimaryPropagationPlan(tree, plan.Changed.Domain, state)
	if err != nil || !primary || !sameDNSV3PlanIdentity(current, plan) {
		return dnsPeerOwnerEditf(transport.DNSPeerOwnerEditCheckDeletionReceipt,
			"native BIND proof local deletion receipt changed (primary=%t, same operation=%t): %s",
			primary, err == nil && sameDNSV3PlanIdentity(current, plan), errorTextOrNone(err))
	}
	_, err = record.Admit(current.Evidence)
	return err
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
