package main

import (
	"context"
	"database/sql"
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
}

// verifyDNSPrimaryPairReadyAt proves that the configured peer has consumed the
// primary's exact catalog. The peer is queried only for ordinary authoritative
// SOA answers: a secondary is not expected to permit outgoing AXFR.
func verifyDNSPrimaryPairReadyAt(
	ctx context.Context,
	evidence dnsPrimaryCatalogEvidence,
	soa dnsZoneSOAProbe,
	axfr dnsCatalogAXFRProbe,
) error {
	if soa == nil || axfr == nil || evidence.Serial == 0 ||
		!canonicalPairReadinessIPv4(evidence.LocalIP) ||
		!canonicalPairReadinessIPv4(evidence.PeerIP) ||
		evidence.LocalIP == evidence.PeerIP ||
		!serviceMutationCanonicalFQDN(evidence.Domain) {
		return errors.New("DNS primary pair readiness identity is invalid")
	}
	members := append([]string(nil), evidence.Members...)
	if !sort.StringsAreSorted(members) {
		return errors.New("DNS primary catalog members are not canonical")
	}
	for index, member := range members {
		if !serviceMutationCanonicalFQDN(member) || member == evidence.Domain ||
			(index > 0 && member == members[index-1]) {
			return errors.New("DNS primary catalog members are invalid")
		}
	}

	proofCtx, cancel := context.WithTimeout(ctx, dnsPairProofLimit)
	defer cancel()
	live, err := axfr(proofCtx, evidence.LocalIP, evidence.Domain)
	if err != nil {
		return errors.New("DNS primary catalog AXFR is unavailable")
	}
	if live.Serial != evidence.Serial || !slices.Equal(live.Members, members) {
		return errors.New("DNS primary catalog differs from its durable evidence")
	}
	peerCatalogSerial, err := exactDNSZoneSerialAtWithProbe(
		proofCtx, evidence.PeerIP, evidence.Domain, soa,
	)
	if err != nil {
		return errors.New("DNS peer catalog SOA is unavailable")
	}
	if peerCatalogSerial != evidence.Serial {
		return errors.New("DNS peer catalog SOA serial differs from the primary")
	}
	for _, member := range members {
		localSerial, localErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, evidence.LocalIP, member, soa,
		)
		peerSerial, peerErr := exactDNSZoneSerialAtWithProbe(
			proofCtx, evidence.PeerIP, member, soa,
		)
		if localErr != nil || peerErr != nil || localSerial != peerSerial {
			return errors.New("DNS catalog member did not converge on the peer")
		}
	}
	return nil
}

func canonicalPairReadinessIPv4(value string) bool {
	parsed := net.ParseIP(value)
	return parsed != nil && parsed.To4() != nil && parsed.To4().String() == value
}

func bindPrimaryCatalogEvidence(
	receipt binddns.Receipt,
) (dnsPrimaryCatalogEvidence, bool, error) {
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
	for _, zone := range receipt.Zones {
		if !zone.Delete {
			members = append(members, zone.Domain)
		}
	}
	sort.Strings(members)
	return dnsPrimaryCatalogEvidence{
		LocalIP: pairing.LocalIP, PeerIP: pairing.PeerIP,
		Domain: pairing.LocalCatalog, Serial: pairing.CatalogSerial,
		Members: members,
	}, true, nil
}

func bindPrimaryPairReady(
	ctx context.Context,
	receipt binddns.Receipt,
) (bool, error) {
	evidence, primary, err := bindPrimaryCatalogEvidence(receipt)
	if err != nil || !primary {
		return false, err
	}
	if err := verifyDNSPrimaryPairReadyAt(
		ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
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
	identity, configured, err := managedPDNSCatalogIdentity()
	if err != nil || !configured {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	db, err := openPDNSEngineDB(pdnsDBPath(), true)
	if err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	defer tx.Rollback()

	var domainID int64
	var name, zoneType, account, catalog string
	if err := tx.QueryRowContext(ctx, `
		SELECT id, name, UPPER(type), COALESCE(account,''), COALESCE(catalog,'')
		FROM domains WHERE name = ? COLLATE NOCASE
	`, identity.Domain).Scan(&domainID, &name, &zoneType, &account, &catalog); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dnsPrimaryCatalogEvidence{}, false, nil
		}
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if name != identity.Domain || zoneType != "PRODUCER" ||
		account != pdnsBINDCatalogAccount || catalog != "" {
		return dnsPrimaryCatalogEvidence{}, false,
			errors.New("PowerDNS producer row is not exact panel authority")
	}
	var ownedCatalogs, peerCatalogs int
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ?`,
		pdnsBINDCatalogAccount,
	).Scan(&ownedCatalogs); err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if err := tx.QueryRowContext(
		ctx, `SELECT COUNT(*) FROM domains WHERE account = ?`,
		pdnsPeerCatalogAccount,
	).Scan(&peerCatalogs); err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if ownedCatalogs != 1 || peerCatalogs != 0 {
		return dnsPrimaryCatalogEvidence{}, false,
			errors.New("PowerDNS producer ownership is ambiguous")
	}
	serial, err := verifyPDNSProducerBaseTx(
		ctx, tx, domainID, identity.Domain, identity.LocalIP,
	)
	if err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT name, UPPER(type) FROM domains
		WHERE catalog = ? COLLATE NOCASE
		ORDER BY name COLLATE BINARY
	`, identity.Domain)
	if err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	members := make([]string, 0)
	for rows.Next() {
		var member, memberType string
		if err := rows.Scan(&member, &memberType); err != nil {
			rows.Close()
			return dnsPrimaryCatalogEvidence{}, false, err
		}
		if !serviceMutationCanonicalFQDN(member) || member == identity.Domain ||
			(memberType != "NATIVE" && memberType != "MASTER" && memberType != "PRIMARY") {
			rows.Close()
			return dnsPrimaryCatalogEvidence{}, false,
				errors.New("PowerDNS producer member is not canonical")
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if err := rows.Close(); err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return dnsPrimaryCatalogEvidence{}, false, err
	}
	if !sort.StringsAreSorted(members) {
		return dnsPrimaryCatalogEvidence{}, false,
			errors.New("PowerDNS producer members are not canonical")
	}
	return dnsPrimaryCatalogEvidence{
		LocalIP: identity.LocalIP, PeerIP: identity.PeerIP,
		Domain: identity.Domain, Serial: serial, Members: members,
	}, true, nil
}

func powerDNSPrimaryPairReady(ctx context.Context) (bool, error) {
	evidence, primary, err := managedPDNSPrimaryCatalogEvidence(ctx)
	if err != nil || !primary {
		return false, err
	}
	if err := verifyDNSPrimaryPairReadyAt(
		ctx, evidence, probeDNSZoneSOA, probeDNSCatalogAXFR,
	); err != nil {
		return false, err
	}
	return true, nil
}
