package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
)

type remoteDNSPublication struct {
	Domain      string      `json:"domain"`
	Generation  int64       `json:"generation"`
	Deleted     bool        `json:"deleted"`
	Records     []DNSRecord `json:"records"`
	PayloadHash string      `json:"payload_hash"`
}
type remoteDNSPublicationReceipt struct {
	ClientID    string `json:"client_id"`
	Domain      string `json:"domain"`
	Generation  int64  `json:"generation"`
	PayloadHash string `json:"payload_hash"`
	Deleted     bool   `json:"deleted"`
	Applied     bool   `json:"applied"`
}

func canonicalRemoteDNSRecords(domain string, records []DNSRecord) ([]DNSRecord, error) {
	canonical, err := hostname.CanonicalFQDN(domain)
	if err != nil || canonical != domain {
		return nil, errors.New("invalid DNS zone identity")
	}
	if len(records) > 1024 {
		return nil, errors.New("too many DNS records")
	}
	result := make([]DNSRecord, 0, len(records))
	seen := map[string]bool{}
	ownerTypes := map[string]map[string]bool{}
	cnameOwners := map[string]bool{}
	for _, record := range records {
		owner, err := normalizeDNSOwner(record.Name, domain)
		if err != nil {
			return nil, err
		}
		kind, content, prio, err := normalizeDNSRecord(record.Type, owner, record.Content, record.TTL, record.Prio, domain)
		if err != nil {
			return nil, err
		}
		record = DNSRecord{Name: owner, Type: kind, Content: content, TTL: record.TTL, Disabled: record.Disabled}
		if prio != nil {
			record.Prio = *prio
		}
		raw, _ := json.Marshal(record)
		key := string(raw)
		if seen[key] {
			return nil, errors.New("duplicate DNS record")
		}
		seen[key] = true
		if !record.Disabled {
			if kind == "CNAME" {
				if cnameOwners[owner] {
					return nil, errors.New("a DNS owner cannot have multiple CNAME targets")
				}
				cnameOwners[owner] = true
			}
			if ownerTypes[owner] == nil {
				ownerTypes[owner] = map[string]bool{}
			}
			ownerTypes[owner][kind] = true
		}
		result = append(result, record)
	}
	for _, types := range ownerTypes {
		if types["CNAME"] && len(types) > 1 {
			return nil, errors.New("a CNAME cannot coexist with other records")
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, _ := json.Marshal(result[i])
		b, _ := json.Marshal(result[j])
		return string(a) < string(b)
	})
	return result, nil
}
func canonicalRemoteDNSPublication(request remoteDNSPublication) (remoteDNSPublication, error) {
	if request.Generation < 1 || request.Generation > 2147483647 {
		return request, errors.New("invalid remote DNS generation")
	}
	records, err := canonicalRemoteDNSRecords(request.Domain, request.Records)
	if err != nil {
		return request, err
	}
	if request.Deleted && len(records) != 0 {
		return request, errors.New("a deleted DNS zone cannot contain records")
	}
	request.Records = records
	request.PayloadHash = ""
	raw, err := remoteDNSCanonicalJSON(request)
	if err != nil {
		return request, err
	}
	request.PayloadHash = remoteDNSHash(string(raw))
	return request, nil
}

