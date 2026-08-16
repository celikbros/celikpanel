package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/hostname"
)

const (
	maxDNSRequestBytes = 64 << 10
	maxDNSTTL          = 1<<31 - 1
	maxDNSTXTBytes     = 4096
	dnsPublishTimeout  = 30 * time.Second
)

var errDNSZoneNotFound = errors.New("DNS zone not found")

// DNSRecord is the ownership-filtered representation returned to the UI.
type DNSRecord struct {
	ID       int    `json:"id"`
	DomainID int    `json:"domain_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio,omitempty"`
	Disabled bool   `json:"disabled"`
}

type DNSZone struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type dnsRecordCreateRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Prio    int    `json:"prio"`
}

type dnsRecordUpdateRequest struct {
	ID       int    `json:"id"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

func (p *Panel) handleDomainDNS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		writeClientError(w, http.StatusBadRequest, "invalid path")
		return
	}
	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil || domainID <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid domain ID")
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
		if _, ready := p.requireActiveDNSPublisherForMutation(w, r.Context()); !ready {
			return
		}
	}

	domainName, err := p.domainNameForDNS(r.Context(), domainID)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/records"):
		switch r.Method {
		case http.MethodGet:
			p.handleListDNSRecords(w, r, domainName)
		case http.MethodPost:
			p.handleAddDNSRecord(w, r, domainName)
		case http.MethodPut:
			p.handleUpdateDNSRecord(w, r, domainName)
		case http.MethodDelete:
			p.handleDeleteDNSRecord(w, r, domainName)
		default:
			rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete})
		}
	case strings.HasSuffix(r.URL.Path, "/zone"):
		switch r.Method {
		case http.MethodPost:
			p.handleCreateDNSZone(w, r, domainName)
		case http.MethodGet:
			p.handleGetDNSZone(w, r, domainName)
		default:
			rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
		}
	default:
		http.NotFound(w, r)
	}
}

func (p *Panel) domainNameForDNS(ctx context.Context, domainID int) (string, error) {
	var domainName string
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT name FROM domains WHERE id = ?`, domainID,
	).Scan(&domainName); err != nil {
		return "", err
	}
	canonical, err := hostname.CanonicalFQDN(domainName)
	if err != nil {
		return "", fmt.Errorf("invalid stored domain name for %d: %w", domainID, err)
	}
	return canonical, nil
}

func (p *Panel) dnsZoneID(ctx context.Context, domainName string) (int, error) {
	var zoneID int
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT id FROM pdns_domains WHERE name = ?`, domainName,
	).Scan(&zoneID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errDNSZoneNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("load DNS zone %s: %w", domainName, err)
	}
	return zoneID, nil
}

func writeDNSZoneLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, errDNSZoneNotFound) {
		writeClientError(w, http.StatusNotFound, "DNS zone not found")
		return
	}
	writeServerError(w, err)
}

func (p *Panel) handleGetDNSZone(w http.ResponseWriter, r *http.Request, domainName string) {
	var zone DNSZone
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT id, name, type FROM pdns_domains WHERE name = ?`, domainName,
	).Scan(&zone.ID, &zone.Name, &zone.Type)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "DNS zone not found")
		return
	}
	if err != nil {
		writeServerError(w, fmt.Errorf("load DNS zone %s: %w", domainName, err))
		return
	}
	if err := json.NewEncoder(w).Encode(zone); err != nil {
		log.Printf("encode DNS zone %s: %v", domainName, err)
	}
}

func (p *Panel) handleCreateDNSZone(w http.ResponseWriter, r *http.Request, domainName string) {
	zoneID, created, err := p.createZoneWithTemplate(r.Context(), domainName)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if !p.publishDNSMutation(w, r.Context(), domainName) {
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true, "id": zoneID, "created": created,
	}); err != nil {
		log.Printf("encode DNS zone creation %s: %v", domainName, err)
	}
}

