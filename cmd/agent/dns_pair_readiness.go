package main

import (
	"context"
	"errors"
	"net"
	"slices"
	"sort"

	"github.com/alicelik/celikpanel/internal/binddns"
)

type dnsPrimaryCatalogEvidence struct {
	LocalIP string
	PeerIP  string
	Domain  string
	Serial  uint32
	Members []string
	// MemberSerials is aligned with Members and comes from durable,
	// transactionally verified authority data. Live local/peer equality alone
	// is insufficient: both servers may still be serving the same stale zone.
	MemberSerials []uint32
}

type dnsPeerAXFRAuthority struct {
	sourceIP      string
	peerIP        string
	catalog       string
	catalogSerial uint32
}

// verifyDNSPrimaryPairReadyAt proves that the configured peer has consumed the
// primary's exact catalog and exposes the managed peer transfer contract.
func verifyDNSPrimaryPairReadyAt(
	ctx context.Context,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerAXFR dnsBoundCatalogAXFRProbe,
) error {
	_, err := verifyDNSPrimaryPairReadyAuthorityAt(
		ctx, evidence, soa, localAXFR, peerAXFR,
	)
	return err
}

func verifyDNSPrimaryPairReadyAuthorityAt(
	ctx context.Context,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe,
	localAXFR dnsCatalogAXFRProbe,
	peerAXFR dnsBoundCatalogAXFRProbe,
) (dnsPeerAXFRAuthority, error) {
	if err := validateDNSPrimaryCatalogEvidence(evidence); err != nil {
		return dnsPeerAXFRAuthority{}, err
	}
	if soa == nil || localAXFR == nil || peerAXFR == nil {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS primary pair readiness identity is invalid")
	}
	members := append([]string(nil), evidence.Members...)

	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	live, err := localAXFR(proofCtx, evidence.LocalIP, evidence.Domain)
	if err != nil {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS primary catalog AXFR is unavailable")
	}
	if live.Serial != evidence.Serial || !slices.Equal(live.Members, members) {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS primary catalog differs from its durable evidence")
	}
	peerLive, err := peerAXFR(
		proofCtx, evidence.LocalIP, evidence.PeerIP, evidence.Domain,
	)
	if err != nil {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS peer catalog AXFR is unavailable")
	}
	if peerLive.Serial != evidence.Serial ||
		!slices.Equal(peerLive.Members, members) {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS peer catalog differs from the primary")
	}
	peerCatalogSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, evidence.PeerIP, evidence.Domain, soa,
	)
	if err != nil {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS peer catalog SOA is unavailable")
	}
	if peerCatalogSerial != evidence.Serial {
		return dnsPeerAXFRAuthority{},
			errors.New("DNS peer catalog SOA serial differs from the primary")
	}
	for index, member := range members {
		localSerial, localErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, evidence.LocalIP, member, soa,
		)
		peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, evidence.PeerIP, member, soa,
		)
		expectedSerial := evidence.MemberSerials[index]
		if localErr != nil || peerErr != nil ||
			localSerial != expectedSerial || peerSerial != expectedSerial {
			return dnsPeerAXFRAuthority{},
				errors.New("DNS catalog member did not converge on the peer")
		}
	}
	return dnsPeerAXFRAuthority{
		sourceIP: evidence.LocalIP, peerIP: evidence.PeerIP,
		catalog: evidence.Domain, catalogSerial: evidence.Serial,
	}, nil
}

func validateDNSPrimaryCatalogEvidence(evidence dnsPrimaryCatalogEvidence) error {
	if evidence.Serial == 0 ||
		!canonicalPairReadinessIPv4(evidence.LocalIP) ||
		!canonicalPairReadinessIPv4(evidence.PeerIP) ||
		evidence.LocalIP == evidence.PeerIP ||
		!serviceMutationCanonicalFQDN(evidence.Domain) ||
		len(evidence.Members) != len(evidence.MemberSerials) {
		return errors.New("DNS primary pair readiness identity is invalid")
	}
	if !sort.StringsAreSorted(evidence.Members) {
		return errors.New("DNS primary catalog members are not canonical")
	}
	for index, member := range evidence.Members {
		if !serviceMutationCanonicalFQDN(member) || member == evidence.Domain ||
			(index > 0 && member == evidence.Members[index-1]) ||
			evidence.MemberSerials[index] == 0 {
			return errors.New("DNS primary catalog members are invalid")
		}
	}
	return nil
}

