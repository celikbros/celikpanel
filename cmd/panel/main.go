package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/rpc"
	"os"
	"strings"
	
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

type Panel struct {
	agentClient  *rpc.Client
	db           *db.SQLiteDB
	orchestrator *services.SiteOrchestrator
}

func main() {
	log.Println("Starting CelikPanel Backend...")

	// Initialize SQLite Database
	databasePath := "./data/celikpanel.db"
	database, err := db.NewSQLiteDB(databasePath)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite: %v", err)
	}
	defer database.Close()

	// Connect to Agent
	client, err := transport.ConnectAgent()
	if err != nil {
		log.Fatalf("Failed to connect to Agent: %v", err)
	}
	log.Println("Connected to Agent RPC")

	// Initialize Site Orchestrator
	orchestrator := services.NewSiteOrchestrator(database.GetDB(), client)

	panel := &Panel{
		agentClient:  client,
		db:           database,
		orchestrator: orchestrator,
	}

	// Managed Services
	http.HandleFunc("/api/v1/managed-services", panel.handleManagedServices)
	
	// System Check
	http.HandleFunc("/api/v1/system/check", panel.handleSystemCheck)

	// PHP Management
	http.HandleFunc("/api/v1/php/pools", panel.handlePHPPools)
	http.HandleFunc("/api/v1/php/pool-config", panel.handlePHPPoolConfig)
	http.HandleFunc("/api/v1/php/extensions", panel.handlePHPExtensions)
	http.HandleFunc("/api/v1/php/config", panel.handlePHPConfig)
	http.HandleFunc("/api/v1/php/extended-config", panel.handlePHPExtendedConfig)


	// Nginx Management
	http.HandleFunc("/api/v1/nginx/global", panel.handleNginxGlobalConfig)
	http.HandleFunc("/api/v1/nginx/ssl", panel.handleNginxSSLConfig)
	http.HandleFunc("/api/v1/nginx/ratelimits", panel.handleNginxRateLimits)

	// Fail2ban Management
	http.HandleFunc("/api/v1/fail2ban/jails", panel.handleFail2banJails)
	http.HandleFunc("/api/v1/fail2ban/banned", panel.handleFail2banBannedIPs)
	http.HandleFunc("/api/v1/fail2ban/config", panel.handleFail2banConfig)

	// Email Management (Postfix & Dovecot)
	http.HandleFunc("/api/v1/postfix/queue", panel.handlePostfixQueue)
	http.HandleFunc("/api/v1/postfix/summary", panel.handlePostfixSummary)
	http.HandleFunc("/api/v1/dovecot/stats", panel.handleDovecotStats)

	http.HandleFunc("/api/v1/config", panel.handleConfig)
	http.HandleFunc("/api/v1/service/action", panel.handleServiceAction)
	http.HandleFunc("/api/v1/service/status", panel.handleServiceStatus)
	
	// Domain Management
	http.HandleFunc("/api/v1/domains", panel.handleDomains)
	http.HandleFunc("/api/v1/domains/create", panel.handleCreateDomain)
	
	// Domain-specific routes (PHP, general, aliases, SSL, logs, databases, delete, etc.)
	http.HandleFunc("/api/v1/domains/", func(w http.ResponseWriter, r *http.Request) {
		// Route to appropriate handler based on path
		if strings.Contains(r.URL.Path, "/php/pool") {
			panel.handleDomainPHPPool(w, r)
		} else if strings.Contains(r.URL.Path, "/php") {
			panel.handleDomainPHPSettings(w, r)
		} else if strings.Contains(r.URL.Path, "/general") {
			panel.handleDomainGeneralSettings(w, r)
		} else if strings.Contains(r.URL.Path, "/aliases") {
			panel.handleDomainAliases(w, r)
		} else if strings.Contains(r.URL.Path, "/ssl/letsencrypt") {
			panel.handleIssueLetsEncrypt(w, r)
		} else if strings.Contains(r.URL.Path, "/ssl/upload") {
			panel.handleUploadCertificate(w, r)
		} else if strings.Contains(r.URL.Path, "/ssl/settings") {
			panel.handleSSLSettings(w, r)
		} else if strings.Contains(r.URL.Path, "/ssl") {
			panel.handleDomainSSL(w, r)
		} else if strings.Contains(r.URL.Path, "/logs/") {
			panel.handleDomainLogs(w, r)
		} else if strings.Contains(r.URL.Path, "/databases/") && len(strings.Split(r.URL.Path, "/")) > 6 {
			panel.handleDeleteDatabase(w, r)
		} else if strings.Contains(r.URL.Path, "/databases") {
			panel.handleDomainDatabases(w, r)
		} else if strings.Contains(r.URL.Path, "/files/download") {
			panel.handleDomainFileDownload(w, r)
		} else if strings.Contains(r.URL.Path, "/files") {
			panel.handleDomainFiles(w, r)
		} else if strings.Contains(r.URL.Path, "/backups/restore") {
			panel.handleRestoreBackup(w, r)
		} else if strings.Contains(r.URL.Path, "/backups") {
			panel.handleDomainBackups(w, r)
		} else if strings.Contains(r.URL.Path, "/cron") {
			panel.handleDomainCronJobs(w, r)
		} else if strings.Contains(r.URL.Path, "/mail") {
			panel.handleDomainMail(w, r)
		} else if strings.Contains(r.URL.Path, "/dns") {
			panel.handleDomainDNS(w, r)
		} else if r.Method == http.MethodDelete {
			panel.handleDeleteDomain(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	// Service Configuration
	http.HandleFunc("/api/v1/config/php", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			panel.handleGetPHPConfig(w, r)
		} else if r.Method == http.MethodPost {
			panel.handleUpdatePHPConfig(w, r)
		}
	})

	// PowerDNS Configuration
	http.HandleFunc("/api/v1/pdns/configure", panel.handleConfigurePowerDNS)

	// Database Management v2 - Server Management
	http.HandleFunc("/api/v2/database-servers", panel.handleListDatabaseServers)
	http.HandleFunc("/api/v2/database-servers/create", panel.handleCreateDatabaseV2Server)
	
	// Combined handler for /api/v2/database-servers/{id}/* routes
	http.HandleFunc("/api/v2/database-servers/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		
		// DELETE /api/v2/database-servers/{id}
		if r.Method == http.MethodDelete && !strings.Contains(path, "/databases") && !strings.Contains(path, "/users") {
			panel.handleDeleteDatabaseV2Server(w, r)
			return
		}
		
		// GET/POST /api/v2/database-servers/{id}/databases
		if strings.Contains(path, "/databases") {
			if r.Method == http.MethodGet {
				panel.handleListDatabasesV2(w, r)
			} else if r.Method == http.MethodPost {
				panel.handleCreateDatabaseV2(w, r)
			}
			return
		}
		
		// GET/POST /api/v2/database-servers/{id}/users
		if strings.Contains(path, "/users") {
			if r.Method == http.MethodGet {
				panel.handleListDatabaseUsers(w, r)
			} else if r.Method == http.MethodPost {
				panel.handleCreateDatabaseV2User(w, r)
			}
			return
		}
		
		http.Error(w, "not found", http.StatusNotFound)
	})

	// Database Management v2 - Database operations
	http.HandleFunc("/api/v2/databases/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && !strings.Contains(r.URL.Path, "/grants") {
			panel.handleDeleteDatabaseV2(w, r)
			return
		}
		
		// GET/POST /api/v2/databases/{id}/grants
		if strings.Contains(r.URL.Path, "/grants") {
			if r.Method == http.MethodGet {
				panel.handleListDatabaseGrants(w, r)
			} else if r.Method == http.MethodPost {
				panel.handleGrantDatabaseAccess(w, r)
			}
			return
		}
		
		http.Error(w, "not found", http.StatusNotFound)
	})

	// Database Management v2 - User/Grant deletions
	http.HandleFunc("/api/v2/database-users/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			panel.handleDeleteDatabaseV2User(w, r)
		}
	})
	
	http.HandleFunc("/api/v2/database-grants/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			panel.handleRevokeDatabaseAccess(w, r)
		}
	})

	http.HandleFunc("/api/v1/config/mysql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			panel.handleGetMySQLConfig(w, r)
		} else if r.Method == http.MethodPost {
			panel.handleUpdateMySQLConfig(w, r)
		}
	})

	// Serve Frontend (Vite Build) with SPA fallback for React Router
	// All non-API routes serve index.html for client-side routing
	fs := http.FileServer(http.Dir("./web/dist"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If it's an API request, don't handle here (handled by specific handlers)
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		
		// Check if the requested file exists
		filePath := "./web/dist" + r.URL.Path
		if _, err := os.Stat(filePath); err == nil {
			// File exists, serve it
			fs.ServeHTTP(w, r)
			return
		}
		
		// File doesn't exist, serve index.html for SPA routing
		http.ServeFile(w, r, "./web/dist/index.html")
	})

	log.Println("Panel listening on :1983 (HTTP)")
	log.Fatal(http.ListenAndServe(":1983", nil))
}

