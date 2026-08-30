package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	updateTestCurrentVersion = "v0.1.0-alpha.13"
	updateTestCurrentCommit  = "cccccccccccccccccccccccccccccccccccccccc"
	updateTestTargetVersion  = "v0.1.0-alpha.14"
	updateTestTargetCommit   = "dddddddddddddddddddddddddddddddddddddddd"
	updateTestTargetSHA      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	updateTestRequestID      = "0123456789abcdef0123456789abcdef"
)

type systemUpdateTestAgent struct {
	verifiedAPTAgentRPCFixture

	mu            sync.Mutex
	version       transport.AgentVersionResponse
	check         transport.SystemUpdateCheckResponse
	checkErr      error
	start         transport.SystemUpdateStartResponse
	startErr      error
	status        transport.SystemUpdateStatusResponse
	statusErr     error
	abandon       transport.SystemUpdateStatusResponse
	abandonErr    error
	checkCalls    int
	startCalls    int
	statusCalls   int
	abandonCalls  int
	lastStart     transport.SystemUpdateStartRequest
	lastAbandon   transport.SystemUpdateAbandonRequest
	lastStatusID  string
	packageFamily string
}

func newSystemUpdateTestAgent() *systemUpdateTestAgent {
	return &systemUpdateTestAgent{
		version: transport.AgentVersionResponse{
			Version: updateTestCurrentVersion, Commit: updateTestCurrentCommit,
			Capabilities: []string{
				transport.AgentCapabilitySystemUpdateV1,
				transport.AgentCapabilitySystemUpdateAbandonV1,
			},
		},
		check: transport.SystemUpdateCheckResponse{
			Supported: true, Available: true,
			CurrentVersion: updateTestCurrentVersion, CurrentCommit: updateTestCurrentCommit,
			TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
			TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
			TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
			PublishedAt: "2026-08-12T12:00:00Z",
		},
		start:  transport.SystemUpdateStartResponse{Accepted: true, Status: "queued"},
		status: transport.SystemUpdateStatusResponse{Found: false},
		abandon: transport.SystemUpdateStatusResponse{
			Found: true, RequestID: updateTestRequestID, Status: "failed",
			TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
			TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
			TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
			Error: "system update start was authoritatively abandoned before acceptance",
		},
		packageFamily: "apt",
	}
}

func (a *systemUpdateTestAgent) Version(_ *transport.Empty, reply *transport.AgentVersionResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*reply = a.version
	return nil
}

func (a *systemUpdateTestAgent) PkgFamily(_ *transport.Empty, reply *string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	*reply = a.packageFamily
	return nil
}

func (a *systemUpdateTestAgent) CheckSystemUpdate(_ *transport.Empty, reply *transport.SystemUpdateCheckResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checkCalls++
	*reply = a.check
	return a.checkErr
}

func (a *systemUpdateTestAgent) StartSystemUpdate(request *transport.SystemUpdateStartRequest, reply *transport.SystemUpdateStartResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.startCalls++
	a.lastStart = *request
	*reply = a.start
	return a.startErr
}

func (a *systemUpdateTestAgent) AbandonSystemUpdate(request *transport.SystemUpdateAbandonRequest, reply *transport.SystemUpdateStatusResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.abandonCalls++
	a.lastAbandon = *request
	*reply = a.abandon
	return a.abandonErr
}

func (a *systemUpdateTestAgent) SystemUpdateStatus(request *transport.SystemUpdateStatusRequest, reply *transport.SystemUpdateStatusResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.statusCalls++
	a.lastStatusID = request.RequestID
	*reply = a.status
	return a.statusErr
}

type systemUpdateTestFixture struct {
	panel    *Panel
	agent    *systemUpdateTestAgent
	database *paneldb.SQLiteDB
}

func newSystemUpdateTestFixture(t *testing.T) systemUpdateTestFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	agent := newSystemUpdateTestAgent()
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
	t.Cleanup(func() { _ = client.Close() })
	return systemUpdateTestFixture{
		panel: &Panel{db: database, agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)},
		agent: agent, database: database,
	}
}

func withSystemUpdateBuild(t *testing.T) {
	t.Helper()
	oldVersion, oldCommit := buildVersion, buildCommit
	buildVersion, buildCommit = updateTestCurrentVersion, updateTestCurrentCommit
	t.Cleanup(func() { buildVersion, buildCommit = oldVersion, oldCommit })
}

func systemUpdateRequest(method, path, body, role string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "panel.example.test"
	request.Header.Set("Origin", "https://panel.example.test")
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{ID: 1, Role: role}))
}

