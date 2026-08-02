package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type panelBackupV2Agent struct {
	mu                               sync.Mutex
	createReqs                       []backupspec.CreateRequest
	restoreReqs                      []backupspec.RestoreRequest
	deleteReqs                       []backupspec.DeleteRequest
	chunkReqs                        []backupspec.ReadChunkRequest
	listResp                         backupspec.ListResponse
	inspectResp                      backupspec.InspectResponse
	content                          []byte
	createOK, restoreOK, deleteOK    bool
	createErr, restoreErr, deleteErr error
}

func (a *panelBackupV2Agent) CreateBackup(req *backupspec.CreateRequest, resp *backupspec.CreateResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copyReq := *req
	copyReq.Databases = append([]backupspec.DatabaseIdentity(nil), req.Databases...)
	a.createReqs = append(a.createReqs, copyReq)
	if a.createErr != nil {
		return a.createErr
	}
	resp.Success = a.createOK
	resp.Backup = backupspec.Info{Name: "created.tar.gz", Type: req.Type, Origin: req.Origin, Restorable: true}
	if !a.createOK {
		resp.Error = "create refused"
	}
	return nil
}

func (a *panelBackupV2Agent) ListBackups(_ *backupspec.ListRequest, resp *backupspec.ListResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*resp = a.listResp
	resp.Backups = append([]backupspec.Info(nil), a.listResp.Backups...)
	return nil
}

func (a *panelBackupV2Agent) InspectBackup(_ *backupspec.InspectRequest, resp *backupspec.InspectResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*resp = a.inspectResp
	resp.Databases = append([]backupspec.DatabaseIdentity(nil), a.inspectResp.Databases...)
	return nil
}

func (a *panelBackupV2Agent) RestoreBackup(req *backupspec.RestoreRequest, resp *backupspec.RestoreResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copyReq := *req
	copyReq.Databases = append([]backupspec.DatabaseIdentity(nil), req.Databases...)
	a.restoreReqs = append(a.restoreReqs, copyReq)
	if a.restoreErr != nil {
		return a.restoreErr
	}
	resp.Success = a.restoreOK
	if !a.restoreOK {
		resp.Error = "restore refused"
	}
	return nil
}

func (a *panelBackupV2Agent) DeleteBackup(req *backupspec.DeleteRequest, ok *bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleteReqs = append(a.deleteReqs, *req)
	if a.deleteErr != nil {
		return a.deleteErr
	}
	*ok = a.deleteOK
	return nil
}

func (a *panelBackupV2Agent) ReadBackupChunk(req *backupspec.ReadChunkRequest, resp *backupspec.ReadChunkResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chunkReqs = append(a.chunkReqs, *req)
	if req.Offset < 0 || req.Offset > int64(len(a.content)) {
		return errors.New("invalid offset")
	}
	end := req.Offset + int64(req.MaxBytes)
	if end > int64(len(a.content)) {
		end = int64(len(a.content))
	}
	resp.Data = append([]byte(nil), a.content[req.Offset:end]...)
	resp.Offset, resp.Size = end, int64(len(a.content))
	resp.EOF = end == int64(len(a.content))
	return nil
}

func attachPanelBackupV2Agent(t *testing.T, p *Panel, agent *panelBackupV2Agent) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register backup agent: %v", err)
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
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

type panelBackupFixture struct {
	panel                 *Panel
	agent                 *panelBackupV2Agent
	domainID, otherDomain int
	mariaID, postgresID   int
	crossDomainDBID       int
}

