package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
)

// subdomainDNSMutationResult distinguishes the durable local ledger state
// from remote publication. Callers must not report full success unless both
// LedgerReady and Published are true.
type subdomainDNSMutationResult struct {
	ParentZoneExists bool
	LedgerChanged    bool
	LedgerReady      bool
	Published        bool
}

var errParentDomainUnavailable = errors.New("parent domain is not active")

// Subdomain support. A subdomain reuses everything a normal site has (its own
// docroot, vhost and PHP pool, created by the orchestrator) but differs in
// exactly one place: DNS. Instead of a fresh zone it gets an address record in
// its parent's existing zone, so it resolves without a separate delegation.
// The parent is simply the longest domain in the same subscription that the
// new name sits under.
//
// Subdomain desteği. Bir subdomain, normal bir sitenin sahip olduğu her şeyi
// yeniden kullanır (kendi belge kökü, vhost ve PHP havuzu, orkestratör
// tarafından oluşturulur) ama tek bir yerde ayrışır: DNS. Taze bir zone
// yerine ana domain'inin var olan zone'una bir adres kaydı alır; böylece ayrı
// bir devir olmadan çözülür. Ana domain, aynı abonelikte yeni adın altında
// kaldığı en uzun domain'dir.

// resolveParentDomain finds the domain in the given subscription that name is
// a subdomain of, if any. "blog.shop.example.com" prefers "shop.example.com"
// over "example.com" — the most specific parent wins.
// resolveParentDomain, verilen abonelikte name'in altında kaldığı domain'i
// bulur (varsa). En özgül ana domain kazanır.
func (p *Panel) resolveParentDomain(
	ctx context.Context,
	subscriptionID int,
	name string,
) (parentID int, parentName string, ok bool, err error) {
	rows, err := p.db.GetDB().QueryContext(ctx,
		`SELECT id, name, status
		 FROM domains
		 WHERE subscription_id = ? AND parent_domain_id IS NULL`,
		subscriptionID)
	if err != nil {
		return 0, "", false, fmt.Errorf("query parent domains: %w", err)
	}
	defer rows.Close()

	canonicalName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	var parentStatus string
	for rows.Next() {
		var id int
		var candidate, candidateStatus string
		if err := rows.Scan(&id, &candidate, &candidateStatus); err != nil {
			return 0, "", false, fmt.Errorf("scan parent domain: %w", err)
		}
		candidate = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(candidate)), ".")
		// name is a subdomain of candidate when it ends with ".candidate"
		// (and is not the candidate itself). Longest match wins.
		// name, candidate'ın subdomain'idir eğer ".candidate" ile bitiyorsa
		// (ve candidate'ın kendisi değilse). En uzun eşleşme kazanır.
		if strings.HasSuffix(canonicalName, "."+candidate) && len(candidate) > len(parentName) {
			parentID, parentName, ok = id, candidate, true
			parentStatus = strings.ToLower(strings.TrimSpace(candidateStatus))
		}
	}
	if err := rows.Err(); err != nil {
		return 0, "", false, fmt.Errorf("iterate parent domains: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, "", false, fmt.Errorf("close parent domain query: %w", err)
	}
	if ok && parentStatus != "active" {
		return 0, "", false, fmt.Errorf(
			"%w: %s has status %s",
			errParentDomainUnavailable,
			parentName,
			parentStatus,
		)
	}
	return parentID, parentName, ok, nil
}

