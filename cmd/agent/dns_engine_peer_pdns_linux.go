//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerenrollment"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/pdnspeerenrollment"
	"github.com/alicelik/celikpanel/internal/pdnspeerjournal"
	"github.com/alicelik/celikpanel/internal/pdnspeerproof"
	"github.com/alicelik/celikpanel/internal/pdnspeertransport"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func pdnsPeerEnrollmentAbsent() bool {
	_, err := pdnspeerenrollment.Read()
	return pdnspeerenrollment.IsCode(err, pdnspeerenrollment.Disabled)
}

type nativePeerEngine string

const (
	nativePeerBIND nativePeerEngine = "bind"
	nativePeerPDNS nativePeerEngine = "pdns"
)

func chooseNativePeerEngineAt(authority dnsPeerAXFRAuthority,
	readBIND func() (dnspeerenrollment.Snapshot, error),
	readPDNS func() (pdnspeerenrollment.Snapshot, error),
) (nativePeerEngine, error) {
	bind, bindErr := readBIND()
	pdns, pdnsErr := readPDNS()
	bindMissing := dnspeerenrollment.IsCode(bindErr, dnspeerenrollment.Disabled)
	pdnsMissing := pdnspeerenrollment.IsCode(pdnsErr, pdnspeerenrollment.Disabled)
	if bindErr != nil && !bindMissing || pdnsErr != nil && !pdnsMissing {
		return "", pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	bindMatch := bindErr == nil && bind.Record.PrimaryIP == authority.sourceIP &&
		bind.Record.PeerIP == authority.peerIP && bind.Record.CatalogName == authority.catalog &&
		bind.Record.View == dnspeerproof.DefaultView
	pdnsMatch := pdnsErr == nil && pdns.Record.PrimaryIP == authority.sourceIP &&
		pdns.Record.PeerIP == authority.peerIP && pdns.Record.CatalogName == authority.catalog &&
		pdns.Record.View == dnspeerproof.DefaultView && pdns.Record.Engine == "pdns"
	if bindErr == nil && !bindMatch || pdnsErr == nil && !pdnsMatch {
		return "", pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	if bindMissing && pdnsMissing {
		return "", pendingBINDPeer(transport.DNSPeerPendingEnrollmentRequired)
	}
	if bindMatch && pdnsMatch {
		// The local pair does not attest which native engine runs at the peer.
		// Two owner enrollments must never choose one by priority.
		return "", pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	if bindMatch {
		return nativePeerBIND, nil
	}
	if pdnsMatch {
		return nativePeerPDNS, nil
	}
	return "", pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
}

func verifyEnrolledDNSPeerDeletion(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) error {
	engine, err := chooseNativePeerEngineAt(authority, dnspeerenrollment.Read, pdnspeerenrollment.Read)
	if err != nil {
		return err
	}
	switch engine {
	case nativePeerBIND:
		return verifyEnrolledBINDPeerDeletion(ctx, authority, plan)
	case nativePeerPDNS:
		return verifyEnrolledPDNSPeerDeletion(ctx, authority, plan)
	default:
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
}

// pdnsInspectionPendingCode maps an incomplete PowerDNS inspection the same way
// the BIND path does: the pending code, plus the reviewed reason the
// authenticated PowerDNS inspector reported, if any.
func pdnsInspectionPendingCode(err error) string {
	return inspectionPendingCode(pdnspeertransport.InspectorReason(err))
}

func mintPDNSPeerDeletionRequest(plan dnsV3PrimaryPropagationPlan, authority dnsPeerAXFRAuthority,
	peerIdentity string, attempt uint64, now time.Time) (pdnspeerproof.RequestV1, error) {
	base, err := mintBINDPeerDeletionRequest(plan, authority, peerIdentity, attempt, now, rand.Reader)
	if err != nil {
		return pdnspeerproof.RequestV1{}, err
	}
	request := pdnspeerproof.RequestV1(base)
	request.Schema = pdnspeerproof.RequestSchemaV1
	if err := request.Validate(); err != nil {
		return pdnspeerproof.RequestV1{}, errors.New("PowerDNS challenge is invalid")
	}
	return request, nil
}

// verifyEnrolledPDNSPeerDeletion is the PowerDNS-secondary form of
// verifyEnrolledBINDPeerDeletion, with the same steps and bounds
// (dnsPeerProofSteps). ctx is the request context, never the completion
// wave's DNS probe context.
func verifyEnrolledPDNSPeerDeletion(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) (result error) {
	if !plan.Changed.Delete || plan.Legacy || ctx.Err() != nil {
		return errors.New("native PowerDNS deletion proof is unavailable")
	}
	tracker, _ := ctx.Value(serviceMutationExecutionTrackerKey{}).(*serviceMutationExecutionTracker)
	if tracker == nil || tracker.manager == nil || tracker.runtime == nil {
		return errors.New("native PowerDNS proof lacks an active mutation attempt")
	}
	m, runtime := tracker.manager, tracker.runtime
	run := newDNSPeerProofRun(ctx, dnsPeerProofSteps, plan.Changed.Domain, "PowerDNS")
	defer func() { run.finish(result) }()
	var (
		attempt           uint64
		localCatalogProbe dnsCatalogAXFRProbe
		peerCatalogProbe  dnsBoundCatalogAXFRProbe
		record            *dnsRecordedProducerCatalog
		enrollment        pdnspeerenrollment.Snapshot
		checks            *nativePeerCurrentChecks
		request           pdnspeerproof.RequestV1
		response          pdnspeerproof.ResponseV1
		authenticated     pdnspeerproof.PeerAuthentication
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
		if enrollment, err = pdnspeerenrollment.Read(); err != nil {
			if pdnspeerenrollment.IsCode(err, pdnspeerenrollment.Disabled) {
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
					return errors.New("PowerDNS proof lost its active operation attempt")
				}
				return nil
			},
			enrollment: func() error {
				if pdnspeerenrollment.Recheck(enrollment) != nil {
					return errors.New("PowerDNS peer enrollment changed")
				}
				if _, e := dnspeerenrollment.Read(); !dnspeerenrollment.IsCode(e, dnspeerenrollment.Disabled) {
					return errors.New("BIND peer enrollment appeared or became unsafe")
				}
				return nil
			},
			local: func(ctx context.Context) error { return recheckNativePeerLocalEvidence(ctx, plan, record) },
		}
		if err := checks.at(ctx)(); err != nil {
			return err
		}
		if err := checks.journalOp(ctx, "previous challenge reconciliation", func(verify func() error) error {
			return reconcileHistoricalPDNSPeerChallenge(m, plan, verify)
		}); err != nil {
			return err
		}
		next := uint64(1)
		previous, err := pdnspeerjournal.Read()
		if err == nil {
			if previous.Request.Attempt == ^uint64(0) {
				return pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown,
					errors.New("challenge journal attempt counter is exhausted"))
			}
			next = previous.Request.Attempt + 1
		} else if !pdnspeerjournal.IsCode(err, pdnspeerjournal.Missing) {
			return pendingDNSPeerCause(transport.DNSPeerPendingJournalUnknown,
				fmt.Errorf("challenge journal read: %w", err))
		}
		// See verifyEnrolledBINDPeerDeletion: a re-stamp admitted since the
		// wave proved the pair is proved again before any challenge exists.
		if plan, err = nativePeerChallengePlan(plan, record, authority); err != nil {
			return err
		}
		if request, err = mintPDNSPeerDeletionRequest(plan, authority,
			enrollment.Record.HostKeySHA256, next, time.Now()); err != nil {
			return err
		}
		run.challengeMinted(time.Unix(request.ExpiresAtUnix, 0))
		return nil
	}); err != nil {
		return err
	}
	if err := run.step(dnsPeerProofStepChallengeWrite, func(ctx context.Context) error {
		if err := checks.journalOp(ctx, "challenge publish", func(verify func() error) error {
			return pdnspeerjournal.Publish(request, enrollment.RecordSHA256, attempt, verify)
		}); err != nil {
			return err
		}
		if err := pdnspeerenrollment.Recheck(enrollment); err != nil {
			return pendingDNSPeerCause(transport.DNSPeerPendingEnrollmentChanged, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := run.stepWithin(dnsPeerProofStepExchange, enrollment.Transport.Timeout, func(ctx context.Context) error {
		var err error
		response, authenticated, err = pdnspeertransport.Inspect(ctx, enrollment.Transport, request, pdnspeertransport.SSH{})
		if err != nil {
			return pendingDNSPeerCause(pdnsInspectionPendingCode(err), err)
		}
		return nil
	}); err != nil {
		return err
	}
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
		_, err := pdnspeerproof.Verify(request, response, authenticated, time.Now(), func(digest string) bool {
			// The answer is accepted: consume-once runs under the fresh
			// consume context, never under an earlier step's clock.
			accepted = true
			consumeErr = checks.journalOp(run.accepted(), "consume-once", func(verify func() error) error {
				return pdnspeerjournal.ConsumeOnce(request, enrollment.RecordSHA256,
					digest, attempt, verify)
			})
			return consumeErr == nil
		})
		// The PowerDNS inspector's outcome fields, bounded (pair 6 could only
		// infer them).
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

func reconcileHistoricalPDNSPeerChallenge(m *serviceMutationManager, plan dnsV3PrimaryPropagationPlan, verifyCurrent func() error) error {
	return reconcileHistoricalPDNSPeerChallengeAt(plan, verifyCurrent,
		m.loadLedgerFromDisk, pdnspeerjournal.Read, pdnspeerjournal.Retire)
}

func reconcileHistoricalPDNSPeerChallengeAt(plan dnsV3PrimaryPropagationPlan, verifyCurrent func() error,
	loadLedger func() (serviceMutationLedger, error),
	read func() (pdnspeerjournal.RecordV1, error),
	retire func(pdnspeerproof.RequestV1, string, string, uint64, pdnspeerjournal.VerifyCurrent) error,
) error {
	if verifyCurrent == nil || loadLedger == nil || read == nil || retire == nil {
		return errors.New("PowerDNS challenge reconciliation unavailable")
	}
	previous, err := read()
	if pdnspeerjournal.IsCode(err, pdnspeerjournal.Missing) {
		return nil
	}
	if err != nil {
		return errors.New("PowerDNS challenge journal unreadable")
	}
	if previous.Request.MutationRequestID == plan.Operation.RequestID &&
		previous.Request.MutationOwnerID == plan.Operation.OwnerID &&
		previous.Request.DeletedZone == plan.Changed.Domain &&
		previous.Request.DeletionQualifier == plan.Operation.Qualifier {
		return nil
	}
	verifyTerminal := func() error {
		if verifyCurrent() != nil {
			return errors.New("current PowerDNS operation changed")
		}
		ledger, err := loadLedger()
		if err != nil || !historicalPDNSPeerChallengeMatchesLedger(previous, &ledger) {
			return errors.New("historical PowerDNS operation is not terminal")
		}
		return nil
	}
	if verifyTerminal() != nil {
		return errors.New("historical PowerDNS challenge is unverified")
	}
	if retire(previous.Request, previous.EnrollmentSHA256, previous.RequestSHA256,
		previous.LedgerAttempt, verifyTerminal) != nil {
		return errors.New("historical PowerDNS challenge retirement failed")
	}
	if _, err := read(); !pdnspeerjournal.IsCode(err, pdnspeerjournal.Missing) {
		return errors.New("historical PowerDNS challenge retirement is unverified")
	}
	return verifyCurrent()
}

func historicalPDNSPeerChallengeMatchesLedger(previous pdnspeerjournal.RecordV1, ledger *serviceMutationLedger) bool {
	if previous.Validate() != nil || previous.LedgerAttempt == 0 ||
		previous.LedgerAttempt > uint64(^uint(0)>>1) {
		return false
	}
	request := previous.Request
	return servicemutationledger.ClassifyDNSZoneV3PeerOperation(ledger,
		request.MutationRequestID, request.MutationOwnerID, request.DeletedZone,
		request.DeletionQualifier, int(previous.LedgerAttempt)) ==
		servicemutationledger.DNSZoneV3PeerOperationHistoricalPublished
}

func retireTerminalPDNSPeerChallenge(ledgerPath, lockPath string, job *ServiceMutationJob) error {
	if job == nil || job.Kind != "dns_zone_sync" {
		return nil
	}
	record, err := pdnspeerjournal.Read()
	if pdnspeerjournal.IsCode(err, pdnspeerjournal.Missing) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Request.MutationRequestID != job.RequestID ||
		record.Request.MutationOwnerID != job.OwnerID ||
		record.Request.DeletedZone != job.Target ||
		record.Request.DeletionQualifier != job.PackageName ||
		uint64(job.Attempt) != record.LedgerAttempt {
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
			hostmutationlock.VerifyInherited(serviceMutationLedgerPublicationLockFile(lockPath), int(lock.publication.file.Fd()), serviceMutationLockOwner()) != nil {
			return errors.New("terminal PowerDNS challenge lost its external locks")
		}
		ledger, err := loadExactTerminalBINDPeerLedger(ledgerPath)
		if err != nil || servicemutationledger.ClassifyDNSZoneV3PeerOperation(&ledger,
			job.RequestID, job.OwnerID, job.Target, job.PackageName, job.Attempt) !=
			servicemutationledger.DNSZoneV3PeerOperationHistoricalPublished {
			return errors.New("terminal PowerDNS challenge lost exact ledger result")
		}
		return nil
	}
	return pdnspeerjournal.Retire(record.Request, record.EnrollmentSHA256,
		record.RequestSHA256, record.LedgerAttempt, verify)
}
