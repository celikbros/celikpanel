package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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

const (
	panelHTTPReadHeaderTimeout = 10 * time.Second
	panelHTTPReadTimeout       = 5 * time.Minute
	panelHTTPWriteTimeout      = 30 * time.Minute
	panelHTTPIdleTimeout       = 2 * time.Minute
)

func newPanelHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: panelHTTPReadHeaderTimeout,
		ReadTimeout:       panelHTTPReadTimeout,
		WriteTimeout:      panelHTTPWriteTimeout,
		IdleTimeout:       panelHTTPIdleTimeout,
	}
}

func emitPanelBuildIdentity(args []string, output io.Writer) bool {
	if len(args) != 1 || args[0] != "--inspect-build-identity" {
		return false
	}
	_, _ = fmt.Fprintf(output, "version=%s\ncommit=%s\n", buildVersion, buildCommit)
	return true
}

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
	// webmailReadinessProbe is injectable only so handler tests never need a
	// real Roundcube process. Production leaves it nil and uses the fixed,
	// Unix-socket-backed probe.
	webmailReadinessProbe func(context.Context) bool
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
	// hostPlatformResolutionMu guards the context-aware shared identity flight.
	// Mutating callers authorize against one published result without holding
	// a mutex across the HostPlatform RPC.
	hostPlatformResolutionMu sync.Mutex
	hostPlatformResolution   *agentRPCHostIdentityResolution
	// hostPlatformVal is the verified identity behind distribution-specific
	// capability decisions. PkgFamily remains for backward-compatible package
	// code, but dnf alone can never qualify a preview target.
	hostPlatformVal   transport.HostPlatformResponse
	hostPlatformKnown bool
	// degraded records subsystems whose startup repair failed. The panel still
	// serves; only that subsystem's mutations are held, with the stored code.
	// degraded, açılış onarımı başarısız olan alt sistemleri kaydeder. Panel
	// yine hizmet verir; yalnız o alt sistemin değişiklikleri saklanan kodla
	// birlikte tutulur.
	degradedMu sync.RWMutex
	degraded   map[string]degradedSubsystem
	// serviceScanMu coalesces page-triggered service scans. A first visit,
	// React StrictMode and multiple open tabs must not probe the host in
	// parallel.
	serviceScanMu sync.Mutex
	// serviceMutationMu is the in-process lease shared by durable installs and
	// every remaining synchronous component mutation. TryLock makes competing
	// requests fail fast instead of racing between an active-operation check
	// and the actual machine change.
	serviceMutationMu sync.Mutex
	// dnsTopologyMu serializes every request-time DNS identity mutation. The
	// setup endpoint and the two legacy endpoints share agent state and one
	// SQLite tuple; their snapshot/apply/commit/rollback sequences must never
	// interleave.
	dnsTopologyMu sync.Mutex
	// mailMutationMu serializes the database/agent two-phase mail mutations.
	// Postfix maps are global files, so two tenants must not snapshot and
	// publish overlapping forwarding or mailbox states concurrently.
	mailMutationMu sync.Mutex
	// dnsEnginePreviews holds short-lived, one-use authorizations for the
	// separately confirmed DNS engine cutover endpoint.
	dnsEnginePreviews dnsEnginePreviewCache
}

type domainSubroute struct {
	domainID int
	kind     string
	methods  []string
}

// strictRouteSegments parses URL path segments without letting encoded
// separators or dot-segments change the route shape after dispatch.
func strictRouteSegments(r *http.Request, prefix string) ([]string, bool) {
	if r == nil || r.URL == nil {
		return nil, false
	}
	escapedPath := r.URL.EscapedPath()
	if !strings.HasPrefix(escapedPath, prefix) {
		return nil, false
	}
	rest := strings.TrimPrefix(escapedPath, prefix)
	if rest == "" || strings.HasSuffix(rest, "/") || strings.Contains(rest, "//") {
		return nil, false
	}
	escapedSegments := strings.Split(rest, "/")
	segments := make([]string, 0, len(escapedSegments))
	for _, escapedSegment := range escapedSegments {
		lower := strings.ToLower(escapedSegment)
		if strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") ||
			strings.Contains(lower, "%5c") || strings.Contains(lower, "%25") {
			return nil, false
		}
		segment, err := url.PathUnescape(escapedSegment)
		if err != nil || segment == "" || segment == "." || segment == ".." ||
			strings.ContainsAny(segment, "/\\\x00\r\n") {
			return nil, false
		}
		segments = append(segments, segment)
	}
	return segments, true
}

func strictPositiveID(segment string) (int, bool) {
	id, err := strconv.Atoi(segment)
	return id, err == nil && id > 0 && strconv.Itoa(id) == segment
}

func routeAllows(method string, allowed []string) bool {
	for _, candidate := range allowed {
		if method == candidate {
			return true
		}
	}
	return false
}

