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
	Evidence dnsPrimaryCatalogEvidence
	Changed  expectedDNSZoneAuthority
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
) (pdnsV3PropagationPlan, error) {
	expected, err := expectedDNSZoneAuthorities(
		[]transport.DNSEngineSwitchZoneSnapshot{zone},
	)
	if err != nil {
		return pdnsV3PropagationPlan{}, err
	}
	evidence, primary, err := managedPDNSPrimaryCatalogEvidence(ctx)
	if err != nil {
		return pdnsV3PropagationPlan{},
			errors.New("PowerDNS paired primary evidence is unavailable")
	}
	plan := pdnsV3PropagationPlan{
		Primary: primary, Evidence: evidence, Changed: expected[0],
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
		return errors.New("PowerDNS zone cache purge failed")
	}
	if !plan.Primary {
		return nil
	}
	if err := runBounded("purge", plan.Evidence.Domain+"$"); err != nil {
		return errors.New("PowerDNS catalog cache purge failed")
	}
	if err := runBounded(
		"notify-host", plan.Evidence.Domain, plan.Evidence.PeerIP,
	); err != nil {
		return errors.New("PowerDNS paired catalog notification failed")
	}
	if !plan.Changed.Delete {
		if err := runBounded(
			"notify-host", plan.Changed.Domain, plan.Evidence.PeerIP,
		); err != nil {
			return errors.New("PowerDNS paired member notification failed")
		}
	}
	return nil
}

func validatePDNSPrimaryPropagationPlan(plan pdnsV3PropagationPlan) error {
	if err := validateDNSPrimaryCatalogEvidence(plan.Evidence); err != nil {
		return errors.New("PowerDNS primary propagation evidence is invalid")
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
			return errors.New("PowerDNS deleted member remains in durable catalog evidence")
		}
		return nil
	}
	if index < 0 || plan.Evidence.MemberSerials[index] != plan.Changed.Serial {
		return errors.New("PowerDNS changed member differs from durable catalog evidence")
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
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	for {
		err := verifyPDNSV3PropagationAt(
			proofCtx, plan, probeDNSZoneSOA, probeDNSCatalogAXFR,
		)
		if err == nil {
			return nil
		}
		select {
		case <-proofCtx.Done():
			if plan.Changed.Delete {
				return errors.New("PowerDNS paired deletion did not converge")
			}
			return errors.New("PowerDNS paired primary propagation did not converge")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func verifyPDNSV3PropagationAt(
	ctx context.Context,
	plan pdnsV3PropagationPlan,
	soa dnsZoneSOAProbe,
	axfr dnsCatalogAXFRProbe,
) error {
	if !plan.Primary {
		return nil
	}
	if err := validatePDNSPrimaryPropagationPlan(plan); err != nil {
		return err
	}
	if err := verifyDNSPrimaryPairReadyAt(ctx, plan.Evidence, soa, axfr); err != nil {
		return err
	}
	if !plan.Changed.Delete {
		return nil
	}
	return verifyDeletedDNSZoneAt(
		ctx, plan.Evidence.PeerIP, plan.Changed.Domain, soa,
	)
}

func verifyDeletedDNSZoneAt(
	ctx context.Context,
	address, domain string,
	probe dnsZoneSOAProbe,
) error {
	if probe == nil || !canonicalPairReadinessIPv4(address) ||
		!serviceMutationCanonicalFQDN(domain) {
		return errors.New("PowerDNS deleted-zone proof identity is invalid")
	}
	for _, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil || !validDeletedDNSZoneProof(domain, result) {
			return errors.New("PowerDNS deleted zone remains served by the peer")
		}
	}
	return nil
}
