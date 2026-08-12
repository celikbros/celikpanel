package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/systemsqlite"
	"github.com/alicelik/celikpanel/internal/transport"
)

const systemSQLiteTestSnapshotToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type systemSQLitePanelAgent struct {
	mu sync.Mutex

	listResponse   systemsqlite.ListResponse
	listError      error
	checkError     error
	optimizeError  error
	createError    error
	readError      error
	releaseError   error
	checkUnhealthy bool

	content        []byte
	chunkLimit     int
	chunkRequests  []systemsqlite.ReadSnapshotChunkRequest
	releases       []systemsqlite.ReleaseSnapshotRequest
	callCount      int
	snapshotSHA256 string

	snapshotDatabaseID string
	chunkDatabaseID    string
}

func (agent *systemSQLitePanelAgent) ListSystemSQLiteDatabases(
	_ *systemsqlite.ListRequest,
	response *systemsqlite.ListResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	if agent.listError != nil {
		return agent.listError
	}
	*response = agent.listResponse
	response.Databases = append([]systemsqlite.DatabaseInfo(nil), agent.listResponse.Databases...)
	return nil
}

func (agent *systemSQLitePanelAgent) CheckSystemSQLiteDatabase(
	request *systemsqlite.DatabaseRequest,
	response *systemsqlite.CheckResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	if agent.checkError != nil {
		return agent.checkError
	}
	healthy := !agent.checkUnhealthy
	response.Success = true
	response.Check = systemsqlite.CheckResult{
		DatabaseID:       request.DatabaseID,
		CheckedAt:        time.Now().UTC(),
		IntegrityOK:      healthy,
		ForeignKeysOK:    healthy,
		IntegrityMessage: "ok",
	}
	return nil
}

func (agent *systemSQLitePanelAgent) OptimizeSystemSQLiteDatabase(
	request *systemsqlite.DatabaseRequest,
	response *systemsqlite.OptimizeResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	if agent.optimizeError != nil {
		return agent.optimizeError
	}
	response.Success = true
	response.Result = systemsqlite.OptimizeResult{
		DatabaseID:  request.DatabaseID,
		OptimizedAt: time.Now().UTC(),
	}
	return nil
}

func (agent *systemSQLitePanelAgent) CreateSystemSQLiteSnapshot(
	request *systemsqlite.DatabaseRequest,
	response *systemsqlite.SnapshotResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	if agent.createError != nil {
		return agent.createError
	}
	digest := sha256.Sum256(agent.content)
	digestValue := hex.EncodeToString(digest[:])
	if agent.snapshotSHA256 != "" {
		digestValue = agent.snapshotSHA256
	}
	agent.snapshotDatabaseID = request.DatabaseID
	response.Success = true
	response.Snapshot = systemsqlite.SnapshotInfo{
		DatabaseID: request.DatabaseID,
		Token:      systemSQLiteTestSnapshotToken,
		SizeBytes:  int64(len(agent.content)),
		SHA256:     digestValue,
		CreatedAt:  time.Date(2026, time.July, 29, 1, 2, 3, 0, time.UTC),
		ExpiresAt:  time.Now().UTC().Add(time.Minute),
	}
	return nil
}

func (agent *systemSQLitePanelAgent) ReadSystemSQLiteSnapshotChunk(
	request *systemsqlite.ReadSnapshotChunkRequest,
	response *systemsqlite.ReadSnapshotChunkResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	agent.chunkRequests = append(agent.chunkRequests, *request)
	if agent.readError != nil {
		return agent.readError
	}
	if request.Offset < 0 || request.Offset > int64(len(agent.content)) {
		return errors.New("invalid offset")
	}
	limit := request.MaxBytes
	if agent.chunkLimit > 0 && agent.chunkLimit < limit {
		limit = agent.chunkLimit
	}
	end := request.Offset + int64(limit)
	if end > int64(len(agent.content)) {
		end = int64(len(agent.content))
	}
	response.Success = true
	response.DatabaseID = agent.snapshotDatabaseID
	if agent.chunkDatabaseID != "" {
		response.DatabaseID = agent.chunkDatabaseID
	}
	response.Data = append([]byte(nil), agent.content[request.Offset:end]...)
	response.NextOffset = end
	response.SizeBytes = int64(len(agent.content))
	response.EOF = end == int64(len(agent.content))
	return nil
}