func rejectRouteMethod(w http.ResponseWriter, allowed []string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func matchDomainSubroute(r *http.Request) (domainSubroute, bool) {
	segments, ok := strictRouteSegments(r, "/api/v1/domains/")
	if !ok {
		return domainSubroute{}, false
	}
	domainID, ok := strictPositiveID(segments[0])
	if !ok {
		return domainSubroute{}, false
	}
	match := domainSubroute{domainID: domainID}
	tail := segments[1:]
	key := strings.Join(tail, "/")
	getPost := []string{http.MethodGet, http.MethodPost}
	switch key {
	case "":
		match.kind, match.methods = "delete", []string{http.MethodDelete}
	case "hosting":
		match.kind, match.methods = "hosting", []string{http.MethodGet, http.MethodPut}
	case "app/status", "app/logs":
		match.kind, match.methods = "app", []string{http.MethodGet}
	case "app/start", "app/stop", "app/restart":
		match.kind, match.methods = "app", []string{http.MethodPost}
	case "php":
		match.kind, match.methods = "php", getPost
	case "php/pool":
		match.kind, match.methods = "php-pool", getPost
	case "general":
		match.kind, match.methods = "general", getPost
	case "aliases":
		match.kind, match.methods = "aliases", []string{http.MethodPost}
	case "dnssec":
		match.kind, match.methods = "dnssec", getPost
	case "ssl/mail":
		match.kind, match.methods = "ssl-mail", []string{http.MethodGet, http.MethodPut}
	case "ssl/retry":
		match.kind, match.methods = "ssl-retry", []string{http.MethodPost}
	case "ssl/renewal":
		match.kind, match.methods = "ssl-renewal", []string{http.MethodPut}
	case "ssl/letsencrypt":
		match.kind, match.methods = "ssl-letsencrypt", []string{http.MethodPost}
	case "ssl/upload":
		match.kind, match.methods = "ssl-upload", []string{http.MethodPost}
	case "ssl/settings":
		match.kind, match.methods = "ssl-settings", []string{http.MethodPost}
	case "ssl":
		match.kind, match.methods = "ssl", []string{http.MethodGet, http.MethodDelete}
	case "logs/access", "logs/error", "logs/php":
		match.kind, match.methods = "logs", []string{http.MethodGet, http.MethodDelete}
	case "databases":
		match.kind, match.methods = "databases", getPost
	case "files":
		match.kind, match.methods = "files", []string{http.MethodGet, http.MethodPost, http.MethodOptions}
	case "files/download":
		match.kind, match.methods = "files-download", []string{http.MethodGet}
	case "backups/schedule":
		match.kind, match.methods = "backup-schedule", []string{http.MethodGet, http.MethodPut, http.MethodDelete}
	case "backups/restore":
		match.kind, match.methods = "backup-restore", []string{http.MethodPost, http.MethodOptions}
	case "backups/download":
		match.kind, match.methods = "backup-download", []string{http.MethodGet}
	case "backups":
		match.kind, match.methods = "backups", []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions}
	case "cron":
		match.kind, match.methods = "cron", []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}
	case "mail/health":
		match.kind, match.methods = "mail-health", []string{http.MethodGet}
	case "mail/accounts/password":
		match.kind, match.methods = "mail", []string{http.MethodPut}
	case "mail/accounts":
		match.kind, match.methods = "mail", []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}
	case "mail/quota", "mail/rbl", "mail/setup", "mail/auth":
		match.kind, match.methods = "mail", []string{http.MethodGet, http.MethodOptions}
	case "mail/forwardings":
		match.kind, match.methods = "mail", []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions}
	case "mail/auth/dkim", "mail/auth/apply":
		match.kind, match.methods = "mail", []string{http.MethodPost, http.MethodOptions}
	case "mail/catch-all":
		match.kind, match.methods = "mail", []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodOptions}
	case "apps/install":
		match.kind, match.methods = "app-install", []string{http.MethodPost}
	case "usage":
		match.kind, match.methods = "usage", []string{http.MethodGet}
	case "connection":
		match.kind, match.methods = "connection", []string{http.MethodGet}
	case "dns/zone":
		match.kind, match.methods = "dns", getPost
	case "dns/records":
		match.kind, match.methods = "dns", []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	default:
		if len(tail) == 2 && tail[0] == "aliases" {
			match.kind, match.methods = "aliases", []string{http.MethodDelete}
		} else if len(tail) == 2 && tail[0] == "databases" {
			if _, ok := strictPositiveID(tail[1]); !ok {
				return domainSubroute{}, false
			}
			match.kind, match.methods = "database-delete", []string{http.MethodDelete}
		} else {
			return domainSubroute{}, false
		}
	}
	return match, true
}

