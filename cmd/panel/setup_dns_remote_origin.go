package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

var remoteDNSOriginMu sync.Mutex
var errRemoteDNSMailConflict = errors.New("existing mail DNS records conflict with the proposed mail service")
var errRemoteDNSPublicationPending = errors.New("reconcile the saved remote DNS publication before making another change")

type remoteDNSDomainTarget struct {
	ID           int
	Domain       string
	ConnectionID string
	Requested    string
}
type remoteDNSQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveRemoteDNSDomain(ctx context.Context, q remoteDNSQuery, name string) (remoteDNSDomainTarget, error) {
	target := remoteDNSDomainTarget{Requested: name}
	current := name
	expected := ""
	for depth := 0; depth < 16; depth++ {
		var parent sql.NullInt64
		var mode string
		err := q.QueryRowContext(ctx, `SELECT id,name,parent_domain_id,dns_management,dns_remote_connection_id FROM domains WHERE name=?`, current).Scan(&target.ID, &target.Domain, &parent, &mode, &target.ConnectionID)
		if err != nil {
			return target, err
		}
		if mode != setupDNSModeExisting || !validServiceOperationID(target.ConnectionID) || (expected != "" && expected != target.ConnectionID) {
			return target, errors.New("domain DNS connection ownership differs")
		}
		expected = target.ConnectionID
		if !parent.Valid {
			return target, nil
		}
		if err = q.QueryRowContext(ctx, `SELECT name FROM domains WHERE id=?`, parent.Int64).Scan(&current); err != nil {
			return target, err
		}
	}
	return target, errors.New("invalid remote DNS parent chain")
}
func remoteDNSNameWithin(name, domain string) bool {
	return name == domain || strings.HasSuffix(name, "."+domain)
}

func readRemoteDNSRecords(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, domainID int) ([]DNSRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,domain_id,name,type,content,ttl,prio,disabled FROM remote_dns_records WHERE domain_id=? ORDER BY type,name,id`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []DNSRecord{}
	for rows.Next() {
		var record DNSRecord
		if err = rows.Scan(&record.ID, &record.DomainID, &record.Name, &record.Type, &record.Content, &record.TTL, &record.Prio, &record.Disabled); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
func (p *Panel) remoteDomainDNSRecords(ctx context.Context, name string) ([]DNSRecord, error) {
	target, err := resolveRemoteDNSDomain(ctx, p.db.GetDB(), name)
	if err != nil {
		return nil, err
	}
	records, err := readRemoteDNSRecords(ctx, p.db.GetDB(), target.ID)
	if err != nil {
		return nil, err
	}
	filtered := []DNSRecord{}
	for _, record := range records {
		if remoteDNSNameWithin(record.Name, name) {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}
func (p *Panel) remoteDomainDNSExists(ctx context.Context, name string) (bool, error) {
	target, err := resolveRemoteDNSDomain(ctx, p.db.GetDB(), name)
	if err != nil {
		return false, err
	}
	var exists bool
	err = p.db.GetDB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM remote_dns_zones WHERE domain_id=? AND deleted=0)`, target.ID).Scan(&exists)
	return exists, err
}

// Only local input validation uses this type. Transport/receiver failures must
// never be reflected verbatim into an administrator response.
type remoteDNSRecordValidationError struct{ cause error }

func (e *remoteDNSRecordValidationError) Error() string { return e.cause.Error() }
func (e *remoteDNSRecordValidationError) Unwrap() error { return e.cause }

type remoteDNSRecordMutation func([]DNSRecord) ([]DNSRecord, error)