func systemUpdateStartBody(targetVersion string) string {
	payload := map[string]any{
		"request_id": updateTestRequestID, "confirmed": true,
		"current_version": updateTestCurrentVersion, "current_commit": updateTestCurrentCommit,
		"version": targetVersion, "commit": updateTestTargetCommit, "sequence": "14",
		"os": "linux", "arch": "amd64", "archive_sha256": updateTestTargetSHA,
		"archive_size": "1048576", "published_at": "2026-08-12T12:00:00Z",
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func TestPanelUpdateEndpointsAreAdminOnly(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, panelUpdateCheckPath, ""},
		{http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion)},
		{http.MethodPost, panelUpdateAbandonPath, systemUpdateStartBody(updateTestTargetVersion)},
		{http.MethodGet, panelUpdateStatusPath + "?request_id=" + updateTestRequestID, ""},
	}
	for _, role := range []string{roleReseller, roleCustomer, "additional_user"} {
		for _, test := range tests {
			recorder := httptest.NewRecorder()
			handler := map[string]http.HandlerFunc{
				panelUpdateCheckPath:   fixture.panel.handlePanelUpdateCheck,
				panelUpdateStartPath:   fixture.panel.handlePanelUpdateStart,
				panelUpdateAbandonPath: fixture.panel.handlePanelUpdateAbandon,
				panelUpdateStatusPath:  fixture.panel.handlePanelUpdateStatus,
			}[strings.Split(test.path, "?")[0]]
			handler(recorder, systemUpdateRequest(test.method, test.path, test.body, role))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s %s role=%s status=%d body=%s", test.method, test.path, role, recorder.Code, recorder.Body.String())
			}
		}
	}
}

func TestPanelUpdateRoutesRemainBehindAuthenticationAndCSRF(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, route := range []string{
		"http.HandleFunc(panelUpdateCheckPath, panel.handlePanelUpdateCheck)",
		"http.HandleFunc(panelUpdateStartPath, panel.handlePanelUpdateStart)",
		"http.HandleFunc(panelUpdateAbandonPath, panel.handlePanelUpdateAbandon)",
		"http.HandleFunc(panelUpdateStatusPath, panel.handlePanelUpdateStatus)",
	} {
		if !strings.Contains(text, route) {
			t.Fatalf("update route is not registered exactly: %s", route)
		}
	}
	if !strings.Contains(text, "applicationHandler := csrfProtect(\n\t\tpanel.requireAuth(http.DefaultServeMux),\n\t)") {
		t.Fatal("default API mux is no longer wrapped by auth then same-origin CSRF")
	}
}

func TestPanelUpdateStartIsProtectedBySameOriginCSRF(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	request := systemUpdateRequest(http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin)
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	csrfProtect(http.HandlerFunc(fixture.panel.handlePanelUpdateStart)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.startCalls != 0 || fixture.agent.checkCalls != 0 {
		t.Fatalf("cross-origin request reached agent: checks=%d starts=%d", fixture.agent.checkCalls, fixture.agent.startCalls)
	}
}

func TestPanelUpdateCheckRequiresCapabilityAndExactBuildPair(t *testing.T) {
	withSystemUpdateBuild(t)
	for _, mutate := range []func(*systemUpdateTestAgent){
		func(agent *systemUpdateTestAgent) { agent.version.Capabilities = nil },
		func(agent *systemUpdateTestAgent) {
			agent.version.Capabilities = []string{transport.AgentCapabilitySystemUpdateV1}
		},
		func(agent *systemUpdateTestAgent) { agent.version.Commit = strings.Repeat("e", 40) },
		func(agent *systemUpdateTestAgent) {
			agent.check.Error = "/var/lib/celikpanel-release-state/private failure"
		},
		func(agent *systemUpdateTestAgent) { agent.check.Supported = false },
	} {
		fixture := newSystemUpdateTestFixture(t)
		mutate(fixture.agent)
		recorder := httptest.NewRecorder()
		fixture.panel.handlePanelUpdateCheck(recorder, systemUpdateRequest(http.MethodGet, panelUpdateCheckPath, "", roleAdmin))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "/var/") || strings.Contains(recorder.Body.String(), "private failure") {
			t.Fatalf("private agent detail leaked: %s", recorder.Body.String())
		}
	}
}

func TestPanelUpdateCheckFailsClosedBeforeDiscoveryOnDNF(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.packageFamily = "dnf"
	response := rhelPolicyTestIdentity()
	fixture.agent.verifiedAPTAgentRPCFixture.hostPlatformResponse = &response
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateCheck(recorder, systemUpdateRequest(
		http.MethodGet, panelUpdateCheckPath, "", roleAdmin,
	))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.checkCalls != 0 || fixture.agent.startCalls != 0 {
		t.Fatalf("unsupported DNF host reached updater: checks=%d starts=%d",
			fixture.agent.checkCalls, fixture.agent.startCalls)
	}
}

