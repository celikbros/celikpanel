package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

type Panel struct {
	agentClient   *transport.ReconnectingClient
	db            *db.SQLiteDB
	orchestrator  *services.SiteOrchestrator
	sessions      *auth.SessionStore
	users         repositories.UserRepository
	secrets       *secrets.Box
	secureCookies bool
	loginLimiter  *rateLimiter
	demoMode      bool
	// pkgFamily caches the host's package-manager family ("apt", "pacman").
	// It is a property of the machine and never changes while the panel runs,
	// so it is asked once instead of being persisted with the service scan —
	// scan data goes stale, a memoised host fact cannot.
	// pkgFamily, makinenin paket-yöneticisi ailesini ("apt", "pacman")
	// önbelleğe alır. Makinenin özelliğidir ve panel çalışırken hiç değişmez;
	// bu yüzden servis taramasıyla birlikte kalıcılaştırılmak yerine bir kez
	// sorulur — tarama verisi bayatlar, bellekteki makine gerçeği bayatlayamaz.
	pkgFamilyMu  sync.Mutex
	pkgFamilyVal string
	// serviceMutationMu is the in-process lease shared by durable installs and
	// every remaining synchronous component mutation. TryLock makes competing
	// requests fail fast instead of racing between an active-operation check
	// and the actual machine change.
	serviceMutationMu sync.Mutex
}