// Record edits and the exact outbox generation commit atomically. The caller
// serializes this origin's publication; no local DNS zone or agent is involved.
func (p *Panel) mutateRemoteDNSRecords(ctx context.Context, name string, deleted bool, mutate remoteDNSRecordMutation) error {
	remoteDNSOriginMu.Lock()
	defer remoteDNSOriginMu.Unlock()
	target, err := resolveRemoteDNSDomain(ctx, p.db.GetDB(), name)
	if err != nil {
		return err
	}
	// Exact pending generations may finish V3 propagation even when the
	// current pair is not yet ready for a new publication.
	if err = p.sendRemoteDNSOutbox(ctx, target, false); err != nil {
		return err
	}
	if _, err = p.remoteDNSConnectionReadiness(ctx, target.ConnectionID); err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target, err = resolveRemoteDNSDomain(ctx, tx, name)
	if err != nil {
		return err
	}
	records, err := readRemoteDNSRecords(ctx, tx, target.ID)
	if err != nil {
		return err
	}
	var generation, applied int64
	var wasDeleted bool
	var oldPayload string
	err = tx.QueryRowContext(ctx, `SELECT generation,applied_generation,deleted,payload_json FROM remote_dns_zones WHERE domain_id=?`, target.ID).Scan(&generation, &applied, &wasDeleted, &oldPayload)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	recreated := false
	if errors.Is(err, sql.ErrNoRows) {
		historyErr := tx.QueryRowContext(ctx, `SELECT generation,applied_generation,deleted,payload_json FROM remote_dns_origin_history WHERE connection_id=? AND zone_name=?`, target.ConnectionID, target.Domain).Scan(&generation, &applied, &wasDeleted, &oldPayload)
		if historyErr != nil && !errors.Is(historyErr, sql.ErrNoRows) {
			return historyErr
		}
		if historyErr == nil {
			if !wasDeleted || generation != applied {
				return errors.New("previous remote DNS ownership requires exact recovery")
			}
			recreated = !deleted
		}
	}
	if generation != applied {
		return errRemoteDNSPublicationPending
	}
	if deleted && generation == 0 {
		return nil
	}
	if wasDeleted && !recreated {
		if deleted && target.Domain == name {
			return nil
		}
		return errors.New("deleted remote DNS zone cannot be silently recreated")
	}
	if mutate != nil {
		records, err = mutate(records)
		if err != nil {
			return err
		}
	}
	zoneDeleted := deleted && name == target.Domain
	if deleted && !zoneDeleted {
		kept := []DNSRecord{}
		for _, record := range records {
			if !remoteDNSNameWithin(record.Name, name) {
				kept = append(kept, record)
			}
		}
		records = kept
	}
	if zoneDeleted {
		records = []DNSRecord{}
	}
	canonical, err := canonicalRemoteDNSPublication(remoteDNSPublication{Domain: target.Domain, Generation: generation + 1, Deleted: zoneDeleted, Records: records})
	if err != nil {
		return &remoteDNSRecordValidationError{cause: err}
	}
	if generation > 0 {
		var prior remoteDNSPublication
		if err = json.Unmarshal([]byte(oldPayload), &prior); err != nil {
			return err
		}
		comparison := canonical
		comparison.Generation = prior.Generation
		comparison, err = canonicalRemoteDNSPublication(comparison)
		if err != nil {
			return err
		}
		if comparison.PayloadHash == prior.PayloadHash {
			return nil
		}
	}
	// Preserve stable UI record IDs for untouched rows; CRUD must not silently
	// make another operator's saved record ID refer to a different record.
	if _, err = tx.ExecContext(ctx, `DELETE FROM remote_dns_records WHERE domain_id=?`, target.ID); err != nil {
		return err
	}
	for _, record := range records {
		owner, ownerErr := normalizeDNSOwner(record.Name, target.Domain)
		if ownerErr != nil {
			return ownerErr
		}
		kind, content, prio, normalizeErr := normalizeDNSRecord(record.Type, owner, record.Content, record.TTL, record.Prio, target.Domain)
		if normalizeErr != nil {
			return normalizeErr
		}
		priority := 0
		if prio != nil {
			priority = *prio
		}
		var recordID any
		if record.ID > 0 {
			recordID = record.ID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO remote_dns_records(id,domain_id,name,type,content,ttl,prio,disabled) VALUES(?,?,?,?,?,?,?,?)`, recordID, target.ID, owner, kind, content, record.TTL, priority, record.Disabled); err != nil {
			return err
		}
	}
	raw, err := remoteDNSCanonicalJSON(canonical)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO remote_dns_zones(domain_id,generation,applied_generation,payload_hash,payload_json,deleted) VALUES(?,?,?,?,?,?) ON CONFLICT(domain_id) DO UPDATE SET generation=excluded.generation,payload_hash=excluded.payload_hash,payload_json=excluded.payload_json,deleted=excluded.deleted`, target.ID, canonical.Generation, applied, canonical.PayloadHash, string(raw), canonical.Deleted)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO remote_dns_origin_history(connection_id,zone_name,generation,applied_generation,payload_hash,payload_json,deleted) VALUES(?,?,?,?,?,?,?) ON CONFLICT(connection_id,zone_name) DO UPDATE SET generation=excluded.generation,applied_generation=excluded.applied_generation,payload_hash=excluded.payload_hash,payload_json=excluded.payload_json,deleted=excluded.deleted`, target.ConnectionID, target.Domain, canonical.Generation, applied, canonical.PayloadHash, string(raw), canonical.Deleted)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return p.sendRemoteDNSOutbox(ctx, target, true)
}

func (p *Panel) sendRemoteDNSOutbox(ctx context.Context, target remoteDNSDomainTarget, required bool) error {
	var raw, hash string
	var generation, applied int64
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT payload_json,payload_hash,generation,applied_generation FROM remote_dns_zones WHERE domain_id=?`, target.ID).Scan(&raw, &hash, &generation, &applied)
	if errors.Is(err, sql.ErrNoRows) && !required {
		return nil
	}
	if err != nil {
		return err
	}
	if generation == applied {
		return nil
	}
	var payload remoteDNSPublication
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.Domain != target.Domain || payload.Generation != generation || payload.PayloadHash != hash {
		return errors.New("invalid persisted remote DNS outbox")
	}
	canonical, err := canonicalRemoteDNSPublication(payload)
	if err != nil || canonical.PayloadHash != hash {
		return errors.New("invalid canonical remote DNS outbox")
	}
	c, err := p.readRemoteDNSConnection(ctx, target.ConnectionID)
	if err != nil {
		return err
	}
	if c.Status != "ready" {
		return errRemoteDNSNotReady
	}
	var receipt remoteDNSPublicationReceipt
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	err = remoteDNSExchange(callCtx, c.Endpoint, "/api/v1/dns/remote/receiver/publish", c.ID+"."+c.credential, payload, &receipt)
	if err != nil {
		return errRemoteDNSPublicationPending
	}
	if !receipt.Applied || receipt.ClientID != c.ID || receipt.Domain != target.Domain || receipt.Generation != generation || receipt.PayloadHash != hash || receipt.Deleted != payload.Deleted {
		return errors.New("remote DNS receipt does not match the saved publication")
	}
	tx, err := p.db.GetDB().BeginTx(callCtx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`UPDATE remote_dns_zones SET applied_generation=? WHERE domain_id=? AND generation=? AND payload_hash=?`, []any{generation, target.ID, generation, hash}},
		{`UPDATE remote_dns_origin_history SET applied_generation=? WHERE connection_id=? AND zone_name=? AND generation=? AND payload_hash=?`, []any{generation, target.ConnectionID, target.Domain, generation, hash}},
	} {
		result, err := tx.ExecContext(callCtx, statement.query, statement.args...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return errors.New("remote DNS receipt no longer matches its durable history")
		}
	}
	return tx.Commit()
}

