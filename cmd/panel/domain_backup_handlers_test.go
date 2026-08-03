package main

import (
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type BackupTestRequest struct {
	SubscriptionID int
	DomainID       int
	Type           string
	DatabaseID     int
	DatabaseName   string
	DatabaseType   string

	// Legacy fields are present only in the test receiver. Gob leaves them
	// empty when the panel uses the hardened, path-free request contract.
	DomainName string
	SourceDir  string
	TargetDir  string
	BackupName string
}

type BackupTestInfo struct {
	Name       string
	Size       int64
	Type       string
	DatabaseID int
	CreatedAt  time.Time
}

type BackupTestResponse struct {
	Success bool
	Backup  BackupTestInfo
	Error   string
}

type BackupTestListResponse struct {
	Backups []BackupTestInfo
}

type BackupTestReadResponse struct {
	Path     string
	Content  string
	Size     int64
	IsBinary bool
}

type backupTestAgent struct {
	mu           sync.Mutex
	createCalls  []BackupTestRequest
	restoreCalls []BackupTestRequest
	deleteCalls  []BackupTestRequest
	listCalls    []BackupTestRequest
	readCalls    []BackupTestRequest
}

func (a *backupTestAgent) CreateBackup(req *BackupTestRequest, resp *BackupTestResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createCalls = append(a.createCalls, *req)
	resp.Success = true
	resp.Backup = BackupTestInfo{
		Name:       "files_20260727_120000.tar.gz",
		Size:       5,
		Type:       req.Type,
		DatabaseID: req.DatabaseID,
		CreatedAt:  time.Unix(1, 0),
	}
	if req.Type == "database" {
		resp.Backup.Name = "db_" + req.DatabaseType + "_" +
			strconv.Itoa(req.DatabaseID) + "_20260727_120000.sql.gz"
	}
	return nil
}

func (a *backupTestAgent) RestoreBackup(req *BackupTestRequest, resp *BackupTestResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restoreCalls = append(a.restoreCalls, *req)
	resp.Success = true
	return nil
}

func (a *backupTestAgent) DeleteBackup(req *BackupTestRequest, resp *bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleteCalls = append(a.deleteCalls, *req)
	*resp = true
	return nil
}

func (a *backupTestAgent) ListBackups(req *BackupTestRequest, resp *BackupTestListResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listCalls = append(a.listCalls, *req)
	resp.Backups = []BackupTestInfo{{
		Name:      "files_20260727_120000.tar.gz",
		Size:      5,
		Type:      "files",
		CreatedAt: time.Unix(1, 0),
	}}
	return nil
}

func (a *backupTestAgent) ReadBackup(req *BackupTestRequest, resp *BackupTestReadResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readCalls = append(a.readCalls, *req)
	resp.Path = req.BackupName
	resp.Content = base64.StdEncoding.EncodeToString([]byte("hello"))
	resp.Size = 5
	resp.IsBinary = true
	return nil
}

func attachBackupTestAgent(t *testing.T, panel *Panel, agent *backupTestAgent) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register backup test agent: %v", err)
	}
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	panel.agentClient = transport.NewReconnectingClient(client)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})
}

func addBackupTestDatabase(
	t *testing.T,
	panel *Panel,
	subscriptionID, domainID int,
	name, serverType string,
) int {
	t.Helper()
	db := panel.db.GetDB()
	var typeID int
	if err := db.QueryRow(
		`SELECT id FROM database_server_types WHERE name = ?`, serverType,
	).Scan(&typeID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`
		INSERT INTO database_servers
		       (subscription_id, type_id, name, version, host, port, is_default, status)
		VALUES (?, ?, ?, 'test', 'localhost', ?, 1, 'active')`,
		subscriptionID, typeID, name+"-server", 20000+domainID,
	)
	if err != nil {
		t.Fatal(err)
	}
	serverID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = db.Exec(`
		INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name)
		VALUES (?, ?, ?, ?)`,
		serverID, subscriptionID, domainID, name,
	)
	if err != nil {
		t.Fatal(err)
	}
	databaseID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(databaseID)
}

func TestCreateFilesBackupSendsOnlyImmutableTenantIdentity(t *testing.T) {
	panel, subscriptionID, domainID := newFileManagerPanelFixture(t)
	agent := &backupTestAgent{}
	attachBackupTestAgent(t, panel, agent)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups",
		strings.NewReader(`{"type":"files"}`),
	)
	recorder := httptest.NewRecorder()
	panel.handleDomainBackups(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.createCalls) != 1 {
		t.Fatalf("create calls = %+v", agent.createCalls)
	}
	call := agent.createCalls[0]
	if call.SubscriptionID != subscriptionID || call.DomainID != domainID ||
		call.Type != "files" {
		t.Fatalf("unexpected create request: %+v", call)
	}
	if call.SourceDir != "" || call.TargetDir != "" || call.DomainName != "" {
		t.Fatalf("legacy path/name fields reached root agent: %+v", call)
	}
	if strings.Contains(recorder.Body.String(), "/etc") {
		t.Fatalf("poisoned document_root leaked through backup API: %s", recorder.Body.String())
	}
}

