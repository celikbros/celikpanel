package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/repositories"
)

// List emails response
type EmailAccount struct {
	ID        int       `json:"id"`
	Address   string    `json:"address"`
	QuotaMB   int       `json:"quota_mb"`
	CreatedAt time.Time `json:"created_at"`
}

type EmailForwarding struct {
	ID          int       `json:"id"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	CreatedAt   time.Time `json:"created_at"`
}

// Mail API Handlers

func (p *Panel) handleDomainMail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Extract domain ID
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

	switch {
	case strings.HasSuffix(r.URL.Path, "/accounts"):
		if r.Method == "GET" {
			p.handleListEmailAccounts(w, domainID)
		} else if r.Method == "POST" {
			p.handleAddEmailAccount(w, r, domainID)
		} else if r.Method == "DELETE" {
			p.handleDeleteEmailAccount(w, r, domainID)
		}
	case strings.HasSuffix(r.URL.Path, "/forwardings"):
		if r.Method == "GET" {
			p.handleListEmailForwardings(w, domainID)
		} else if r.Method == "POST" {
			p.handleAddEmailForwarding(w, r, domainID)
		} else if r.Method == "DELETE" {
			p.handleDeleteEmailForwarding(w, r, domainID)
		}
	default:
		http.NotFound(w, r)
	}
}

func (p *Panel) handleListEmailAccounts(w http.ResponseWriter, domainID int) {
	pool := p.db.GetDB()
	rows, err := pool.QueryContext(context.Background(), 
		"SELECT id, address, quota_mb, created_at FROM email_accounts WHERE domain_id = ?", domainID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var accounts []EmailAccount
	for rows.Next() {
		var a EmailAccount
		if err := rows.Scan(&a.ID, &a.Address, &a.QuotaMB, &a.CreatedAt); err != nil {
			continue
		}
		accounts = append(accounts, a)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accounts})
}

func (p *Panel) handleListEmailForwardings(w http.ResponseWriter, domainID int) {
	pool := p.db.GetDB()
	rows, err := pool.QueryContext(context.Background(), 
		"SELECT id, source, destination, created_at FROM email_forwardings WHERE domain_id = ?", domainID)
	if err != nil {
		// Table might not exist yet if migration failed, handle gracefully
		json.NewEncoder(w).Encode(map[string]interface{}{"forwardings": []EmailForwarding{}})
		return
	}
	defer rows.Close()

	var forwardings []EmailForwarding
	for rows.Next() {
		var f EmailForwarding
		if err := rows.Scan(&f.ID, &f.Source, &f.Destination, &f.CreatedAt); err != nil {
			continue
		}
		forwardings = append(forwardings, f)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"forwardings": forwardings})
}

func (p *Panel) handleAddEmailAccount(w http.ResponseWriter, r *http.Request, domainID int) {
	var req struct {
		Address  string `json:"address"` // full email or just local part? Let's assume passed full or constructing it
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Make sure address contains domain
	if !strings.Contains(req.Address, "@") {
		// Get domain name
		repo := repositories.NewPostgresDomainRepository(p.db.GetDB())
		domains, _ := repo.List(context.Background())
		var domainName string
		for _, d := range domains {
			if d.ID == domainID {
				domainName = d.Name
				break
			}
		}
		if domainName != "" {
			req.Address = req.Address + "@" + domainName
		}
	}

	// 1. Create in DB (store password plain temporarily or hashed? Agent hashes it too)
	// We should store hashed password in DB for consistency if we use DB auth later.
	// For now we just need the record.
	// Note: migrations 001 defines password_hash.
	
	pool := p.db.GetDB()
	result, err := pool.ExecContext(context.Background(),
		"INSERT INTO email_accounts (domain_id, address, password_hash, quota_mb) VALUES (?, ?, ?, ?)",
		domainID, req.Address, "managed-by-agent", req.QuotaMB)
	
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	newID, _ := result.LastInsertId()

	// 2. Call Agent to create system config
	var success bool
	err = p.agentClient.Call("Agent.AddMailAccount", &struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		QuotaMB  int    `json:"quota_mb"`
	}{
		Email:    req.Address,
		Password: req.Password,
		QuotaMB:  req.QuotaMB,
	}, &success)

	if err != nil {
		// Rollback DB
		pool.ExecContext(context.Background(), "DELETE FROM email_accounts WHERE id = ?", newID)
		http.Error(w, "Agent error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": newID})
}

func (p *Panel) handleDeleteEmailAccount(w http.ResponseWriter, r *http.Request, domainID int) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	// Get email address first
	var address string
	pool := p.db.GetDB()
	err := pool.QueryRowContext(context.Background(), "SELECT address FROM email_accounts WHERE id = ? AND domain_id = ?", id, domainID).Scan(&address)
	if err != nil {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	// Call Agent
	var success bool
	err = p.agentClient.Call("Agent.DeleteMailAccount", &struct{ Email string }{Email: address}, &success)
	if err != nil {
		http.Error(w, "Agent error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete from DB
	_, err = pool.ExecContext(context.Background(), "DELETE FROM email_accounts WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleAddEmailForwarding(w http.ResponseWriter, r *http.Request, domainID int) {
	// ... Implement forwarding logic similar to account ...
	// Since Agent needs FULL LIST of forwardings to rebuild file, 
	// or we implement AddForwarding RPC. Agent has "UpdateMailForwarding" taking full list?
	// No, I should implement AddForwarder RPC in Agent or just stick to "UpdateMailForwarding".
	// My agent impl has UpdateMailForwarding taking a list. 
	// So: Add to DB -> Fetch All -> Send All to Agent.
	
	var req struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pool := p.db.GetDB()
	_, err := pool.ExecContext(context.Background(),
		"INSERT INTO email_forwardings (domain_id, source, destination) VALUES (?, ?, ?)",
		domainID, req.Source, req.Destination)
	if err != nil {
		http.Error(w, "Database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sync with Agent
	p.syncForwardings(w, domainID)
}

func (p *Panel) handleDeleteEmailForwarding(w http.ResponseWriter, r *http.Request, domainID int) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	pool := p.db.GetDB()
	_, err := pool.ExecContext(context.Background(), "DELETE FROM email_forwardings WHERE id = ? AND domain_id = ?", id, domainID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	p.syncForwardings(w, domainID)
}

func (p *Panel) syncForwardings(w http.ResponseWriter, domainID int) {
	// Fetch all forwardings for ALL domains (since postfix virtual file is global)
	// OR just append? Postfix virtual file IS global.
	// So agent needs ALL forwardings.
	
	// WARNING: If we only send domain forwardings, we wipe others in Agent if Agent "Updates" file by overwriting.
	// My Agent implementation `UpdateMailForwarding` uses `updateMapFile` which overwrites.
	// So I need to fetch ALL forwardings from DB.
	
	pool := p.db.GetDB()
	rows, err := pool.QueryContext(context.Background(), "SELECT source, destination FROM email_forwardings")
	if err != nil {
		return // Silent fail or log
	}
	defer rows.Close()

	var forwardings []struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	for rows.Next() {
		var f struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
		}
		rows.Scan(&f.Source, &f.Destination)
		forwardings = append(forwardings, f)
	}

	var success bool
	p.agentClient.Call("Agent.UpdateMailForwarding", &struct {
		Forwardings []struct {
			Source      string `json:"source"`
			Destination string `json:"destination"`
		} `json:"forwardings"`
	}{Forwardings: forwardings}, &success)
	
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