func (p *Panel) handleListDNSRecords(w http.ResponseWriter, r *http.Request, domainName string) {
	zoneID, err := p.dnsZoneID(r.Context(), domainName)
	if err != nil {
		writeDNSZoneLookupError(w, err)
		return
	}

	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT id, domain_id, name, type, content, ttl, prio, disabled
		FROM pdns_records
		WHERE domain_id = ?
		ORDER BY type, name, id`, zoneID)
	if err != nil {
		writeServerError(w, fmt.Errorf("list DNS records for %s: %w", domainName, err))
		return
	}
	defer rows.Close()

	records := make([]DNSRecord, 0)
	for rows.Next() {
		var record DNSRecord
		var prio sql.NullInt64
		if err := rows.Scan(
			&record.ID, &record.DomainID, &record.Name, &record.Type,
			&record.Content, &record.TTL, &prio, &record.Disabled,
		); err != nil {
			writeServerError(w, fmt.Errorf("scan DNS record for %s: %w", domainName, err))
			return
		}
		if prio.Valid {
			record.Prio = int(prio.Int64)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, fmt.Errorf("read DNS records for %s: %w", domainName, err))
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"records": records}); err != nil {
		log.Printf("encode DNS records for %s: %v", domainName, err)
	}
}

func decodeDNSRequest(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxDNSRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid DNS record")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeClientError(w, http.StatusBadRequest, "invalid DNS record")
		return false
	}
	return true
}

func normalizeDNSOwner(rawName, rawZone string) (string, error) {
	zone, err := hostname.CanonicalFQDN(rawZone)
	if err != nil {
		return "", err
	}
	rawName = strings.TrimSpace(rawName)
	if rawName == "@" {
		return zone, nil
	}
	if rawName == "" {
		return "", errors.New("record name is required")
	}

	absolute := strings.HasSuffix(rawName, ".")
	name := strings.ToLower(strings.TrimSuffix(rawName, "."))
	if name == "" {
		return "", errors.New("record name is required")
	}
	if absolute && name != zone && !strings.HasSuffix(name, "."+zone) {
		return "", errors.New("absolute record name is outside this DNS zone")
	}
	if name != zone && !strings.HasSuffix(name, "."+zone) {
		name += "." + zone
	}
	if name != zone && !strings.HasSuffix(name, "."+zone) {
		return "", errors.New("record name is outside this DNS zone")
	}
	if err := validateDNSOwner(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateDNSOwner(name string) error {
	if name == "" || len(name) > 253 {
		return errors.New("invalid DNS record name")
	}
	labels := strings.Split(name, ".")
	for i, label := range labels {
		if label == "" || len(label) > 63 {
			return errors.New("invalid DNS record name")
		}
		if label == "*" {
			if i != 0 {
				return errors.New("wildcard is only allowed in the left-most label")
			}
			continue
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid DNS record name")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') &&
				character != '-' && character != '_' {
				return errors.New("invalid DNS record name")
			}
		}
	}
	return nil
}

func normalizeDNSRecord(
	recordType, ownerName, rawContent string,
	ttl, priority int,
	zoneName string,
) (string, string, *int, error) {
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	switch recordType {
	case "A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV":
	default:
		return "", "", nil, errors.New("unsupported DNS record type")
	}
	if ttl < 0 || ttl > maxDNSTTL {
		return "", "", nil, errors.New("TTL must be between 0 and 2147483647")
	}
	if (recordType == "CNAME" || recordType == "NS") && ownerName == zoneName {
		if recordType == "CNAME" {
			return "", "", nil, errors.New("a zone apex cannot be a CNAME")
		}
		return "", "", nil, errors.New("apex nameservers are managed in DNS settings")
	}

	content := strings.TrimSpace(rawContent)
	var normalized string
	switch recordType {
	case "A", "AAAA":
		address, err := netip.ParseAddr(content)
		if err != nil || (recordType == "A" && !address.Is4()) || (recordType == "AAAA" && !address.Is6()) {
			return "", "", nil, fmt.Errorf("invalid %s address", recordType)
		}
		normalized = address.String()
	case "CNAME", "MX", "NS":
		target, err := hostname.CanonicalFQDN(content)
		if err != nil {
			return "", "", nil, fmt.Errorf("invalid %s target", recordType)
		}
		normalized = target
	case "TXT":
		value, err := decodeDNSUserTXT(content)
		if err != nil {
			return "", "", nil, err
		}
		normalized, err = encodeDNSUserTXT(value)
		if err != nil {
			return "", "", nil, err
		}
	case "SRV":
		fields := strings.Fields(content)
		if len(fields) != 3 {
			return "", "", nil, errors.New("SRV content must be: weight port target")
		}
		weight, err := strconv.ParseUint(fields[0], 10, 16)
		if err != nil {
			return "", "", nil, errors.New("invalid SRV weight")
		}
		port, err := strconv.ParseUint(fields[1], 10, 16)
		if err != nil {
			return "", "", nil, errors.New("invalid SRV port")
		}
		target := fields[2]
		if target != "." {
			target, err = hostname.CanonicalFQDN(target)
			if err != nil {
				return "", "", nil, errors.New("invalid SRV target")
			}
		}
		normalized = fmt.Sprintf("%d %d %s", weight, port, target)
	}

	var prio *int
	if recordType == "MX" || recordType == "SRV" {
		if priority < 0 || priority > 65535 {
			return "", "", nil, errors.New("priority must be between 0 and 65535")
		}
		value := priority
		prio = &value
	}
	return recordType, normalized, prio, nil
}

func decodeDNSUserTXT(content string) (string, error) {
	if content == "" {
		return "", errors.New("TXT content is required")
	}
	if !strings.HasPrefix(content, `"`) {
		if strings.ContainsAny(content, `"\\`) {
			return "", errors.New("TXT content cannot contain quotes or backslashes")
		}
		return content, nil
	}

	var value strings.Builder
	for rest := strings.TrimSpace(content); rest != ""; {
		if rest[0] != '"' {
			return "", errors.New("invalid quoted TXT content")
		}
		rest = rest[1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 || strings.ContainsRune(rest[:end], '\\') {
			return "", errors.New("invalid quoted TXT content")
		}
		value.WriteString(rest[:end])
		rest = strings.TrimSpace(rest[end+1:])
	}
	if value.Len() == 0 {
		return "", errors.New("TXT content is required")
	}
	return value.String(), nil
}

