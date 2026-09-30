package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsControlRunner func(context.Context, ...string) error

type pdnsV3PropagationPlan struct {
	State     dnsEngineStateReceipt
	Primary   bool
	Legacy    bool
	Evidence  dnsPrimaryCatalogEvidence
	Changed   expectedDNSZoneAuthority
	Operation dnsV3DeletionOperation
}

// The exact accepted V3 mutation is carried from its durable zone receipt.
// Native peer proof is unavailable unless all of these fields are verified.
type dnsV3DeletionOperation struct {
	RequestID  string
	OwnerID    string
	Generation int64
	Qualifier  string
}

// dnsV3PrimaryPropagationPlan is engine-neutral durable authority evidence for
// one primary-side V3 mutation. Both managed BIND and PowerDNS must prove this
// exact catalog/member state at the peer before reporting terminal success.
type dnsV3PrimaryPropagationPlan struct {
	SourceState dnsEngineStateReceipt
	Evidence    dnsPrimaryCatalogEvidence
	Changed     expectedDNSZoneAuthority
	Legacy      bool
	Operation   dnsV3DeletionOperation
	// RefreshEvidence is set only for a native V3 PowerDNS producer. It
	// rereads the durable catalog evidence so the completion wave can follow
	// the one change the daemon makes by itself after a membership change:
	// a higher producer SOA serial (pdnsDaemonCatalogSerialAdvance).
	RefreshEvidence func(context.Context) (dnsPrimaryCatalogEvidence, error)
}

// pdnsDaemonCatalogSerialAdvance reports whether fresh differs from planned
// only by a higher producer serial. After CelikPanel's own publication
// raises the producer serial by one, PowerDNS 4.9 recomputes CATALOG-HASH on
// its next primary check and re-stamps the producer SOA serial (measured as
// the current epoch time). Identity, member set and member serials must stay
// exact; any other difference is not the daemon's and keeps the plan.
func pdnsDaemonCatalogSerialAdvance(planned, fresh dnsPrimaryCatalogEvidence) bool {
	return fresh.Serial > planned.Serial &&
		fresh.LocalIP == planned.LocalIP && fresh.PeerIP == planned.PeerIP &&
		fresh.Domain == planned.Domain &&
		slices.Equal(fresh.Members, planned.Members) &&
		slices.Equal(fresh.MemberSerials, planned.MemberSerials)
}

func trustedPDNSControl(ctx context.Context, args ...string) error {
	control, err := firstTrustedExecutable(
		[]string{"/usr/bin/pdns_control", "/usr/sbin/pdns_control"}, "pdns_control",
	)
	if err != nil {
		return errors.New("trusted PowerDNS control executable is unavailable")
	}
	output, err := serviceMutationCommand(
		ctx, control, args...,
	).CombinedOutputLimited(64 << 10)
	_ = output
	if err != nil {
		return errors.New("PowerDNS control command failed")
	}
	return nil
}

func prepareManagedPDNSV3Propagation(
	ctx context.Context,
	zone transport.DNSEngineSwitchZoneSnapshot,
	state dnsEngineStateReceipt,
	binding transport.ServiceMutationBinding,
) (pdnsV3PropagationPlan, error) {
	expected, err := expectedDNSZoneAuthorities(
		[]transport.DNSEngineSwitchZoneSnapshot{zone},
	)
	if err != nil {
		return pdnsV3PropagationPlan{}, err
	}
	evidence, primary, err := managedPDNSPrimaryCatalogEvidenceForState(ctx, state)
	if err != nil {
		return pdnsV3PropagationPlan{},
			errors.New("PowerDNS paired primary evidence is unavailable")
	}
	plan := pdnsV3PropagationPlan{
		State: state, Primary: primary, Evidence: evidence, Changed: expected[0],
		Operation: dnsV3DeletionOperation{
			RequestID:  binding.MutationRequestID,
			OwnerID:    binding.MutationOwnerID,
			Generation: zone.DesiredGeneration,
			Qualifier:  zone.ZoneQualifier,
		},
	}
	if primary && state.PairRole == `` {
		if state.PrimaryCatalogSerial != 0 {
			return pdnsV3PropagationPlan{},
				errors.New(`legacy PowerDNS primary receipt has an unexpected catalog serial`)
		}
		plan.Legacy = true
	}
	if err := preparePDNSV3PropagationAt(
		ctx, plan, trustedPDNSControl,
	); err != nil {
		return pdnsV3PropagationPlan{}, err
	}
	return plan, nil
}