func main() {
	checkWALAwareServiceOperationsIdleFlag := flag.Bool("check-service-operations-idle-wal-aware", false, "Prove that the service operation queue is idle in a running database or a stopped database with a WAL, then exit")
	checkWALAwarePreLedgerServiceOperationsIdleFlag := flag.Bool("check-pre-ledger-service-operations-idle-wal-aware", false, "Prove that a running pre-ledger database or a stopped pre-ledger database with a WAL is safe to migrate, then exit")
	createAdmin := flag.Bool("create-admin", false, "Create or update an administrator, then exit / Bir yönetici oluştur ya da güncelle, sonra çık")
	insecureCookies := flag.Bool("insecure-cookies", false, "Send session cookies without the Secure flag (HTTP-only local dev) / Oturum çerezlerini Secure bayrağı olmadan gönder (yalnızca HTTP yerel geliştirme)")
	demo := flag.Bool("demo", false, "Development only: seed one account per role and show quick-login credentials on the login screen / Yalnızca geliştirme: her rol için hesap oluştur ve giriş ekranında hızlı-giriş bilgilerini göster")
	countUsersFlag := flag.Bool("count-users", false, "Print the number of users and exit (used by install.sh) / Kullanıcı sayısını yazıp çık (install.sh kullanır)")
	checkServiceOperationsIdleFlag := flag.Bool("check-service-operations-idle", false, "Exit successfully only when no queued or running service operation exists / Yalnızca sırada veya çalışan servis işlemi yoksa başarıyla çık")
	checkPreLedgerServiceOperationsIdleFlag := flag.Bool("check-pre-ledger-service-operations-idle", false, "Verify migration history through version 20 and reject partial service queue objects, then exit / 20. sürüme kadarki migration geçmişini doğrula ve yarım servis kuyruğu nesnelerini reddet, sonra çık")
	createServiceOperationSnapshotFlag := flag.String("create-service-operation-snapshot", "", "Create a transaction-consistent standalone panel database snapshot at this absolute path, then exit / Bu mutlak yolda işlem tutarlı bağımsız panel veritabanı anlık görüntüsü oluştur, sonra çık")
	restoreServiceOperationSnapshotFlag := flag.String("restore-service-operation-snapshot", "", "Offline root-only restore from this trusted absolute celikpanel.db; both services must be stopped and the inherited release guard must be held / Bu güvenilir mutlak celikpanel.db dosyasından çevrim dışı yalnız-root geri yükleme; iki servis durmalı ve devralınan yayın koruması tutulmalıdır")
	ensureServiceOperationRescueSnapshotFlag := flag.String("ensure-service-operation-rescue-snapshot", "", "Ensure the transaction-bound root-only recovery snapshot exists without changing the canonical database, then exit / Kanonik veritabanını değiştirmeden işleme bağlı yalnız-root kurtarma anlık görüntüsünün varlığını doğrula, sonra çık")
	releaseTransactionFDFlag := flag.Int("release-transaction-fd", -1, "Inherited descriptor that owns the global release transaction lock / Global yayın işlem kilidinin sahibi olan devralınmış descriptor")
	releaseTransactionTokenFlag := flag.String("release-transaction-token", "", "Exact 64-character lowercase hexadecimal release transaction token / Tam 64 karakterli küçük harf onaltılık yayın işlem belirteci")
	releaseTransactionOperationFlag := flag.String("release-transaction-operation", "", "Exact release transaction operation: update or rollback / Tam yayın işlem operasyonu: update veya rollback")
	releaseTransactionSnapshotFlag := flag.String("release-transaction-snapshot", "", "Safe snapshot basename recorded by the release transaction / Yayın işleminin kaydettiği güvenli anlık görüntü temel adı")
	serviceOperationSnapshotSchemaFlag := flag.String("snapshot-schema", "", "Snapshot schema contract: normal or pre-ledger / Anlık görüntü şema sözleşmesi: normal veya pre-ledger")
	migrateOnlyFlag := flag.Bool("migrate-only", false, "Open the canonical database, apply embedded migrations, and exit before agent or HTTP startup / Kanonik veritabanını aç, gömülü migration'ları uygula ve agent ya da HTTP başlamadan çık")
	flag.Parse()

	log.Println("Starting CelikPanel Backend...")
	releaseTransaction := serviceOperationReleaseTransaction{
		fd:        *releaseTransactionFDFlag,
		token:     *releaseTransactionTokenFlag,
		operation: *releaseTransactionOperationFlag,
		snapshot:  *releaseTransactionSnapshotFlag,
	}
	createOrRestorePathRequestedByFlags :=
		strings.TrimSpace(*createServiceOperationSnapshotFlag) != "" ||
			strings.TrimSpace(*restoreServiceOperationSnapshotFlag) != ""
	rescueSnapshotRequestedByFlags :=
		strings.TrimSpace(*ensureServiceOperationRescueSnapshotFlag) != ""
	transactionMetadataRequestedByFlags :=
		strings.TrimSpace(*serviceOperationSnapshotSchemaFlag) != "" ||
			releaseTransaction.fd != -1 ||
			strings.TrimSpace(releaseTransaction.token) != "" ||
			strings.TrimSpace(releaseTransaction.operation) != "" ||
			strings.TrimSpace(releaseTransaction.snapshot) != ""
	databaseActionRequestedByFlags := createOrRestorePathRequestedByFlags ||
		(!rescueSnapshotRequestedByFlags && transactionMetadataRequestedByFlags)
	if err := validatePanelCommandModes(panelCommandModes{
		createAdmin:                *createAdmin,
		countUsers:                 *countUsersFlag,
		checkIdle:                  *checkServiceOperationsIdleFlag,
		checkPreLedgerIdle:         *checkPreLedgerServiceOperationsIdleFlag,
		checkWALAwareIdle:          *checkWALAwareServiceOperationsIdleFlag,
		checkWALAwarePreLedgerIdle: *checkWALAwarePreLedgerServiceOperationsIdleFlag,
		createOrRestore:            databaseActionRequestedByFlags,
		rescueSnapshot:             rescueSnapshotRequestedByFlags,
		migrateOnly:                *migrateOnlyFlag,
		demo:                       *demo,
		insecureCookies:            *insecureCookies,
	}); err != nil {
		log.Fatalf("Invalid panel command mode: %v", err)
	}
	if rescueSnapshotRequestedByFlags {
		rescueSchema, _, err := validateServiceOperationRescueSnapshotRequest(
			*ensureServiceOperationRescueSnapshotFlag,
			*serviceOperationSnapshotSchemaFlag,
			releaseTransaction,
			createOrRestorePathRequestedByFlags,
		)
		if err != nil {
			log.Fatalf("Invalid service operation rescue snapshot request: %v", err)
		}
		if err := ensureServiceOperationRescueSnapshot(
			databaseFile(),
			*ensureServiceOperationRescueSnapshotFlag,
			rescueSchema,
			releaseTransaction,
		); err != nil {
			log.Fatalf("Ensure service operation rescue snapshot failed: %v", err)
		}
		log.Printf("Service operation rescue snapshot is ready at %s", *ensureServiceOperationRescueSnapshotFlag)
		return
	}
	databaseAction, snapshotSchema, databaseActionRequested, err := validateServiceOperationDatabaseActionRequest(
		*createServiceOperationSnapshotFlag,
		*restoreServiceOperationSnapshotFlag,
		*serviceOperationSnapshotSchemaFlag,
		releaseTransaction,
		false,
	)
	if err != nil {
		log.Fatalf("Invalid service operation database request: %v", err)
	}
	if databaseActionRequested {
		switch databaseAction {
		case serviceOperationDatabaseActionCreate:
			if err := createReleaseServiceOperationSnapshot(
				databaseFile(),
				*createServiceOperationSnapshotFlag,
				snapshotSchema,
				releaseTransaction,
			); err != nil {
				log.Fatalf("Create service operation snapshot failed: %v", err)
			}
			log.Printf("Service operation snapshot created at %s", *createServiceOperationSnapshotFlag)
		case serviceOperationDatabaseActionRestore:
			if err := restoreServiceOperationSnapshot(
				*restoreServiceOperationSnapshotFlag,
				snapshotSchema,
				releaseTransaction,
			); err != nil {
				log.Fatalf(
					"Restore service operation snapshot failed (%s): %v",
					serviceOperationRestoreCommandContract(),
					err,
				)
			}
			log.Printf("Canonical panel database restored from %s", *restoreServiceOperationSnapshotFlag)
		default:
			log.Fatalf("Unsupported service operation database action %q", databaseAction)
		}
		return
	}
	if *checkWALAwarePreLedgerServiceOperationsIdleFlag {
		if err := checkWALAwarePreLedgerServiceOperationsIdle(databaseFile()); err != nil {
			log.Fatalf("WAL-aware pre-ledger service operation check failed: %v", err)
		}
		log.Println("WAL-aware pre-ledger service operation state is idle")
		return
	}
	if *checkWALAwareServiceOperationsIdleFlag {
		if err := checkWALAwareServiceOperationsIdle(databaseFile()); err != nil {
			log.Fatalf("WAL-aware service operation idle check failed: %v", err)
		}
		log.Println("WAL-aware service operation state is idle")
		return
	}
	if *checkPreLedgerServiceOperationsIdleFlag {
		if err := checkPreLedgerServiceOperationsIdle(databaseFile()); err != nil {
			log.Fatalf("Pre-ledger service operation check failed: %v", err)
		}
		log.Println("Pre-ledger service operation state is idle")
		return
	}
	if *checkServiceOperationsIdleFlag {
		if err := checkServiceOperationsIdle(databaseFile()); err != nil {
			log.Fatalf("Service operation idle check failed: %v", err)
		}
		log.Println("Service operation state is idle")
		return
	}

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
	if *migrateOnlyFlag {
		log.Println("Canonical panel database migrations completed")
		return
	}
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
	orchestrator := services.NewSiteOrchestrator(
		database.GetDB(),
		client,
		buildCommit,
	)

	sessions := auth.NewSessionStore(database.GetDB())

	// Load (or on first boot, create) the key that seals stored credentials
	// such as database root passwords. It lives next to the SQLite file: the
	// data dir is already the panel's private state, and a lost data dir loses
	// the ciphertexts along with the key that opened them.
	// Saklanan kimlik bilgilerini (örn. veritabanı root parolaları) mühürleyen
	// anahtarı yükle (ilk açılışta oluştur). SQLite dosyasının yanında yaşar:
	// veri dizini zaten panelin özel durumu; kaybolan veri dizini, şifreli
	// metinleri onları açan anahtarla birlikte kaybeder.
	secretBox, err := secrets.LoadOrCreate(filepath.Join(dataDir(), "secret.key"))
	if err != nil {
		log.Fatalf("Failed to load secret key: %v", err)
	}

	panel := &Panel{
		agentClient:   client,
		db:            database,
		orchestrator:  orchestrator,
		sessions:      sessions,
		users:         repositories.NewPostgresUserRepository(database.GetDB()),
		secrets:       secretBox,
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
	if recovered, err := panel.recoverInterruptedServiceOperations(context.Background()); err != nil {
		log.Fatalf("Failed to recover interrupted service operations: %v", err)
	} else if recovered > 0 {
		log.Printf("Reconciled %d interrupted service operation(s)", recovered)
	}

	// One-time repair of pre-A4 rows: seal any database root password that
	// was stored as plaintext. Fatal on failure — booting with credentials
	// half plaintext, half sealed hides the very problem A4 closes.
	// A4 öncesi satırların tek seferlik onarımı: düz metin saklanmış her
	// veritabanı root parolasını mühürle. Hata ölümcül — kimlik bilgileri
	// yarı düz metin, yarı mühürlü açılmak, A4'ün kapattığı sorunu gizler.
	if err := panel.encryptLegacyDBPasswords(context.Background()); err != nil {
		log.Fatalf("Failed to encrypt legacy database passwords: %v", err)
	}
	if err := panel.encryptLegacyVPNPresharedKeys(context.Background()); err != nil {
		log.Fatalf("Failed to encrypt legacy VPN preshared keys: %v", err)
	}

	// Repair only derived certificate runtime state from the durable ledger.
	// This removes crash-left staging lineages/validation names; it never
	// changes a user's panel setting.
	panel.reconcileCertificateRuntimeAtStartup()

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

	// Fail closed before accepting HTTP: a peer whose one-time private config
	// was interrupted must not survive a process restart as ghost access.
	// HTTP kabulünden önce kapalı kal: tek kullanımlık özel config'i yarıda kalan
	// bir peer süreç yeniden başladığında hayalet erişim olarak yaşamamalıdır.
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 45*time.Second)
	if err := panel.recoverVPNProvisioningState(recoveryCtx); err != nil {
		recoveryCancel()
		log.Fatalf("recover incomplete VPN provisioning: %v", err)
	}
	recoveryCancel()

	// Revoke expired or suspended subscription VPN peers in the background.
	// Süresi dolan veya askıya alınan abonelik VPN peer'larını arka planda kaldır.
	panel.startVPNEntitlementReconciler()

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
	http.HandleFunc("/api/v1/store", panel.handleStore)
	http.HandleFunc("/api/v1/store/", panel.handleStore)
	http.HandleFunc("/api/v1/admin/store-catalog", panel.handleStoreCatalogAdmin)
	http.HandleFunc("/api/v1/admin/store-catalog/", panel.handleStoreCatalogAdmin)
	http.HandleFunc("/api/v1/audit-logs", panel.handleAuditLogs)
	http.HandleFunc("/api/v1/dashboard", panel.handleDashboard)
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
	http.HandleFunc("/api/v1/metrics/history", panel.handleMetricsHistory)
	// History needs a historian: sample for as long as the panel lives.
	// Geçmiş, tarihçi ister: panel yaşadıkça örnekle.
	panel.startMetricsSampler()

	// A fix that only helps FUTURE installs leaves every existing server
	// broken until someone happens to press the right button — and nobody
	// knows which button, because the symptom is invisible: a spam filter that
	// runs and filters nothing. Servers that installed Rspamd before the milter
	// chain existed are in exactly that state right now. Re-composing the chain
	// once at startup makes the upgrade itself the repair. It is idempotent
	// (same inputs, same two settings) and a no-op where postfix is absent.
	//
	// Yalnız GELECEKTEKİ kurulumlara yarayan bir düzeltme, mevcut her sunucuyu,
	// biri doğru düğmeye basana dek bozuk bırakır — ve kimse hangi düğme
	// olduğunu bilmez, çünkü belirti görünmezdir: koşan ama hiçbir şey süzmeyen
	// bir spam filtresi. Milter zinciri var olmadan önce Rspamd kurmuş
	// sunucular şu anda tam olarak bu durumdadır. Zinciri açılışta bir kez
	// yeniden bestelemek, yükseltmenin kendisini onarım hâline getirir.
	// Etkisi değişmezdir (aynı girdi, aynı iki ayar) ve postfix yoksa boş işlem.
	panel.wireMailFiltersAtStartup()

	// PHP Management
	http.HandleFunc("/api/v1/php/pools", panel.handlePHPPools)
	http.HandleFunc("/api/v1/php/pool-config", panel.handlePHPPoolConfig)
	http.HandleFunc("/api/v1/php/extensions", panel.handlePHPExtensions)
	http.HandleFunc("/api/v1/php/config", panel.handlePHPConfig)
	http.HandleFunc("/api/v1/php/extended-config", panel.handlePHPExtendedConfig)

	// Nginx Management
	http.HandleFunc("/api/v1/nginx/global", panel.handleNginxGlobalConfig)
	http.HandleFunc("/api/v1/nginx/ssl", panel.handleNginxSSLConfig)
	http.HandleFunc("/api/v1/ssl/providers", panel.handleACMEProviders)
	// One nameserver pair for the whole server — see nameservers.go.
	// Sunucunun tamamı için tek ad sunucusu çifti — bkz. nameservers.go.
	http.HandleFunc("/api/v1/settings/nameservers", panel.handleNameserverSettings)
	http.HandleFunc("/api/v1/settings/dns-cluster", panel.handleDNSCluster)
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
	http.HandleFunc("/api/v1/service/logs", panel.handleServiceLogs)

	// Version: one truth for "which build is this server running?"
	// Sürüm: "bu sunucu hangi yapıyı koşuyor?" sorusunun tek doğrusu.
	http.HandleFunc("/api/v1/panel/version", panel.handleVersion)
	http.HandleFunc("/api/v1/service/status", panel.handleServiceStatus)
	http.HandleFunc("/api/v1/service/install", panel.handleServiceInstall)
	http.HandleFunc("/api/v1/service/operation", panel.handleServiceOperation)
	http.HandleFunc("/api/v1/service/candidate", panel.handleServiceCandidate)
	http.HandleFunc("/api/v1/service/uninstall", panel.handleServiceUninstall)
	http.HandleFunc("/api/v1/firewall", panel.handleFirewall)
	http.HandleFunc("/api/v1/repo", panel.handleRepo)
	http.HandleFunc("/api/v1/hosting/capabilities", panel.handleHostingCapabilities)
	http.HandleFunc("/api/v1/panel/certificate", panel.handlePanelCertificate)
	http.HandleFunc("/dbtool/", panel.handleDBToolProxy)
	http.HandleFunc("/webmail/", panel.handleWebmailProxy)

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
		} else if strings.Contains(r.URL.Path, "/ssl/retry") {
			panel.handleRetrySSLActivation(w, r)
		} else if strings.Contains(r.URL.Path, "/ssl/renewal") {
			panel.handleSSLRenewalSetting(w, r, domainID)
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
		} else if strings.Contains(r.URL.Path, "/connection") {
			panel.handleDomainConnection(w, r, domainID)
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
	http.HandleFunc("/api/v1/vpn/sync", panel.handleVPNSync)
	http.HandleFunc("/api/v1/vpn/peers", panel.handleVPNPeers)
	http.HandleFunc("/api/v1/vpn/peers/", panel.handleVPNPeerByID)
	http.HandleFunc("/api/v1/pdns/enable", panel.handlePDNSEnable)
	http.HandleFunc("/api/v1/mail/configure", panel.handleMailConfigure)
	http.HandleFunc("/api/v1/mail/policy", panel.handleMailPolicy)
	http.HandleFunc("/api/v1/apps", panel.handleAppCatalog)

	// System SQLite maintenance is a fixed-ID, administrator-only surface.
	// Sistem SQLite bakımı, sabit kimlikli ve yalnız yöneticiye açık bir yüzeydir.
	// It never accepts filesystem paths or arbitrary SQL from the browser.
	// Tarayıcıdan hiçbir zaman dosya yolu ya da keyfi SQL kabul etmez.
	http.HandleFunc("/api/v1/system-databases", panel.handleSystemSQLiteDatabases)
	http.HandleFunc("/api/v1/system-databases/", panel.handleSystemSQLiteDatabaseAction)

	// Node runtime management (admin-only via isAdminOnlyPath)
	// Node runtime yönetimi (isAdminOnlyPath ile yalnızca admin)
	http.HandleFunc("/api/v1/runtimes/node", panel.handleNodeRuntimes)
	http.HandleFunc("/api/v1/runtimes/node/", panel.handleNodeRuntimeSub)

	// cPanel importer (admin-only via isAdminOnlyPath)
	// cPanel içe aktarıcı (isAdminOnlyPath ile yalnızca admin)
	http.HandleFunc("/api/v1/import/cpanel/inspect", panel.handleImportInspect)
	http.HandleFunc("/api/v1/import/cpanel/apply", panel.handleImportApply)

	// Databases — ONE API surface (B1: v2 folded into v1, Jul 18). The
	// handlers parse path segments by name, not by version, so the merge is
	// routing-only. Creation is a plain POST on the collection — the old
	// "/create" suffix was a wart with exactly one obvious spelling now.
	// There is no /api/v2/ anymore; a stale client gets an honest 404.
	// Veritabanları — TEK API yüzeyi (B1: v2, v1'e katlandı, 18 Tem).
	// Handler'lar yol parçalarını sürümle değil adla çözer; birleştirme yalnız
	// yönlendirmedir. Oluşturma koleksiyona düz POST — eski "/create" eki
	// artık tek bariz yazımı olan bir pürüzdü. /api/v2/ artık yok; bayat
	// istemci dürüst bir 404 alır.
	http.HandleFunc("/api/v1/database-servers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			panel.handleListDatabaseServers(w, r)
		case http.MethodPost:
			panel.handleCreateDatabaseV2Server(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Combined handler for /api/v1/database-servers/{id}/* routes
	http.HandleFunc("/api/v1/database-servers/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// DELETE /api/v1/database-servers/{id}
		if r.Method == http.MethodDelete && !strings.Contains(path, "/databases") && !strings.Contains(path, "/users") {
			panel.handleDeleteDatabaseV2Server(w, r)
			return
		}

		// GET/POST /api/v1/database-servers/{id}/databases
		if strings.Contains(path, "/databases") {
			if r.Method == http.MethodGet {
				panel.handleListDatabasesV2(w, r)
			} else if r.Method == http.MethodPost {
				panel.handleCreateDatabaseV2(w, r)
			}
			return
		}

		// GET/POST /api/v1/database-servers/{id}/users
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

	// Database operations
	http.HandleFunc("/api/v1/databases/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && !strings.Contains(r.URL.Path, "/grants") {
			panel.handleDeleteDatabaseV2(w, r)
			return
		}

		// GET/POST /api/v1/databases/{id}/grants
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

	// User/Grant deletions
	http.HandleFunc("/api/v1/database-users/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			panel.handleDeleteDatabaseV2User(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	http.HandleFunc("/api/v1/database-grants/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			panel.handleRevokeDatabaseAccess(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
			// Vite fingerprints everything under /assets/ (index-Ab12Cd.js), so
			// those may be cached forever; a product update changes the name.
			// Vite, /assets/ altındaki her şeye parmak izi verir; sonsuza dek
			// önbelleklenebilir — ürün güncellemesi adı değiştirir.
			if strings.HasPrefix(cleanPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fs.ServeHTTP(w, r)
			return
		}

		// File doesn't exist, serve index.html for SPA routing. The entry
		// point must never be cached: a stale index.html keeps loading the
		// OLD bundle and users keep seeing the previous UI after an update —
		// observed live in the alpha.
		// Dosya yoksa SPA yönlendirmesi için index.html. Giriş noktası asla
		// önbelleklenmemeli: bayat bir index.html ESKİ paketi yüklemeye devam
		// eder ve kullanıcı güncellemeden sonra önceki arayüzü görür — alfada
		// canlı görüldü.
		w.Header().Set("Cache-Control", "no-cache")
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
	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	defer release()

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

	switch req.Action {
	case "start", "stop", "restart", "reload":
	default:
		writeClientError(w, http.StatusBadRequest, "invalid action")
		return
	}
	var reply transport.ServiceActionResult
	err := p.withStandaloneAgentMutation(r.Context(), "service_"+req.Action, serviceName, "", func(callCtx context.Context, binding agentMutationBinding) error {
		if err := p.agentClient.CallContext(callCtx, "Agent.ServiceMutationAction", &struct {
			MutationRequestID string `json:"mutation_request_id"`
			MutationOwnerID   string `json:"mutation_owner_id"`
			ServiceName       string `json:"service_name"`
			Action            string `json:"action"`
		}{binding.MutationRequestID, binding.MutationOwnerID, serviceName, req.Action}, &reply); err != nil {
			return err
		}
		if reply.Error != "" {
			return errors.New(reply.Error)
		}
		return nil
	})

	if err != nil {
		// Start/stop/restart changed (or failed to change) the server's real
		// state and left NO trace in the ledger — the operator could not show
		// what they had done, and neither could I reconstruct it (25 Jul).
		// Başlat/durdur/yeniden başlat, sunucunun gerçek durumunu değiştirdi
		// (ya da değiştiremedi) ve defterde HİÇ iz bırakmıyordu — operatör ne
		// yaptığını gösteremiyordu, ben de yeniden kuramıyordum (25 Tem).
		p.audit(r, "service."+req.Action+".failed:"+serviceName+" — "+auditReason(err.Error()), "service", 0)
		writeServerError(w, err)
		return
	}
	if reply.Error != "" {
		p.audit(r, "service."+req.Action+".failed:"+serviceName+" — "+auditReason(reply.Error), "service", 0)
		writeClientError(w, http.StatusConflict, reply.Error)
		return
	}
	p.audit(r, "service."+req.Action+":"+serviceName, "service", 0)

	// The action changed real state, so the cached scan is stale; refresh it
	// here — one deliberate action, one scan — so pages keep loading from
	// cache without ever probing on their own.
	// Eylem gerçek durumu değiştirdi; önbellekteki tarama bayatladı. Burada
	// tazele — bir bilinçli eylem, bir tarama — sayfalar kendi başına
	// yoklama yapmadan önbellekten yüklenmeye devam etsin.
	if _, err := p.scanManagedServices(r.Context()); err != nil {
		log.Printf("service scan after %s %s: %v", req.Action, serviceName, err)
		p.audit(r, "service."+req.Action+".refresh.failed:"+serviceName+" — "+auditReason(err.Error()), "service", 0)
		writeServiceStateRefreshFailed(w)
		return
	}

	json.NewEncoder(w).Encode(reply)
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
			// A refused root write must be as visible as a granted one:
			// silence is how a security refusal becomes indistinguishable
			// from a success nobody noticed.
			// Reddedilen bir root yazması, kabul edilen kadar görünür olmalı:
			// sessizlik, güvenlik retlerini kimsenin fark etmediği bir
			// başarıdan ayırt edilemez kılar.
			p.audit(r, "config.write.failed:"+req.Path+" — "+auditReason(err.Error()), "config", 0)
			// A REFUSAL is not a server fault, and hiding it behind "internal
			// server error" would leave an operator editing a legitimate file
			// with no idea why nothing happened. The agent's reason is safe to
			// show: it names a path the caller already supplied.
			// RET, sunucu arızası değildir; onu "internal server error"un
			// arkasına gizlemek, meşru bir dosyayı düzenleyen operatörü hiçbir
			// şeyin neden olmadığını bilmeden bırakırdı. Agent'ın gerekçesi
			// gösterilebilir: zaten çağıranın verdiği bir yolu adlandırır.
			msg := err.Error()
			if strings.Contains(msg, "not a managed configuration file") ||
				strings.Contains(msg, "protected and cannot be edited") ||
				strings.Contains(msg, "symbolic link") ||
				strings.Contains(msg, "must be absolute") {
				writeCodedError(w, http.StatusForbidden, errCodeConfigPathRefused, msg, "")
				return
			}
			// A syntax error is the operator's own text being wrong, not a
			// server fault — and the file was already rolled back, so saying
			// exactly what the checker complained about is both safe and the
			// only useful answer.
			// Sözdizim hatası, sunucu arızası değil operatörün kendi metninin
			// yanlış olmasıdır — üstelik dosya zaten geri alındı; denetleyicinin
			// neye takıldığını tam olarak söylemek hem güvenli hem de tek
			// yararlı cevap.
			if strings.Contains(msg, "config validation failed") {
				writeCodedError(w, http.StatusUnprocessableEntity, errCodeConfigInvalid, msg, "")
				return
			}
			writeServerError(w, err)
			return
		}

		// Writing a root-owned file is the last thing that should be quieter
		// than a service restart, which has always been audited.
		// Root'a ait bir dosyayı yazmak, her zaman denetlenen servis yeniden
		// başlatmasından daha sessiz olmaması gereken son şeydir.
		p.audit(r, "config.write:"+req.Path, "config", 0)
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
