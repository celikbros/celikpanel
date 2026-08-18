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

func verifyDNSLegacyPrimaryPairReadyAuthorityAt(
	ctx context.Context,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe,
	peerAXFR dnsBoundCatalogAXFRProbe,
) (dnsPeerAXFRAuthority, error) {
	if err := validateDNSPrimaryCatalogEvidence(evidence); err != nil {
		return dnsPeerAXFRAuthority{}, err
	}
	if soa == nil || peerAXFR == nil {
		return dnsPeerAXFRAuthority{},
			errors.New(`legacy DNS primary pair proof is unavailable`)
	}
	members := append([]string(nil), evidence.Members...)
	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	localCatalogSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, evidence.LocalIP, evidence.Domain, soa,
	)
	if err != nil || localCatalogSerial != evidence.Serial {
		return dnsPeerAXFRAuthority{},
			errors.New(`legacy DNS primary catalog local SOA differs from durable evidence`)
	}
	peerLive, err := peerAXFR(
		proofCtx, evidence.LocalIP, evidence.PeerIP, evidence.Domain,
	)
	if err != nil || peerLive.Serial != evidence.Serial ||
		!slices.Equal(peerLive.Members, members) {
		return dnsPeerAXFRAuthority{},
			errors.New(`legacy DNS peer catalog AXFR differs from durable evidence`)
	}
	peerCatalogSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, evidence.PeerIP, evidence.Domain, soa,
	)
	if err != nil || peerCatalogSerial != evidence.Serial {
		return dnsPeerAXFRAuthority{},
			errors.New(`legacy DNS peer catalog SOA differs from durable evidence`)
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
				errors.New(`legacy DNS catalog member did not converge on the peer`)
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

func bindStateTreePairContract(
	root string,
	state dnsEngineStateReceipt,
	tree binddns.VerifiedTree,
	allowGenerationLag bool,
	allowLegacySecondary bool,
	allowDirectionalTupleRepair bool,
) (bool, error) {
	receipt := tree.CurrentReceipt()
	if state.Engine != "bind" || receipt.EngineEpoch != state.EngineEpoch ||
		(!allowGenerationLag && receipt.Generation != state.Generation) {
		return false, errors.New("BIND tree differs from its durable engine state")
	}
	pairing := receipt.Pairing
	if pairing == nil {
		if state.PairRole != "" || state.PairLocalIP != "" ||
			state.PairPeerIP != "" || state.PrimaryCatalogSerial != 0 {
			return false, errors.New("standalone BIND tree conflicts with directional state")
		}
		if err := binddns.VerifyCurrentConfig(root, tree); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := requireHostOwnedDNSPairAddress(pairing.LocalIP); err != nil {
		return false, err
	}
	primaryACLStyle := binddns.PrimaryTransferACLNone
	if pairing.Role == binddns.PairRolePrimary {
		var err error
		primaryACLStyle, err = binddns.ClassifyPrimaryTransferACL(root, tree)
		if err != nil {
			return false, err
		}
	} else if err := binddns.VerifyCurrentConfig(root, tree); err != nil {
		return false, err
	}
	missingTuple := state.PairLocalIP == "" && state.PairPeerIP == ""
	if missingTuple {
		if state.PairRole != "" || state.PrimaryCatalogSerial != 0 {
			return false, errors.New("legacy BIND state contains directional identity")
		}
		switch pairing.Role {
		case binddns.PairRolePrimary:
			switch primaryACLStyle {
			case binddns.PrimaryTransferACLLegacyPeerOnly:
				return true, nil
			case binddns.PrimaryTransferACLDirectionalSelfPeer:
				if allowDirectionalTupleRepair {
					return false, nil
				}
				return false, errors.New("directional BIND tree has no durable pair identity")
			default:
				return false, errors.New("legacy BIND primary transfer policy is invalid")
			}
		case binddns.PairRoleSecondary:
			if allowLegacySecondary {
				return true, nil
			}
			return false, errors.New("BIND secondary has no panel-local write authority")
		default:
			return false, errors.New("BIND tree has an invalid pair role")
		}
	}
	if state.PairRole != pairing.Role ||
		state.PairLocalIP != pairing.LocalIP ||
		state.PairPeerIP != pairing.PeerIP {
		return false, errors.New("BIND pair tree differs from its durable state")
	}
	switch pairing.Role {
	case binddns.PairRolePrimary:
		if state.PrimaryCatalogSerial == 0 ||
			state.PrimaryCatalogSerial > pairing.CatalogSerial {
			return false, errors.New("BIND catalog serial differs from durable state")
		}
		if primaryACLStyle != binddns.PrimaryTransferACLDirectionalSelfPeer {
			return false, errors.New("BIND primary transfer policy differs from directional state")
		}
		if state.Generation == receipt.Generation &&
			state.PrimaryCatalogSerial != pairing.CatalogSerial {
			return false, errors.New("BIND catalog serial differs from the current generation")
		}
	case binddns.PairRoleSecondary:
		if state.PrimaryCatalogSerial != 0 {
			return false, errors.New("BIND secondary state claims a primary catalog")
		}
	default:
		return false, errors.New("BIND tree has an invalid pair role")
	}
	return false, nil
}

func bindStateForPublishedReceipt(
	state dnsEngineStateReceipt,
	receipt binddns.Receipt,
) (dnsEngineStateReceipt, error) {
	if receipt.EngineEpoch != state.EngineEpoch {
		return dnsEngineStateReceipt{},
			errors.New("BIND target receipt differs from the active engine epoch")
	}
	next := state
	next.Generation = receipt.Generation
	if receipt.Pairing == nil {
		next.PairRole = ""
		next.PairLocalIP = ""
		next.PairPeerIP = ""
		next.PrimaryCatalogSerial = 0
	} else {
		next.PairRole = receipt.Pairing.Role
		next.PairLocalIP = receipt.Pairing.LocalIP
		next.PairPeerIP = receipt.Pairing.PeerIP
		if receipt.Pairing.Role == binddns.PairRolePrimary {
			next.PrimaryCatalogSerial = receipt.Pairing.CatalogSerial
		} else {
			next.PrimaryCatalogSerial = 0
		}
	}
	if err := validateDNSEngineState(next); err != nil {
		return dnsEngineStateReceipt{}, err
	}
	return next, nil
}

func bindPrimaryPairReadyForState(
	ctx context.Context,
	root string,
	tree binddns.VerifiedTree,
	state dnsEngineStateReceipt,
) (bool, error) {
	legacy, err := bindStateTreePairContract(
		root, state, tree, false, true, false,
	)
	if err != nil {
		return false, err
	}
	evidence, primary, err := bindPrimaryCatalogEvidence(tree)
	if err != nil || !primary {
		return false, err
	}
	if legacy {
		_, err = verifyDNSLegacyPrimaryPairReadyAuthorityAt(
			ctx, evidence, probeDNSZoneSOA, probeDNSBoundCatalogAXFR,
		)
	} else {
		err = verifyDNSPrimaryPairReadyAt(
			ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
			probeDNSBoundCatalogAXFR,
		)
	}
	if err != nil {
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
	return managedPDNSPrimaryCatalogEvidenceForRole(ctx, ``)
}

func managedPDNSPrimaryCatalogEvidenceForRole(
	ctx context.Context,
	pairRole string,
) (dnsPrimaryCatalogEvidence, bool, error) {
	identity, primary, err := readManagedPDNSPrimaryCatalogForRole(ctx, pairRole)
	if err != nil || !primary {
		return dnsPrimaryCatalogEvidence{}, primary, err
	}
	return dnsPrimaryCatalogEvidence{
		LocalIP: identity.LocalIP, PeerIP: identity.PeerIP,
		Domain: identity.Domain, Serial: identity.Serial,
		Members: identity.Members, MemberSerials: identity.MemberSerials,
	}, true, nil
}

func managedPDNSPrimaryCatalogEvidenceForState(
	ctx context.Context,
	state dnsEngineStateReceipt,
) (dnsPrimaryCatalogEvidence, bool, error) {
	identity, primary, err := readManagedPDNSPrimaryCatalogForState(ctx, state)
	if err != nil || !primary {
		return dnsPrimaryCatalogEvidence{}, primary, err
	}
	return dnsPrimaryCatalogEvidence{
		LocalIP: identity.LocalIP, PeerIP: identity.PeerIP,
		Domain: identity.Domain, Serial: identity.Serial,
		Members: identity.Members, MemberSerials: identity.MemberSerials,
	}, true, nil
}

func powerDNSPrimaryPairReady(
	ctx context.Context,
	state dnsEngineStateReceipt,
) (bool, error) {
	evidence, primary, err := managedPDNSPrimaryCatalogEvidenceForState(ctx, state)
	if err != nil || !primary {
		return false, err
	}
	if state.PairRole == "" && state.PrimaryCatalogSerial == 0 {
		_, err = verifyDNSLegacyPrimaryPairReadyAuthorityAt(
			ctx, evidence, probeDNSZoneSOA, probeDNSBoundCatalogAXFR,
		)
	} else {
		err = verifyDNSPrimaryPairReadyAt(
			ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
			probeDNSBoundCatalogAXFR,
		)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
