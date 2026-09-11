//go:build linux

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/auth"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// This opt-in daemon runs only inside a separately provisioned virgin QEMU
// profile fixture. Its driver uses real HTTPS, session/CSRF authentication and
// production setup handlers. The real agent owns every host mutation. Only the
// license trust root and initial administrator session are fixture dependencies;
// no readiness probe, child operation, certificate source or worker is mocked.
// The production certificate operation restarts this named systemd service, so
// its normal TLS startup and persisted child recovery are exercised as well.
func TestServerSetupDisposableProfileDaemon(t *testing.T) {
	profile := os.Getenv("CELIKPANEL_SETUP_PROFILE_VM")
	if profile == "" {
		t.Skip("requires the explicit disposable profile VM driver")
	}
	if !slices.Contains([]string{"web", "application", "webmail"}, profile) {
		t.Fatal("invalid purpose fixture profile")
	}
	runServerSetupDisposableProfileDaemon(t, profile)
}

func runServerSetupDisposableProfileDaemon(t *testing.T, profile string) {
	t.Helper()
	account, accountErr := user.Lookup("celikpanel")
	if accountErr != nil {
		t.Fatal(accountErr)
	}
	panelUID, uidErr := strconv.Atoi(account.Uid)
	if !slices.Contains([]string{"web", "application", "webmail", "dnsprimary", "dnssecondary"}, profile) || uidErr != nil || panelUID == 0 || os.Geteuid() != panelUID {
		t.Fatal("not an authorized disposable profile fixture")
	}
	marker, err := os.ReadFile("/var/lib/celikpanel-profile-vm/fixture")
	if err != nil || string(marker) != "fresh-setup-profile-acceptance-v1\n" {
		t.Fatal("missing virgin profile fixture marker")
	}
	host, err := os.Hostname()
	if err != nil || (host != "profile65-"+profile && host != "mail."+profile+".setup.test") {
		t.Fatalf("unexpected disposable host identity %q", host)
	}
	if _, err = os.Stat("/opt/celikpanel/bin/panel"); !os.IsNotExist(err) {
		t.Fatal("refusing to run on an installed panel")
	}
	if buildCommit != "64a0000000000000000000000000000000000000" {
		t.Fatal("fixture binary build identity is missing or differs")
	}
	if dataDir() != "/var/lib/celikpanel" || tlsDir() != panelManagedTLSDirectory || listenAddr() != ":2083" {
		t.Fatal("fixture must use the reviewed standard runtime paths")
	}
	if err = os.MkdirAll(dataDir(), 0750); err != nil {
		t.Fatal(err)
	}
	database, err := paneldb.NewSQLiteDB(databaseFile())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	raw, _, err := connectAgentPatiently(context.Background(), dialAgentOnce, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	client := transport.NewReconnectingClient(raw)
	box, err := secrets.LoadOrCreate(filepath.Join(dataDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	p := &Panel{db: database, agentClient: client, secrets: box, sessions: auth.NewSessionStore(database.GetDB()), users: repositories.NewPostgresUserRepository(database.GetDB()), secureCookies: true, loginLimiter: newRateLimiter(10, 5*time.Minute)}
	licenseState := os.Getenv("CELIKPANEL_SETUP_PROFILE_LICENSE")
	if licenseState == "" {
		licenseState = "active"
	}
	if licenseState != "active" && licenseState != "expired" {
		t.Fatal("invalid fixture license state")
	}
	p.license = serverSetupProfileFixtureLicense(t, licenseState)
	p.orchestrator = services.NewSiteOrchestrator(database.GetDB(), panelSiteAgentClient{panel: p}, buildCommit)
	if _, err = database.GetDB().Exec(`INSERT OR IGNORE INTO users(id,username,password_hash,email,role) VALUES(1,'profile-fixture','not-a-password','fixture@setup.test','admin')`); err != nil {
		t.Fatal(err)
	}
	token, err := p.sessions.Create(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// Private driver handoff, never included in evidence output or HTTP errors.
	if err = os.WriteFile("/var/lib/celikpanel-profile-vm/session", []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc(serverSetupPath, p.handleServerSetup)
	mux.HandleFunc(serverSetupPath+"/plan", p.handleServerSetupPlan)
	mux.HandleFunc(serverSetupPath+"/start", p.handleServerSetupStart)
	mux.HandleFunc(serverSetupPath+"/operation", p.handleServerSetupOperation)
	mux.HandleFunc(serverSetupPath+"/complete", p.handleServerSetupComplete)
	mux.HandleFunc(serverSetupPath+"/revise", p.handleServerSetupRevise)
	mux.HandleFunc("/api/v1/auth/me", p.handleMe)
	mux.HandleFunc("/api/v1/domains", p.handleDomains)
	mux.HandleFunc("/api/v1/domains/create", p.handleCreateDomain)
	mux.HandleFunc("/api/v1/domains/", p.handleDomainSubroute)
	mux.HandleFunc("/api/v1/service/operation", p.handleServiceOperation)
	gate := newPanelHTTPStartupGate(p.requireRemoteDNSMachineAuth(csrfProtect(p.requireAuth(mux))))
	server := newPanelHTTPServer(listenAddr(), securityHeaders(true, gate))
	on, cert, key, err := tlsSettings()
	if err != nil || !on {
		t.Fatalf("fixture TLS startup: %v", err)
	}
	running, err := startPanelHTTP(server, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.verifySecretKeyIdentity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = p.recoverInterruptedServiceOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Match production's startup barrier before independent certificate repair.
	p.serviceMutationMu.Lock()
	p.serviceMutationMu.Unlock()
	recoveryCtx, cancel := context.WithTimeout(context.Background(), panelMutationRecoveryTimeout)
	if err = p.requireStartupAgentMutationSlot(recoveryCtx); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	p.reconcileCertificateRuntimeAtStartup()
	gate.Open()
	p.resumeServerSetupExecutions()
	state, err := p.loadServerSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	readback, _ := json.Marshal(map[string]any{"profile": profile, "build_commit": strings.TrimSpace(buildCommit), "revision": state.Revision, "status": state.Status})
	t.Logf("disposable profile daemon ready %s", readback)
	if err = waitPanelHTTP(running); err != nil {
		t.Fatal(err)
	}
}
