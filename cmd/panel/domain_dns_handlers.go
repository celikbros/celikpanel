package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// DNS Record structure matching pdns_records
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

// DNS Zone structure matching pdns_domains
type DNSZone struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (p *Panel) handleDomainDNS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract domain ID from URL /api/v1/domains/:id/dns...
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	domainID, err := strconv.Atoi(pathParts[4])
	if err != nil {
		http.Error(w, "Invalid domain ID", http.StatusBadRequest)
		return
	}

	// Resolve domain name from ID (Panel Domain ID -> Name -> PDNS Domain Name)
	repo := repositories.NewPostgresDomainRepository(p.db.GetDB())
	domainsList, _ := repo.List(context.Background())
	var domainName string
	for _, d := range domainsList {
		if d.ID == domainID {
			domainName = d.Name
			break
		}
	}
	if domainName == "" {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Dispatch
	if strings.HasSuffix(r.URL.Path, "/records") {
		switch r.Method {
		case "GET":
			p.handleListDNSRecords(w, domainName)
		case "POST":
			p.handleAddDNSRecord(w, r, domainName)
		case "PUT":
			p.handleUpdateDNSRecord(w, r, domainName) // Update entire record or specific fields?
		case "DELETE":
			p.handleDeleteDNSRecord(w, r, domainName)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	} else if strings.HasSuffix(r.URL.Path, "/zone") {
		// Check/Create Zone
		if r.Method == "POST" {
			p.handleCreateDNSZone(w, r, domainName)
		} else if r.Method == "GET" {
			p.handleGetDNSZone(w, domainName)
		}
	} else {
		http.NotFound(w, r)
	}
}

func (p *Panel) handleGetDNSZone(w http.ResponseWriter, domainName string) {
	pool := p.db.GetDB()
	var zone DNSZone
	err := pool.QueryRowContext(context.Background(), "SELECT id, name, type FROM pdns_domains WHERE name = ?", domainName).Scan(&zone.ID, &zone.Name, &zone.Type)
	if err != nil {
		// Not found is 404
		http.Error(w, "Zone not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(zone)
}

func (p *Panel) handleCreateDNSZone(w http.ResponseWriter, r *http.Request, domainName string) {
	zoneID, created, err := p.createZoneWithTemplate(r.Context(), domainName)
	if err != nil {
		writeServerError(w, err)
		return
	}
	// POST is intentionally idempotent: an existing zone is the normal retry
	// target after domain creation partially succeeded. Always publish the full
	// ledger zone, whether it was just created or already existed.
	if err := p.syncZoneToDNS(r.Context(), domainName, false); err != nil {
		var publicationErr *dnsAgentPublicationError
		if errors.As(err, &publicationErr) {
			log.Printf("[409][dns] publish zone %s: %v", domainName, err)
			writeCodedError(w, http.StatusConflict, "DNS_PUBLICATION_FAILED",
				"the DNS zone is saved, but it could not be published; check the DNS service and retry", "")
			return
		}
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": zoneID, "created": created})
}

func (p *Panel) handleListDNSRecords(w http.ResponseWriter, domainName string) {
	pool := p.db.GetDB()

	// Get Zone ID
	var zoneID int
	err := pool.QueryRowContext(context.Background(), "SELECT id FROM pdns_domains WHERE name = ?", domainName).Scan(&zoneID)
	if err != nil {
		http.Error(w, "Zone not found", http.StatusNotFound)
		return
	}

	rows, err := pool.QueryContext(context.Background(),
		"SELECT id, domain_id, name, type, content, ttl, prio, disabled FROM pdns_records WHERE domain_id = ? ORDER BY type, name", zoneID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	var records []DNSRecord
	for rows.Next() {
		var r DNSRecord
		var prio *int // handle nullable
		if err := rows.Scan(&r.ID, &r.DomainID, &r.Name, &r.Type, &r.Content, &r.TTL, &prio, &r.Disabled); err != nil {
			continue
		}
		if prio != nil {
			r.Prio = *prio
		}
		records = append(records, r)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"records": records})
}

func (p *Panel) handleAddDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	var req DNSRecord
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	pool := p.db.GetDB()

	// Get Zone ID
	var zoneID int
	err := pool.QueryRowContext(context.Background(), "SELECT id FROM pdns_domains WHERE name = ?", domainName).Scan(&zoneID)
	if err != nil {
		http.Error(w, "Zone not found", http.StatusNotFound)
		return
	}

	// Insert
	// Normalize name: if @ use domainName, if not fully qualified, append domainName?
	// PowerDNS expects fully qualified names usually?
	// If name == "@", replace with domainName
	// If name doesn't end with dot, append domainName?
	// Usually users type "www", we store "www.domain.com"

	finalName := req.Name
	if finalName == "@" {
		finalName = domainName
	} else if !strings.HasSuffix(finalName, domainName) {
		finalName = finalName + "." + domainName
	}

	// Priorty is for MX/SRV
	var prioPtr *int
	if req.Type == "MX" || req.Type == "SRV" {
		prioPtr = &req.Prio
	}

	_, err = pool.ExecContext(context.Background(),
		"INSERT INTO pdns_records (domain_id, name, type, content, ttl, prio) VALUES (?, ?, ?, ?, ?, ?)",
		zoneID, finalName, req.Type, req.Content, req.TTL, prioPtr)

	if err != nil {
		writeServerError(w, err)
		return
	}

	p.syncZoneToDNS(context.Background(), domainName, false)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleDeleteDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	pool := p.db.GetDB()
	// Ensure record belongs to domain
	var count int
	pool.QueryRowContext(context.Background(),
		"SELECT count(*) FROM pdns_records r JOIN pdns_domains d ON r.domain_id = d.id WHERE r.id = ? AND d.name = ?",
		id, domainName).Scan(&count)

	if count == 0 {
		http.Error(w, "Record not found or access denied", http.StatusForbidden)
		return
	}

	_, err := pool.ExecContext(context.Background(), "DELETE FROM pdns_records WHERE id = ?", id)
	if err != nil {
		writeServerError(w, err)
		return
	}

	p.syncZoneToDNS(context.Background(), domainName, false)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// Update handles just content/ttl update
func (p *Panel) handleUpdateDNSRecord(w http.ResponseWriter, r *http.Request, domainName string) {
	var req DNSRecord
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	pool := p.db.GetDB()
	// Check ownership
	var count int
	pool.QueryRowContext(context.Background(),
		"SELECT count(*) FROM pdns_records r JOIN pdns_domains d ON r.domain_id = d.id WHERE r.id = ? AND d.name = ?",
		req.ID, domainName).Scan(&count)

	if count == 0 {
		http.Error(w, "Record not found", http.StatusNotFound)
		return
	}

	// Update
	_, err := pool.ExecContext(context.Background(),
		"UPDATE pdns_records SET content=?, ttl=?, prio=?, disabled=? WHERE id=?",
		req.Content, req.TTL, req.Prio, req.Disabled, req.ID)

	if err != nil {
		writeServerError(w, err)
		return
	}

	p.syncZoneToDNS(context.Background(), domainName, false)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
