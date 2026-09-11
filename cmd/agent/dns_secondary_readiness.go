package main

import (
	"context"
	"errors"
	"slices"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Consumer readiness is deliberately separate from primary publishing
// readiness. The secondary proves that the current primary catalog and every
// member have transferred; this grants no panel-local zone write authority.
func verifyDNSSecondaryPairReadyAt(ctx context.Context, localIP, peerIP string, soa dnsZoneSOAProbe, axfr dnsBoundCatalogAXFRProbe) error {
	if !canonicalPairReadinessIPv4(localIP) || !canonicalPairReadinessIPv4(peerIP) || localIP == peerIP || soa == nil || axfr == nil {
		return errors.New("DNS secondary pair identity is invalid")
	}
	domain, err := binddns.CatalogDomain(peerIP)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	catalog, err := axfr(ctx, localIP, peerIP, domain)
	if err != nil || catalog.Serial == 0 {
		return errors.New("DNS primary catalog cannot be read from the configured secondary")
	}
	localSerial, localErr := exactDNSZoneSerialAtWithProbe(ctx, localIP, domain, soa)
	peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(ctx, peerIP, domain, soa)
	if localErr != nil || peerErr != nil || localSerial != catalog.Serial || peerSerial != catalog.Serial {
		return errors.New("DNS secondary catalog is not current")
	}
	for _, member := range catalog.Members {
		localSerial, localErr := exactDNSZoneSerialAtWithProbe(ctx, localIP, member, soa)
		peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(ctx, peerIP, member, soa)
		if localErr != nil || peerErr != nil || localSerial != peerSerial {
			return errors.New("DNS secondary is missing a current authoritative member")
		}
	}
	current, err := axfr(ctx, localIP, peerIP, domain)
	if err != nil || current.Serial != catalog.Serial || !slices.Equal(current.Members, catalog.Members) {
		return errors.New("DNS primary catalog changed during secondary verification")
	}
	return nil
}

func bindSecondaryPairReadyForState(ctx context.Context, root string, tree binddns.VerifiedTree, state dnsEngineStateReceipt) (bool, error) {
	pairing := tree.CurrentReceipt().Pairing
	if pairing == nil || pairing.Role != binddns.PairRoleSecondary {
		return false, nil
	}
	if _, err := bindStateTreePairContract(root, state, tree, false, true, false); err != nil {
		return false, err
	}
	if err := verifyDNSSecondaryPairReadyAt(ctx, pairing.LocalIP, pairing.PeerIP, probeDNSZoneSOA, probeDNSBoundCatalogAXFR); err != nil {
		return false, err
	}
	return true, nil
}

func powerDNSSecondaryPairReady(ctx context.Context, state dnsEngineStateReceipt) (bool, error) {
	if state.PairRole != transport.DNSPairRoleSecondary {
		return false, nil
	}
	identity, enabled, err := managedPDNSCatalogIdentityForState(state)
	if err != nil || !enabled {
		return false, err
	}
	if err := verifyDNSSecondaryPairReadyAt(ctx, identity.LocalIP, identity.PeerIP, probeDNSZoneSOA, probeDNSBoundCatalogAXFR); err != nil {
		return false, err
	}
	return true, nil
}