func TestPanelUpdateStartBindsExactTargetAndRequestIdentity(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	request := fixture.agent.lastStart
	fixture.agent.mu.Unlock()
	if request.RequestID != updateTestRequestID || request.TargetVersion != updateTestTargetVersion ||
		request.TargetCommit != updateTestTargetCommit || request.TargetSequence != "14" ||
		request.TargetArchiveSHA256 != updateTestTargetSHA || request.TargetArchiveSize != "1048576" ||
		request.ExpectedCurrentVersion != updateTestCurrentVersion || request.ExpectedCurrentCommit != updateTestCurrentCommit {
		t.Fatalf("agent request lost exact identity: %+v", request)
	}

	recorder = httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody("v0.1.0-alpha.15"), roleAdmin,
	))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("changed target status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPanelUpdateStartSerializesWithServiceMutations(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.panel.serviceMutationMu.Lock()
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	fixture.panel.serviceMutationMu.Unlock()
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "PANEL_UPDATE_BUSY") {
		t.Fatalf("locked status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	_, err := fixture.panel.createServiceOperation(context.Background(), serviceOperationKindInstall, "certbot", "", serviceOperationActor{})
	if err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "PANEL_UPDATE_BUSY") {
		t.Fatalf("active operation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPanelUpdateAbandonPublishesExactReceiptUnderServiceMutationLock(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateAbandon(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateAbandonPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	request := fixture.agent.lastAbandon
	calls := fixture.agent.abandonCalls
	fixture.agent.mu.Unlock()
	if calls != 1 || request.RequestID != updateTestRequestID ||
		request.TargetCommit != updateTestTargetCommit ||
		request.ExpectedCurrentVersion != updateTestCurrentVersion ||
		request.ExpectedCurrentCommit != updateTestCurrentCommit {
		t.Fatalf("agent abandon lost exact identity: calls=%d request=%+v", calls, request)
	}
	var response panelUpdateStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Found || response.Status != "failed" || response.Target == nil ||
		!samePanelUpdateTarget(*response.Target, fixture.agent.checkTarget()) {
		t.Fatalf("abandon response = %#v", response)
	}

	fixture.panel.serviceMutationMu.Lock()
	recorder = httptest.NewRecorder()
	fixture.panel.handlePanelUpdateAbandon(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateAbandonPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	fixture.panel.serviceMutationMu.Unlock()
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "PANEL_UPDATE_BUSY") {
		t.Fatalf("locked abandon status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.abandonCalls != 1 {
		t.Fatalf("locked abandon reached agent %d times", fixture.agent.abandonCalls)
	}
}

func (a *systemUpdateTestAgent) checkTarget() panelUpdateTarget {
	a.mu.Lock()
	defer a.mu.Unlock()
	return panelUpdateTargetFromCheck(a.check)
}

func TestPanelUpdateStartRejectsAuthoritativeFailedReceipt(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.status = fixture.agent.abandon
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "PANEL_UPDATE_START_REFUSED") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.startCalls != 0 || fixture.agent.checkCalls != 0 {
		t.Fatalf("failed receipt was relaunched: checks=%d starts=%d", fixture.agent.checkCalls, fixture.agent.startCalls)
	}
}

func TestPanelUpdateStartReplayUsesExactStatusWithoutSecondStart(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.status = transport.SystemUpdateStatusResponse{
		Found: true, RequestID: updateTestRequestID, Status: "succeeded",
		TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
		TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
		TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.startCalls != 0 || fixture.agent.checkCalls != 0 || fixture.agent.statusCalls != 1 {
		t.Fatalf("replay calls status=%d check=%d start=%d", fixture.agent.statusCalls, fixture.agent.checkCalls, fixture.agent.startCalls)
	}
}

func TestPanelUpdateStartReplaySurvivesInstalledBuildTransition(t *testing.T) {
	oldVersion, oldCommit := buildVersion, buildCommit
	buildVersion, buildCommit = updateTestTargetVersion, updateTestTargetCommit
	t.Cleanup(func() { buildVersion, buildCommit = oldVersion, oldCommit })
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.version.Version = updateTestTargetVersion
	fixture.agent.version.Commit = updateTestTargetCommit
	fixture.agent.status = transport.SystemUpdateStatusResponse{
		Found: true, RequestID: updateTestRequestID, Status: "succeeded",
		TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
		TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
		TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.startCalls != 0 || fixture.agent.checkCalls != 0 || fixture.agent.statusCalls != 1 {
		t.Fatalf("replay calls status=%d check=%d start=%d",
			fixture.agent.statusCalls, fixture.agent.checkCalls, fixture.agent.startCalls)
	}
}

func TestPanelUpdateStartNeverRetriesPastAmbiguousStatusFailure(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.status = transport.SystemUpdateStatusResponse{Error: "open /var/lib/private/state: denied"}
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStart(recorder, systemUpdateRequest(
		http.MethodPost, panelUpdateStartPath, systemUpdateStartBody(updateTestTargetVersion), roleAdmin,
	))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fixture.agent.mu.Lock()
	defer fixture.agent.mu.Unlock()
	if fixture.agent.startCalls != 0 || fixture.agent.checkCalls != 0 {
		t.Fatalf("ambiguous status triggered a new attempt: checks=%d starts=%d", fixture.agent.checkCalls, fixture.agent.startCalls)
	}
	if strings.Contains(recorder.Body.String(), "/var/") || strings.Contains(recorder.Body.String(), "private") {
		t.Fatalf("status error leaked: %s", recorder.Body.String())
	}
}

func TestPanelUpdateStatusReturnsExactIdentityWithoutPrivatePaths(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.status = transport.SystemUpdateStatusResponse{
		Found: true, RequestID: updateTestRequestID, Status: "failed",
		TargetVersion: updateTestTargetVersion, TargetCommit: updateTestTargetCommit,
		TargetSequence: "14", TargetOS: "linux", TargetArch: "amd64",
		TargetArchiveSHA256: updateTestTargetSHA, TargetArchiveSize: "1048576",
		Error: "failed at /var/lib/celikpanel-release-state/private.tar.gz",
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateStatus(recorder, systemUpdateRequest(
		http.MethodGet, panelUpdateStatusPath+"?request_id="+updateTestRequestID, "", roleAdmin,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, updateTestTargetCommit) || !strings.Contains(body, `"sequence":"14"`) {
		t.Fatalf("exact status identity missing: %s", body)
	}
	if strings.Contains(body, "/var/") || strings.Contains(body, "private.tar.gz") {
		t.Fatalf("internal path leaked: %s", body)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q", recorder.Header().Get("Cache-Control"))
	}
}

func TestPanelUpdateValidationRejectsUnsafeCanonicalValues(t *testing.T) {
	valid := panelUpdateTarget{
		Version: updateTestTargetVersion, Commit: updateTestTargetCommit, Sequence: "14",
		OS: "linux", Arch: "amd64", ArchiveSHA256: updateTestTargetSHA, ArchiveSize: "1048576",
	}
	if !validPanelUpdateTarget(valid) {
		t.Fatal("known valid target rejected")
	}
	tests := []panelUpdateTarget{
		func() panelUpdateTarget { v := valid; v.Version = "v1.0.0+build"; return v }(),
		func() panelUpdateTarget { v := valid; v.Version = "v1.0.0-alpha.01"; return v }(),
		func() panelUpdateTarget { v := valid; v.Sequence = "01"; return v }(),
		func() panelUpdateTarget { v := valid; v.ArchiveSize = "2147483649"; return v }(),
		func() panelUpdateTarget { v := valid; v.OS = "windows"; return v }(),
	}
	for _, target := range tests {
		if validPanelUpdateTarget(target) {
			t.Fatalf("unsafe target accepted: %+v", target)
		}
	}
	if got := sanitizePanelUpdateSummary("failed /etc/shadow"); got != "" {
		t.Fatalf("path-bearing summary=%q", got)
	}
	if got := sanitizePanelUpdateSummary(strings.Repeat("x", 241)); got != "" {
		t.Fatalf("oversized summary length=%d", len(got))
	}
}

func TestPanelUpdateAgentFailureIsStableAndPrivate(t *testing.T) {
	withSystemUpdateBuild(t)
	fixture := newSystemUpdateTestFixture(t)
	fixture.agent.checkErr = errors.New("open /root/private-signing-key: permission denied")
	recorder := httptest.NewRecorder()
	fixture.panel.handlePanelUpdateCheck(recorder, systemUpdateRequest(http.MethodGet, panelUpdateCheckPath, "", roleAdmin))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "/root/") || strings.Contains(recorder.Body.String(), "signing-key") {
		t.Fatalf("agent error leaked: %s", recorder.Body.String())
	}
}

func TestPanelUpdateIdentityDeliberatelyExcludesInformationalPublishedAt(t *testing.T) {
	left := panelUpdateTarget{
		Version: updateTestTargetVersion, Commit: updateTestTargetCommit, Sequence: "14",
		OS: "linux", Arch: "amd64", ArchiveSHA256: updateTestTargetSHA, ArchiveSize: "1048576",
		PublishedAt: "2026-08-12T12:00:00Z",
	}
	right := left
	right.PublishedAt = ""
	if !samePanelUpdateTarget(left, right) {
		t.Fatal("informational publication timestamp changed the signed request identity")
	}
	right.ArchiveSize = "1048577"
	if samePanelUpdateTarget(left, right) {
		t.Fatal("security identity ignored archive size")
	}
}