func (p *Panel) handleDomainSubroute(w http.ResponseWriter, r *http.Request) {
	match, ok := matchDomainSubroute(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !routeAllows(r.Method, match.methods) {
		rejectRouteMethod(w, match.methods)
		return
	}
	if !p.authorizeDomainSubroute(w, r, match.domainID, match.kind) {
		return
	}
	switch match.kind {
	case "delete":
		p.handleDeleteDomain(w, r)
	case "hosting":
		p.handleDomainHosting(w, r, match.domainID)
	case "app":
		p.handleDomainApp(w, r, match.domainID)
	case "php":
		p.handleDomainPHPSettings(w, r)
	case "php-pool":
		p.handleDomainPHPPool(w, r)
	case "general":
		p.handleDomainGeneralSettings(w, r)
	case "aliases":
		p.handleDomainAliases(w, r)
	case "dnssec":
		p.handleDomainDNSSEC(w, r, match.domainID)
	case "ssl-mail":
		p.handleDomainSSLMail(w, r, match.domainID)
	case "ssl-retry":
		p.handleRetrySSLActivation(w, r)
	case "ssl-renewal":
		p.handleSSLRenewalSetting(w, r, match.domainID)
	case "ssl-letsencrypt":
		p.handleIssueLetsEncrypt(w, r)
	case "ssl-upload":
		p.handleUploadCertificate(w, r)
	case "ssl-settings":
		p.handleSSLSettings(w, r)
	case "ssl":
		p.handleDomainSSL(w, r)
	case "logs":
		p.handleDomainLogs(w, r)
	case "databases":
		p.handleDomainDatabases(w, r)
	case "database-delete":
		p.handleDeleteDatabase(w, r)
	case "files":
		p.handleDomainFiles(w, r)
	case "files-download":
		p.handleDomainFileDownload(w, r)
	case "backup-schedule":
		p.handleBackupSchedule(w, r, match.domainID)
	case "backup-restore":
		p.handleRestoreBackup(w, r)
	case "backup-download":
		p.handleDownloadBackup(w, r)
	case "backups":
		p.handleDomainBackups(w, r)
	case "cron":
		p.handleDomainCronJobs(w, r)
	case "mail-health":
		p.handleMailHealth(w, r, match.domainID)
	case "mail":
		p.handleDomainMail(w, r)
	case "app-install":
		p.handleAppInstall(w, r, match.domainID)
	case "usage":
		p.handleDomainUsage(w, r)
	case "connection":
		p.handleDomainConnection(w, r, match.domainID)
	case "dns":
		p.handleDomainDNS(w, r)
	default:
		http.NotFound(w, r)
	}
}

type databaseSubroute struct {
	resourceID int
	kind       string
	methods    []string
}

func matchDatabaseSubroute(r *http.Request, prefix string) (databaseSubroute, bool) {
	segments, ok := strictRouteSegments(r, prefix)
	if !ok {
		return databaseSubroute{}, false
	}
	resourceID, ok := strictPositiveID(segments[0])
	if !ok {
		return databaseSubroute{}, false
	}
	match := databaseSubroute{resourceID: resourceID}
	switch prefix {
	case "/api/v1/database-servers/":
		switch {
		case len(segments) == 1:
			match.kind, match.methods = "server-delete", []string{http.MethodDelete}
		case len(segments) == 2 && segments[1] == "databases":
			match.kind, match.methods = "server-databases", []string{http.MethodGet, http.MethodPost}
		case len(segments) == 2 && segments[1] == "users":
			match.kind, match.methods = "server-users", []string{http.MethodGet, http.MethodPost}
		default:
			return databaseSubroute{}, false
		}
	case "/api/v1/databases/":
		switch {
		case len(segments) == 1:
			match.kind, match.methods = "database-delete", []string{http.MethodDelete}
		case len(segments) == 2 && segments[1] == "grants":
			match.kind, match.methods = "database-grants", []string{http.MethodGet, http.MethodPost}
		default:
			return databaseSubroute{}, false
		}
	case "/api/v1/database-users/":
		if len(segments) != 1 {
			return databaseSubroute{}, false
		}
		match.kind, match.methods = "user-delete", []string{http.MethodDelete}
	case "/api/v1/database-grants/":
		if len(segments) != 1 {
			return databaseSubroute{}, false
		}
		match.kind, match.methods = "grant-delete", []string{http.MethodDelete}
	default:
		return databaseSubroute{}, false
	}
	return match, true
}

func (p *Panel) handleDatabaseSubroute(w http.ResponseWriter, r *http.Request, prefix string) {
	match, ok := matchDatabaseSubroute(r, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !routeAllows(r.Method, match.methods) {
		rejectRouteMethod(w, match.methods)
		return
	}
	switch match.kind {
	case "server-delete":
		p.handleDeleteDatabaseV2Server(w, r)
	case "server-databases":
		if r.Method == http.MethodGet {
			p.handleListDatabasesV2(w, r)
		} else {
			p.handleCreateDatabaseV2(w, r)
		}
	case "server-users":
		if r.Method == http.MethodGet {
			p.handleListDatabaseUsers(w, r)
		} else {
			p.handleCreateDatabaseV2User(w, r)
		}
	case "database-delete":
		p.handleDeleteDatabaseV2(w, r)
	case "database-grants":
		if r.Method == http.MethodGet {
			p.handleListDatabaseGrants(w, r)
		} else {
			p.handleGrantDatabaseAccess(w, r)
		}
	case "user-delete":
		p.handleDeleteDatabaseV2User(w, r)
	case "grant-delete":
		p.handleRevokeDatabaseAccess(w, r)
	default:
		http.NotFound(w, r)
	}
}

func main() {
	// This hidden read-only probe exits before flag parsing, database access,
	// RPC setup, sockets, or background work. The enrollment tool uses it to
	// prove the installed panel and agent are the exact same trusted build.
	if emitPanelBuildIdentity(os.Args[1:], os.Stdout) {
		return
	}
	checkWALAwareServiceOperationsIdleFlag := flag.Bool("check-service-operations-idle-wal-aware", false, "Prove that the service operation queue is idle in a running database or a stopped database with a WAL, then exit")
	checkWALAwarePreLedgerServiceOperationsIdleFlag := flag.Bool("check-pre-ledger-service-operations-idle-wal-aware", false, "Prove that a running pre-ledger database or a stopped pre-ledger database with a WAL is safe to migrate, then exit")
	createAdmin := flag.Bool("create-admin", false, "Create or update an administrator, then exit / Bir yönetici oluştur ya da güncelle, sonra çık")
	var adminCredentialsFileFlag inheritedAdminCredentialsFileFlag
	flag.Var(&adminCredentialsFileFlag, "admin-credentials-file", "Read strict admin credentials JSON from bounded stdin; exact value must be -")
	var validateAdminCredentialsFileFlag inheritedAdminCredentialsFileFlag
	flag.Var(&validateAdminCredentialsFileFlag, "validate-admin-credentials-file", "Validate strict admin credentials JSON from bounded stdin without changing panel state; exact value must be -")
	insecureCookies := flag.Bool("insecure-cookies", false, "Send session cookies without the Secure flag (HTTP-only local dev) / Oturum çerezlerini Secure bayrağı olmadan gönder (yalnızca HTTP yerel geliştirme)")
	demo := flag.Bool("demo", false, "Development only: seed one account per role and show quick-login credentials on the login screen / Yalnızca geliştirme: her rol için hesap oluştur ve giriş ekranında hızlı-giriş bilgilerini göster")
	countUsersFlag := flag.Bool("count-users", false, "Print the number of users and exit (used by install.sh) / Kullanıcı sayısını yazıp çık (install.sh kullanır)")
	countUsersReadOnlyWALAwareFlag := flag.Bool("count-users-read-only-wal-aware", false, "Print the number of usable users from a read-only WAL-aware private snapshot, then exit")
	checkServiceOperationsIdleFlag := flag.Bool("check-service-operations-idle", false, "Exit successfully only when no queued or running service operation exists / Yalnızca sırada veya çalışan servis işlemi yoksa başarıyla çık")
	checkPreLedgerServiceOperationsIdleFlag := flag.Bool("check-pre-ledger-service-operations-idle", false, "Verify migration history through version 20 and reject partial service queue objects, then exit / 20. sürüme kadarki migration geçmişini doğrula ve yarım servis kuyruğu nesnelerini reddet, sonra çık")
	createServiceOperationSnapshotFlag := flag.String("create-service-operation-snapshot", "", "Create a transaction-consistent standalone panel database snapshot at this absolute path, then exit / Bu mutlak yolda işlem tutarlı bağımsız panel veritabanı anlık görüntüsü oluştur, sonra çık")
	restoreServiceOperationSnapshotFlag := flag.String("restore-service-operation-snapshot", "", "Offline root-only restore from this trusted absolute celikpanel.db; both services must be stopped and the inherited release guard must be held / Bu güvenilir mutlak celikpanel.db dosyasından çevrim dışı yalnız-root geri yükleme; iki servis durmalı ve devralınan yayın koruması tutulmalıdır")
	ensureServiceOperationRescueSnapshotFlag := flag.String("ensure-service-operation-rescue-snapshot", "", "Ensure the transaction-bound root-only recovery snapshot exists without changing the canonical database, then exit / Kanonik veritabanını değiştirmeden işleme bağlı yalnız-root kurtarma anlık görüntüsünün varlığını doğrula, sonra çık")
	provePreLedgerSnapshotEquivalenceFlag := flag.String("prove-pre-ledger-snapshot-equivalence", "", "Read-only root proof that this standalone active-update snapshot is logically equivalent to the WAL-aware canonical pre-ledger database, then exit / Bu bağımsız etkin-güncelleme anlık görüntüsünün WAL-duyarlı kanonik defter-öncesi veritabanına mantıksal olarak eşit olduğunu salt-okunur root kanıtıyla doğrula, sonra çık")
	releaseTransactionFDFlag := flag.Int("release-transaction-fd", -1, "Inherited descriptor that owns the global release transaction lock / Global yayın işlem kilidinin sahibi olan devralınmış descriptor")
	releaseTransactionTokenFlag := flag.String("release-transaction-token", "", "Exact 64-character lowercase hexadecimal release transaction token / Tam 64 karakterli küçük harf onaltılık yayın işlem belirteci")
	releaseTransactionOperationFlag := flag.String("release-transaction-operation", "", "Exact release transaction operation: update or rollback / Tam yayın işlem operasyonu: update veya rollback")
	releaseTransactionSnapshotFlag := flag.String("release-transaction-snapshot", "", "Safe snapshot basename recorded by the release transaction / Yayın işleminin kaydettiği güvenli anlık görüntü temel adı")
	serviceOperationSnapshotSchemaFlag := flag.String("snapshot-schema", "", "Snapshot schema contract: normal or pre-ledger / Anlık görüntü şema sözleşmesi: normal veya pre-ledger")
	migrateOnlyFlag := flag.Bool("migrate-only", false, "Open the canonical database, apply embedded migrations, and exit before agent or HTTP startup / Kanonik veritabanını aç, gömülü migration'ları uygula ve agent ya da HTTP başlamadan çık")
	generateControlPlaneKeyFlag := flag.Bool("generate-control-plane-key", false, "Print one fresh control-plane backup key and exit; it is never stored on this host / Yeni bir kontrol düzlemi yedek anahtarı yazıp çık; bu makinede hiç saklanmaz")
	createControlPlaneArchiveFlag := flag.String("create-control-plane-archive", "", "Write one sealed control-plane archive to this absolute path, then exit / Bu mutlak yola tek bir mühürlü kontrol düzlemi arşivi yaz, sonra çık")
	restoreControlPlaneArchiveFlag := flag.String("restore-control-plane-archive", "", "Restore this sealed control-plane archive onto a fresh host, then exit / Bu mühürlü kontrol düzlemi arşivini temiz bir makineye geri yükle, sonra çık")
	var controlPlaneKeyFileFlag inheritedControlPlaneKeyFileFlag
	flag.Var(&controlPlaneKeyFileFlag, "control-plane-key-file", "Read the control-plane backup key from bounded stdin; exact value must be -")
	if err := validateAdminCredentialsFileArgumentSpellings(os.Args[1:]); err != nil {
		log.Fatalf("Invalid admin credentials file argument: %v", err)
	}
	if err := validateControlPlaneKeyFileArgumentSpellings(os.Args[1:]); err != nil {
		log.Fatalf("Invalid control-plane key file argument: %v", err)
	}
	flag.Parse()
	if err := validateAdminCredentialsFileFlags(
		*createAdmin,
		adminCredentialsFileFlag,
		validateAdminCredentialsFileFlag,
	); err != nil {
		log.Fatalf("Invalid admin credentials file flags: %v", err)
	}
	if err := validateControlPlaneCommandFlags(
		*generateControlPlaneKeyFlag,
		*createControlPlaneArchiveFlag,
		*restoreControlPlaneArchiveFlag,
		controlPlaneKeyFileFlag,
	); err != nil {
		log.Fatalf("Invalid control-plane archive flags (%s): %v", controlPlaneCommandContract(), err)
	}

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
	proveSnapshotEquivalenceRequestedByFlags :=
		strings.TrimSpace(*provePreLedgerSnapshotEquivalenceFlag) != ""
	transactionMetadataRequestedByFlags :=
		strings.TrimSpace(*serviceOperationSnapshotSchemaFlag) != "" ||
			releaseTransaction.fd != -1 ||
			strings.TrimSpace(releaseTransaction.token) != "" ||
			strings.TrimSpace(releaseTransaction.operation) != "" ||
			strings.TrimSpace(releaseTransaction.snapshot) != ""
	databaseActionRequestedByFlags := createOrRestorePathRequestedByFlags ||
		(!rescueSnapshotRequestedByFlags &&
			!proveSnapshotEquivalenceRequestedByFlags &&
			transactionMetadataRequestedByFlags)
	if err := validatePanelCommandModes(panelCommandModes{
		createAdmin:                *createAdmin,
		validateAdminCredentials:   validateAdminCredentialsFileFlag.set,
		countUsers:                 *countUsersFlag,
		countUsersReadOnlyWALAware: *countUsersReadOnlyWALAwareFlag,
		checkIdle:                  *checkServiceOperationsIdleFlag,
		checkPreLedgerIdle:         *checkPreLedgerServiceOperationsIdleFlag,
		checkWALAwareIdle:          *checkWALAwareServiceOperationsIdleFlag,
		checkWALAwarePreLedgerIdle: *checkWALAwarePreLedgerServiceOperationsIdleFlag,
		createOrRestore:            databaseActionRequestedByFlags,
		rescueSnapshot:             rescueSnapshotRequestedByFlags,
		proveSnapshotEquivalence:   proveSnapshotEquivalenceRequestedByFlags,
		migrateOnly:                *migrateOnlyFlag,
		generateControlPlaneKey:    *generateControlPlaneKeyFlag,
		createControlPlaneArchive:  strings.TrimSpace(*createControlPlaneArchiveFlag) != "",
		restoreControlPlaneArchive: strings.TrimSpace(*restoreControlPlaneArchiveFlag) != "",
		demo:                       *demo,
		insecureCookies:            *insecureCookies,
	}); err != nil {
		log.Fatalf("Invalid panel command mode: %v", err)
	}
	if validateAdminCredentialsFileFlag.set {
		if err := validateAdminCredentialsFile(os.Stdin); err != nil {
			log.Fatalf("Admin credentials file validation failed: %v", err)
		}
		return
	}
	// The control-plane modes run here, before the panel announces itself and
	// before any database is opened: two of them are offline root operations on
	// a host whose panel must not be running, and the third only prints a key.
	// Kontrol düzlemi kipleri burada, panel kendini duyurmadan ve hiçbir
	// veritabanı açılmadan önce çalışır.
	if *generateControlPlaneKeyFlag {
		if err := runGenerateControlPlaneKey(os.Stdout); err != nil {
			log.Fatalf("Generate control-plane key failed: %v", err)
		}
		return
	}
	if strings.TrimSpace(*createControlPlaneArchiveFlag) != "" {
		if err := runCreateControlPlaneArchive(
			*createControlPlaneArchiveFlag,
			os.Stdin,
			os.Stdout,
		); err != nil {
			log.Fatalf(
				"Create control-plane archive failed (%s): %v",
				controlPlaneCommandContract(),
				err,
			)
		}
		return
	}
	if strings.TrimSpace(*restoreControlPlaneArchiveFlag) != "" {
		if err := runRestoreControlPlaneArchive(
			*restoreControlPlaneArchiveFlag,
			os.Stdin,
			os.Stdout,
		); err != nil {
			log.Fatalf(
				"Restore control-plane archive failed (%s): %v",
				controlPlaneCommandContract(),
				err,
			)
		}
		return
	}
	log.Println("Starting CelikPanel Backend...")
	if *countUsersReadOnlyWALAwareFlag {
		count, err := countUsableUsersReadOnlyWALAware(databaseFile())
		if err != nil {
			log.Fatalf("count-users-read-only-wal-aware failed: %v", err)
		}
		fmt.Println(count)
		return
	}
	if proveSnapshotEquivalenceRequestedByFlags {
		_, err := validatePreLedgerSnapshotEquivalenceRequest(
			*provePreLedgerSnapshotEquivalenceFlag,
			*serviceOperationSnapshotSchemaFlag,
			releaseTransaction,
			createOrRestorePathRequestedByFlags || rescueSnapshotRequestedByFlags,
		)
		if err != nil {
			log.Fatalf("Invalid pre-ledger snapshot equivalence request: %v", err)
		}
		if err := provePreLedgerSnapshotEquivalence(
			databaseFile(),
			*provePreLedgerSnapshotEquivalenceFlag,
			releaseTransaction,
		); err != nil {
			log.Fatalf("Pre-ledger snapshot equivalence proof failed: %v", err)
		}
		log.Printf(
			"Staged pre-ledger snapshot at %s is logically equivalent to the canonical database",
			*provePreLedgerSnapshotEquivalenceFlag,
		)
		return
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
		var err error
		if adminCredentialsFileFlag.set {
			err = runCreateAdminFromCredentialsFile(database, os.Stdin, os.Stdout)
		} else {
			err = runCreateAdmin(database)
		}
		if err != nil {
			log.Fatalf("create-admin failed: %v", err)
		}
		return
	}

	// Connect to Agent. The reconnecting wrapper survives agent restarts and
	// poisoned RPC streams without needing a panel restart.
	// Agent'a bağlan. Yeniden bağlanan sarmalayıcı, panel yeniden başlatılmadan
	// agent yeniden başlamalarını ve bozulmuş RPC akışlarını atlatır.
	rawClient, waited, err := connectAgentPatiently(
		context.Background(), dialAgentOnce, nil, nil,
	)
	if err != nil {
		log.Fatalf(
			"Failed to connect to Agent after waiting %s: %v", waited.Round(time.Second), err,
		)
	}
	client := transport.NewReconnectingClient(rawClient)
	if waited > time.Second {
		log.Printf("Connected to Agent RPC after waiting %s", waited.Round(time.Second))
	} else {
		log.Println("Connected to Agent RPC")
	}

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
	panel.orchestrator = services.NewSiteOrchestrator(
		database.GetDB(),
		panelSiteAgentClient{panel: panel},
		buildCommit,
	)

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

	// Bind and serve TLS before durable mutation recovery. Certificate
	// activation restarts the panel and then verifies the published leaf over
	// this listener; an atomic gate returns 503 for every application request
	// until all startup recovery and route registration is complete.
	applicationHandler := csrfProtect(
		panel.requireAuth(http.DefaultServeMux),
	)
	startupGate := newPanelHTTPStartupGate(applicationHandler)
	handler := securityHeaders(panel.secureCookies, startupGate)
	addr := listenAddr()
	server := newPanelHTTPServer(addr, handler)
	tlsOn, certPath, keyPath, err := tlsSettings()
	if err != nil {
		log.Fatalf("TLS setup failed: %v", err)
	}
	// Plain HTTP with Secure cookies would hand the browser a cookie it
	// silently drops. Refuse it before publishing the listener.
	if !tlsOn && panel.secureCookies {
		log.Fatal("refusing to serve over plain HTTP with secure cookies: enable TLS (CELIKPANEL_TLS=1 or CELIKPANEL_TLS_CERT/KEY) or pass --insecure-cookies for development")
	}
	runningServer, err := startPanelHTTP(server, certPath, keyPath)
	if err != nil {
		log.Fatalf("Failed to start panel listener: %v", err)
	}
	if tlsOn {
		log.Printf("Panel startup listener active on %s (HTTPS; application gated)", addr)
	} else {
		log.Printf("Panel startup listener active on %s (HTTP; application gated)", addr)
	}

	// A failed reconcile degrades its subsystem; it does not end the process.
	// The durable markers stay for the next replay, the listener stays bound,
	// and the operator gets a coded refusal instead of a flapping port (A17).
	// Başarısız bir uzlaştırma alt sistemini kısıtlar, süreci bitirmez. Kalıcı
	// işaretler sonraki denemeye kalır, dinleyici ayakta kalır ve operatör
	// çırpınan bir port yerine kodlu bir ret alır (A17).
	if recovered, err := panel.recoverInterruptedServiceOperations(context.Background()); err != nil {
		panel.markSubsystemDegraded(
			degradedSubsystemServiceOperations,
			errCodeStartupRecoveryFailed,
			err,
		)
		log.Printf(
			"DEGRADED %s: failed to recover interrupted service operations: %v",
			degradedSubsystemServiceOperations, err,
		)
	} else if recovered > 0 {
		log.Printf("Reconciled %d interrupted service operation(s)", recovered)
	}
	if recovered, err := panel.recoverInterruptedAppInstallOperations(context.Background()); err != nil {
		panel.markSubsystemDegraded(
			degradedSubsystemAppInstalls,
			errCodeStartupRecoveryFailed,
			err,
		)
		log.Printf(
			"DEGRADED %s: failed to recover interrupted application installs: %v",
			degradedSubsystemAppInstalls, err,
		)
	} else if recovered > 0 {
		log.Printf("Marked %d interrupted application install(s) for review", recovered)
	}

	// One-time repair of pre-A4 rows: seal any database root password that
	// was stored as plaintext. Fatal on failure — booting with credentials
	// half plaintext, half sealed hides the very problem A4 closes.
	// A4 öncesi satırların tek seferlik onarımı: düz metin saklanmış her
	// veritabanı root parolasını mühürle. Hata ölümcül — kimlik bilgileri
	// yarı düz metin, yarı mühürlü açılmak, A4'ün kapattığı sorunu gizler.
	// Two different failures reach here and they deserve different answers.
	// A structural fault (bad query, write error) still exits: booting with
	// credentials half plaintext, half sealed hides the very problem this
	// migration closes. But a sealed value that will not open is a statement
	// about the pair (this database, this secret.key) — the shape of a restore
	// without its key — and exiting there is self-defeating: the panel is the
	// only surface that could re-enter those credentials, and an unbootable
	// panel under an automatic restart never lets the operator reach it.
	//
	// Buraya iki farklı hata ulaşır ve ikisi farklı yanıt hak eder. Yapısal bir
	// arıza (bozuk sorgu, yazma hatası) hâlâ çıkışa götürür: kimlik bilgileri
	// yarı düz metin yarı mühürlü açılmak, bu göçün kapattığı sorunu gizler.
	// Ama açılmayan mühürlü bir değer, (bu veritabanı, bu secret.key) çifti
	// hakkında bir ifadedir — anahtarsız bir geri yüklemenin biçimi — ve orada
	// çıkmak kendi kendini yenilgiye uğratır: o kimlik bilgilerini yeniden
	// girebilecek tek yüzey paneldir ve açılamayan bir panele operatör hiç
	// ulaşamaz.
	// Identity first, and for every family at once. Each migration commits on its
	// own, so without a single up-front check the first family that happens to be
	// all-plaintext gets sealed under a key that cannot open the others — one
	// database, two keys, half of it lost, and no error at the moment of loss.
	// Proving the pairing before any of them runs is what makes "stop on the
	// first mismatch" mean "stop before the first write".
	//
	// Önce kimlik, ve bütün aileler için birden. Her göç kendi başına commit
	// ettiğinden, tek bir ön denetim olmadan tesadüfen tamamen düz metin olan ilk
	// aile, diğerlerini açamayan bir anahtarla mühürlenir — tek veritabanı, iki
	// anahtar, yarısı kayıp ve kayıp anında hiçbir hata yok. Eşleşmeyi hepsinden
	// önce kanıtlamak, "ilk uyuşmazlıkta dur"un "ilk yazımdan önce dur" anlamına
	// gelmesini sağlayan şeydir.
	secretsUsable := true
	if err := panel.verifySecretKeyIdentity(context.Background()); err != nil {
		if errors.Is(err, errSecretKeyMismatch) {
			secretsUsable = false
			panel.markSubsystemDegraded(
				degradedSubsystemSealedSecrets, errCodeSealedSecretUnreadable, err,
			)
			log.Printf(
				"DEGRADED %s: %v; no sealed secret will be written this boot, and the "+
					"affected credentials can be re-entered once the correct key is restored",
				degradedSubsystemSealedSecrets, err,
			)
		} else {
			log.Fatalf("Failed to verify the secret key against this database: %v", err)
		}
	}

	for _, migration := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"legacy database passwords", panel.encryptLegacyDBPasswords},
		{"TOTP secrets", panel.encryptLegacyTOTPSecrets},
		{"legacy VPN preshared keys", panel.encryptLegacyVPNPresharedKeys},
	} {
		if !secretsUsable {
			continue
		}
		err := migration.run(context.Background())
		switch {
		case err == nil:
		case errors.Is(err, errSealedSecretUnreadable):
			panel.markSubsystemDegraded(
				degradedSubsystemSealedSecrets,
				errCodeSealedSecretUnreadable,
				err,
			)
			log.Printf(
				"DEGRADED %s: %s could not be opened with the current key; "+
					"the panel is serving so the affected credentials can be re-entered: %v",
				degradedSubsystemSealedSecrets, migration.name, err,
			)
		default:
			log.Fatalf("Failed to migrate %s: %v", migration.name, err)
		}
	}

	// Repair only derived certificate runtime state from the durable ledger.
	// This removes crash-left staging lineages/validation names; it never
	// changes a user's panel setting.
	//
	// A resumed durable operation owns serviceMutationMu until its panel row is
	// terminal. Consume that barrier before startup maintenance, then release
	// it: the maintenance helpers own their component/direct mutation locks and
	// may legitimately acquire serviceMutationMu themselves.
	panel.serviceMutationMu.Lock()
	panel.serviceMutationMu.Unlock()
	activationCtx, activationCancel := context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	if err := panel.requireStartupAgentMutationSlot(activationCtx); err != nil {
		activationCancel()
		panel.markSubsystemDegraded(
			degradedSubsystemCertificates,
			errCodeStartupRecoveryFailed,
			err,
		)
		log.Printf(
			"DEGRADED %s: no startup mutation lease for certificate reconcile: %v",
			degradedSubsystemCertificates, err,
		)
	} else {
		activationCancel()
		panel.reconcileCertificateRuntimeAtStartup()
	}

	// Fail closed before accepting HTTP: a peer whose one-time private config
	// was interrupted must not survive a process restart as ghost access.
	activationCtx, activationCancel = context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	// Exiting here never contained the risk it was written for: wg0 is its own
	// service, so a dead panel leaves an interrupted peer exactly as reachable
	// while removing the only surface that can see or revoke it. Degrading
	// keeps that surface up and names the peer state as unresolved.
	// Buradan çıkmak, yazıldığı riski hiçbir zaman sınırlamadı: wg0 kendi
	// servisidir, ölü bir panel yarım kalmış bir peer'i aynı ölçüde erişilebilir
	// bırakır ve onu görebilecek ya da iptal edebilecek tek yüzeyi ortadan
	// kaldırır. Kısıtlamak o yüzeyi ayakta tutar ve durumu çözülmemiş olarak adlandırır.
	if err := panel.requireStartupAgentMutationSlot(activationCtx); err != nil {
		activationCancel()
		panel.markSubsystemDegraded(
			degradedSubsystemVPN,
			errCodeStartupRecoveryFailed,
			err,
		)
		log.Printf(
			"DEGRADED %s: no startup mutation lease for VPN recovery: %v",
			degradedSubsystemVPN, err,
		)
	} else {
		activationCancel()
		recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 45*time.Second)
		if err := panel.recoverVPNProvisioningState(recoveryCtx); err != nil {
			panel.markSubsystemDegraded(
				degradedSubsystemVPN,
				errCodeStartupRecoveryFailed,
				err,
			)
			log.Printf(
				"DEGRADED %s: incomplete VPN provisioning not recovered: %v",
				degradedSubsystemVPN, err,
			)
		}
		recoveryCancel()
	}
	activationCtx, activationCancel = context.WithTimeout(
		context.Background(),
		panelMutationRecoveryTimeout,
	)
	if err := panel.requireStartupAgentMutationSlot(activationCtx); err != nil {
		activationCancel()
		panel.markSubsystemDegraded(
			degradedSubsystemMailFilters,
			errCodeStartupRecoveryFailed,
			err,
		)
		log.Printf(
			"DEGRADED %s: no startup mutation lease for mail repair: %v",
			degradedSubsystemMailFilters, err,
		)
	} else {
		activationCancel()
		panel.wireMailFiltersSynchronouslyAtStartup()
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

	// Demo credentials (public, but empty unless --demo is set).
	// Demo kimlik bilgileri (herkese açık, ama --demo yoksa boş).
	http.HandleFunc("/api/v1/auth/demo", panel.handleDemoAccounts)
	http.HandleFunc("/api/v1/auth/login/totp", panel.handleLoginTOTP)

	// Account management (admin + reseller; role rules inside handlers).
	// Hesap yönetimi (admin + bayi; rol kuralları işleyicilerin içinde).
	http.HandleFunc("/api/v1/users", panel.handleUsers)
	http.HandleFunc("/api/v1/users/", panel.handleUserByID)
	http.HandleFunc("/api/v1/team-members", panel.handleTeamMembers)
	http.HandleFunc("/api/v1/team-members/", panel.handleTeamMemberByID)
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
	// The two legacy endpoints remain readable for old clients. Their partial
	// PUT contracts fail closed; all topology writes go through dns-setup.
	http.HandleFunc("/api/v1/settings/nameservers", panel.handleNameserverSettings)
	http.HandleFunc("/api/v1/settings/dns-cluster", panel.handleDNSCluster)
	http.HandleFunc("/api/v1/settings/dns-setup", panel.handleDNSSetup)
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
	http.HandleFunc(panelUpdateCheckPath, panel.handlePanelUpdateCheck)
	http.HandleFunc(panelUpdateStartPath, panel.handlePanelUpdateStart)
	http.HandleFunc(panelUpdateAbandonPath, panel.handlePanelUpdateAbandon)
	http.HandleFunc(panelUpdateStatusPath, panel.handlePanelUpdateStatus)
	http.HandleFunc("/api/v1/service/status", panel.handleServiceStatus)
	http.HandleFunc("/api/v1/service/install", panel.handleServiceInstall)
	http.HandleFunc(mailProfileInstallPath, panel.handleMailProfileInstall)
	http.HandleFunc("/api/v1/service/operation", panel.handleServiceOperation)
	http.HandleFunc("/api/v1/service/candidate", panel.handleServiceCandidate)
	http.HandleFunc("/api/v1/service/uninstall", panel.handleServiceUninstall)
	http.HandleFunc(hostMutationReadinessPath, panel.handleHostMutationReadiness)
	http.HandleFunc("/api/v1/firewall", panel.handleFirewall)
	http.HandleFunc("/api/v1/repo", panel.handleRepo)
	http.HandleFunc("/api/v1/hosting/capabilities", panel.handleHostingCapabilities)
	http.HandleFunc("/api/v1/panel/certificate", panel.handlePanelCertificate)
	http.HandleFunc(securityAuditPath, panel.handleSecurityAudit)
	http.HandleFunc("/dbtool/", panel.handleDBToolProxy)
	http.HandleFunc("/webmail/", panel.handleWebmailProxy)

	// Domain Management
	http.HandleFunc("/api/v1/domains", panel.handleDomains)
	http.HandleFunc("/api/v1/domains/create", panel.handleCreateDomain)

	// Domain-specific routes (PHP, general, aliases, SSL, logs, databases, delete, etc.)
	http.HandleFunc("/api/v1/domains/", panel.handleDomainSubroute)
	// Single ownership chokepoint: every /domains/{id}/... sub-resource
	// flows through here, so one guard covers them all. The first path
	// segment after the prefix is the domain ID.
	// Tek sahiplik kapısı: her /domains/{id}/... alt kaynağı buradan geçer,
	// bu yüzden tek koruma hepsini kapsar. Önekten sonraki ilk yol parçası
	// domain kimliğidir.

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
	http.HandleFunc("/api/v1/dns/engine", panel.handleDNSEngine)
	http.HandleFunc("/api/v1/dns/engine/reconcile", panel.handleDNSEngineReconcile)
	http.HandleFunc("/api/v1/dns/engine/switch/preview", panel.handleDNSEngineSwitchPreview)
	http.HandleFunc("/api/v1/dns/engine/switch", panel.handleDNSEngineSwitch)
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

	http.HandleFunc("/api/v1/database-servers/", func(w http.ResponseWriter, r *http.Request) {
		panel.handleDatabaseSubroute(w, r, "/api/v1/database-servers/")
	})

	// Database operations
	http.HandleFunc("/api/v1/databases/", func(w http.ResponseWriter, r *http.Request) {
		panel.handleDatabaseSubroute(w, r, "/api/v1/databases/")
	})

	// User/Grant deletions
	http.HandleFunc("/api/v1/database-users/", func(w http.ResponseWriter, r *http.Request) {
		panel.handleDatabaseSubroute(w, r, "/api/v1/database-users/")
	})

	http.HandleFunc("/api/v1/database-grants/", func(w http.ResponseWriter, r *http.Request) {
		panel.handleDatabaseSubroute(w, r, "/api/v1/database-grants/")
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
	http.Handle("/", frontendHandler(webRoot))

	startupGate.Open()
	// Immediate background host mutators are started only after recovery,
	// route registration, and admission of normal application traffic.
	panel.startBackupScheduler()
	panel.startCertRenewalScheduler()
	panel.startVPNEntitlementReconciler()
	protocol := "HTTP"
	if tlsOn {
		protocol = "HTTPS"
	}
	log.Printf("Panel ready on %s (%s)", addr, protocol)
	if err := waitPanelHTTP(runningServer); err != nil {
		log.Fatal(err)
	}
}

// countUsers reports how many users exist, to gate startup.
// countUsers, başlangıcı kısıtlamak için kaç kullanıcı olduğunu bildirir.
func (p *Panel) countUsers() (int, error) {
	var n int
	err := p.db.GetDB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// wireMailFiltersSynchronouslyAtStartup is the bounded startup form of the
// legacy background repair. The recovery barrier has already drained; this
// helper owns a durable agent lease and finishes before HTTP admission.
func (p *Panel) wireMailFiltersSynchronouslyAtStartup() {
	var response transport.WireMailFiltersResponse
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := p.withStandaloneAgentMutation(
		ctx,
		"mail_filter_wire",
		"startup",
		"",
		func(callCtx context.Context, binding agentMutationBinding) error {
			request := transport.ServiceMutationRequest{
				ServiceMutationBinding: binding,
			}
			if err := p.callAgentContext(
				callCtx,
				"Agent.WireMailFilters",
				&request,
				&response,
			); err != nil {
				return err
			}
			if response.Error != "" {
				return errors.New(response.Error)
			}
			return nil
		},
	)
	if err != nil {
		return
	}
	if response.Detail != "" {
		log.Printf("milter chain: %s", response.Detail)
	}
}

func (p *Panel) handleServices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Call RPC
	var services []core.Service
	err := p.callAgent("Agent.GetServices", &transport.Empty{}, &services)
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
	serviceName = strings.TrimSuffix(strings.TrimSpace(serviceName), ".service")
	service := core.ServiceForUnit(serviceName)
	if serviceName == "" || service == nil {
		writeClientError(w, http.StatusBadRequest, "unknown managed service")
		return
	}

	switch req.Action {
	case "start", "stop", "restart", "reload":
	default:
		writeClientError(w, http.StatusBadRequest, "invalid action")
		return
	}
	if managedDNSEngineService(service) {
		writeDNSEngineWorkflowRequired(w)
		return
	}
	release, busy := p.beginServiceMutation(w, r)
	if busy {
		return
	}
	defer release()
	var reply transport.ServiceActionResult
	err := p.withStandaloneAgentMutation(r.Context(), "service_"+req.Action, serviceName, "", func(callCtx context.Context, binding agentMutationBinding) error {
		request := transport.ServiceMutationActionRequest{
			ServiceMutationBinding: binding,
			ServiceName:            serviceName,
			Action:                 req.Action,
		}
		if err := p.callAgentContext(callCtx, "Agent.ServiceMutationAction", &request, &reply); err != nil {
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

		var reply transport.UpdateConfigResponse
		err := p.callAgent("Agent.UpdateConfig", &transport.UpdateConfigArgs{
			Path:    req.Path,
			Content: req.Content,
		}, &reply)

		if err != nil {
			// Wire, protocol and unexpected agent failures remain server
			// errors. Expected operator failures are returned below as typed
			// response data and must never be inferred from this text.
			p.audit(r, "config.write.failed:"+req.Path+" — "+auditReason(err.Error()), "config", 0)
			writeServerError(w, err)
			return
		}
		if reply.Error != nil {
			p.audit(r, "config.write.failed:"+req.Path+" — "+auditReason(reply.Error.Message), "config", 0)
			writeConfigRPCError(w, reply.Error)
			return
		}
		if !reply.Success {
			err := errors.New("agent did not confirm configuration update")
			p.audit(r, "config.write.failed:"+req.Path+" — "+auditReason(err.Error()), "config", 0)
			writeServerError(w, err)
			return
		}

		// Writing a root-owned file is the last thing that should be quieter
		// than a service restart, which has always been audited.
		// Root'a ait bir dosyayı yazmak, her zaman denetlenen servis yeniden
		// başlatmasından daha sessiz olmaması gereken son şeydir.
		p.audit(r, "config.write:"+req.Path, "config", 0)
		json.NewEncoder(w).Encode(map[string]bool{"success": reply.Success})
		return
	}

	// GET
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	var reply transport.ConfigResponse
	err := p.callAgent("Agent.GetConfig", &transport.GetConfigArgs{Path: path}, &reply)
	if err != nil {
		p.audit(r, `config.read.failed:`+path+` — `+auditReason(err.Error()), `config`, 0)
		writeServerError(w, err)
		return
	}
	if reply.Error != nil {
		p.audit(r, `config.read.failed:`+path+` — `+auditReason(reply.Error.Message), `config`, 0)
		writeConfigRPCError(w, reply.Error)
		return
	}

	p.audit(r, `config.read:`+path, `config`, 0)
	json.NewEncoder(w).Encode(reply)
}