func (p *Panel) handleServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // For dev

	// Call RPC
	var services []core.Service
	err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &services)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter domain-specific configs from nginx
	for i := range services {
		if services[i].Name == "nginx" {
			filtered := []core.ConfigFile{}
			for _, cf := range services[i].ConfigFiles {
				// Exclude vhost configs - they belong to Domains section
				if !strings.Contains(cf.Path, "/etc/nginx/sites-available/") &&
					!strings.Contains(cf.Path, "/etc/nginx/sites-enabled/") {
					filtered = append(filtered, cf)
				}
			}
			services[i].ConfigFiles = filtered
		}
	}

	json.NewEncoder(w).Encode(services)
}

func (p *Panel) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServiceName string `json:"service_name"`
		Name        string `json:"name"` // Support 'name' from frontend
		Action      string `json:"action"` // start, stop, restart, reload
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Use Name if ServiceName is empty
	serviceName := req.ServiceName
	if serviceName == "" {
		serviceName = req.Name
	}

	var reply bool
	var err error
	args := &transport.ServiceArgs{ServiceName: serviceName}

	switch req.Action {
	case "start":
		err = p.agentClient.Call("Agent.StartService", args, &reply)
	case "stop":
		err = p.agentClient.Call("Agent.StopService", args, &reply)
	case "restart":
		err = p.agentClient.Call("Agent.RestartService", args, &reply)
	case "reload":
		err = p.agentClient.Call("Agent.ReloadService", args, &reply)
	default:
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": reply})
}

func (p *Panel) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method == "POST" {
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var reply bool
		err := p.agentClient.Call("Agent.UpdateConfig", &transport.UpdateConfigArgs{
			Path:    req.Path,
			Content: req.Content,
		}, &reply)
		
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		json.NewEncoder(w).Encode(map[string]bool{"success": reply})
		return
	}

	// GET
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	var reply transport.ConfigResponse
	err := p.agentClient.Call("Agent.GetConfig", &transport.GetConfigArgs{Path: path}, &reply)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(reply)
}
