package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

func (p *Panel) handleRemoteDomainDNS(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("X-CelikPanel-DNS-Management", setupDNSModeExisting)
	w.Header().Set("Content-Type", "application/json")
	target, err := resolveRemoteDNSDomain(r.Context(), p.db.GetDB(), name)
	if err != nil {
		writeDNSZoneLookupError(w, err)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/zone") {
		switch r.Method {
		case http.MethodGet:
			exists, err := p.remoteDomainDNSExists(r.Context(), name)
			if err != nil {
				writeServerError(w, err)
				return
			}
			if !exists {
				writeClientError(w, 404, "DNS zone not found")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": target.ID, "name": name, "type": "REMOTE", "management": setupDNSModeExisting, "managed": true})
			return
		case http.MethodPost:
			if err = p.ensureRemoteDomainDNS(r.Context(), name); err != nil {
				writeRemoteDNSPublicationError(w, err)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "id": target.ID})
			return
		default:
			rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost})
			return
		}
	}
	if !strings.HasSuffix(r.URL.Path, "/records") {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		records, err := p.remoteDomainDNSRecords(r.Context(), name)
		if err != nil {
			writeServerError(w, err)
			return
		}
		var published bool
		err = p.db.GetDB().QueryRowContext(r.Context(), `SELECT generation=applied_generation AND deleted=0 FROM remote_dns_zones WHERE domain_id=?`, target.ID).Scan(&published)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"records": records, "management": setupDNSModeExisting, "published": published})
		return
	}
	var mutation remoteDNSRecordMutation
	switch r.Method {
	case http.MethodPost:
		var request dnsRecordCreateRequest
		if !decodeDNSRequest(w, r, &request) {
			return
		}
		owner, err := normalizeDNSOwner(request.Name, name)
		if err != nil {
			writeClientError(w, 400, err.Error())
			return
		}
		canonical, err := canonicalRemoteDNSRecords(target.Domain, []DNSRecord{{Name: owner, Type: request.Type, Content: request.Content, TTL: request.TTL, Prio: request.Prio}})
		if err != nil {
			writeClientError(w, 400, err.Error())
			return
		}
		next := canonical[0]
		mutation = func(records []DNSRecord) ([]DNSRecord, error) {
			for _, record := range records {
				if record.Name == next.Name && record.Type == next.Type && record.Content == next.Content && record.TTL == next.TTL && record.Prio == next.Prio && record.Disabled == next.Disabled {
					return records, nil
				}
			}
			return append(records, next), nil
		}
	case http.MethodPut:
		var request dnsRecordUpdateRequest
		if !decodeDNSRequest(w, r, &request) {
			return
		}
		mutation = func(records []DNSRecord) ([]DNSRecord, error) {
			for index, record := range records {
				if record.ID != request.ID || !remoteDNSNameWithin(record.Name, name) {
					continue
				}
				_, content, prio, err := normalizeDNSRecord(record.Type, record.Name, request.Content, request.TTL, request.Prio, target.Domain)
				if err != nil {
					return nil, &remoteDNSRecordValidationError{cause: err}
				}
				record.Content = content
				record.TTL = request.TTL
				record.Disabled = request.Disabled
				record.Prio = 0
				if prio != nil {
					record.Prio = *prio
				}
				records[index] = record
				return records, nil
			}
			return nil, &remoteDNSRecordValidationError{cause: errors.New("DNS record not found in this domain")}
		}
	case http.MethodDelete:
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil || id <= 0 {
			writeClientError(w, 400, "invalid DNS record identity")
			return
		}
		mutation = func(records []DNSRecord) ([]DNSRecord, error) {
			kept := []DNSRecord{}
			for _, record := range records {
				if record.ID == id {
					if !remoteDNSNameWithin(record.Name, name) {
						return nil, &remoteDNSRecordValidationError{cause: errors.New("DNS record belongs to another domain")}
					}
					continue
				}
				kept = append(kept, record)
			}
			return kept, nil
		}
	default:
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete})
		return
	}
	if err = p.mutateRemoteDNSRecords(r.Context(), name, false, mutation); err != nil {
		writeRemoteDNSPublicationError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
func writeRemoteDNSPublicationError(w http.ResponseWriter, err error) {
	var validation *remoteDNSRecordValidationError
	if errors.As(err, &validation) {
		writeClientError(w, http.StatusBadRequest, validation.Error())
		return
	}
	if errors.Is(err, errRemoteDNSMailConflict) {
		writeCodedError(w, 409, "REMOTE_DNS_MAIL_RECORD_CONFLICT", "Existing mail DNS records differ; review them before enabling this mail service", "")
		return
	}
	writeCodedError(w, 409, "REMOTE_DNS_PUBLICATION_PENDING", "Check the authorized DNS connection and reconcile its saved publication before making another change", "")
}