// addSubdomainToParentZone inserts an address record for the subdomain into
// its parent's zone and pushes the zone to PowerDNS. A/AAAA only when the
// server's address is known — never a guessed IP (same rule as the zone
// template).
// addSubdomainToParentZone, subdomain için ana domain'in zone'una bir adres
// kaydı ekler ve zone'u PowerDNS'e iter. A/AAAA yalnız sunucunun adresi
// biliniyorsa — asla tahmini IP değil.
func (p *Panel) addSubdomainToParentZone(
	ctx context.Context,
	parentName string,
	subName string,
) (result subdomainDNSMutationResult, err error) {
	parentName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parentName)), ".")
	subName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(subName)), ".")
	if parentName == "" || subName == "" {
		return result, fmt.Errorf("parent and subdomain names are required")
	}

	ipv4 := strings.TrimSpace(serverPrimaryIP())
	if ipv4 != "" {
		parsed := net.ParseIP(ipv4)
		if parsed == nil || parsed.To4() == nil {
			return result, fmt.Errorf("server IPv4 address is invalid")
		}
		ipv4 = parsed.To4().String()
	}
	ipv6 := strings.TrimSpace(serverPrimaryIPv6())
	if ipv6 != "" {
		parsed := net.ParseIP(ipv6)
		if parsed == nil || parsed.To4() != nil {
			return result, fmt.Errorf("server IPv6 address is invalid")
		}
		ipv6 = parsed.String()
	}

	addresses := []struct {
		typ     string
		content string
	}{
		{typ: "A", content: ipv4},
		{typ: "AAAA", content: ipv6},
	}
	if addresses[0].content == "" && addresses[1].content == "" {
		return result, fmt.Errorf("server has no publishable address for subdomain DNS")
	}

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin subdomain DNS ledger transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var zoneID int
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, parentName,
	).Scan(&zoneID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("parent DNS zone %q does not exist: %w", parentName, err)
		}
		return result, fmt.Errorf("find parent DNS zone: %w", err)
	}
	result.ParentZoneExists = true

	ledgerChanged := false
	for _, address := range addresses {
		if address.content == "" {
			continue
		}
		var total, exact int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*),
			       COALESCE(SUM(CASE WHEN content = ? THEN 1 ELSE 0 END), 0)
			FROM pdns_records
			WHERE domain_id = ? AND name = ? AND type = ?`,
			address.content,
			zoneID,
			subName,
			address.typ,
		).Scan(&total, &exact); err != nil {
			return result, fmt.Errorf("inspect existing %s record: %w", address.typ, err)
		}
		if total > exact {
			return result, fmt.Errorf(
				"subdomain %q already has a different %s address record",
				subName,
				address.typ,
			)
		}
		if exact > 0 {
			continue
		}

		insertResult, err := tx.ExecContext(ctx, `
			INSERT INTO pdns_records (domain_id, name, type, content, ttl)
			VALUES (?, ?, ?, ?, 3600)`,
			zoneID,
			subName,
			address.typ,
			address.content,
		)
		if err != nil {
			return result, fmt.Errorf("insert subdomain %s record: %w", address.typ, err)
		}
		rowsAffected, err := insertResult.RowsAffected()
		if err != nil {
			return result, fmt.Errorf("verify subdomain %s insert: %w", address.typ, err)
		}
		if rowsAffected != 1 {
			return result, fmt.Errorf(
				"subdomain %s insert changed %d rows, expected 1",
				address.typ,
				rowsAffected,
			)
		}
		ledgerChanged = true
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit subdomain DNS ledger: %w", err)
	}
	result.LedgerChanged = ledgerChanged
	result.LedgerReady = true

	if err := p.syncZoneToDNS(ctx, parentName, false); err != nil {
		return result, fmt.Errorf("publish parent DNS zone: %w", err)
	}
	result.Published = true
	return result, nil
}

// removeSubdomainFromParentZone deletes the subdomain's records from its
// parent zone and re-pushes it — used when a subdomain is deleted so the
// parent zone stops answering for it.
// removeSubdomainFromParentZone, subdomain'in kayıtlarını ana zone'undan
// siler ve zone'u yeniden iter.
func (p *Panel) removeSubdomainFromParentZone(
	ctx context.Context,
	parentName string,
	subName string,
) (result subdomainDNSMutationResult, err error) {
	parentName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(parentName)), ".")
	subName = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(subName)), ".")
	if parentName == "" || subName == "" {
		return result, fmt.Errorf("parent and subdomain names are required")
	}

	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin subdomain DNS removal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var zoneID int
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, parentName,
	).Scan(&zoneID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("parent DNS zone %q does not exist: %w", parentName, err)
		}
		return result, fmt.Errorf("find parent DNS zone: %w", err)
	}
	result.ParentZoneExists = true

	deleteResult, err := tx.ExecContext(ctx,
		`DELETE FROM pdns_records WHERE domain_id = ? AND name = ?`,
		zoneID,
		subName,
	)
	if err != nil {
		return result, fmt.Errorf("remove subdomain DNS records: %w", err)
	}
	rowsAffected, err := deleteResult.RowsAffected()
	if err != nil {
		return result, fmt.Errorf("verify subdomain DNS removal: %w", err)
	}
	ledgerChanged := rowsAffected > 0

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit subdomain DNS removal: %w", err)
	}
	result.LedgerChanged = ledgerChanged
	result.LedgerReady = true

	if err := p.syncZoneToDNS(ctx, parentName, false); err != nil {
		return result, fmt.Errorf("publish parent DNS zone: %w", err)
	}
	result.Published = true
	return result, nil
}