func (p *Panel) handleRemoteDNSReceivePublication(w http.ResponseWriter, r *http.Request, claim remoteDNSMachineClaim) {
	var request remoteDNSPublication
	if !decodeRemoteDNSBody(w, r, &request) {
		return
	}
	canonical, err := canonicalRemoteDNSPublication(request)
	if err != nil || !remoteDNSHashEqual(canonical.PayloadHash, request.PayloadHash) {
		writeClientError(w, 400, "invalid canonical DNS publication")
		return
	}
	// The receiver owns its local authority. The remote client controls only
	// records in its claimed zone, never topology, agent commands or NS/SOA.
	p.serviceMutationMu.Lock()
	defer p.serviceMutationMu.Unlock()
	dnsPublicationMu.Lock()
	defer dnsPublicationMu.Unlock()
	if !p.remoteDNSClientAuthorized(r.Context(), claim.clientID, claim.credentialHash) {
		writeClientError(w, 401, "Remote DNS authorization was revoked")
		return
	}
	var priorOwner, priorHash string
	var priorGeneration, priorApplied int64
	var priorDeleted bool
	priorErr := p.db.GetDB().QueryRowContext(r.Context(), `SELECT client_id,generation,applied_generation,payload_hash,deleted FROM remote_dns_zone_ownership WHERE zone_name=?`, canonical.Domain).Scan(&priorOwner, &priorGeneration, &priorApplied, &priorHash, &priorDeleted)
	if priorErr != nil && !errors.Is(priorErr, sql.ErrNoRows) {
		writeServerError(w, priorErr)
		return
	}
	exactPending := priorErr == nil && priorOwner == claim.clientID && priorGeneration == canonical.Generation && priorHash == canonical.PayloadHash && priorDeleted == canonical.Deleted && priorApplied < priorGeneration
	applied := false
	if !exactPending {
		proof, proofErr := remoteDNSReadLocalAuthority(p, r.Context())
		if proofErr != nil {
			remoteDNSWriteNotReady(w)
			return
		}
		applied, err = p.prepareRemoteDNSReceivedPublication(r.Context(), claim.clientID, canonical, proof)
		if err != nil {
			writeCodedError(w, 409, "REMOTE_DNS_PUBLICATION_CONFLICT", "The DNS zone owner or pending generation differs; reconcile the saved publication", "")
			return
		}
	}
	if !applied {
		if err = remoteDNSPublishReceived(p, r.Context(), canonical.Domain, canonical.Deleted); err != nil {
			writeCodedError(w, 409, "REMOTE_DNS_PUBLICATION_PENDING", "The exact DNS generation is saved and is waiting for authoritative publication", "")
			return
		}
		result, err := p.db.GetDB().ExecContext(r.Context(), `UPDATE remote_dns_zone_ownership SET applied_generation=? WHERE zone_name=? AND client_id=? AND generation=? AND payload_hash=? AND deleted=?`, canonical.Generation, canonical.Domain, claim.clientID, canonical.Generation, canonical.PayloadHash, canonical.Deleted)
		if err != nil {
			writeServerError(w, err)
			return
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			writeClientError(w, 409, "DNS publication receipt changed")
			return
		}
	}
	_ = json.NewEncoder(w).Encode(remoteDNSPublicationReceipt{ClientID: claim.clientID, Domain: canonical.Domain, Generation: canonical.Generation, PayloadHash: canonical.PayloadHash, Deleted: canonical.Deleted, Applied: true})
}

// Caller holds the normal receiver host/publication locks. The production
// implementation reuses V3 exact agent leases, generation and paired receipts.
var remoteDNSPublishReceived = func(p *Panel, ctx context.Context, domain string, deleted bool) error {
	return p.syncZoneToDNSLocked(ctx, domain, deleted)
}

