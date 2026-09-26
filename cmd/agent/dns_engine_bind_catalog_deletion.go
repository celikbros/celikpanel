package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

// The primary's receipt-addressed local catalog AXFR binds a deleted member to
// the exact durable generation. A bare REFUSED is never enough: an ACL could
// refuse a still-loaded zone. The catalog plus two empty SOA refusals and the
// verified tree's absent member establish the local BIND transition.
func verifyBINDCatalogDeletionAt(
	ctx context.Context, address, domain string,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe, axfr dnsCatalogAXFRProbe,
) error {
	if soa == nil || axfr == nil || address != evidence.LocalIP ||
		validateDNSPrimaryCatalogEvidence(evidence) != nil {
		return errors.New("BIND deletion catalog proof is unavailable")
	}
	for _, member := range evidence.Members {
		if member == domain {
			return errors.New("deleted BIND zone remains in the durable catalog")
		}
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	var last error
	for {
		last = verifyBINDCatalogDeletionOnce(proofCtx, address, domain, evidence, soa, axfr)
		if last == nil {
			return nil
		}
		select {
		case <-proofCtx.Done():
			return fmt.Errorf("BIND zone %s catalog deletion did not converge: %w: %v", domain, proofCtx.Err(), last)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func verifyBINDCatalogDeletionOnce(
	ctx context.Context, address, domain string,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe, axfr dnsCatalogAXFRProbe,
) error {
	catalogCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
	actual, err := axfr(catalogCtx, address, evidence.Domain)
	cancel()
	if err != nil || actual.Serial != evidence.Serial || !slices.Equal(actual.Members, evidence.Members) {
		return errors.New("local BIND catalog does not match the deletion receipt")
	}
	for _, network := range []string{"udp", "tcp"} {
		queryCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		result, err := soa(queryCtx, network, address, domain)
		cancel()
		if err != nil {
			return fmt.Errorf("removed BIND zone %s SOA probe over %s failed: %w", domain, network, err)
		}
		if validDeletedDNSZoneProof(domain, result) {
			continue
		}
		if result.RCode != dnsRCodeRefused || result.Authoritative ||
			result.AnswerCount != 0 || len(result.SOASerials) != 0 ||
			len(result.AnswerSOAOwners) != 0 || len(result.AuthoritySOAOwners) != 0 {
			return fmt.Errorf("removed BIND zone %s did not return empty REFUSED over %s", domain, network)
		}
	}
	return nil
}

func verifyBINDV3AuthoritiesForTree(
	ctx context.Context, tree binddns.VerifiedTree,
	expected []expectedDNSZoneAuthority,
) error {
	evidence, primary, err := bindPrimaryCatalogEvidence(tree)
	if err != nil {
		return err
	}
	if !primary {
		return verifyBINDV3Authorities(ctx, expected)
	}
	if err := requireHostOwnedDNSPairAddress(evidence.LocalIP); err != nil {
		return err
	}
	address := evidence.LocalIP
	for _, zone := range expected {
		if zone.Delete {
			if err := verifyBINDCatalogDeletionAt(ctx, address, zone.Domain,
				evidence, probeDNSZoneSOA, probeDNSCatalogAXFR); err != nil {
				return err
			}
		} else if err := verifyDNSZoneAuthoritiesAt(ctx, address,
			[]expectedDNSZoneAuthority{zone}, probeDNSZoneSOA); err != nil {
			return err
		}
	}
	return nil
}

func verifyBINDV3ZoneManifestAuthorityForTree(
	ctx context.Context, tree binddns.VerifiedTree,
	zones []transport.DNSEngineSwitchZoneSnapshot,
) error {
	expected, err := expectedDNSZoneAuthorities(zones)
	if err != nil {
		return err
	}
	return verifyBINDV3AuthoritiesForTree(ctx, tree, expected)
}
