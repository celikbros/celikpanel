package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	adminAccountUserID         = 9821
	adminAccountSubscriptionID = 9822
)

// databaseAdminAccountAgent answers the two calls this feature makes and
// records what it was asked, so a test can assert on the request rather than
// on a host nobody has.
// databaseAdminAccountAgent, bu ozelligin yaptigi iki cagriya yanit verir.
type databaseAdminAccountAgent struct {
	services    []core.Service
	provisioned []transport.ProvisionDatabaseAdminAccountRequest
	refuse      string
	transport   error
	removed     []string
}

func (a *databaseAdminAccountAgent) GetServices(_ *transport.Empty, reply *[]core.Service) error {
	*reply = append([]core.Service(nil), a.services...)
	return nil
}

func (a *databaseAdminAccountAgent) ProvisionDatabaseAdminAccount(
	req *transport.ProvisionDatabaseAdminAccountRequest,
	resp *transport.ProvisionDatabaseAdminAccountResponse,
) error {
	a.provisioned = append(a.provisioned, *req)
	if a.transport != nil {
		return a.transport
	}
	resp.Username = transport.DatabaseAdminAccountName
	if a.refuse != "" {
		resp.Error = a.refuse
		return nil
	}
	resp.Provisioned = true
	return nil
}

func (a *databaseAdminAccountAgent) RemoveDatabaseAdminAccount(
	req *transport.RemoveDatabaseAdminAccountRequest,
	resp *transport.RemoveDatabaseAdminAccountResponse,
) error {
	a.removed = append(a.removed, req.Engine)
	resp.Removed = true
	return nil
}

func newDatabaseAdminAccountFixture(
	t *testing.T, agent *databaseAdminAccountAgent,
) (*Panel, *paneldb.SQLiteDB) {
	t.Helper()
	directory := t.TempDir()
	database, err := paneldb.NewSQLiteDB(filepath.Join(directory, "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES (?, 'admin-account-owner', 'x', 'admin-account@example.test', 'admin', 'active')
	`, adminAccountUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES (?, ?, 'Admin account subscription', 'active')
	`, adminAccountSubscriptionID, adminAccountUserID); err != nil {
		t.Fatal(err)
	}
	box, err := secrets.LoadOrCreate(filepath.Join(directory, "panel.key"))
	if err != nil {
		t.Fatal(err)
	}

	panel := &Panel{db: database, secrets: box}
	// Opening an account changes a host, and the RPC policy will not let a
	// host-mutating call go out until the panel knows exactly what machine it
	// is talking to. A test that skipped this would be testing a call the
	// product would refuse to make.
	// Hesap acmak bir makineyi degistirir ve RPC politikasi, panel hangi
	// makineyle konustugunu tam olarak bilene kadar boyle bir cagriya izin
	// vermez.
	panel.pkgFamilyMu.Lock()
	panel.pkgFamilyVal = "apt"
	panel.hostPlatformVal = debianPolicyTestIdentity()
	panel.hostPlatformKnown = true
	panel.pkgFamilyMu.Unlock()

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
	return panel, database
}

func adminAccountServices() []core.Service {
	return []core.Service{
		{Name: "mariadb.service", Version: "11.4", Status: "running"},
	}
}

func adminAccountRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	return request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: adminAccountUserID, Role: roleAdmin},
	))
}

func onlyDatabaseServer(t *testing.T, panel *Panel) *core.DatabaseServer {
	t.Helper()
	repository := repositories.NewPostgresDatabaseServerRepository(panel.db.GetDB())
	servers, err := repository.ListByType(
		context.Background(), adminAccountSubscriptionID, "mariadb")
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected one registered MariaDB server, got %d", len(servers))
	}
	return servers[0]
}

