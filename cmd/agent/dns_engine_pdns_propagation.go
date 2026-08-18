package main

import (
	"context"
	"errors"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type pdnsControlRunner func(context.Context, ...string) error

type pdnsV3PropagationPlan struct {
	Primary  bool
	Legacy   bool
	Evidence dnsPrimaryCatalogEvidence
	Changed  expectedDNSZoneAuthority
}

// dnsV3PrimaryPropagationPlan is engine-neutral durable authority evidence for
// one primary-side V3 mutation. Both managed BIND and PowerDNS must prove this
// exact catalog/member state at the peer before reporting terminal success.
type dnsV3PrimaryPropagationPlan struct {
	Evidence dnsPrimaryCatalogEvidence
	Changed  expectedDNSZoneAuthority
	Legacy   bool
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
		Primary: primary, Evidence: evidence, Changed: expected[0],
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
	if err := runBounded(
		"notify-host", plan.Evidence.Domain, plan.Evidence.PeerIP,
	); err != nil {
		return dnsZoneV3RecoveryPending(errors.New("PowerDNS paired catalog notification failed"))
	}
	if !plan.Changed.Delete {
		if err := runBounded(
			"notify-host", plan.Changed.Domain, plan.Evidence.PeerIP,
		); err != nil {
			return dnsZoneV3RecoveryPending(errors.New("PowerDNS paired member notification failed"))
		}
	}
	return nil
}

func validatePDNSPrimaryPropagationPlan(plan pdnsV3PropagationPlan) error {
	return validateDNSV3PrimaryPropagationPlan(dnsV3PrimaryPropagationPlan{
		Evidence: plan.Evidence,
		Changed:  plan.Changed,
		Legacy:   plan.Legacy,
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
	err := completeDNSV3PrimaryPropagation(ctx, dnsV3PrimaryPropagationPlan{
		Evidence: plan.Evidence,
		Changed:  plan.Changed,
		Legacy:   plan.Legacy,
	})
	return dnsZoneV3RecoveryPending(err)
}

func completeDNSV3PrimaryPropagation(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
) error {
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for {
		err := verifyDNSV3PrimaryPropagationAt(
			proofCtx, plan, probeDNSZoneSOA, probeDNSCatalogAXFR,
			probeDNSBoundCatalogAXFR, probeDNSBoundZoneAXFR,
		)
		if err == nil {
			return nil
		}
		select {
		case <-proofCtx.Done():
			if plan.Changed.Delete {
				return errors.New("paired DNS deletion did not converge")
			}
			return errors.New("paired DNS primary propagation did not converge")
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
			Evidence: plan.Evidence,
			Changed:  plan.Changed,
			Legacy:   plan.Legacy,
		},
		soa, localAXFR, peerCatalogAXFR, peerZoneAXFR,
	)
}

func verifyDNSV3PrimaryPropagationAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerCatalogAXFR dnsBoundCatalogAXFRProbe,
	peerZoneAXFR dnsBoundZoneAXFRProbe,
) error {
	if err := validateDNSV3PrimaryPropagationPlan(plan); err != nil {
		return err
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
		return err
	}
	if !plan.Changed.Delete {
		return nil
	}
	return verifyPeerDeletedDNSZoneAt(
		ctx, authority, plan.Changed.Domain, peerZoneAXFR,
	)
}

func verifyPeerDeletedDNSZoneAt(
	ctx context.Context,
	authority dnsPeerAXFRAuthority,
	domain string,
	probe dnsBoundZoneAXFRProbe,
) error {
	// The opaque authority is issued only after this source address completed
	// an exact AXFR of the same peer's catalog. Managed BIND secondaries expose
	// that catalog and every member through one inherited peer-only ACL;
	// managed PowerDNS consumers use the same peer-only allow-axfr-ips value.
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
	if err != nil || state != dnsZoneAXFRAbsent {
		return errors.New("deleted DNS zone remains served by the peer")
	}
	return nil
}

func verifyDeletedDNSZoneAt(
	ctx context.Context,
	address, domain string,
	probe dnsZoneSOAProbe,
) error {
	if probe == nil || !canonicalPairReadinessIPv4(address) ||
		!serviceMutationCanonicalFQDN(domain) {
		return errors.New("deleted DNS zone proof identity is invalid")
	}
	for _, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil || !validDeletedDNSZoneProof(domain, result) {
			return errors.New("deleted DNS zone remains served by the peer")
		}
	}
	return nil
}