func TestDatabaseBackupUsesAuthenticatedDatabaseIDNotUserName(t *testing.T) {
	panel, subscriptionID, domainID := newFileManagerPanelFixture(t)
	databaseID := addBackupTestDatabase(
		t, panel, subscriptionID, domainID, "files_example_test_app", "mariadb",
	)
	agent := &backupTestAgent{}
	attachBackupTestAgent(t, panel, agent)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups",
		strings.NewReader(`{"type":"database","database_id":`+strconv.Itoa(databaseID)+`}`),
	)
	recorder := httptest.NewRecorder()
	panel.handleDomainBackups(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.createCalls) != 1 {
		t.Fatalf("create calls = %+v", agent.createCalls)
	}
	call := agent.createCalls[0]
	if call.DatabaseID != databaseID ||
		call.DatabaseName != "files_example_test_app" ||
		call.DatabaseType != "mysql" {
		t.Fatalf("database identity was not resolved from metadata: %+v", call)
	}
}

func TestDatabaseBackupRejectsLegacyNameAndCrossDomainIDBeforeRPC(t *testing.T) {
	panel, subscriptionID, domainID := newFileManagerPanelFixture(t)
	agent := &backupTestAgent{}
	attachBackupTestAgent(t, panel, agent)

	legacyRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups",
		strings.NewReader(`{
			"type":"database",
			"database_name":"victim; touch /tmp/pwned",
			"database_type":"mysql"
		}`),
	)
	legacyRecorder := httptest.NewRecorder()
	panel.handleDomainBackups(legacyRecorder, legacyRequest)
	if legacyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("legacy status=%d body=%s", legacyRecorder.Code, legacyRecorder.Body.String())
	}

	otherDomainResult, err := panel.db.GetDB().Exec(
		`INSERT INTO domains (subscription_id, name) VALUES (?, 'other.example.test')`,
		subscriptionID,
	)
	if err != nil {
		t.Fatal(err)
	}
	otherDomainID64, err := otherDomainResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	otherDatabaseID := addBackupTestDatabase(
		t, panel, subscriptionID, int(otherDomainID64), "other_example_test_app", "mariadb",
	)
	crossRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups",
		strings.NewReader(`{"type":"database","database_id":`+strconv.Itoa(otherDatabaseID)+`}`),
	)
	crossRecorder := httptest.NewRecorder()
	panel.handleDomainBackups(crossRecorder, crossRequest)
	if crossRecorder.Code != http.StatusNotFound {
		t.Fatalf("cross-domain status=%d body=%s", crossRecorder.Code, crossRecorder.Body.String())
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.createCalls) != 0 {
		t.Fatalf("invalid database requests reached root agent: %+v", agent.createCalls)
	}
}

func TestDatabaseRestoreReauthenticatesBackupDatabaseIdentity(t *testing.T) {
	panel, subscriptionID, domainID := newFileManagerPanelFixture(t)
	databaseID := addBackupTestDatabase(
		t, panel, subscriptionID, domainID, "files_example_test_pg", "postgresql",
	)
	agent := &backupTestAgent{}
	attachBackupTestAgent(t, panel, agent)
	name := "db_postgresql_" + strconv.Itoa(databaseID) + "_20260727_120000.sql.gz"

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups/restore",
		strings.NewReader(`{"backup_name":"`+name+`"}`),
	)
	recorder := httptest.NewRecorder()
	panel.handleRestoreBackup(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.restoreCalls) != 1 {
		t.Fatalf("restore calls = %+v", agent.restoreCalls)
	}
	call := agent.restoreCalls[0]
	if call.DatabaseID != databaseID ||
		call.DatabaseName != "files_example_test_pg" ||
		call.DatabaseType != "postgresql" {
		t.Fatalf("restore database identity was not authenticated: %+v", call)
	}
	if call.TargetDir != "" || call.DomainName != "" {
		t.Fatalf("legacy restore path/name reached root agent: %+v", call)
	}
}

func TestBackupTraversalNamesAreRejectedBeforeRPC(t *testing.T) {
	panel, _, domainID := newFileManagerPanelFixture(t)
	agent := &backupTestAgent{}
	attachBackupTestAgent(t, panel, agent)

	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/backups?name=../../etc/shadow",
		nil,
	)
	recorder := httptest.NewRecorder()
	panel.handleDomainBackups(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.deleteCalls) != 0 {
		t.Fatalf("traversal delete reached root agent: %+v", agent.deleteCalls)
	}
}