// R-057. The row autodiscovery inserts is the panel claiming it can manage
// that engine, and until this change the claim was false: the row was born
// with no credential and a packaged engine refuses the empty one. The claim
// and the account are now made together.
//
// R-057. Autodiscovery'nin ekledigi satir, panelin o motoru yonetebilecegi
// iddiasidir; bu degisiklige kadar iddia yanlisti.
func TestAutodiscoveryOpensThePanelsAccountOnAnEngineItJustClaimed(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, _ := newDatabaseAdminAccountFixture(t, agent)

	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}

	if len(agent.provisioned) != 1 {
		t.Fatalf("the engine was asked %d times, want once: %+v", len(agent.provisioned), agent.provisioned)
	}
	if agent.provisioned[0].Engine != "mariadb" {
		t.Fatalf("provisioned engine %q, want mariadb", agent.provisioned[0].Engine)
	}
	if strings.TrimSpace(agent.provisioned[0].Password) == "" {
		t.Fatal("the panel asked for an account with no password")
	}

	server := onlyDatabaseServer(t, panel)
	if server.AdminUsername != transport.DatabaseAdminAccountName {
		t.Fatalf("recorded account %q, want %q",
			server.AdminUsername, transport.DatabaseAdminAccountName)
	}
	// Sealed at rest, and the seal is the panel's own.
	// Beklerken muhurlu ve muhur panelin kendisinin.
	if !secrets.IsEncrypted(server.AdminPasswordEncrypted) {
		t.Fatalf("the credential is not sealed: %q", server.AdminPasswordEncrypted)
	}
	opened, err := panel.secrets.Decrypt(server.AdminPasswordEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if opened != agent.provisioned[0].Password {
		t.Fatal("the sealed credential is not the one the engine was given")
	}
}

// Once, and only for a row that did not exist a moment ago. Asking again on
// every listing would mean an engine whose local door has been closed on
// purpose being asked forever.
// Bir kez, ve yalnizca bir an once var olmayan bir satir icin.
func TestAutodiscoveryDoesNotAskAgainForAnEngineItAlreadyClaimed(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, _ := newDatabaseAdminAccountFixture(t, agent)

	for attempt := 0; attempt < 3; attempt++ {
		if err := panel.ensureInstalledDBServers(
			context.Background(), adminAccountSubscriptionID); err != nil {
			t.Fatalf("attempt %d: %v", attempt+1, err)
		}
	}
	if len(agent.provisioned) != 1 {
		t.Fatalf("the engine was asked %d times across three listings, want once", len(agent.provisioned))
	}
}

// An engine that will not give the panel an account is a card that says so,
// not a page that will not render. The row is still correct and the engine is
// still installed.
// Panele hesap vermeyen bir motor, acilmayan bir sayfa degil bunu soyleyen bir
// kart demektir.
func TestAutodiscoverySurvivesAnEngineThatRefusesTheAccount(t *testing.T) {
	agent := &databaseAdminAccountAgent{
		services: adminAccountServices(),
		refuse:   "this engine would not open an account",
	}
	panel, database := newDatabaseAdminAccountFixture(t, agent)

	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatalf("a refused account failed autodiscovery: %v", err)
	}
	var count int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM database_servers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("registered %d servers, want 1", count)
	}
	if server := onlyDatabaseServer(t, panel); server.AdminUsername != "" {
		t.Fatalf("a refused account was recorded as %q", server.AdminUsername)
	}
}

// A transport failure is the same story: the engine is installed, the row is
// right, and nothing false was written down.
// Bir tasima hatasi da ayni hikayedir.
func TestAutodiscoverySurvivesAnAgentThatCannotBeReached(t *testing.T) {
	agent := &databaseAdminAccountAgent{
		services:  adminAccountServices(),
		transport: errors.New("the agent is not answering"),
	}
	panel, _ := newDatabaseAdminAccountFixture(t, agent)

	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatalf("an unreachable agent failed autodiscovery: %v", err)
	}
	if server := onlyDatabaseServer(t, panel); server.AdminUsername != "" {
		t.Fatalf("an account nobody opened was recorded as %q", server.AdminUsername)
	}
}

