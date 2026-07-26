package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// nextSOASerial returns a strictly newer YYYYMMDDnn-style serial. Existing
// serials are never moved backwards, including older zones that used the hour
// as their final two digits.
func nextSOASerial(content string, now time.Time) (string, error) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return "", fmt.Errorf("invalid SOA record")
	}
	old, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil {
		return "", fmt.Errorf("invalid SOA serial: %w", err)
	}
	candidate, _ := strconv.ParseUint(now.UTC().Format("20060102")+"00", 10, 32)
	if candidate <= old {
		if old == uint64(^uint32(0)) {
			return "", fmt.Errorf("SOA serial exhausted")
		}
		candidate = old + 1
	}
	fields[2] = strconv.FormatUint(candidate, 10)
	return strings.Join(fields, " "), nil
}

// prepareZoneForSync makes every publication observable by a secondary:
// advance SOA and keep the ledger's zone kind aligned with the current mode.
func (p *Panel) prepareZoneForSync(ctx context.Context, domain string) error {
	zoneType := p.dnsZoneType(ctx)
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var zoneID, recordID int
	var soa string
	if err := tx.QueryRowContext(ctx, `
		SELECT d.id, r.id, r.content
		FROM pdns_domains d
		JOIN pdns_records r ON r.domain_id = d.id AND r.type = 'SOA' AND r.name = d.name
		WHERE d.name = ? LIMIT 1`, domain).Scan(&zoneID, &recordID, &soa); err != nil {
		return fmt.Errorf("zone %s has no valid SOA: %w", domain, err)
	}
	next, err := nextSOASerial(soa, time.Now())
	if err != nil {
		return fmt.Errorf("zone %s: %w", domain, err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pdns_records SET content = ? WHERE id = ?`, next, recordID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pdns_domains SET type = ? WHERE id = ?`, zoneType, zoneID); err != nil {
		return err
	}
	return tx.Commit()
}
