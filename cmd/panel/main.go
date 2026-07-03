package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/rpc"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

type Panel struct {
	agentClient   *rpc.Client
	db            *db.SQLiteDB
	orchestrator  *services.SiteOrchestrator
	sessions      *auth.SessionStore
	users         repositories.UserRepository
	secureCookies bool
	loginLimiter  *rateLimiter
}

func main() {
	createAdmin := flag.Bool("create-admin", false, "Create or update an administrator, then exit / Bir yönetici oluştur ya da güncelle, sonra çık")
	insecureCookies := flag.Bool("insecure-cookies", false, "Send session cookies without the Secure flag (HTTP-only local dev) / Oturum çerezlerini Secure bayrağı olmadan gönder (yalnızca HTTP yerel geliştirme)")
	flag.Parse()

	log.Println("Starting CelikPanel Backend...")

	// Initialize SQLite Database
	databasePath := "./data/celikpanel.db"
	database, err := db.NewSQLiteDB(databasePath)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite: %v", err)
	}
	defer database.Close()

	// Admin bootstrap runs without needing the agent, then exits.
	// Yönetici önyüklemesi agent'a ihtiyaç duymadan çalışır, sonra çıkar.
	if *createAdmin {
		if err := runCreateAdmin(database); err != nil {
			log.Fatalf("create-admin failed: %v", err)
		}
		return
	}

	// Connect to Agent
	client, err := transport.ConnectAgent()
	if err != nil {
		log.Fatalf("Failed to connect to Agent: %v", err)
	}
	log.Println("Connected to Agent RPC")

	// Initialize Site Orchestrator
	orchestrator := services.NewSiteOrchestrator(database.GetDB(), client)

	sessions := auth.NewSessionStore(database.GetDB())

	panel := &Panel{
		agentClient:   client,
		db:            database,
		orchestrator:  orchestrator,
		sessions:      sessions,
		users:         repositories.NewPostgresUserRepository(database.GetDB()),
		secureCookies: !*insecureCookies,
		// 10 login attempts per IP per 5 minutes slows brute force while
		// staying invisible to a legitimate user.
		// IP başına 5 dakikada 10 giriş denemesi; kaba kuvveti yavaşlatır,
		// meşru kullanıcıya görünmez kalır.
		loginLimiter: newRateLimiter(10, 5*time.Minute),
	}

	// Refuse to start wide open: if no user exists yet, the operator must
	// bootstrap an admin first.
	// Ardına kadar açık başlamayı reddet: henüz hiç kullanıcı yoksa, önce
	// bir yönetici önyüklenmelidir.
	if n, err := panel.countUsers(); err != nil {
		log.Fatalf("Failed to check users: %v", err)
	} else if n == 0 {
		log.Fatal("No users exist. Create the first admin with:  ./bin/panel --create-admin")
	}

	// Purge expired sessions on startup and then hourly.
	// Başlangıçta ve sonra saatlik olarak süresi dolmuş oturumları temizle.
	_ = sessions.DeleteExpired(context.Background())
	go func() {
		for range time.Tick(time.Hour) {
			_ = sessions.DeleteExpired(context.Background())
		}
	}()

	// Authentication routes (login is public; logout/me require a session).
	// Kimlik doğrulama rotaları (giriş herkese açık; çıkış/me oturum ister).
	http.HandleFunc("/api/v1/auth/login", panel.handleLogin)
	http.HandleFunc("/api/v1/auth/logout", panel.handleLogout)
	http.HandleFunc("/api/v1/auth/me", panel.handleMe)

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
		
		// Clean the request path before touching the filesystem. Rooting
		// it at "/" collapses any ".." so a crafted URL cannot escape the
		// dist directory.
		// Dosya sistemine dokunmadan önce istek yolunu temizle. "/" ile
		// köklemek her ".."yi eritir; böylece hazırlanmış bir URL dist
		// dizininden dışarı çıkamaz.
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		filePath := filepath.Join("./web/dist", cleanPath)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			// File exists, serve it
			fs.ServeHTTP(w, r)
			return
		}

		// File doesn't exist, serve index.html for SPA routing
		http.ServeFile(w, r, "./web/dist/index.html")
	})

	// Middleware chain, outermost first: security headers on everything →
	// CSRF block on cross-origin writes → auth gate → handlers.
	// Ara katman zinciri, en dıştan içe: her şeyde güvenlik başlıkları →
	// köken-dışı yazmalarda CSRF engeli → kimlik doğrulama kapısı → işleyici.
	handler := securityHeaders(panel.secureCookies,
		csrfProtect(
			panel.requireAuth(http.DefaultServeMux)))

	log.Println("Panel listening on :1983 (HTTP)")
	log.Fatal(http.ListenAndServe(":1983", handler))
}

// countUsers reports how many users exist, to gate startup.
// countUsers, başlangıcı kısıtlamak için kaç kullanıcı olduğunu bildirir.
func (p *Panel) countUsers() (int, error) {
	var n int
	err := p.db.GetDB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func (p *Panel) handleServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Call RPC
	var services []core.Service
	err := p.agentClient.Call("Agent.GetServices", &transport.Empty{}, &services)
	if err != nil {
		writeServerError(w, err)
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
		writeClientError(w, http.StatusBadRequest, "invalid request")
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
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": reply})
}

func (p *Panel) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		return
	}

	if r.Method == "POST" {
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}

		var reply bool
		err := p.agentClient.Call("Agent.UpdateConfig", &transport.UpdateConfigArgs{
			Path:    req.Path,
			Content: req.Content,
		}, &reply)
		
		if err != nil {
			writeServerError(w, err)
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
		writeServerError(w, err)
		return
	}

	json.NewEncoder(w).Encode(reply)
}