// The operator is allowed to read the password the panel holds - it is their
// machine - and reading it leaves a record, because a credential that can be
// read without a trace is one nobody can account for.
//
// Operator, panelin tuttugu parolayi okuyabilir - makine onundur - ve okumak
// kayit birakir.
func TestAnAdministratorCanReadTheCredentialAndTheReadingIsRecorded(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, database := newDatabaseAdminAccountFixture(t, agent)
	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	server := onlyDatabaseServer(t, panel)

	request := adminAccountRequest(http.MethodGet,
		"/api/v1/database-servers/"+strconv.Itoa(server.ID)+"/admin-account")
	response := httptest.NewRecorder()
	panel.handleRevealDatabaseAdminAccountPassword(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Username != transport.DatabaseAdminAccountName {
		t.Fatalf("revealed account %q", body.Username)
	}
	if body.Password != agent.provisioned[0].Password {
		t.Fatal("the revealed password is not the one the engine was given")
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	var reads int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'database.admin_account.reveal%'`,
	).Scan(&reads); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatalf("the reading left %d audit entries, want 1", reads)
	}
}

// Not everybody who can see the databases page may read the credential.
// Veritabanlari sayfasini gorebilen herkes kimlik bilgisini okuyamaz.
func TestReadingTheCredentialIsAdministratorWork(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, database := newDatabaseAdminAccountFixture(t, agent)
	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	server := onlyDatabaseServer(t, panel)

	for _, caller := range []*Caller{
		nil,
		{ID: adminAccountUserID, Role: "user"},
		{ID: adminAccountUserID, Role: "reseller"},
	} {
		request := httptest.NewRequest(http.MethodGet,
			"/api/v1/database-servers/"+strconv.Itoa(server.ID)+"/admin-account", nil)
		if caller != nil {
			request = request.WithContext(
				context.WithValue(request.Context(), callerKey, caller))
		}
		response := httptest.NewRecorder()
		panel.handleRevealDatabaseAdminAccountPassword(response, request)

		if response.Code != http.StatusForbidden {
			t.Fatalf("caller %+v got status %d, want 403", caller, response.Code)
		}
		if strings.Contains(response.Body.String(), agent.provisioned[0].Password) {
			t.Fatalf("a refused caller was shown the password: %q", response.Body.String())
		}
	}

	var reads int
	if err := database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action LIKE 'database.admin_account.reveal%'`,
	).Scan(&reads); err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatalf("a refused read left %d audit entries", reads)
	}
}

