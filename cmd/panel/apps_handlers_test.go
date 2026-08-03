package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	wordpressAdminID           = 7801
	wordpressSubscription      = 7802
	wordpressDomain            = 7803
	wordpressSite              = 7804
	wordpressDatabaseServer    = 7805
	panelWordPressOperationID  = "00112233445566778899aabbccddeeff"
	panelWordPressCleanupToken = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
)

type wordpressInstallAgent struct {
	mu sync.Mutex

	createRequests  []transport.CreateDatabaseRequest
	deleteRequests  []transport.DeleteDatabaseRequest
	installRequests []transport.InstallWordPressRequest

	createResponse  transport.CreateDatabaseResponse
	deleteResponse  transport.DeleteDatabaseResponse
	installResponse transport.InstallWordPressResponse
	createErr       error
	deleteErr       error
	installErr      error
	services        []core.Service
	servicesErr     error
}

func (a *wordpressInstallAgent) GetServices(
	_ *transport.Empty,
	resp *[]core.Service,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*resp = append([]core.Service(nil), a.services...)
	return a.servicesErr
}

func (a *wordpressInstallAgent) CreateDatabase(
	req transport.CreateDatabaseRequest,
	resp *transport.CreateDatabaseResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createRequests = append(a.createRequests, req)
	*resp = a.createResponse
	return a.createErr
}

func (a *wordpressInstallAgent) DeleteDatabase(
	req transport.DeleteDatabaseRequest,
	resp *transport.DeleteDatabaseResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleteRequests = append(a.deleteRequests, req)
	*resp = a.deleteResponse
	return a.deleteErr
}

func (a *wordpressInstallAgent) InstallWordPress(
	req *transport.InstallWordPressRequest,
	resp *transport.InstallWordPressResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installRequests = append(a.installRequests, *req)
	*resp = a.installResponse
	return a.installErr
}

func (a *wordpressInstallAgent) snapshot() (
	[]transport.CreateDatabaseRequest,
	[]transport.DeleteDatabaseRequest,
	[]transport.InstallWordPressRequest,
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]transport.CreateDatabaseRequest(nil), a.createRequests...),
		append([]transport.DeleteDatabaseRequest(nil), a.deleteRequests...),
		append([]transport.InstallWordPressRequest(nil), a.installRequests...)
}

type wordpressInstallFixture struct {
	panel *Panel
	box   *secrets.Box
}

func newWordPressInstallFixture(t *testing.T) wordpressInstallFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	docRoot, err := hostingpath.DocumentRoot(wordpressSubscription, wordpressDomain)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role)
		VALUES (7801, 'wordpress-admin', 'x', 'wordpress-admin@example.test', 'admin');
		INSERT INTO subscriptions (id, owner_id, name, max_databases, status)
		VALUES (7802, 7801, 'WordPress subscription', 10, 'active');
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (7803, 7802, 'wordpress.example', 'active');
		INSERT INTO sites (id, domain_id, document_root, project_type, status)
		VALUES (7804, 7803, ?, 'php', 'active');
		INSERT INTO database_servers
			(id, subscription_id, type_id, name, host, port, is_default, status)
		VALUES (7805, 7802, 2, 'local-mariadb', 'localhost', 3306, 1, 'active');
	`, docRoot)
	if err != nil {
		t.Fatal(err)
	}
	return wordpressInstallFixture{
		panel: &Panel{db: database, secrets: box},
		box:   box,
	}
}

func attachWordPressInstallAgent(t *testing.T, panel *Panel, agent any) {
	t.Helper()
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
}

func (f wordpressInstallFixture) install(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/7803/apps",
		strings.NewReader(`{"app":"wordpress"}`),
	)
	req = req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: wordpressAdminID, Role: roleAdmin},
	))
	recorder := httptest.NewRecorder()
	f.panel.handleAppInstall(recorder, req, wordpressDomain)
	return recorder
}

func (f wordpressInstallFixture) ledgerCounts(t *testing.T) (int, int, int) {
	t.Helper()
	var databases, users, grants int
	if err := f.panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM databases_v2`).Scan(&databases); err != nil {
		t.Fatal(err)
	}
	if err := f.panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM database_users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := f.panel.db.GetDB().QueryRow(`SELECT COUNT(*) FROM database_user_grants`).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	return databases, users, grants
}