func (p *Panel) ensureRemoteDomainDNS(ctx context.Context, name string) error {
	return p.mutateRemoteDNSRecords(ctx, name, false, func(records []DNSRecord) ([]DNSRecord, error) {
		for _, record := range records {
			if remoteDNSNameWithin(record.Name, name) {
				return records, nil
			}
		}
		return append(records, externalDomainDNSRecords(name, serverPrimaryIP(), serverPrimaryIPv6())...), nil
	})
}
func (p *Panel) syncRemoteDomainDNS(ctx context.Context, name string, deleted bool) error {
	return p.mutateRemoteDNSRecords(ctx, name, deleted, nil)
}
func (p *Panel) setRemoteDomainDNSRecords(ctx context.Context, name string, replacement []DNSRecord) error {
	canonical, err := canonicalRemoteDNSRecords(name, replacement)
	if err != nil {
		return err
	}
	return p.mutateRemoteDNSRecords(ctx, name, false, func(records []DNSRecord) ([]DNSRecord, error) {
		kept := []DNSRecord{}
		for _, record := range records {
			if !remoteDNSNameWithin(record.Name, name) {
				kept = append(kept, record)
			}
		}
		return append(kept, canonical...), nil
	})
}

func remoteDNSMailTXTGroup(record DNSRecord) string {
	if record.Type != "TXT" {
		return ""
	}
	value, err := decodeDNSUserTXT(record.Content)
	if err != nil {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"v=spf1", "v=dkim1", "v=dmarc1"} {
		if value == prefix || strings.HasPrefix(value, prefix+" ") || strings.HasPrefix(value, prefix+";") {
			return prefix
		}
	}
	return ""
}

func (p *Panel) remoteDNSMailRecords(ctx context.Context, name string, desired []DNSRecord) error {
	canonical, err := canonicalRemoteDNSRecords(name, desired)
	if err != nil {
		return err
	}
	return p.mutateRemoteDNSRecords(ctx, name, false, func(records []DNSRecord) ([]DNSRecord, error) {
		groups := map[string][]DNSRecord{}
		txtGroups := map[string]bool{}
		mailHost := "mail." + name
		hasMailAddress := false
		hasIPv6 := false
		for _, record := range canonical {
			key := record.Name + "/" + record.Type
			if record.Type == "TXT" {
				family := remoteDNSMailTXTGroup(record)
				if family == "" {
					return nil, errors.New("mail DNS apply requires an SPF, DKIM or DMARC TXT record")
				}
				txtGroups[record.Name+"/"+family] = true
			}
			groups[key] = append(groups[key], record)
			if record.Name == mailHost && (record.Type == "A" || record.Type == "AAAA") {
				hasMailAddress = true
				if record.Type == "AAAA" {
					hasIPv6 = true
				}
			}
		}
		for _, record := range records {
			if hasMailAddress && record.Name == mailHost && !record.Disabled && (record.Type == "CNAME" || (record.Type == "AAAA" && !hasIPv6)) {
				return nil, errRemoteDNSMailConflict
			}
			replacements := groups[record.Name+"/"+record.Type]
			if len(replacements) == 0 || record.Type == "TXT" {
				continue
			}
			compatible := false
			for _, next := range replacements {
				if record.Content == next.Content && record.Prio == next.Prio && !record.Disabled {
					compatible = true
				}
			}
			if !compatible {
				return nil, errRemoteDNSMailConflict
			}
		}
		kept := []DNSRecord{}
		for _, record := range records {
			_, replace := groups[record.Name+"/"+record.Type]
			if record.Type == "TXT" {
				replace = txtGroups[record.Name+"/"+remoteDNSMailTXTGroup(record)]
			}
			if !replace {
				kept = append(kept, record)
			}
		}
		return append(kept, canonical...), nil
	})
}