func newPanelBackupFixture(t *testing.T) panelBackupFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(database.Close)
	db := database.GetDB()
	mustID := func(query string, args ...any) int {
		result, err := db.Exec(query, args...)
		if err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return int(id)
	}
	userID := mustID(`INSERT INTO users (username,password_hash,email,role) VALUES ('backup-owner','x','backup@example.test','customer')`)
	subID := mustID(`INSERT INTO subscriptions (owner_id,name) VALUES (?, 'backup-sub')`, userID)
	domainID := mustID(`INSERT INTO domains (subscription_id,name) VALUES (?, 'one.example')`, subID)
	otherDomain := mustID(`INSERT INTO domains (subscription_id,name) VALUES (?, 'two.example')`, subID)
	if _, err := db.Exec(`INSERT INTO sites (domain_id,document_root) VALUES (?, '/srv/one.example')`, domainID); err != nil {
		t.Fatal(err)
	}
	var mariaTypeID, postgresTypeID int
	if err := db.QueryRow(`SELECT id FROM database_server_types WHERE name='mariadb'`).Scan(&mariaTypeID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM database_server_types WHERE name='postgresql'`).Scan(&postgresTypeID); err != nil {
		t.Fatal(err)
	}
	mariaServer := mustID(`INSERT INTO database_servers (subscription_id,type_id,name,host,port) VALUES (?,?,'maria','localhost',3306)`, subID, mariaTypeID)
	postgresServer := mustID(`INSERT INTO database_servers (subscription_id,type_id,name,host,port) VALUES (?,?,'postgres','localhost',5432)`, subID, postgresTypeID)
	mariaID := mustID(`INSERT INTO databases_v2 (server_id,subscription_id,domain_id,name) VALUES (?,?,?,'orders_archive_2026')`, mariaServer, subID, domainID)
	postgresID := mustID(`INSERT INTO databases_v2 (server_id,subscription_id,domain_id,name) VALUES (?,?,?,'analytics_store_v2')`, postgresServer, subID, domainID)
	crossID := mustID(`INSERT INTO databases_v2 (server_id,subscription_id,domain_id,name) VALUES (?,?,?,'other_domain_db')`, mariaServer, subID, otherDomain)

	panel := &Panel{db: database}
	agent := &panelBackupV2Agent{createOK: true, restoreOK: true, deleteOK: true}
	attachPanelBackupV2Agent(t, panel, agent)
	return panelBackupFixture{
		panel: panel, agent: agent, domainID: domainID, otherDomain: otherDomain,
		mariaID: mariaID, postgresID: postgresID, crossDomainDBID: crossID,
	}
}

func backupActions(t *testing.T, p *Panel) []string {
	t.Helper()
	rows, err := p.db.GetDB().Query(`SELECT action FROM audit_logs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	return actions
}

func hasBackupAction(actions []string, prefix string) bool {
	for _, action := range actions {
		if strings.HasPrefix(action, prefix) {
			return true
		}
	}
	return false
}

func TestPanelBackupV2RejectsCrossDomainDatabaseAndAudits(t *testing.T) {
	f := newPanelBackupFixture(t)
	body := fmt.Sprintf(`{"type":"database","database_id":%d}`, f.crossDomainDBID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups", f.domainID), strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.panel.handleDomainBackups(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(f.agent.createReqs) != 0 {
		t.Fatalf("agent received cross-domain create: %+v", f.agent.createReqs)
	}
	if !hasBackupAction(backupActions(t, f.panel), "backup.create.failed:database") {
		t.Fatal("missing failed create audit")
	}
}