func (f wordpressInstallFixture) latestOperation(t *testing.T) (string, string, string) {
	t.Helper()
	var operationID, status, sealedCleanupToken string
	if err := f.panel.db.GetDB().QueryRow(`
		SELECT operation_id, status, cleanup_token_encrypted
		FROM application_install_operations
		ORDER BY created_at DESC, rowid DESC
		LIMIT 1
	`).Scan(&operationID, &status, &sealedCleanupToken); err != nil {
		t.Fatal(err)
	}
	return operationID, status, sealedCleanupToken
}

func TestWordPressInstallPersistsEncryptedCompleteLedger(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse:  transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		deleteResponse:  transport.DeleteDatabaseResponse{Success: true},
		installResponse: transport.InstallWordPressResponse{Installed: true, Detail: "installed"},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	previousCommit := buildCommit
	buildCommit = "wordpress-paired-build"
	t.Cleanup(func() { buildCommit = previousCommit })

	recorder := fixture.install(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(createRequests) != 1 || len(deleteRequests) != 0 || len(installRequests) != 1 {
		t.Fatalf("unexpected RPC calls: create=%d delete=%d install=%d",
			len(createRequests), len(deleteRequests), len(installRequests))
	}
	install := installRequests[0]
	if install.ExpectedBuildCommit != "wordpress-paired-build" ||
		install.OperationID == "" || install.Domain != "wordpress.example" ||
		install.SiteID != wordpressSite ||
		install.SubscriptionID != wordpressSubscription ||
		install.DomainID != wordpressDomain {
		t.Fatalf("installer received the wrong immutable identity: %+v", install)
	}
	if install.DBName != createRequests[0].Name || install.DBUser != createRequests[0].User ||
		install.DBPass != createRequests[0].Password {
		t.Fatalf("database credentials drifted between create and install RPCs")
	}
	if install.DBPass == "" {
		t.Fatal("generated database password is empty")
	}
	if createRequests[0].OperationID != install.OperationID ||
		len(createRequests[0].CleanupToken) != 64 {
		t.Fatal("database creation and installer operation ownership drifted")
	}

	databases, users, grants := fixture.ledgerCounts(t)
	if databases != 1 || users != 1 || grants != 1 {
		t.Fatalf("incomplete ledger: databases=%d users=%d grants=%d", databases, users, grants)
	}
	var storedPassword string
	if err := fixture.panel.db.GetDB().QueryRow(`SELECT password FROM database_users`).Scan(&storedPassword); err != nil {
		t.Fatal(err)
	}
	if storedPassword == install.DBPass || !secrets.IsEncrypted(storedPassword) {
		t.Fatal("database password was not encrypted at rest")
	}
	plainPassword, err := fixture.box.Decrypt(storedPassword)
	if err != nil {
		t.Fatal(err)
	}
	if plainPassword != install.DBPass {
		t.Fatal("stored database password does not match the generated credential")
	}
	operationID, status, sealedCleanupToken := fixture.latestOperation(t)
	if operationID != install.OperationID || status != "applied" {
		t.Fatalf("operation=%s status=%s, want applied %s", operationID, status, install.OperationID)
	}
	if sealedCleanupToken == createRequests[0].CleanupToken ||
		!secrets.IsEncrypted(sealedCleanupToken) {
		t.Fatal("cleanup ownership token was not encrypted at rest")
	}
}

func TestWordPressInstallFailureCompensatesDatabaseAndLedger(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		deleteResponse: transport.DeleteDatabaseResponse{Success: true},
		installResponse: transport.InstallWordPressResponse{
			CompensationSafe: true,
			Error:            "injected installer failure",
		},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(installRequests) != 1 || len(deleteRequests) != 1 {
		t.Fatalf("expected install and compensation calls; delete=%d install=%d",
			len(deleteRequests), len(installRequests))
	}
	if !deleteRequests[0].RequireUserCleanup || !deleteRequests[0].RequireOwnershipProof ||
		deleteRequests[0].OperationID == "" || deleteRequests[0].CleanupToken == "" {
		t.Fatal("compensation did not carry complete operation ownership proof")
	}
	if deleteRequests[0].OperationID != createRequests[0].OperationID ||
		deleteRequests[0].CleanupToken != createRequests[0].CleanupToken {
		t.Fatal("compensation ownership proof drifted from the create operation")
	}
	databases, users, grants := fixture.ledgerCounts(t)
	if databases != 0 || users != 0 || grants != 0 {
		t.Fatalf("compensated install left ledger rows: databases=%d users=%d grants=%d",
			databases, users, grants)
	}
}

func TestWordPressLedgerFailureCompensatesBeforeInstall(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	if _, err := fixture.panel.db.GetDB().Exec(`
		CREATE TRIGGER fail_wordpress_database_user
		BEFORE INSERT ON database_users
		BEGIN
			SELECT RAISE(ABORT, 'injected database user ledger failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		deleteResponse: transport.DeleteDatabaseResponse{Success: true},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	_, deleteRequests, installRequests := agent.snapshot()
	if len(deleteRequests) != 0 || len(installRequests) != 0 {
		t.Fatalf("ledger failure calls: delete=%d install=%d", len(deleteRequests), len(installRequests))
	}
	databases, users, grants := fixture.ledgerCounts(t)
	if databases != 0 || users != 0 || grants != 0 {
		t.Fatalf("failed ledger transaction was not atomic: databases=%d users=%d grants=%d",
			databases, users, grants)
	}
}

func TestPersistAppInstallOutcomeReturnsLedgerFailureWithOriginalCause(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	sealedPassword, err := fixture.box.Encrypt("database-password")
	if err != nil {
		t.Fatal(err)
	}
	sealedToken, err := fixture.box.Encrypt(panelWordPressCleanupToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.panel.storeAppDatabase(
		context.Background(), wordpressDatabaseServer, wordpressSubscription,
		wordpressDomain, wordpressSite, panelWordPressOperationID,
		"wordpress_wp", "wordpress_wp", sealedPassword, sealedToken,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.panel.db.GetDB().Exec(`
		CREATE TRIGGER fail_wordpress_status_update
		BEFORE UPDATE OF status ON application_install_operations
		BEGIN
			SELECT RAISE(ABORT, 'injected operation status failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	originalCause := errors.New("agent response was lost")
	err = fixture.panel.persistAppInstallOutcome(
		panelWordPressOperationID, "needs_review", originalCause.Error(), originalCause,
	)
	if err == nil {
		t.Fatal("status persistence failure was ignored")
	}
	if !errors.Is(err, originalCause) {
		t.Fatalf("original operation failure was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "injected operation status failure") {
		t.Fatalf("status persistence failure was lost: %v", err)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "reserved" {
		t.Fatalf("failed status update changed operation to %q", status)
	}
}

func TestWordPressCleanupFailureKeepsLedgerVisible(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		deleteResponse: transport.DeleteDatabaseResponse{Error: "injected cleanup failure"},
		installResponse: transport.InstallWordPressResponse{
			CompensationSafe: true,
			Error:            "injected installer failure",
		},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	databases, users, grants := fixture.ledgerCounts(t)
	if databases != 1 || users != 1 || grants != 1 {
		t.Fatalf("failed physical cleanup was hidden from the administrator: databases=%d users=%d grants=%d",
			databases, users, grants)
	}
}

func TestWordPressInstallRejectsTamperedDocumentRootBeforeMutation(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	if _, err := fixture.panel.db.GetDB().Exec(
		`UPDATE sites SET document_root = '/tmp/tenant-controlled' WHERE id = ?`, wordpressSite,
	); err != nil {
		t.Fatal(err)
	}
	agent := &wordpressInstallAgent{}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(createRequests)+len(deleteRequests)+len(installRequests) != 0 {
		t.Fatalf("tampered document root reached agent: create=%d delete=%d install=%d",
			len(createRequests), len(deleteRequests), len(installRequests))
	}
	if databases, users, grants := fixture.ledgerCounts(t); databases+users+grants != 0 {
		t.Fatalf("tampered document root mutated ledger: %d/%d/%d", databases, users, grants)
	}
}

func TestWordPressInstallRejectsNonPHPSiteBeforeMutation(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	if _, err := fixture.panel.db.GetDB().Exec(
		`UPDATE sites SET project_type = 'static' WHERE id = ?`, wordpressSite,
	); err != nil {
		t.Fatal(err)
	}
	agent := &wordpressInstallAgent{}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(createRequests)+len(deleteRequests)+len(installRequests) != 0 {
		t.Fatal("non-PHP site reached the agent")
	}
}

func TestWordPressCreateTransportFailureNeverRunsDestructiveCleanup(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Error: "injected partial create failure"},
		deleteResponse: transport.DeleteDatabaseResponse{Success: true},
		createErr:      errors.New("injected transport failure"),
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(createRequests) != 1 || len(deleteRequests) != 0 || len(installRequests) != 0 {
		t.Fatalf("ambiguous create result triggered unsafe calls: create=%d delete=%d install=%d",
			len(createRequests), len(deleteRequests), len(installRequests))
	}
	if databases, users, grants := fixture.ledgerCounts(t); databases != 1 || users != 1 || grants != 1 {
		t.Fatalf("ambiguous resources were hidden: %d/%d/%d", databases, users, grants)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "needs_review" {
		t.Fatalf("operation status=%s, want needs_review", status)
	}
}

func TestWordPressExplicitCreateFailureUsesAgentSelfCleanupOnly(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Error: "exclusive create failed"},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, deleteRequests, installRequests := agent.snapshot()
	if len(createRequests) != 1 || len(deleteRequests) != 0 || len(installRequests) != 0 {
		t.Fatalf("explicit create failure calls=%d/%d/%d", len(createRequests), len(deleteRequests), len(installRequests))
	}
	if databases, users, grants := fixture.ledgerCounts(t); databases+users+grants != 0 {
		t.Fatalf("self-cleaned create left metadata: %d/%d/%d", databases, users, grants)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "failed" {
		t.Fatalf("status=%s, want failed", status)
	}
}

func TestWordPressIncompleteCreateCleanupRemainsVisible(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{
			CleanupIncomplete: true,
			Error:             "exclusive create failed",
		},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if databases, users, grants := fixture.ledgerCounts(t); databases != 1 || users != 1 || grants != 1 {
		t.Fatalf("uncertain resources were hidden: %d/%d/%d", databases, users, grants)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "needs_review" {
		t.Fatalf("status=%s, want needs_review", status)
	}
}

func TestWordPressInstallTransportFailureNeverDeletesDatabase(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse: transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		installErr:     errors.New("lost install response"),
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	_, deleteRequests, installRequests := agent.snapshot()
	if len(installRequests) != 1 || len(deleteRequests) != 0 {
		t.Fatalf("ambiguous install result triggered cleanup: delete=%d install=%d", len(deleteRequests), len(installRequests))
	}
	if databases, users, grants := fixture.ledgerCounts(t); databases != 1 || users != 1 || grants != 1 {
		t.Fatalf("ambiguous install ledger hidden: %d/%d/%d", databases, users, grants)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "needs_review" {
		t.Fatalf("status=%s, want needs_review", status)
	}
}

func TestWordPressUnsafeExplicitInstallFailureNeverDeletesDatabase(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	agent := &wordpressInstallAgent{
		createResponse:  transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		installResponse: transport.InstallWordPressResponse{Error: "publication state uncertain"},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)

	recorder := fixture.install(t)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	_, deleteRequests, _ := agent.snapshot()
	if len(deleteRequests) != 0 {
		t.Fatal("database cleanup ran without a compensation-safe installer result")
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "needs_review" {
		t.Fatalf("status=%s, want needs_review", status)
	}
}

func TestRecoverInterruptedWordPressInstallMarksNeedsReview(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	sealedPassword, err := fixture.box.Encrypt("database-password")
	if err != nil {
		t.Fatal(err)
	}
	sealedToken, err := fixture.box.Encrypt(panelWordPressCleanupToken)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := fixture.panel.storeAppDatabase(
		context.Background(), wordpressDatabaseServer, wordpressSubscription,
		wordpressDomain, wordpressSite, panelWordPressOperationID,
		"wordpress_wp", "wordpress_wp", sealedPassword, sealedToken,
	)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.OperationID == "" {
		t.Fatal("operation ledger was not created")
	}
	if err := fixture.panel.setAppInstallStatus(panelWordPressOperationID, "files_installing", ""); err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.panel.recoverInterruptedAppInstallOperations(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	_, status, _ := fixture.latestOperation(t)
	if status != "needs_review" {
		t.Fatalf("status=%s, want needs_review", status)
	}
}

func TestAppInstallMutexHandlesMinimumInt(t *testing.T) {
	minimumInt := -int(^uint(0)>>1) - 1
	if appInstallMutex(minimumInt) == nil {
		t.Fatal("minimum integer domain ID did not map to a mutex")
	}
}

func TestWordPressRejectsNonLocalInactiveOrWrongPortDatabaseServer(t *testing.T) {
	tests := []struct {
		name   string
		column string
		value  any
	}{
		{name: "remote host", column: "host", value: "db.example"},
		{name: "inactive", column: "status", value: "inactive"},
		{name: "wrong port", column: "port", value: 3307},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWordPressInstallFixture(t)
			query := "UPDATE database_servers SET " + test.column + " = ? WHERE id = ?"
			if _, err := fixture.panel.db.GetDB().Exec(query, test.value, wordpressDatabaseServer); err != nil {
				t.Fatal(err)
			}
			agent := &wordpressInstallAgent{}
			attachWordPressInstallAgent(t, fixture.panel, agent)
			recorder := fixture.install(t)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			createRequests, deleteRequests, installRequests := agent.snapshot()
			if len(createRequests)+len(deleteRequests)+len(installRequests) != 0 {
				t.Fatal("invalid database server reached privileged RPC")
			}
		})
	}
}

func TestWordPressFreshSubscriptionDiscoversLocalMariaDB(t *testing.T) {
	fixture := newWordPressInstallFixture(t)
	if _, err := fixture.panel.db.GetDB().Exec(
		"DELETE FROM database_servers WHERE subscription_id = ?", wordpressSubscription,
	); err != nil {
		t.Fatal(err)
	}
	agent := &wordpressInstallAgent{
		services:        []core.Service{{Name: "mariadb", Version: "11.4", Status: "running"}},
		createResponse:  transport.CreateDatabaseResponse{Success: true, OwnedByOperation: true},
		installResponse: transport.InstallWordPressResponse{Installed: true},
	}
	attachWordPressInstallAgent(t, fixture.panel, agent)
	recorder := fixture.install(t)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	createRequests, _, installRequests := agent.snapshot()
	if len(createRequests) != 1 || len(installRequests) != 1 {
		t.Fatalf("fresh subscription did not reach install: create=%d install=%d", len(createRequests), len(installRequests))
	}
}