// pdnsNotifyHostTarget is the notify-host destination with an explicit DNS
// port. PowerDNS 4.8.0 through 4.9.17 queues a port-less notify-host address
// as "<ip>:0", sends to port 53, and then matches the answer on address and
// port. Every answer is therefore "spurious", the NOTIFY is re-sent four
// times and the queue logs "to <ip>:0 failed after retries" although the
// secondary accepted it (PowerDNS issue 13576; upstream fix 1ec11cb0 is not
// in 4.9.17). With ":53" the queued entry matches the answer and is removed
// after the first acknowledgement. The peer is a canonical IPv4 address
// (validateDNSPrimaryCatalogEvidence); JoinHostPort also brackets IPv6.
func pdnsNotifyHostTarget(peerIP string) string {
	return net.JoinHostPort(peerIP, "53")
}

// preparePDNSV3PropagationAt runs only after the SQLite zone transaction and
// exact receipt have committed. notify-host targets the immutable /32 peer;
// retries are safe because purge and notification are idempotent.
func preparePDNSV3PropagationAt(
	ctx context.Context,
	plan pdnsV3PropagationPlan,
	run pdnsControlRunner,
) error {
	if run == nil || !serviceMutationCanonicalFQDN(plan.Changed.Domain) ||
		(!plan.Changed.Delete && plan.Changed.Serial == 0) {
		return errors.New("PowerDNS propagation plan is invalid")
	}
	if plan.Primary {
		if err := validatePDNSPrimaryPropagationPlan(plan); err != nil {
			return err
		}
	}
	runBounded := func(args ...string) error {
		commandCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), dnsProbeTimeout,
		)
		defer cancel()
		return run(commandCtx, args...)
	}
	if err := runBounded("purge", plan.Changed.Domain+"$"); err != nil {
		return dnsZoneV3RecoveryPending(errors.New("PowerDNS zone cache purge failed"))
	}
	if !plan.Primary {
		return nil
	}
	if err := runBounded("purge", plan.Evidence.Domain+"$"); err != nil {
		return dnsZoneV3RecoveryPending(errors.New("PowerDNS catalog cache purge failed"))
	}
	notifyTarget := pdnsNotifyHostTarget(plan.Evidence.PeerIP)
	if err := runBounded(
		"notify-host", plan.Evidence.Domain, notifyTarget,
	); err != nil {
		return dnsZoneV3RecoveryPending(errors.New("PowerDNS paired catalog notification failed"))
	}
	if !plan.Changed.Delete {
		if err := runBounded(
			"notify-host", plan.Changed.Domain, notifyTarget,
		); err != nil {
			return dnsZoneV3RecoveryPending(errors.New("PowerDNS paired member notification failed"))
		}
	}
	return nil
}

func validatePDNSPrimaryPropagationPlan(plan pdnsV3PropagationPlan) error {
	return validateDNSV3PrimaryPropagationPlan(dnsV3PrimaryPropagationPlan{
		Evidence:  plan.Evidence,
		Changed:   plan.Changed,
		Legacy:    plan.Legacy,
		Operation: plan.Operation,
	})
}

func validateDNSV3PrimaryPropagationPlan(plan dnsV3PrimaryPropagationPlan) error {
	if err := validateDNSPrimaryCatalogEvidence(plan.Evidence); err != nil {
		return errors.New("DNS primary propagation evidence is invalid")
	}
	if !serviceMutationCanonicalFQDN(plan.Changed.Domain) ||
		plan.Changed.Domain == plan.Evidence.Domain ||
		(!plan.Changed.Delete && plan.Changed.Serial == 0) {
		return errors.New("changed DNS authority identity is invalid")
	}
	index := -1
	for memberIndex, member := range plan.Evidence.Members {
		if member == plan.Changed.Domain {
			index = memberIndex
			break
		}
	}
	if plan.Changed.Delete {
		if index >= 0 || plan.Changed.Serial != 0 {
			return errors.New("deleted DNS member remains in durable catalog evidence")
		}
		return nil
	}
	if index < 0 || plan.Evidence.MemberSerials[index] != plan.Changed.Serial {
		return errors.New("changed DNS member differs from durable catalog evidence")
	}
	return nil
}