func TestPanelBackupV2DatabaseIDPreservesUnderscoreIdentity(t *testing.T) {
	f := newPanelBackupFixture(t)
	body := fmt.Sprintf(`{"type":"database","database_id":%d}`, f.mariaID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups", f.domainID), strings.NewReader(body))
	rec := httptest.NewRecorder()
	f.panel.handleDomainBackups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(f.agent.createReqs) != 1 {
		t.Fatalf("create calls=%d", len(f.agent.createReqs))
	}
	got := f.agent.createReqs[0]
	if got.Database.ID != f.mariaID || got.Database.Name != "orders_archive_2026" || got.Database.Type != "mariadb" {
		t.Fatalf("database identity=%+v", got.Database)
	}
	if got.DatabaseName != got.Database.Name || got.DatabaseType != got.Database.Type {
		t.Fatalf("transition identity=(%q,%q)", got.DatabaseName, got.DatabaseType)
	}
	if got.ProtocolVersion != backupspec.ProtocolVersion || got.SubscriptionID == 0 || got.DomainID != f.domainID || got.Origin != backupspec.OriginManual {
		t.Fatalf("scope/origin=%+v", got)
	}
	if !hasBackupAction(backupActions(t, f.panel), "backup.create:database") {
		t.Fatal("missing successful create audit")
	}
}

func TestPanelBackupV2FullIncludesAllCurrentDomainDatabases(t *testing.T) {
	f := newPanelBackupFixture(t)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups", f.domainID), strings.NewReader(`{"type":"full"}`))
	rec := httptest.NewRecorder()
	f.panel.handleDomainBackups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := f.agent.createReqs[0].Databases
	if len(got) != 2 {
		t.Fatalf("databases=%+v", got)
	}
	if got[0].ID != f.mariaID || got[0].Name != "orders_archive_2026" || got[1].ID != f.postgresID || got[1].Name != "analytics_store_v2" {
		t.Fatalf("full database identities=%+v", got)
	}
}

func TestPanelBackupV2StrictBodyRejectsUnknownFields(t *testing.T) {
	f := newPanelBackupFixture(t)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups", f.domainID), strings.NewReader(`{"type":"files","path":"/etc"}`))
	rec := httptest.NewRecorder()
	f.panel.handleDomainBackups(rec, req)
	if rec.Code != http.StatusBadRequest || len(f.agent.createReqs) != 0 {
		t.Fatalf("status=%d calls=%d body=%s", rec.Code, len(f.agent.createReqs), rec.Body.String())
	}
}

func TestPanelBackupV2RestoreInspectsAndReauthorizesEveryDatabase(t *testing.T) {
	f := newPanelBackupFixture(t)
	f.agent.inspectResp = backupspec.InspectResponse{
		Success: true,
		Backup:  backupspec.Info{Name: "full.tar.gz", Type: backupspec.TypeFull, Restorable: true},
		Databases: []backupspec.DatabaseIdentity{
			{ID: f.mariaID, Name: "spoofed", Type: "postgresql"},
			{ID: f.postgresID, Name: "also_spoofed", Type: "mariadb"},
		},
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups/restore", f.domainID), strings.NewReader(`{"backup_name":"full.tar.gz"}`))
	rec := httptest.NewRecorder()
	f.panel.handleRestoreBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(f.agent.restoreReqs) != 1 {
		t.Fatalf("restore calls=%d", len(f.agent.restoreReqs))
	}
	got := f.agent.restoreReqs[0]
	if len(got.Databases) != 2 || got.Databases[0].Name != "orders_archive_2026" || got.Databases[0].Type != "mariadb" || got.Databases[1].Name != "analytics_store_v2" || got.Databases[1].Type != "postgresql" {
		t.Fatalf("reauthorized databases=%+v", got.Databases)
	}
	if got.TargetDir != "/srv/one.example" || got.SubscriptionID == 0 || got.DomainID != f.domainID {
		t.Fatalf("restore scope=%+v", got)
	}
	if !hasBackupAction(backupActions(t, f.panel), "backup.restore:full") {
		t.Fatal("missing successful restore audit")
	}
}

func TestPanelBackupV2RestoreAllowsSafeLegacyFilesAndFullBackups(t *testing.T) {
	for _, backupType := range []string{backupspec.TypeFiles, backupspec.TypeFull} {
		t.Run(backupType, func(t *testing.T) {
			f := newPanelBackupFixture(t)
			f.agent.inspectResp = backupspec.InspectResponse{
				Success: true,
				Backup: backupspec.Info{
					Name:       "legacy-" + backupType + ".tar.gz",
					Type:       backupType,
					Legacy:     true,
					Restorable: true,
				},
			}
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/domains/%d/backups/restore", f.domainID),
				strings.NewReader(fmt.Sprintf(`{"backup_name":"legacy-%s.tar.gz"}`, backupType)),
			)
			rec := httptest.NewRecorder()

			f.panel.handleRestoreBackup(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if len(f.agent.restoreReqs) != 1 {
				t.Fatalf("restore calls=%d", len(f.agent.restoreReqs))
			}
			got := f.agent.restoreReqs[0]
			if got.BackupName != "legacy-"+backupType+".tar.gz" || got.TargetDir != "/srv/one.example" {
				t.Fatalf("restore request=%+v", got)
			}
			if got.Database.ID != 0 || len(got.Databases) != 0 {
				t.Fatalf("legacy %s restore unexpectedly authorized databases: %+v", backupType, got)
			}
			if !hasBackupAction(backupActions(t, f.panel), "backup.restore:"+backupType) {
				t.Fatalf("missing successful %s restore audit", backupType)
			}
		})
	}
}

func TestPanelBackupV2RestoreRejectsCrossDomainAndLegacyMetadata(t *testing.T) {
	for _, tc := range []struct {
		name       string
		inspection func(panelBackupFixture) backupspec.InspectResponse
	}{
		{"cross-domain", func(f panelBackupFixture) backupspec.InspectResponse {
			return backupspec.InspectResponse{Success: true, Backup: backupspec.Info{Name: "x.tar.gz", Type: backupspec.TypeDatabase, DatabaseID: f.crossDomainDBID, Restorable: true}, Databases: []backupspec.DatabaseIdentity{{ID: f.crossDomainDBID}}}
		}},
		{"legacy", func(panelBackupFixture) backupspec.InspectResponse {
			return backupspec.InspectResponse{Success: true, Backup: backupspec.Info{Name: "x.tar.gz", Type: backupspec.TypeFiles, Legacy: true, Restorable: false}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPanelBackupFixture(t)
			f.agent.inspectResp = tc.inspection(f)
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/domains/%d/backups/restore", f.domainID), strings.NewReader(`{"backup_name":"x.tar.gz"}`))
			rec := httptest.NewRecorder()
			f.panel.handleRestoreBackup(rec, req)
			if rec.Code != http.StatusConflict || len(f.agent.restoreReqs) != 0 {
				t.Fatalf("status=%d restore calls=%d body=%s", rec.Code, len(f.agent.restoreReqs), rec.Body.String())
			}
			if !hasBackupAction(backupActions(t, f.panel), "backup.restore.failed") {
				t.Fatal("missing failed restore audit")
			}
		})
	}
}

func TestPanelBackupV2TraversalAndDeleteAudits(t *testing.T) {
	f := newPanelBackupFixture(t)
	for _, name := range []string{"", ".hidden", "../escape", `..\escape`, "dir/file"} {
		if validBackupName(name) {
			t.Fatalf("accepted unsafe name %q", name)
		}
	}
	badReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/domains/%d/backups?name=..%%2Fescape", f.domainID), nil)
	badRec := httptest.NewRecorder()
	f.panel.handleDomainBackups(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || len(f.agent.deleteReqs) != 0 {
		t.Fatalf("unsafe delete status=%d calls=%d", badRec.Code, len(f.agent.deleteReqs))
	}
	goodReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/domains/%d/backups?name=manual.tar.gz", f.domainID), nil)
	goodRec := httptest.NewRecorder()
	f.panel.handleDomainBackups(goodRec, goodReq)
	if goodRec.Code != http.StatusOK || len(f.agent.deleteReqs) != 1 {
		t.Fatalf("delete status=%d calls=%d body=%s", goodRec.Code, len(f.agent.deleteReqs), goodRec.Body.String())
	}
	actions := backupActions(t, f.panel)
	if !hasBackupAction(actions, "backup.delete.failed") || !hasBackupAction(actions, "backup.delete") {
		t.Fatalf("delete audits=%v", actions)
	}
}

type countingBackupWriter struct {
	header           http.Header
	bytes, writes    int64
	maxWrite, status int
}

func (w *countingBackupWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *countingBackupWriter) WriteHeader(status int) { w.status = status }
func (w *countingBackupWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.bytes += int64(len(data))
	w.writes++
	if len(data) > w.maxWrite {
		w.maxWrite = len(data)
	}
	return len(data), nil
}

func TestPanelBackupV2DownloadStreamsMoreThanTenMiBInBoundedChunks(t *testing.T) {
	f := newPanelBackupFixture(t)
	f.agent.content = bytes.Repeat([]byte("z"), 10*1024*1024+12345)
	f.agent.inspectResp = backupspec.InspectResponse{
		Success: true,
		Backup:  backupspec.Info{Name: "large.tar.gz", Type: backupspec.TypeFull, Size: int64(len(f.agent.content)), Restorable: true},
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/domains/%d/backups/download?name=large.tar.gz", f.domainID), nil)
	w := &countingBackupWriter{}
	f.panel.handleDownloadBackup(w, req)
	if w.status != http.StatusOK || w.bytes != int64(len(f.agent.content)) {
		t.Fatalf("status=%d bytes=%d want=%d", w.status, w.bytes, len(f.agent.content))
	}
	if w.writes < 11 || w.maxWrite > backupspec.MaxChunkBytes {
		t.Fatalf("writes=%d maxWrite=%d", w.writes, w.maxWrite)
	}
	if len(f.agent.chunkReqs) != int(w.writes) {
		t.Fatalf("chunk calls=%d writes=%d", len(f.agent.chunkReqs), w.writes)
	}
	var expected int64
	for _, chunkReq := range f.agent.chunkReqs {
		if chunkReq.Offset != expected || chunkReq.MaxBytes != backupspec.MaxChunkBytes || chunkReq.DomainID != f.domainID {
			t.Fatalf("chunk request=%+v expected offset=%d", chunkReq, expected)
		}
		expected += int64(min(chunkReq.MaxBytes, len(f.agent.content)-int(expected)))
	}
}

func TestPanelBackupV2ScheduledFullIncludesEveryDatabase(t *testing.T) {
	f := newPanelBackupFixture(t)
	d, err := f.panel.loadBackupDomain(context.Background(), f.domainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.panel.runScheduledBackup(
		context.Background(), d, backupspec.TypeFull, 7, "schedule:test-full",
	); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.createReqs) != 1 {
		t.Fatalf("create calls=%d", len(f.agent.createReqs))
	}
	got := f.agent.createReqs[0]
	if got.Origin != backupspec.OriginScheduled || got.JobKey != "schedule:test-full" ||
		len(got.Databases) != 2 || got.Databases[0].ID != f.mariaID || got.Databases[1].ID != f.postgresID {
		t.Fatalf("scheduled full request=%+v", got)
	}
}

func TestPanelBackupV2RetentionOnlyDeletesScheduledOrigin(t *testing.T) {
	f := newPanelBackupFixture(t)
	d, err := f.panel.loadBackupDomain(context.Background(), f.domainID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f.agent.listResp.Backups = []backupspec.Info{
		{Name: "manual-old.tar.gz", Type: backupspec.TypeFull, Origin: backupspec.OriginManual, CreatedAt: now.Add(-10 * time.Hour)},
		{Name: "safety-old.tar.gz", Type: backupspec.TypeFull, Origin: backupspec.OriginPreRestore, CreatedAt: now.Add(-9 * time.Hour)},
		{Name: "scheduled-old.tar.gz", Type: backupspec.TypeFiles, Origin: backupspec.OriginScheduled, CreatedAt: now.Add(-2 * time.Hour)},
		{Name: "scheduled-new.tar.gz", Type: backupspec.TypeFull, Origin: backupspec.OriginScheduled, CreatedAt: now},
	}
	if err := f.panel.pruneBackups(context.Background(), d, 1); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.deleteReqs) != 1 || f.agent.deleteReqs[0].BackupName != "scheduled-old.tar.gz" {
		t.Fatalf("retention deletes=%+v", f.agent.deleteReqs)
	}
}