func (p *Panel) prepareRemoteDNSReceivedPublication(ctx context.Context, clientID string, request remoteDNSPublication, proof remoteDNSAuthority) (bool, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var owner, hash string
	var generation, applied int64
	var deleted bool
	err = tx.QueryRowContext(ctx, `SELECT client_id,generation,payload_hash,applied_generation,deleted FROM remote_dns_zone_ownership WHERE zone_name=?`, request.Domain).Scan(&owner, &generation, &hash, &applied, &deleted)
	fresh := errors.Is(err, sql.ErrNoRows)
	if err != nil && !fresh {
		return false, err
	}
	if fresh {
		if request.Generation != 1 || request.Deleted {
			return false, errors.New("a new zone starts with its first live generation")
		}
		// A grant cannot adopt local zones or shadow another owner's namespace.
		rows, err := tx.QueryContext(ctx, `SELECT name,'' FROM domains UNION ALL SELECT hostname,'' FROM hostname_reservations UNION ALL SELECT d.name,COALESCE(o.client_id,'') FROM pdns_domains d LEFT JOIN remote_dns_zone_ownership o ON o.zone_name=d.name UNION ALL SELECT zone_name,client_id FROM remote_dns_zone_ownership`)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var name, id string
			if err = rows.Scan(&name, &id); err != nil {
				rows.Close()
				return false, err
			}
			overlaps := name == request.Domain || strings.HasSuffix(name, "."+request.Domain) || strings.HasSuffix(request.Domain, "."+name)
			if overlaps && (id == "" || id != clientID || name == request.Domain) {
				rows.Close()
				return false, errors.New("DNS namespace already belongs to another authority")
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		rows.Close()
		_, err = tx.ExecContext(ctx, `INSERT INTO remote_dns_zone_ownership(zone_name,client_id) VALUES(?,?)`, request.Domain, clientID)
		if err != nil {
			return false, err
		}
	} else {
		if owner != clientID {
			return false, errors.New("DNS zone belongs to another publishing client")
		}
		if request.Generation == generation {
			if hash != request.PayloadHash || deleted != request.Deleted {
				return false, errors.New("generation payload differs")
			}
			return applied == generation, tx.Commit()
		}
		if request.Generation != generation+1 || applied != generation {
			return false, errors.New("reconcile the previous exact generation first")
		}
		if deleted && request.Deleted {
			return false, errors.New("replay the exact applied deletion receipt")
		}
	}
	if request.Deleted {
		if _, err = tx.ExecContext(ctx, `DELETE FROM pdns_domains WHERE name=?`, request.Domain); err != nil {
			return false, err
		}
	} else {
		var zoneID int
		err = tx.QueryRowContext(ctx, `SELECT id FROM pdns_domains WHERE name=?`, request.Domain).Scan(&zoneID)
		if errors.Is(err, sql.ErrNoRows) && (fresh || deleted) {
			result, createErr := tx.ExecContext(ctx, `INSERT INTO pdns_domains(name,type) VALUES(?,'MASTER')`, request.Domain)
			if createErr != nil {
				return false, createErr
			}
			id, idErr := result.LastInsertId()
			if idErr != nil {
				return false, idErr
			}
			zoneID = int(id)
		} else if err != nil {
			return false, errors.New("owned DNS zone is missing; explicit recovery is required")
		}
		serial := time.Now().UTC().Format("2006010200")
		if !fresh && !deleted {
			var soa string
			if err = tx.QueryRowContext(ctx, `SELECT content FROM pdns_records WHERE domain_id=? AND type='SOA'`, zoneID).Scan(&soa); err != nil {
				return false, err
			}
			fields := strings.Fields(soa)
			if len(fields) != 7 {
				return false, errors.New("invalid previous DNS SOA")
			}
			serial = fields[2]
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM pdns_records WHERE domain_id=?`, zoneID); err != nil {
			return false, err
		}
		records := append([]DNSRecord(nil), request.Records...)
		records = append(records, DNSRecord{Name: request.Domain, Type: "SOA", Content: fmt.Sprintf("%s hostmaster.%s %s 10800 3600 604800 3600", proof.Nameservers[0], request.Domain, serial), TTL: 3600})
		for _, ns := range proof.Nameservers {
			records = append(records, DNSRecord{Name: request.Domain, Type: "NS", Content: ns, TTL: 3600})
		}
		for _, record := range records {
			if _, err = tx.ExecContext(ctx, `INSERT INTO pdns_records(domain_id,name,type,content,ttl,prio,disabled) VALUES(?,?,?,?,?,?,?)`, zoneID, record.Name, record.Type, record.Content, record.TTL, record.Prio, record.Disabled); err != nil {
				return false, err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE remote_dns_zone_ownership SET generation=?,payload_hash=?,deleted=? WHERE zone_name=? AND client_id=?`, request.Generation, request.PayloadHash, request.Deleted, request.Domain, clientID)
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}
