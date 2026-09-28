package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A parentless deleted zone normally returns empty REFUSED from PowerDNS. Only
// the fresh native V3 primary may combine that observation with its exact
// durable tombstone. All other engine and topology paths retain the strict
// authoritative parent-negative rule.
func verifyPDNSV3PublishedZoneAuthority(
	ctx context.Context,
	state dnsEngineStateReceipt,
	zone transport.DNSEngineSwitchZoneSnapshot,
	binding transport.ServiceMutationBinding,
) error {
	if !zone.Delete || state.Mode != transport.DNSEngineSwitchModeSwitch ||
		state.Engine != transport.DNSEnginePowerDNS ||
		state.PairRole != transport.DNSPairRolePrimary ||
		state.NativeCatalogV3 != dnsengineartifact.NativeCatalogDebian49V3 {
		return verifyDNSZoneManifestAuthority(ctx, []transport.DNSEngineSwitchZoneSnapshot{zone})
	}
	if err := requireHostOwnedDNSPairAddress(state.PairLocalIP); err != nil {
		return err
	}
	verifyTombstone := func() error {
		snapshot, exact, err := readPDNSV3ZoneSnapshot(
			ctx, pdnsDBPath(), state, zone.Domain, zone.ZoneQualifier, binding,
		)
		if err != nil {
			return fmt.Errorf("verify PowerDNS V3 deletion receipt: %w", err)
		}
		if !exact || !snapshot.Delete || snapshot.Domain != zone.Domain ||
			snapshot.DesiredGeneration != zone.DesiredGeneration ||
			snapshot.ZoneQualifier != zone.ZoneQualifier ||
			snapshot.ZoneType != zone.ZoneType ||
			!reflect.DeepEqual(snapshot.Records, zone.Records) {
			return errors.New("PowerDNS V3 deletion receipt or database tombstone is not exact")
		}
		return nil
	}
	if err := verifyTombstone(); err != nil {
		return err
	}
	if err := verifyFreshPDNSV3DeletedZoneAt(
		ctx, state.PairLocalIP, zone.Domain, probeDNSZoneSOA,
	); err != nil {
		return err
	}
	if err := verifyTombstone(); err != nil {
		return err
	}
	after, exists, err := readDNSEngineState()
	if err != nil || !exists || !reflect.DeepEqual(after, state) {
		if err != nil {
			return err
		}
		return errors.New("PowerDNS engine state changed during deletion proof")
	}
	return nil
}

func verifyFreshPDNSV3DeletedZoneAt(
	ctx context.Context, address, domain string, probe dnsZoneSOAProbe,
) error {
	if probe == nil || !canonicalPairReadinessIPv4(address) ||
		!serviceMutationCanonicalFQDN(domain) {
		return errors.New("PowerDNS V3 local deletion proof identity is invalid")
	}
	for _, network := range []string{"udp", "tcp"} {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := probe(probeCtx, network, address, domain)
		cancel()
		if err != nil || result.LocalIP != address {
			return fmt.Errorf("PowerDNS V3 local deletion response is unavailable or unbound over %s", network)
		}
		if validDeletedDNSZoneProof(domain, result) {
			continue
		}
		if result.RCode == dnsRCodeRefused && !result.Authoritative &&
			result.AnswerCount == 0 && len(result.SOASerials) == 0 &&
			len(result.AnswerSOAOwners) == 0 && len(result.AuthoritySOAOwners) == 0 {
			continue
		}
		return fmt.Errorf("PowerDNS V3 deleted zone returned a contradictory or unverifiable answer over %s", network)
	}
	return nil
}