func completePDNSV3Propagation(
	ctx context.Context,
	plan pdnsV3PropagationPlan,
) error {
	if !plan.Primary {
		return nil
	}
	primaryPlan := dnsV3PrimaryPropagationPlan{
		SourceState: plan.State,
		Evidence:    plan.Evidence,
		Changed:     plan.Changed,
		Legacy:      plan.Legacy,
		Operation:   plan.Operation,
	}
	if !plan.Legacy && plan.State.NativeCatalogV3 == dnsengineartifact.NativeCatalogDebian49V3 {
		state := plan.State
		primaryPlan.RefreshEvidence = func(ctx context.Context) (dnsPrimaryCatalogEvidence, error) {
			evidence, primary, err := managedPDNSPrimaryCatalogEvidenceForState(ctx, state)
			if err != nil || !primary {
				return dnsPrimaryCatalogEvidence{}, errors.Join(errors.New("PowerDNS producer evidence is unavailable"), err)
			}
			return evidence, nil
		}
	}
	err := completeDNSV3PrimaryPropagation(ctx, primaryPlan)
	return dnsZoneV3RecoveryPending(err)
}

func completeDNSV3PrimaryPropagation(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
) error {
	return completeDNSV3PrimaryPropagationWithNativeAt(
		ctx, plan, probeDNSZoneSOA, probeDNSPDNSCatalogAXFR,
		probeDNSBoundPDNSCatalogAXFR, probeDNSBoundZoneAXFR,
		verifyEnrolledDNSPeerDeletion,
	)
}

// A managed BIND primary may use one explicitly owner-enrolled native BIND or
// PowerDNS secondary inspector when parent-authoritative deletion proof is unavailable.
// A PowerDNS primary uses the same strict authenticated native fallback.
func completeBINDDNSV3PrimaryPropagation(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
) error {
	return completeDNSV3PrimaryPropagationWithNativeAt(
		ctx, plan, probeDNSZoneSOA, probeDNSCatalogAXFR,
		probeDNSBoundCatalogAXFR, probeDNSBoundZoneAXFR,
		verifyEnrolledDNSPeerDeletion,
	)
}

// Select by the frozen source engine, never by the daemon answering the AXFR:
// a BIND secondary preserves the PowerDNS producer's catalog PTR names/TTLs.
func catalogAXFRProbesForSourceEngine(engine transport.DNSEngine) (dnsCatalogAXFRProbe, dnsBoundCatalogAXFRProbe, error) {
	switch engine {
	case transport.DNSEngineBIND:
		return probeDNSCatalogAXFR, probeDNSBoundCatalogAXFR, nil
	case transport.DNSEnginePowerDNS:
		return probeDNSPDNSCatalogAXFR, probeDNSBoundPDNSCatalogAXFR, nil
	default:
		return nil, nil, errors.New("DNS catalog producer engine is unavailable")
	}
}

func completeDNSV3PrimaryPropagationAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
) error {
	return completeDNSV3PrimaryPropagationWithNativeAt(ctx, plan, soa, localAXFR, peerCatalogAXFR, peerZoneAXFR, nil)
}

func completeDNSV3PrimaryPropagationWithNativeAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
	native dnsNativePeerDeletionProof,
) error {
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	// One completion wave may retry ordinary DNS observations while the peer
	// catches up, but it mints at most one durable native challenge and opens
	// at most one SSH inspection. A later owner retry is a new ledger attempt.
	nativeAttempted := false
	daemonAdvances := 0
	var nativePending error
	nativeOnce := native
	if native != nil {
		nativeOnce = func(ctx context.Context, authority dnsPeerAXFRAuthority, plan dnsV3PrimaryPropagationPlan) error {
			if nativeAttempted {
				return errors.New("native peer inspection was already attempted for this completion")
			}
			nativeAttempted = true
			raw := native(ctx, authority, plan)
			if raw == nil {
				return nil
			}
			code := pendingDNSPeerCode(raw)
			if code == "" {
				code = transport.DNSPeerPendingNativeUnknown
			}
			nativePending = pendingBINDPeer(code)
			return nativePending
		}
	}
	for {
		check, err := verifyDNSV3PrimaryPropagationCheckWithNativeAt(
			proofCtx, plan, soa, localAXFR, peerCatalogAXFR, peerZoneAXFR, nativeOnce,
		)
		if err == nil {
			return nil
		}
		if nativePending != nil {
			// One authenticated inspection is permitted per completion wave.
			// Once it is inconclusive, later DNS retries cannot repeat that
			// challenge and must not replace its reviewed reason with a
			// different final check just before the deadline.
			return errors.Join(
				fmt.Errorf("paired DNS deletion is unverified (check=%s); the peer administrator must check native zone state and DNS access, then retry verification of the same operation", dnsV3ProofNativePeer),
				nativePending,
			)
		}
		// Follow the daemon's own producer serial re-stamp, and nothing else,
		// before any native inspection binds this wave to a serial.
		if check == dnsV3ProofCatalogPair && plan.RefreshEvidence != nil && !nativeAttempted &&
			daemonAdvances < 2 {
			if fresh, refreshErr := plan.RefreshEvidence(proofCtx); refreshErr == nil &&
				pdnsDaemonCatalogSerialAdvance(plan.Evidence, fresh) {
				plan.Evidence = fresh
				daemonAdvances++
				continue
			}
		}
		select {
		case <-proofCtx.Done():
			if plan.Changed.Delete {
				reason := fmt.Errorf("paired DNS deletion is unverified (check=%s); the peer administrator must check native zone state and DNS access, then retry verification of the same operation", check)
				if check == dnsV3ProofNativePeer && nativePending != nil {
					return errors.Join(reason, nativePending)
				}
				return reason
			}
			return fmt.Errorf("paired DNS primary propagation did not converge (check=%s)", check)
		case <-time.After(250 * time.Millisecond):
		}
	}
}
func verifyPDNSV3PropagationAt(
	ctx context.Context,
	plan pdnsV3PropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
) error {
	if !plan.Primary {
		return nil
	}
	return verifyDNSV3PrimaryPropagationAt(
		ctx,
		dnsV3PrimaryPropagationPlan{
			Evidence:  plan.Evidence,
			Changed:   plan.Changed,
			Legacy:    plan.Legacy,
			Operation: plan.Operation,
		},
		soa, localAXFR, peerCatalogAXFR, peerZoneAXFR,
	)
}

type dnsV3ProofCheck string

const (
	dnsV3ProofPlan         dnsV3ProofCheck = "plan"
	dnsV3ProofCatalogPair  dnsV3ProofCheck = "catalog_pair"
	dnsV3ProofZoneTransfer dnsV3ProofCheck = "peer_zone_transfer"
	dnsV3ProofZoneSOA      dnsV3ProofCheck = "peer_zone_soa"
	dnsV3ProofNativePeer   dnsV3ProofCheck = "peer_native_zone"
	dnsV3ProofVerified     dnsV3ProofCheck = "verified"
)

func verifyDNSV3PrimaryPropagationAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
) error {
	_, err := verifyDNSV3PrimaryPropagationCheckAt(
		ctx, plan, soa, localAXFR, peerCatalogAXFR, peerZoneAXFR,
	)
	return err
}

// The fixed check identifies the proof boundary without putting probe errors,
// peer output, addresses, or other untrusted material in the pending operation.
type dnsNativePeerDeletionProof func(
	context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan,
) error

func verifyDNSV3PrimaryPropagationCheckAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
) (dnsV3ProofCheck, error) {
	return verifyDNSV3PrimaryPropagationCheckWithNativeAt(
		ctx, plan, soa, localAXFR, peerCatalogAXFR, peerZoneAXFR, nil,
	)
}