// Asking again gives the account a new password and records the new one, so
// the panel and the engine cannot come to disagree about what it is.
// Yeniden istemek hesaba yeni bir parola verir ve yenisini kaydeder.
func TestAskingAgainRotatesTheCredentialOnBothSides(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, _ := newDatabaseAdminAccountFixture(t, agent)
	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	first := onlyDatabaseServer(t, panel)

	request := adminAccountRequest(http.MethodPost,
		"/api/v1/database-servers/"+strconv.Itoa(first.ID)+"/admin-account")
	response := httptest.NewRecorder()
	panel.handleProvisionDatabaseAdminAccount(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	// The password is not handed back by the endpoint that sets it; reading it
	// is a separate, recorded act.
	// Parolayi belirleyen uc nokta onu geri vermez.
	if strings.Contains(response.Body.String(), agent.provisioned[1].Password) {
		t.Fatalf("the rotating response carried the password: %q", response.Body.String())
	}

	if len(agent.provisioned) != 2 {
		t.Fatalf("the engine was asked %d times, want twice", len(agent.provisioned))
	}
	if agent.provisioned[0].Password == agent.provisioned[1].Password {
		t.Fatal("rotating produced the same password twice")
	}
	second := onlyDatabaseServer(t, panel)
	opened, err := panel.secrets.Decrypt(second.AdminPasswordEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if opened != agent.provisioned[1].Password {
		t.Fatal("the panel kept the old password after rotating")
	}
}

// An engine on another machine has no local privileged door for this panel to
// go through, and the refusal says so instead of trying.
// Baska bir makinedeki motorun bu panelin gecebilecegi yerel bir kapisi yoktur.
func TestThePanelWillNotOpenAnAccountOnAnotherMachine(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, database := newDatabaseAdminAccountFixture(t, agent)
	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	server := onlyDatabaseServer(t, panel)
	if _, err := database.GetDB().Exec(
		`UPDATE database_servers SET host = 'db.elsewhere.example' WHERE id = ?`,
		server.ID); err != nil {
		t.Fatal(err)
	}
	asked := len(agent.provisioned)

	request := adminAccountRequest(http.MethodPost,
		"/api/v1/database-servers/"+strconv.Itoa(server.ID)+"/admin-account")
	response := httptest.NewRecorder()
	panel.handleProvisionDatabaseAdminAccount(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q, want 409", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "username and password") {
		t.Fatalf("the refusal does not offer the other way in: %q", response.Body.String())
	}
	if len(agent.provisioned) != asked {
		t.Fatal("a remote engine was asked for an account anyway")
	}
}

// Removing the account forgets the credential too. A panel that kept a
// password for an account it had been told to give up would be keeping a
// secret for nothing.
// Hesabi kaldirmak kimlik bilgisini de unutur.
func TestRemovingTheAccountForgetsTheCredential(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, _ := newDatabaseAdminAccountFixture(t, agent)
	if err := panel.ensureInstalledDBServers(
		context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	server := onlyDatabaseServer(t, panel)

	request := adminAccountRequest(http.MethodDelete,
		"/api/v1/database-servers/"+strconv.Itoa(server.ID)+"/admin-account")
	response := httptest.NewRecorder()
	panel.handleRemoveDatabaseAdminAccount(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if len(agent.removed) != 1 || agent.removed[0] != "mariadb" {
		t.Fatalf("removed %v, want one mariadb removal", agent.removed)
	}
	after := onlyDatabaseServer(t, panel)
	if after.AdminUsername != "" || after.AdminPasswordEncrypted != "" {
		t.Fatalf("the credential survived removal: %+v", after)
	}
}

// R-067. Found on a real machine, not in a test: install CelikPanel on a clean
// server, install MariaDB through CelikPanel, open Databases - and the page
// said "no database engine installed" about an engine the panel had just
// installed and could see running.
//
// Nothing was broken. There was simply nowhere to put it: a fresh install has
// no subscriptions at all, the scope for every database operation comes from
// the caller's subscription, and the only thing that made one was adding a
// domain - which on a fresh server needs DNS set up first. Three steps nobody
// would guess, in front of a page that explained none of them.
//
// R-067. Bir testte degil gercek bir makinede bulundu: temiz bir sunucuya
// CelikPanel kur, CelikPanel'den MariaDB kur, Veritabanlari'ni ac - ve sayfa,
// panelin az once kurdugu ve calistigini gordugu bir motor hakkinda "veritabani
// motoru kurulu degil" diyordu.
func TestAFreshAdministratorSeesTheEngineTheyJustInstalled(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, database := newDatabaseAdminAccountFixture(t, agent)

	// The state a fresh install is actually in: an administrator, and not one
	// subscription anywhere. Migration 006 drops the placeholder admin and its
	// seed subscription, so this is not a contrived fixture - it is the shape
	// of every new server.
	// Taze bir kurulumun gercekten icinde oldugu durum.
	if _, err := database.GetDB().Exec(`DELETE FROM subscriptions`); err != nil {
		t.Fatal(err)
	}

	request := adminAccountRequest(http.MethodGet, "/api/v1/database-servers")
	response := httptest.NewRecorder()
	panel.handleListDatabaseServers(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var servers []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	if len(servers) == 0 {
		t.Fatal("a fresh administrator was shown no database engine at all, " +
			"while the agent reports one installed and running")
	}
	if got := servers[0]["admin_username"]; got != transport.DatabaseAdminAccountName {
		t.Fatalf("the engine was listed but the panel opened no account on it: %v", got)
	}

	// The subscription was made once, and asking again does not make another.
	// Abonelik bir kez olusturuldu; yeniden sormak bir tane daha olusturmaz.
	second := httptest.NewRecorder()
	panel.handleListDatabaseServers(second, adminAccountRequest(http.MethodGet, "/api/v1/database-servers"))
	if second.Code != http.StatusOK {
		t.Fatalf("second listing status=%d", second.Code)
	}
	var count int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM subscriptions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the administrator owns %d subscriptions, want exactly 1", count)
	}
}

// A customer with no subscription genuinely has nothing, and an empty list is
// the truth for them. The bootstrap is an administrator's, not everybody's.
// Aboneligi olmayan bir musterinin gercekten hicbir seyi yoktur.
func TestACustomerWithNoSubscriptionIsNotGivenOne(t *testing.T) {
	agent := &databaseAdminAccountAgent{services: adminAccountServices()}
	panel, database := newDatabaseAdminAccountFixture(t, agent)
	if _, err := database.GetDB().Exec(`DELETE FROM subscriptions`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/database-servers", nil)
	request = request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: adminAccountUserID, Role: "user"}))
	response := httptest.NewRecorder()
	panel.handleListDatabaseServers(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var count int
	if err := database.GetDB().QueryRow(`SELECT COUNT(*) FROM subscriptions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a customer's listing created %d subscriptions", count)
	}
}
