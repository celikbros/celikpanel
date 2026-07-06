package main

import (
	"context"
	"strings"
)

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
func (p *Panel) resolveParentDomain(ctx context.Context, subscriptionID int, name string) (parentID int, parentName string, ok bool) {
	rows, err := p.db.GetDB().QueryContext(ctx,
		`SELECT id, name FROM domains WHERE subscription_id = ? AND parent_domain_id IS NULL`,
		subscriptionID)
	if err != nil {
		return 0, "", false
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var candidate string
		if rows.Scan(&id, &candidate) != nil {
			continue
		}
		// name is a subdomain of candidate when it ends with ".candidate"
		// (and is not the candidate itself). Longest match wins.
		// name, candidate'ın subdomain'idir eğer ".candidate" ile bitiyorsa
		// (ve candidate'ın kendisi değilse). En uzun eşleşme kazanır.
		if strings.HasSuffix(name, "."+candidate) && len(candidate) > len(parentName) {
			parentID, parentName, ok = id, candidate, true
		}
	}
	return parentID, parentName, ok
}

// addSubdomainToParentZone inserts an address record for the subdomain into
// its parent's zone and pushes the zone to PowerDNS. A/AAAA only when the
// server's address is known — never a guessed IP (same rule as the zone
// template).
// addSubdomainToParentZone, subdomain için ana domain'in zone'una bir adres
// kaydı ekler ve zone'u PowerDNS'e iter. A/AAAA yalnız sunucunun adresi
// biliniyorsa — asla tahmini IP değil.
func (p *Panel) addSubdomainToParentZone(ctx context.Context, parentName, subName string) {
	var zoneID int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, parentName).Scan(&zoneID); err != nil {
		return
	}

	add := func(typ, content string) {
		if content == "" {
			return
		}
		// Idempotent: don't stack duplicate records if the subdomain is
		// recreated. / Idempotent: subdomain yeniden oluşturulursa kayıt yığma.
		var exists int
		p.db.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM pdns_records WHERE domain_id = ? AND name = ? AND type = ?`,
			zoneID, subName, typ).Scan(&exists)
		if exists > 0 {
			return
		}
		p.db.GetDB().ExecContext(ctx,
			`INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, ?, ?, 3600)`,
			zoneID, subName, typ, content)
	}
	add("A", serverPrimaryIP())
	add("AAAA", serverPrimaryIPv6())

	p.syncZoneToDNS(ctx, parentName, false)
}

// removeSubdomainFromParentZone deletes the subdomain's records from its
// parent zone and re-pushes it — used when a subdomain is deleted so the
// parent zone stops answering for it.
// removeSubdomainFromParentZone, subdomain'in kayıtlarını ana zone'undan
// siler ve zone'u yeniden iter.
func (p *Panel) removeSubdomainFromParentZone(ctx context.Context, parentName, subName string) {
	var zoneID int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, parentName).Scan(&zoneID); err != nil {
		return
	}
	p.db.GetDB().ExecContext(ctx,
		`DELETE FROM pdns_records WHERE domain_id = ? AND name = ?`, zoneID, subName)
	p.syncZoneToDNS(ctx, parentName, false)
}
