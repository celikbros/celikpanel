package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
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
	agentClient   *transport.ReconnectingClient
	db            *db.SQLiteDB
	orchestrator  *services.SiteOrchestrator
	sessions      *auth.SessionStore
	users         repositories.UserRepository
	secureCookies bool
	loginLimiter  *rateLimiter
	demoMode      bool
}

func main() {
	createAdmin := flag.Bool("create-admin", false, "Create or update an administrator, then exit / Bir yönetici oluştur ya da güncelle, sonra çık")
	insecureCookies := flag.Bool("insecure-cookies", false, "Send session cookies without the Secure flag (HTTP-only local dev) / Oturum çerezlerini Secure bayrağı olmadan gönder (yalnızca HTTP yerel geliştirme)")
	demo := flag.Bool("demo", false, "Development only: seed one account per role and show quick-login credentials on the login screen / Yalnızca geliştirme: her rol için hesap oluştur ve giriş ekranında hızlı-giriş bilgilerini göster")
	countUsersFlag := flag.Bool("count-users", false, "Print the number of users and exit (used by install.sh) / Kullanıcı sayısını yazıp çık (install.sh kullanır)")
	flag.Parse()

	log.Println("Starting CelikPanel Backend...")

	// Initialize SQLite Database. The data directory is created if missing so
	// a fresh install boots without a manual mkdir.
	// SQLite veritabanını başlat. Veri dizini yoksa oluşturulur; böylece taze
	// bir kurulum elle mkdir olmadan açılır.
	if err := os.MkdirAll(dataDir(), 0o750); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}
	database, err := db.NewSQLiteDB(databaseFile())
	if err != nil {
		log.Fatalf("Failed to initialize SQLite: %v", err)
	}
	defer database.Close()

	// These bootstrap modes only touch the database, then exit — no agent.
	// Bu önyükleme modları yalnızca veritabanına dokunup çıkar — agent yok.
	if *countUsersFlag {
		p := &Panel{db: database}
		n, err := p.countUsers()
		if err != nil {
			log.Fatalf("count-users failed: %v", err)
		}
		fmt.Println(n)
		return
	}
	if *createAdmin {
		if err := runCreateAdmin(database); err != nil {
			log.Fatalf("create-admin failed: %v", err)
		}
		return
	}

	// Connect to Agent. The reconnecting wrapper survives agent restarts and
	// poisoned RPC streams without needing a panel restart.
	// Agent'a bağlan. Yeniden bağlanan sarmalayıcı, panel yeniden başlatılmadan
	// agent yeniden başlamalarını ve bozulmuş RPC akışlarını atlatır.
	rawClient, err := transport.ConnectAgent()
	if err != nil {
		log.Fatalf("Failed to connect to Agent: %v", err)
	}
	client := transport.NewReconnectingClient(rawClient)
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
		demoMode:     *demo,
	}

	// Development demo accounts (gated behind --demo).
	// Geliştirme demo hesapları (--demo bayrağının arkasında).
	if *demo {
		panel.seedDemoAccounts()
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

	// Run scheduled backups in the background from here on.
	// Buradan itibaren zamanlanmış yedekleri arka planda koştur.
	panel.startBackupScheduler()

	// Renew expiring certificates automatically.
	// Süresi yaklaşan sertifikaları otomatik yenile.
	panel.startCertRenewalScheduler()

	// Authentication routes (login is public; logout/me require a session).
	// Kimlik doğrulama rotaları (giriş herkese açık; çıkış/me oturum ister).
	http.HandleFunc("/api/v1/auth/login", panel.handleLogin)
	http.HandleFunc("/api/v1/auth/logout", panel.handleLogout)
	http.HandleFunc("/api/v1/auth/me", panel.handleMe)

	// Demo credentials (public, but empty unless --demo is set).
	// Demo kimlik bilgileri (herkese açık, ama --demo yoksa boş).
	http.HandleFunc("/api/v1/auth/demo", panel.handleDemoAccounts)
	http.HandleFunc("/api/v1/auth/login/totp", panel.handleLoginTOTP)

	// Account management (admin + reseller; role rules inside handlers).
	// Hesap yönetimi (admin + bayi; rol kuralları işleyicilerin içinde).
	http.HandleFunc("/api/v1/users", panel.handleUsers)
	http.HandleFunc("/api/v1/users/", panel.handleUserByID)
	http.HandleFunc("/api/v1/plans", panel.handlePlans)
	http.HandleFunc("/api/v1/plans/", panel.handlePlanByID)
	http.HandleFunc("/api/v1/subscriptions", panel.handleSubscriptions)
	http.HandleFunc("/api/v1/subscriptions/", panel.handleSubscriptionEntitlements)
	http.HandleFunc("/api/v1/products", panel.handleProducts)
	http.HandleFunc("/api/v1/audit-logs", panel.handleAuditLogs)
	http.HandleFunc("/api/v1/auth/password", panel.handleChangeOwnPassword)
	http.HandleFunc("/api/v1/auth/2fa/status", panel.handle2FA)
	http.HandleFunc("/api/v1/auth/2fa/setup", panel.handle2FA)
	http.HandleFunc("/api/v1/auth/2fa/enable", panel.handle2FA)
	http.HandleFunc("/api/v1/auth/2fa/disable", panel.handle2FA)
	http.HandleFunc("/api/v1/auth/unimpersonate", panel.handleUnimpersonate)

	// Managed Services
	http.HandleFunc("/api/v1/managed-services", panel.handleManagedServices)
	http.HandleFunc("/api/v1/managed-services/scan", panel.handleManagedServicesScan)

	// System Check
	http.HandleFunc("/api/v1/system/check", panel.handleSystemCheck)

	// System Stats (dashboard metrics: CPU, RAM, disk, uptime, load)
	http.HandleFunc("/api/v1/system/stats", panel.handleSystemStats)

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
	http.HandleFunc("/api/v1/service/install", panel.handleServiceInstall)
	http.HandleFunc("/api/v1/service/candidate", panel.handleServiceCandidate)

	// Domain Management
	http.HandleFunc("/api/v1/domains", panel.handleDomains)
	http.HandleFunc("/api/v1/domains/create", panel.handleCreateDomain)

	// Domain-specific routes (PHP, general, aliases, SSL, logs, databases, delete, etc.)
	http.HandleFunc("/api/v1/domains/", func(w http.ResponseWriter, r *http.Request) {
		// Single ownership chokepoint: every /domains/{id}/... sub-resource
		// flows through here, so one guard covers them all. The first path
		// segment after the prefix is the domain ID.
		// Tek sahiplik kapısı: her /domains/{id}/... alt kaynağı buradan geçer,
		// bu yüzden tek koruma hepsini kapsar. Önekten sonraki ilk yol parçası
		// domain kimliğidir.
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/domains/")
		idStr := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			idStr = rest[:i]
		}
		domainID, err := strconv.Atoi(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !panel.authorizeDomain(w, r, domainID) {
			return
		}

		// Route to appropriate handler based on path
		if strings.Contains(r.URL.Path, "/hosting") {
			panel.handleDomainHosting(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/app/") || strings.HasSuffix(r.URL.Path, "/app") {
			panel.handleDomainApp(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/php/pool") {
			panel.handleDomainPHPPool(w, r)
		} else if strings.Contains(r.URL.Path, "/php") {
			panel.handleDomainPHPSettings(w, r)
		} else if strings.Contains(r.URL.Path, "/general") {
			panel.handleDomainGeneralSettings(w, r)
		} else if strings.Contains(r.URL.Path, "/aliases") {
			panel.handleDomainAliases(w, r)
		} else if strings.Contains(r.URL.Path, "/dnssec") {
			panel.handleDomainDNSSEC(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/ssl/mail") {
			panel.handleDomainSSLMail(w, r, domainID)
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
		} else if strings.Contains(r.URL.Path, "/backups/schedule") {
			panel.handleBackupSchedule(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/backups/restore") {
			panel.handleRestoreBackup(w, r)
		} else if strings.Contains(r.URL.Path, "/backups/download") {
			panel.handleDownloadBackup(w, r)
		} else if strings.Contains(r.URL.Path, "/backups") {
			panel.handleDomainBackups(w, r)
		} else if strings.Contains(r.URL.Path, "/cron") {
			panel.handleDomainCronJobs(w, r)
		} else if strings.Contains(r.URL.Path, "/mail/health") {
			panel.handleMailHealth(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/mail") {
			panel.handleDomainMail(w, r)
		} else if strings.Contains(r.URL.Path, "/apps/install") {
			panel.handleAppInstall(w, r, domainID)
		} else if strings.Contains(r.URL.Path, "/usage") {
			panel.handleDomainUsage(w, r)
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
	http.HandleFunc("/api/v1/vpn/status", panel.handleVPNStatus)
	http.HandleFunc("/api/v1/vpn/setup", panel.handleVPNSetup)
	http.HandleFunc("/api/v1/vpn/peers", panel.handleVPNPeers)
	http.HandleFunc("/api/v1/vpn/peers/", panel.handleVPNPeerByID)
	http.HandleFunc("/api/v1/pdns/enable", panel.handlePDNSEnable)
	http.HandleFunc("/api/v1/mail/configure", panel.handleMailConfigure)
	http.HandleFunc("/api/v1/mail/policy", panel.handleMailPolicy)
	http.HandleFunc("/api/v1/apps", panel.handleAppCatalog)

	// Node runtime management (admin-only via isAdminOnlyPath)
	// Node runtime yönetimi (isAdminOnlyPath ile yalnızca admin)
	http.HandleFunc("/api/v1/runtimes/node", panel.handleNodeRuntimes)

	// cPanel importer (admin-only via isAdminOnlyPath)
	// cPanel içe aktarıcı (isAdminOnlyPath ile yalnızca admin)
	http.HandleFunc("/api/v1/import/cpanel/inspect", panel.handleImportInspect)
	http.HandleFunc("/api/v1/import/cpanel/apply", panel.handleImportApply)

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
	webRoot := webDir()
	fs := http.FileServer(http.Dir(webRoot))
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
		filePath := filepath.Join(webRoot, cleanPath)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			// File exists, serve it
			fs.ServeHTTP(w, r)
			return
		}

		// File doesn't exist, serve index.html for SPA routing
		http.ServeFile(w, r, filepath.Join(webRoot, "index.html"))
	})

	// Middleware chain, outermost first: security headers on everything →
	// CSRF block on cross-origin writes → auth gate → handlers.
	// Ara katman zinciri, en dıştan içe: her şeyde güvenlik başlıkları →
	// köken-dışı yazmalarda CSRF engeli → kimlik doğrulama kapısı → işleyici.
	handler := securityHeaders(panel.secureCookies,
		csrfProtect(
			panel.requireAuth(http.DefaultServeMux)))

	addr := listenAddr()

	// Serve HTTPS when a certificate is configured (or self-sign one on
	// request); fall back to plain HTTP for development.
	// Sertifika yapılandırıldığında (ya da talep üzerine kendinden-imzalı
	// üretilince) HTTPS sun; geliştirme için düz HTTP'ye düş.
	tlsOn, certPath, keyPath, err := tlsSettings()
	if err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}
	if tlsOn {
		log.Printf("Panel listening on %s (HTTPS)", addr)
		log.Fatal(http.ListenAndServeTLS(addr, certPath, keyPath, handler))
	}

	// Plain HTTP with Secure cookies would hand the browser a cookie it
	// silently drops — refuse the footgun unless --insecure-cookies is set.
	// Secure çerezli düz HTTP, tarayıcıya sessizce düşürdüğü bir çerez verir
	// — --insecure-cookies verilmedikçe bu tuzağı reddet.
	if panel.secureCookies {
		log.Fatal("refusing to serve over plain HTTP with secure cookies: enable TLS (CELIKPANEL_TLS=1 or CELIKPANEL_TLS_CERT/KEY) or pass --insecure-cookies for development")
	}
	log.Printf("Panel listening on %s (HTTP)", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
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
		Name        string `json:"name"`   // Support 'name' from frontend
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

	// The action changed real state, so the cached scan is stale; refresh it
	// here — one deliberate action, one scan — so pages keep loading from
	// cache without ever probing on their own.
	// Eylem gerçek durumu değiştirdi; önbellekteki tarama bayatladı. Burada
	// tazele — bir bilinçli eylem, bir tarama — sayfalar kendi başına
	// yoklama yapmadan önbellekten yüklenmeye devam etsin.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("service scan after %s %s: %v", req.Action, serviceName, err)
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