func encodeDNSUserTXT(value string) (string, error) {
	if value == "" || len(value) > maxDNSTXTBytes {
		return "", errors.New("TXT content must be between 1 and 4096 bytes")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f || character == '"' || character == '\\' {
			return "", errors.New("TXT content contains unsupported control or quote characters")
		}
	}

	segments := make([]string, 0, len(value)/255+1)
	for len(value) > 0 {
		end := 0
		for index, character := range value {
			size := utf8.RuneLen(character)
			if index+size > 255 {
				break
			}
			end = index + size
		}
		if end == 0 {
			return "", errors.New("TXT content contains an invalid UTF-8 sequence")
		}
		segments = append(segments, `"`+value[:end]+`"`)
		value = value[end:]
	}
	return strings.Join(segments, " "), nil
}

func managedDNSRecord(zoneName, ownerName, recordType string) bool {
	recordType = strings.ToUpper(strings.TrimSpace(recordType))
	return recordType == "SOA" || (recordType == "NS" && ownerName == zoneName)
}

func (p *Panel) publishDNSMutation(w http.ResponseWriter, ctx context.Context, domainName string) bool {
	// The ledger mutation is already committed. A disconnected browser must
	// not cancel the corresponding authoritative publication halfway through.
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dnsPublishTimeout)
	defer cancel()
	if err := p.syncZoneToDNS(publishCtx, domainName, false); err != nil {
		var publicationErr *dnsAgentPublicationError
		if errors.As(err, &publicationErr) {
			log.Printf("[409][dns] publish zone %s: %v", domainName, err)
			writeCodedError(w, http.StatusConflict, errCodeDNSPublicationFailed,
				"the DNS change is saved, but it could not be published; check the DNS service and retry", "")
			return false
		}
		writeServerError(w, fmt.Errorf("publish DNS zone %s: %w", domainName, err))
		return false
	}
	return true
}