func canonicalPairReadinessIPv4(value string) bool {
	parsed := net.ParseIP(value)
	return parsed != nil && parsed.To4() != nil && parsed.To4().String() == value
}

func bindPrimaryCatalogEvidence(
	tree binddns.VerifiedTree,
) (dnsPrimaryCatalogEvidence, bool, error) {
	receipt := tree.CurrentReceipt()
	pairing := receipt.Pairing
	if pairing == nil || pairing.Role != binddns.PairRolePrimary {
		return dnsPrimaryCatalogEvidence{}, false, nil
	}
	wantDomain, err := binddns.CatalogDomain(pairing.LocalIP)
	if err != nil || wantDomain != pairing.LocalCatalog || pairing.CatalogSerial == 0 {
		return dnsPrimaryCatalogEvidence{}, false,
			errors.New("BIND primary catalog receipt is invalid")
	}
	members := make([]string, 0, len(receipt.Zones))
	serialByMember := make(map[string]uint32, len(receipt.Zones))
	for _, zone := range receipt.Zones {
		verifiedReceipt, data, found := tree.Zone(zone.Domain)
		if !found || verifiedReceipt != zone {
			return dnsPrimaryCatalogEvidence{}, false,
				errors.New("BIND primary zone receipt is unavailable")
		}
		expected, err := expectedDNSZoneAuthorityFromBINDTree(verifiedReceipt, data)
		if err != nil {
			return dnsPrimaryCatalogEvidence{}, false, err
		}
		if !expected.Delete {
			members = append(members, expected.Domain)
			serialByMember[expected.Domain] = expected.Serial
		}
	}
	sort.Strings(members)
	serials := make([]uint32, len(members))
	for index, member := range members {
		serials[index] = serialByMember[member]
	}
	return dnsPrimaryCatalogEvidence{
		LocalIP: pairing.LocalIP, PeerIP: pairing.PeerIP,
		Domain: pairing.LocalCatalog, Serial: pairing.CatalogSerial,
		Members: members, MemberSerials: serials,
	}, true, nil
}

func bindPrimaryPairReady(
	ctx context.Context,
	tree binddns.VerifiedTree,
) (bool, error) {
	evidence, primary, err := bindPrimaryCatalogEvidence(tree)
	if err != nil || !primary {
		return false, err
	}
	if err := verifyDNSPrimaryPairReadyAt(
		ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
		probeDNSBoundCatalogAXFR,
	); err != nil {
		return false, err
	}
	return true, nil
}

// managedPDNSPrimaryCatalogEvidence is read-only. It recognizes a primary
// only from the exact panel-managed producer config and database identity; a
// standalone server or a directional consumer never becomes pair-ready.
func managedPDNSPrimaryCatalogEvidence(
	ctx context.Context,
) (dnsPrimaryCatalogEvidence, bool, error) {
	identity, primary, err := readManagedPDNSPrimaryCatalog(ctx)
	if err != nil || !primary {
		return dnsPrimaryCatalogEvidence{}, primary, err
	}
	return dnsPrimaryCatalogEvidence{
		LocalIP: identity.LocalIP, PeerIP: identity.PeerIP,
		Domain: identity.Domain, Serial: identity.Serial,
		Members: identity.Members, MemberSerials: identity.MemberSerials,
	}, true, nil
}

func powerDNSPrimaryPairReady(ctx context.Context) (bool, error) {
	evidence, primary, err := managedPDNSPrimaryCatalogEvidence(ctx)
	if err != nil || !primary {
		return false, err
	}
	if err := verifyDNSPrimaryPairReadyAt(
		ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
		probeDNSBoundCatalogAXFR,
	); err != nil {
		return false, err
	}
	return true, nil
}