func (agent *systemSQLitePanelAgent) ReleaseSystemSQLiteSnapshot(
	request *systemsqlite.ReleaseSnapshotRequest,
	response *systemsqlite.ReleaseSnapshotResponse,
) error {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.callCount++
	agent.releases = append(agent.releases, *request)
	if agent.releaseError != nil {
		return agent.releaseError
	}
	response.Success = true
	response.Released = true
	return nil
}

func attachSystemSQLitePanelAgent(t *testing.T, panel *Panel, agent *systemSQLitePanelAgent) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register system SQLite agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConnection, clientConnection := net.Pipe()
		go server.ServeConn(serverConnection)
		return rpc.NewClient(clientConnection), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func systemSQLiteRequest(method, target, role string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	return request.WithContext(context.WithValue(
		request.Context(),
		callerKey,
		&Caller{ID: 17, Role: role},
	))
}

func systemSQLiteDownloadRequest(target, token, role string) *http.Request {
	values := url.Values{"download_token": {token}}
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request.WithContext(context.WithValue(
		request.Context(),
		callerKey,
		&Caller{ID: 17, Role: role},
	))
}

func TestSystemSQLiteListRequiresAdminAndReturnsSafeInventory(t *testing.T) {
	agent := &systemSQLitePanelAgent{
		listResponse: systemsqlite.ListResponse{
			Success: true,
			Databases: []systemsqlite.DatabaseInfo{{
				ID:        systemsqlite.DatabasePanel,
				Name:      "CelikPanel state",
				PathHint:  "data-dir/celikpanel.db",
				Available: true,
				Mutable:   true,
				Status:    "ready",
			}},
		},
	}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabases(
		recorder,
		systemSQLiteRequest(http.MethodGet, "/api/v1/system-databases", roleAdmin),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "CelikPanel state") {
		t.Fatalf("inventory response = %s", recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	panel.handleSystemSQLiteDatabases(
		recorder,
		systemSQLiteRequest(http.MethodGet, "/api/v1/system-databases", roleCustomer),
	)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestSystemSQLiteListRejectsAbsoluteAgentPathWithoutLeakingIt(t *testing.T) {
	const privatePath = "/var/lib/celikpanel/celikpanel.db"
	agent := &systemSQLitePanelAgent{
		listResponse: systemsqlite.ListResponse{
			Success: true,
			Databases: []systemsqlite.DatabaseInfo{{
				ID:        systemsqlite.DatabasePanel,
				Name:      "CelikPanel state",
				PathHint:  privatePath,
				Available: true,
			}},
		},
	}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)
	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabases(
		recorder,
		systemSQLiteRequest(http.MethodGet, "/api/v1/system-databases", roleAdmin),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privatePath) {
		t.Fatalf("absolute agent path leaked: %s", recorder.Body.String())
	}
}

func TestSafeSystemSQLitePathHintRejectsUnixAndWindowsAbsoluteForms(t *testing.T) {
	for _, value := range []string{
		"/var/lib/celikpanel/celikpanel.db",
		`C:\ProgramData\CelikPanel\celikpanel.db`,
		`C:celikpanel.db`,
		`\private\celikpanel.db`,
		`\\server\share\celikpanel.db`,
		"data/../private.db",
		"data\\..\\private.db",
		"data\nprivate.db",
		strings.Repeat("a", 257),
	} {
		if safeSystemSQLitePathHint(value) {
			t.Errorf("safeSystemSQLitePathHint(%q) = true", value)
		}
	}
	for _, value := range []string{"", "data-dir/celikpanel.db", "powerdns/pdns.sqlite3"} {
		if !safeSystemSQLitePathHint(value) {
			t.Errorf("safeSystemSQLitePathHint(%q) = false", value)
		}
	}
}

func TestSystemSQLiteCheckAndOptimizeAdminSuccess(t *testing.T) {
	agent := &systemSQLitePanelAgent{}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	for _, action := range []string{"check", "optimize"} {
		t.Run(action, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			panel.handleSystemSQLiteDatabaseAction(
				recorder,
				systemSQLiteRequest(
					http.MethodPost,
					"/api/v1/system-databases/panel/"+action,
					roleAdmin,
				),
			)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"success":true`) {
				t.Fatalf("response = %s", recorder.Body.String())
			}
		})
	}
}

func TestSystemSQLiteActionRejectsNonAdminMalformedAndUnknownRoutes(t *testing.T) {
	agent := &systemSQLitePanelAgent{}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/check",
			roleReseller,
		),
	)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	tests := []struct {
		method string
		target string
		status int
	}{
		{http.MethodPost, "/api/v1/system-databases/panel/check/extra", http.StatusNotFound},
		{http.MethodPost, "/api/v1/system-databases/unknown/check", http.StatusNotFound},
		{http.MethodPost, "/api/v1/system-databases/panel/drop", http.StatusNotFound},
		{http.MethodGet, "/api/v1/system-databases/panel/check", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/system-databases/panel/snapshot-download", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/system-databases/panel/snapshot-download/extra", http.StatusNotFound},
	}
	for _, test := range tests {
		recorder = httptest.NewRecorder()
		panel.handleSystemSQLiteDatabaseAction(
			recorder,
			systemSQLiteRequest(test.method, test.target, roleAdmin),
		)
		if recorder.Code != test.status {
			t.Errorf("%s %s status = %d, want %d", test.method, test.target, recorder.Code, test.status)
		}
	}
}

func TestSystemSQLiteAgentFailureIsGeneric(t *testing.T) {
	const privatePath = "/root/private/system.sqlite"
	agent := &systemSQLitePanelAgent{
		listResponse: systemsqlite.ListResponse{
			Success: false,
			Error:   privatePath + ": permission denied",
		},
	}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabases(
		recorder,
		systemSQLiteRequest(http.MethodGet, "/api/v1/system-databases", roleAdmin),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privatePath) ||
		!strings.Contains(recorder.Body.String(), "internal server error") {
		t.Fatalf("unsafe error response = %s", recorder.Body.String())
	}
}

func TestSystemSQLiteCheckAuditsSuccessAndFailureWithoutAgentPath(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open panel database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role)
		VALUES (17, 'sqlite-admin', 'x', 'sqlite-admin@example.test', 'admin')`); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	agent := &systemSQLitePanelAgent{}
	panel := &Panel{db: database}
	attachSystemSQLitePanelAgent(t, panel, agent)
	target := "/api/v1/system-databases/panel/check"

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(http.MethodPost, target, roleAdmin),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("success status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	agent.mu.Lock()
	agent.checkError = errors.New("/root/private/panel.sqlite: check failed")
	agent.mu.Unlock()
	recorder = httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(http.MethodPost, target, roleAdmin),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("failure status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	agent.mu.Lock()
	agent.checkError = nil
	agent.checkUnhealthy = true
	agent.mu.Unlock()
	recorder = httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(http.MethodPost, target, roleAdmin),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("warning status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	rows, err := database.GetDB().Query(`
		SELECT action
		FROM audit_logs
		WHERE action LIKE 'system_sqlite.check.%'
		ORDER BY id`)
	if err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"system_sqlite.check.completed:panel",
		"system_sqlite.check.failed:panel",
		"system_sqlite.check.warning:panel",
	}
	if len(actions) != len(want) {
		t.Fatalf("audit actions = %#v, want %#v", actions, want)
	}
	for index := range want {
		if actions[index] != want[index] || strings.Contains(actions[index], "/root/") {
			t.Fatalf("audit action %d = %q, want %q", index, actions[index], want[index])
		}
	}
}

func TestSystemSQLiteSnapshotPreparesThenDownloadsExactChunksAndReleases(t *testing.T) {
	content := []byte("SQLite format 3\x00bounded snapshot bytes")
	agent := &systemSQLitePanelAgent{content: content, chunkLimit: 7}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	prepareRecorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		prepareRecorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if prepareRecorder.Code != http.StatusOK {
		t.Fatalf("prepare status = %d, body = %s", prepareRecorder.Code, prepareRecorder.Body.String())
	}
	if got := prepareRecorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("prepare Content-Type = %q", got)
	}
	if got := prepareRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("prepare Cache-Control = %q", got)
	}
	var prepared struct {
		DownloadToken string `json:"download_token"`
		SizeBytes     int64  `json:"size_bytes"`
	}
	if err := json.NewDecoder(prepareRecorder.Body).Decode(&prepared); err != nil {
		t.Fatalf("decode prepare response: %v", err)
	}
	if prepared.DownloadToken != systemSQLiteTestSnapshotToken || !validSystemSQLiteSnapshotToken(prepared.DownloadToken) {
		t.Fatalf("prepared token = %q", prepared.DownloadToken)
	}
	if prepared.SizeBytes != int64(len(content)) {
		t.Fatalf("prepared size = %d, want %d", prepared.SizeBytes, len(content))
	}
	agent.mu.Lock()
	if len(agent.releases) != 0 {
		t.Fatalf("prepare released a downloadable token: %#v", agent.releases)
	}
	agent.mu.Unlock()

	downloadRecorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		downloadRecorder,
		systemSQLiteDownloadRequest(
			"/api/v1/system-databases/panel/snapshot-download",
			prepared.DownloadToken,
			roleAdmin,
		),
	)
	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", downloadRecorder.Code, downloadRecorder.Body.String())
	}
	if !bytes.Equal(downloadRecorder.Body.Bytes(), content) {
		t.Fatalf("snapshot bytes = %q, want %q", downloadRecorder.Body.Bytes(), content)
	}
	if got := downloadRecorder.Header().Get("Content-Type"); got != "application/vnd.sqlite3" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := downloadRecorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := downloadRecorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if disposition := downloadRecorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "celikpanel-panel-") || !strings.Contains(disposition, ".sqlite3") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 || agent.releases[0].Token != systemSQLiteTestSnapshotToken {
		t.Fatalf("release requests = %#v", agent.releases)
	}
	secondPass := -1
	for index := 1; index < len(agent.chunkRequests); index++ {
		if agent.chunkRequests[index].Offset == 0 {
			secondPass = index
			break
		}
	}
	if secondPass <= 0 || len(agent.chunkRequests) != secondPass*2 {
		t.Fatalf("snapshot chunk passes = %#v", agent.chunkRequests)
	}
	var expectedOffset int64
	for index := 0; index < secondPass; index++ {
		firstRequest := agent.chunkRequests[index]
		secondRequest := agent.chunkRequests[index+secondPass]
		if firstRequest.Offset != expectedOffset || secondRequest.Offset != expectedOffset {
			t.Fatalf("chunk %d offsets = (%d, %d), want %d", index, firstRequest.Offset, secondRequest.Offset, expectedOffset)
		}
		if firstRequest.MaxBytes != systemsqlite.MaxChunkSize || secondRequest.MaxBytes != systemsqlite.MaxChunkSize {
			t.Fatalf("chunk %d max bytes = (%d, %d)", index, firstRequest.MaxBytes, secondRequest.MaxBytes)
		}
		expectedOffset += 7
		if expectedOffset > int64(len(content)) {
			expectedOffset = int64(len(content))
		}
	}
	if expectedOffset != int64(len(content)) {
		t.Fatalf("final chunk offset = %d, want %d", expectedOffset, len(content))
	}
}

func TestSystemSQLiteSnapshotDownloadRejectsTokenFromDifferentDatabaseEndpoint(t *testing.T) {
	content := []byte("SQLite format 3\\x00database-bound snapshot")
	agent := &systemSQLitePanelAgent{content: content, chunkLimit: 7}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	prepareRecorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		prepareRecorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if prepareRecorder.Code != http.StatusOK {
		t.Fatalf("prepare status = %d, body = %s", prepareRecorder.Code, prepareRecorder.Body.String())
	}

	downloadRecorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		downloadRecorder,
		systemSQLiteDownloadRequest(
			"/api/v1/system-databases/powerdns/snapshot-download",
			systemSQLiteTestSnapshotToken,
			roleAdmin,
		),
	)
	if downloadRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("cross-database download status = %d, body = %s", downloadRecorder.Code, downloadRecorder.Body.String())
	}
	if downloadRecorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("cross-database token committed download headers: %#v", downloadRecorder.Header())
	}
	if bytes.Equal(downloadRecorder.Body.Bytes(), content) {
		t.Fatal("cross-database token returned snapshot bytes")
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 || agent.releases[0].Token != systemSQLiteTestSnapshotToken {
		t.Fatalf("release requests = %#v", agent.releases)
	}
}

func TestSystemSQLiteSnapshotDownloadRejectsInvalidTokenWithoutAgentCalls(t *testing.T) {
	agent := &systemSQLitePanelAgent{content: []byte("snapshot")}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	for _, token := range []string{"too-short", strings.Repeat("A", sha256.Size*2)} {
		recorder := httptest.NewRecorder()
		panel.handleSystemSQLiteDatabaseAction(
			recorder,
			systemSQLiteDownloadRequest(
				"/api/v1/system-databases/panel/snapshot-download",
				token,
				roleAdmin,
			),
		)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("token %q status = %d, body = %s", token, recorder.Code, recorder.Body.String())
		}
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.chunkRequests) != 0 || len(agent.releases) != 0 {
		t.Fatalf("invalid tokens reached the agent: chunks=%#v releases=%#v", agent.chunkRequests, agent.releases)
	}
}

func TestSystemSQLiteSnapshotDigestMismatchIsAuditedAndReleased(t *testing.T) {
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open panel database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role)
		VALUES (17, 'snapshot-admin', 'x', 'snapshot-admin@example.test', 'admin')`); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	agent := &systemSQLitePanelAgent{
		content:        []byte("digest mismatch"),
		chunkLimit:     4,
		snapshotSHA256: strings.Repeat("0", sha256.Size*2),
	}
	panel := &Panel{db: database}
	attachSystemSQLitePanelAgent(t, panel, agent)
	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("digest mismatch committed download headers: %#v", recorder.Header())
	}
	if bytes.Equal(recorder.Body.Bytes(), agent.content) {
		t.Fatal("digest mismatch returned the unverified snapshot bytes")
	}

	var completed, failed int
	if err := database.GetDB().QueryRow(`
		SELECT
			SUM(CASE WHEN action = 'system_sqlite.snapshot.completed:panel' THEN 1 ELSE 0 END),
			SUM(CASE WHEN action = 'system_sqlite.snapshot.failed:panel' THEN 1 ELSE 0 END)
		FROM audit_logs`).Scan(&completed, &failed); err != nil {
		t.Fatalf("query snapshot audit: %v", err)
	}
	if completed != 0 || failed != 1 {
		t.Fatalf("snapshot audit completed=%d failed=%d", completed, failed)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 {
		t.Fatalf("release requests = %#v", agent.releases)
	}
}

func TestSystemSQLiteSnapshotVerificationRejectsMismatchedChunkDatabaseID(t *testing.T) {
	content := []byte("SQLite format 3\\x00mismatched identity")
	agent := &systemSQLitePanelAgent{
		content:         content,
		chunkDatabaseID: systemsqlite.DatabasePowerDNS,
	}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("mismatched identity committed download headers: %#v", recorder.Header())
	}
	if bytes.Equal(recorder.Body.Bytes(), content) {
		t.Fatal("mismatched identity returned snapshot bytes")
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 {
		t.Fatalf("release requests = %#v", agent.releases)
	}
}

func TestSystemSQLiteSnapshotRejectsZeroSizeAndReleasesToken(t *testing.T) {
	agent := &systemSQLitePanelAgent{}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)
	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("zero-size snapshot committed download headers: %#v", recorder.Header())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 {
		t.Fatalf("release requests = %#v", agent.releases)
	}
}

func TestSystemSQLiteSnapshotFirstChunkFailureHasNoHeadersOrPathAndReleases(t *testing.T) {
	const privatePath = "/var/lib/private/snapshot.sqlite"
	agent := &systemSQLitePanelAgent{
		content:   []byte("snapshot"),
		readError: errors.New(privatePath + ": read failed"),
	}
	panel := &Panel{}
	attachSystemSQLitePanelAgent(t, panel, agent)

	recorder := httptest.NewRecorder()
	panel.handleSystemSQLiteDatabaseAction(
		recorder,
		systemSQLiteRequest(
			http.MethodPost,
			"/api/v1/system-databases/panel/snapshot",
			roleAdmin,
		),
	)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privatePath) {
		t.Fatalf("snapshot error leaked a path: %s", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("download headers were committed before first chunk: %#v", recorder.Header())
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.releases) != 1 {
		t.Fatalf("release requests = %#v", agent.releases)
	}
}