// An optional native peer proof can close the parentless REFUSED case.
// The production callback is fail-closed unless the owner enrolled a pinned
// inspector and the current V3 runtime retains exact durable authority.
func verifyDNSV3PrimaryPropagationCheckWithNativeAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
	native dnsNativePeerDeletionProof,
) (dnsV3ProofCheck, error) {
	if err := validateDNSV3PrimaryPropagationPlan(plan); err != nil {
		return dnsV3ProofPlan, err
	}
	var authority dnsPeerAXFRAuthority
	var err error
	if plan.Legacy {
		authority, err = verifyDNSLegacyPrimaryPairReadyAuthorityAt(
			ctx, plan.Evidence, soa, peerCatalogAXFR,
		)
	} else {
		authority, err = verifyDNSPrimaryPairReadyAuthorityAt(
			ctx, plan.Evidence, soa, localAXFR, peerCatalogAXFR,
		)
	}
	if err != nil {
		return dnsV3ProofCatalogPair, err
	}
	if !plan.Changed.Delete {
		return dnsV3ProofVerified, nil
	}
	if err := verifyPeerZoneNoTransferAt(
		ctx, authority, plan.Changed.Domain, peerZoneAXFR,
	); err != nil {
		return dnsV3ProofZoneTransfer, err
	}
	// An AXFR refusal alone can also mean a still-loaded zone denies transfer.
	// Require parent-negative SOA on both transports, or empty REFUSED plus an
	// independently authenticated native loaded-zone inspection. Contradictory
	// positive DNS answers never reach the native fallback.
	observation, err := observeDeletedDNSZoneAt(
		ctx, authority.sourceIP, authority.peerIP, plan.Changed.Domain, soa,
	)
	if err != nil {
		return dnsV3ProofZoneSOA, err
	}
	if observation == dnsDeletedZoneEmptyRefused {
		if native == nil {
			return dnsV3ProofZoneSOA, errors.New("peer native deletion proof is not configured")
		}
		if err := native(ctx, authority, plan); err != nil {
			code := pendingDNSPeerCode(err)
			if code == "" {
				code = transport.DNSPeerPendingNativeUnknown
			}
			return dnsV3ProofNativePeer, pendingBINDPeer(code)
		}
	}
	return dnsV3ProofVerified, nil
}
func verifyPeerZoneNoTransferAt(
	ctx context.Context,
	authority dnsPeerAXFRAuthority,
	domain string,
	probe dnsBoundZoneAXFRProbe,
) error {
	// The opaque authority is issued only after an exact AXFR of this peer's
	// catalog from the same source address. A negative zone AXFR rules out a
	// transfer on that path, not a loaded zone with a different transfer ACL.
	// The caller must also prove authoritative negative SOA over UDP and TCP.
	if probe == nil || authority.catalogSerial == 0 ||
		!canonicalPairReadinessIPv4(authority.sourceIP) ||
		!canonicalPairReadinessIPv4(authority.peerIP) ||
		authority.sourceIP == authority.peerIP ||
		!serviceMutationCanonicalFQDN(authority.catalog) ||
		!serviceMutationCanonicalFQDN(domain) ||
		domain == authority.catalog {
		return errors.New("DNS peer deletion AXFR authority is invalid")
	}
	state, err := probe(
		ctx, authority.sourceIP, authority.peerIP, domain,
	)
	if err != nil || state != dnsZoneAXFRNoTransfer {
		return errors.New("peer DNS zone transfer is still present or unverified; check the peer native zone and transfer policy")
	}
	return nil
}

func verifyDeletedDNSZoneAt(
	ctx context.Context,
	source, address, domain string,
	probe dnsZoneSOAProbe,
) error {
	state, err := observeDeletedDNSZoneAt(ctx, source, address, domain, probe)
	if err != nil || state != dnsDeletedZoneParentNegative {
		return errors.New("peer DNS zone removal is unverified; check the peer native zone and query access, then verify the same operation")
	}
	return nil
}

type dnsDeletedZoneSOAState uint8

const (
	dnsDeletedZoneUnknown dnsDeletedZoneSOAState = iota
	dnsDeletedZoneParentNegative
	dnsDeletedZoneEmptyRefused
)

// observeDeletedDNSZoneAt distinguishes an authoritative parent-negative
// answer from BIND's normal empty REFUSED for a removed parentless zone. A
// REFUSED result is never sufficient by itself: a separate authenticated
// native peer observation must prove that the exact zone is unloaded.
func observeDeletedDNSZoneAt(
	ctx context.Context,
	source, address, domain string,
	probe dnsZoneSOAProbe,
) (dnsDeletedZoneSOAState, error) {
	if probe == nil || !canonicalPairReadinessIPv4(source) ||
		!canonicalPairReadinessIPv4(address) || source == address ||
		!serviceMutationCanonicalFQDN(domain) {
		return dnsDeletedZoneUnknown, errors.New("deleted DNS zone proof identity is invalid")
	}
	state := dnsDeletedZoneParentNegative
	for _, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil || result.LocalIP != source {
			return dnsDeletedZoneUnknown, errors.New("peer DNS zone response is unavailable or unbound")
		}
		if validDeletedDNSZoneProof(domain, result) {
			continue
		}
		if result.RCode == dnsRCodeRefused && !result.Authoritative &&
			result.AnswerCount == 0 && len(result.SOASerials) == 0 &&
			len(result.AnswerSOAOwners) == 0 && len(result.AuthoritySOAOwners) == 0 {
			state = dnsDeletedZoneEmptyRefused
			continue
		}
		return dnsDeletedZoneUnknown, errors.New("peer DNS zone returned a contradictory or unverifiable answer")
	}
	return state, nil
}