func (p *Panel) handleAddDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	var request dnsRecordCreateRequest
	if !decodeDNSRequest(w, r, &request) {
		return
	}
	ownerName, err := normalizeDNSOwner(request.Name, domainName)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	recordType, content, priority, err := normalizeDNSRecord(
		request.Type, ownerName, request.Content, request.TTL, request.Prio, domainName,
	)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}

	zoneID, err := p.dnsZoneID(r.Context(), domainName)
	if err != nil {
		writeDNSZoneLookupError(w, err)
		return
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()

	var conflictCount int
	if recordType == "CNAME" {
		err = tx.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM pdns_records WHERE domain_id = ? AND name = ?`,
			zoneID, ownerName,
		).Scan(&conflictCount)
	} else {
		err = tx.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM pdns_records WHERE domain_id = ? AND name = ? AND type = 'CNAME'`,
			zoneID, ownerName,
		).Scan(&conflictCount)
	}
	if err != nil {
		writeServerError(w, fmt.Errorf("check DNS record conflicts: %w", err))
		return
	}
	if conflictCount != 0 {
		writeClientError(w, http.StatusConflict, "CNAME records cannot coexist with other records at the same name")
		return
	}

	var duplicateCount int
	if err := tx.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM pdns_records
		WHERE domain_id = ? AND name = ? AND type = ? AND content = ?
		  AND COALESCE(prio, 0) = COALESCE(?, 0)`,
		zoneID, ownerName, recordType, content, priority,
	).Scan(&duplicateCount); err != nil {
		writeServerError(w, fmt.Errorf("check duplicate DNS record: %w", err))
		return
	}
	if duplicateCount != 0 {
		writeClientError(w, http.StatusConflict, "this DNS record already exists")
		return
	}

	result, err := tx.ExecContext(r.Context(), `
		INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio)
		VALUES (?, ?, ?, ?, ?, ?)`,
		zoneID, ownerName, recordType, content, request.TTL, priority,
	)
	if err != nil {
		writeServerError(w, fmt.Errorf("insert DNS record: %w", err))
		return
	}
	recordID, err := result.LastInsertId()
	if err != nil {
		writeServerError(w, fmt.Errorf("read inserted DNS record identity: %w", err))
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, fmt.Errorf("commit DNS record: %w", err))
		return
	}
	if !p.publishDNSMutation(w, r.Context(), domainName) {
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": recordID}); err != nil {
		log.Printf("encode DNS record creation for %s: %v", domainName, err)
	}
}

func (p *Panel) handleDeleteDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	recordID, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || recordID <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid DNS record ID")
		return
	}
	zoneID, err := p.dnsZoneID(r.Context(), domainName)
	if err != nil {
		writeDNSZoneLookupError(w, err)
		return
	}

	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	var ownerName, recordType string
	err = tx.QueryRowContext(r.Context(), `
		SELECT name, type FROM pdns_records WHERE id = ? AND domain_id = ?`,
		recordID, zoneID,
	).Scan(&ownerName, &recordType)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "DNS record not found")
		return
	}
	if err != nil {
		writeServerError(w, fmt.Errorf("load DNS record: %w", err))
		return
	}
	if managedDNSRecord(domainName, ownerName, recordType) {
		writeClientError(w, http.StatusConflict, "SOA and apex nameserver records are managed by DNS settings")
		return
	}
	result, err := tx.ExecContext(r.Context(),
		`DELETE FROM pdns_records WHERE id = ? AND domain_id = ?`, recordID, zoneID)
	if err != nil {
		writeServerError(w, fmt.Errorf("delete DNS record: %w", err))
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, fmt.Errorf("verify DNS record deletion: %w", err))
		return
	}
	if affected != 1 {
		writeServerError(w, fmt.Errorf("delete DNS record: expected one affected row, got %d", affected))
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, fmt.Errorf("commit DNS record deletion: %w", err))
		return
	}
	if !p.publishDNSMutation(w, r.Context(), domainName) {
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		log.Printf("encode DNS record deletion for %s: %v", domainName, err)
	}
}

func (p *Panel) handleUpdateDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	var request dnsRecordUpdateRequest
	if !decodeDNSRequest(w, r, &request) {
		return
	}
	if request.ID <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid DNS record ID")
		return
	}
	zoneID, err := p.dnsZoneID(r.Context(), domainName)
	if err != nil {
		writeDNSZoneLookupError(w, err)
		return
	}

	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	var ownerName, recordType string
	err = tx.QueryRowContext(r.Context(), `
		SELECT name, type FROM pdns_records WHERE id = ? AND domain_id = ?`,
		request.ID, zoneID,
	).Scan(&ownerName, &recordType)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "DNS record not found")
		return
	}
	if err != nil {
		writeServerError(w, fmt.Errorf("load DNS record: %w", err))
		return
	}
	if managedDNSRecord(domainName, ownerName, recordType) {
		writeClientError(w, http.StatusConflict, "SOA and apex nameserver records are managed by DNS settings")
		return
	}
	_, content, priority, err := normalizeDNSRecord(
		recordType, ownerName, request.Content, request.TTL, request.Prio, domainName,
	)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := tx.ExecContext(r.Context(), `
		UPDATE pdns_records
		SET content = ?, ttl = ?, prio = ?, disabled = ?
		WHERE id = ? AND domain_id = ?`,
		content, request.TTL, priority, request.Disabled, request.ID, zoneID,
	)
	if err != nil {
		writeServerError(w, fmt.Errorf("update DNS record: %w", err))
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, fmt.Errorf("verify DNS record update: %w", err))
		return
	}
	if affected != 1 {
		writeServerError(w, fmt.Errorf("update DNS record: expected one affected row, got %d", affected))
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, fmt.Errorf("commit DNS record update: %w", err))
		return
	}
	if !p.publishDNSMutation(w, r.Context(), domainName) {
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		log.Printf("encode DNS record update for %s: %v", domainName, err)
	}
}
