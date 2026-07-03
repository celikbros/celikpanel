package main

import (
	"context"
	"encoding/json"
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
			p.handleCreateDNSZone(w, domainName)
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

// handleConfigurePowerDNS configures PowerDNS with PostgreSQL backend
func (p *Panel) handleConfigurePowerDNS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Connection settings come from the request; secrets never live in
	// source code.
	// Bağlantı ayarları istekten gelir; sırlar asla kaynak kodda durmaz.
	req := &ConfigurePowerDNSRequest{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Host == "" || req.User == "" || req.Password == "" || req.DBName == "" {
		http.Error(w, "host, user, password and dbname are required", http.StatusBadRequest)
		return
	}
	if req.Port == 0 {
		req.Port = 5432
	}

	var success bool
	err := p.agentClient.Call("Agent.ConfigurePowerDNS", req, &success)
	if err != nil {
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": success})
}

// ConfigurePowerDNSRequest matches Agent RPC structure
type ConfigurePowerDNSRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
}

func (p *Panel) handleCreateDNSZone(w http.ResponseWriter, domainName string) {
	pool := p.db.GetDB()
	
	// Check if exists
	var exists bool
	pool.QueryRowContext(context.Background(), "SELECT EXISTS(SELECT 1 FROM pdns_domains WHERE name=?)", domainName).Scan(&exists)
	if exists {
		http.Error(w, "Zone already exists", http.StatusConflict)
		return
	}

	// Create Zone
	// type=NATIVE for postgres backend usually
	var zoneID int
	result, err := pool.ExecContext(context.Background(), 
		"INSERT INTO pdns_domains (name, type) VALUES (?, 'NATIVE')", domainName)
	if err != nil {
		writeServerError(w, err)
		return
	}
	id64, _ := result.LastInsertId()
	zoneID = int(id64)

	// Add default records? (SOA, NS)
	// SOA is critical for PowerDNS to serve the zone.
	soaContent := "ns1." + domainName + " hostmaster." + domainName + " 2023010101 10800 3600 604800 3600"
	pool.ExecContext(context.Background(),
		"INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, 'SOA', ?, 3600)",
		zoneID, domainName, soaContent)
	
	// NS
	pool.ExecContext(context.Background(),
		"INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, 'NS', ?, 3600)",
		zoneID, domainName, "ns1."+domainName)
	pool.ExecContext(context.Background(),
		"INSERT INTO pdns_records (domain_id, name, type, content, ttl) VALUES (?, ?, 'NS', ?, 3600)",
		zoneID, domainName, "ns2."+domainName)

	// A record for ns1, ns2 pointing to server IP? 
	// Need checking IPs. For now we assume user adds A records.
	
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": zoneID})
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

	// Notify PDNS to reload zone? Not needed with Postgres backend usually, unless caching
	// We can execute `pdns_control purge zone` via Agent if needed.
	
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
	
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
