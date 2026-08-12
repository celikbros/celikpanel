package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

// canonicalCpmoveImportDomain binds the requested import target to a domain
// that was actually declared by the inspected archive.  The agent inspects
// and extracts the same root-owned archive descriptor policy, but the panel
// still must not let an operator relabel archive contents as an unrelated
// hostname.
func canonicalCpmoveImportDomain(preview *cpmovePreview, requested string) (string, error) {
	if preview == nil {
		return "", errors.New("cpmove preview is unavailable")
	}
	if strings.TrimSpace(requested) == "" {
		requested = preview.MainDomain
	}
	requested, err := hostname.CanonicalFQDN(requested)
	if err != nil {
		return "", fmt.Errorf("invalid import domain: %w", err)
	}
	if _, err := hostname.MailFQDN(requested); err != nil {
		return "", fmt.Errorf("invalid import domain: %w", err)
	}

	declared := append([]string(nil), preview.Domains...)
	if strings.TrimSpace(preview.MainDomain) != "" {
		declared = append(declared, preview.MainDomain)
	}
	for _, candidate := range declared {
		canonical, err := hostname.CanonicalFQDN(candidate)
		if err == nil && canonical == requested {
			return requested, nil
		}
	}
	return "", fmt.Errorf("domain %q is not declared by this cpmove archive", requested)
}

func cpmoveDNSRecordsForDomain(preview *cpmovePreview, domain string) ([]transport.CpmoveDNSRecord, bool) {
	if preview == nil {
		return nil, false
	}
	for zone, records := range preview.DNSZones {
		canonical, err := hostname.CanonicalFQDN(zone)
		if err == nil && canonical == domain {
			return records, true
		}
	}
	return nil, false
}

func normalizeCpmoveDNSRecords(zone string, records []transport.CpmoveDNSRecord) ([]transport.CpmoveDNSRecord, error) {
	zone, err := hostname.CanonicalFQDN(zone)
	if err != nil {
		return nil, fmt.Errorf("invalid DNS zone: %w", err)
	}

	normalized := make([]transport.CpmoveDNSRecord, 0, len(records))
	typesByOwner := make(map[string]map[string]struct{})
	seen := make(map[string]struct{})
	for index, record := range records {
		recordType := strings.ToUpper(strings.TrimSpace(record.Type))
		if recordType == "SOA" || recordType == "NS" {
			continue
		}

		owner, err := normalizeDNSOwner(record.Name, zone)
		if err != nil {
			return nil, fmt.Errorf("DNS record %d owner: %w", index+1, err)
		}
		var content string
		var priority *int
		if recordType == "CAA" {
			content, err = normalizeCpmoveCAA(record.Content)
			if record.TTL < 0 || record.TTL > maxDNSTTL {
				err = errors.New("TTL must be between 0 and 2147483647")
			}
		} else {
			recordType, content, priority, err = normalizeDNSRecord(
				recordType, owner, record.Content, record.TTL, record.Prio, zone,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("DNS record %d (%s %s): %w", index+1, recordType, owner, err)
		}

		prio := 0
		if priority != nil {
			prio = *priority
		}
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%d", owner, recordType, content, prio)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		ownerTypes := typesByOwner[owner]
		if ownerTypes == nil {
			ownerTypes = make(map[string]struct{})
			typesByOwner[owner] = ownerTypes
		}
		if recordType == "CNAME" && len(ownerTypes) != 0 {
			return nil, fmt.Errorf("CNAME %s cannot coexist with another record", owner)
		}
		if recordType != "CNAME" {
			if _, hasCNAME := ownerTypes["CNAME"]; hasCNAME {
				return nil, fmt.Errorf("record %s cannot coexist with a CNAME", owner)
			}
		}
		ownerTypes[recordType] = struct{}{}

		normalized = append(normalized, transport.CpmoveDNSRecord{
			Name: owner, Type: recordType, Content: content, TTL: record.TTL, Prio: prio,
		})
	}
	return normalized, nil
}

func normalizeCpmoveCAA(raw string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 3 {
		return "", errors.New("CAA content must be: flags tag value")
	}
	flags, err := strconv.ParseUint(fields[0], 10, 8)
	if err != nil {
		return "", errors.New("invalid CAA flags")
	}
	tag := strings.ToLower(fields[1])
	if tag == "" || len(tag) > 15 {
		return "", errors.New("invalid CAA tag")
	}
	for _, character := range tag {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return "", errors.New("invalid CAA tag")
		}
	}
	value := strings.TrimSpace(strings.Join(fields[2:], " "))
	value = strings.Trim(value, `"`)
	if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\r\n\x00\"") {
		return "", errors.New("invalid CAA value")
	}
	return fmt.Sprintf(`%d %s "%s"`, flags, tag, value), nil
}

// replaceCpmoveDNSRecords swaps imported records in one transaction.  The
// panel-owned apex SOA/NS records survive, and either the complete validated
// imported set becomes visible or the previous set remains untouched.
func replaceCpmoveDNSRecords(
	ctx context.Context,
	db *sql.DB,
	zoneID int,
	zone string,
	records []transport.CpmoveDNSRecord,
) (int, error) {
	if db == nil || zoneID <= 0 {
		return 0, errors.New("DNS import database or zone identity is invalid")
	}
	normalized, err := normalizeCpmoveDNSRecords(zone, records)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin DNS import: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pdns_records WHERE domain_id = ? AND type NOT IN ('SOA','NS')`, zoneID,
	); err != nil {
		return 0, fmt.Errorf("clear previous DNS records: %w", err)
	}
	for _, record := range normalized {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio)
			VALUES (?, ?, ?, ?, ?, ?)`,
			zoneID, record.Name, record.Type, record.Content, record.TTL, nullableCpmoveDNSPriority(record),
		); err != nil {
			return 0, fmt.Errorf("insert imported %s record for %s: %w", record.Type, record.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit DNS import: %w", err)
	}
	return len(normalized), nil
}

func nullableCpmoveDNSPriority(record transport.CpmoveDNSRecord) any {
	if record.Type == "MX" || record.Type == "SRV" {
		return record.Prio
	}
	return nil
}

func setCpmoveImportStatus(ctx context.Context, db *sql.DB, domainID, siteID int, status string) error {
	if db == nil || domainID <= 0 || siteID <= 0 {
		return errors.New("cpmove import identity is invalid")
	}
	if status != "pending" && status != "active" {
		return fmt.Errorf("unsupported cpmove import status %q", status)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin import status update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	domainQuery := `UPDATE domains SET status = ? WHERE id = ?`
	domainArgs := []any{status, domainID}
	if status == "active" {
		domainQuery += ` AND NOT EXISTS (
			SELECT 1 FROM domain_deletion_operations WHERE domain_id = ?
		)`
		domainArgs = append(domainArgs, domainID)
	}
	for _, mutation := range []struct {
		query string
		args  []any
		name  string
	}{
		{domainQuery, domainArgs, "domain"},
		{`UPDATE sites SET status = ? WHERE id = ?`, []any{status, siteID}, "site"},
	} {
		result, err := tx.ExecContext(ctx, mutation.query, mutation.args...)
		if err != nil {
			return fmt.Errorf("update %s import status: %w", mutation.name, err)
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			if err == nil {
				err = fmt.Errorf("updated %d rows", affected)
			}
			return fmt.Errorf("verify %s import status: %w", mutation.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit import status: %w", err)
	}
	return nil
}
