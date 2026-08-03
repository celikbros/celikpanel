package main

import (
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

type FileManagerTestScope struct {
	SubscriptionID int
	DomainID       int
	Path           string
}

type FileManagerTestInfo struct {
	Name        string
	Path        string
	IsDir       bool
	Size        int64
	Permissions string
	ModTime     time.Time
}

type FileManagerTestListResponse struct {
	Path  string
	Files []FileManagerTestInfo
}

type FileManagerTestReadResponse struct {
	Path     string
	Content  string
	Size     int64
	IsBinary bool
}

type fileManagerTestAgent struct {
	mu        sync.Mutex
	listCalls []FileManagerTestScope
	readCalls []FileManagerTestScope
}

func (a *fileManagerTestAgent) ListFiles(req *FileManagerTestScope, resp *FileManagerTestListResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listCalls = append(a.listCalls, *req)
	resp.Path = req.Path
	resp.Files = []FileManagerTestInfo{{
		Name:        "child.txt",
		Path:        req.Path + "/child.txt",
		Size:        5,
		Permissions: "-rw-r--r--",
		ModTime:     time.Unix(1, 0),
	}}
	return nil
}

func (a *fileManagerTestAgent) ReadFile(req *FileManagerTestScope, resp *FileManagerTestReadResponse) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readCalls = append(a.readCalls, *req)
	resp.Path = req.Path
	resp.Content = "hello"
	resp.Size = 5
	return nil
}

func attachFileManagerTestAgent(t *testing.T, p *Panel, agent *fileManagerTestAgent) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register file-manager test agent: %v", err)
	}
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	p.agentClient = transport.NewReconnectingClient(client)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})
}

func newFileManagerPanelFixture(t *testing.T) (*Panel, int, int) {
	t.Helper()
	p := newDNSPanelForTest(t)
	db := p.db.GetDB()
	user, err := db.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('files-owner', 'hash', 'files-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := user.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := db.Exec(`
		INSERT INTO subscriptions (owner_id, name) VALUES (?, 'Files test')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID64, err := subscription.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	domain, err := db.Exec(`
		INSERT INTO domains (subscription_id, name) VALUES (?, 'files.example.test')`,
		subscriptionID64)
	if err != nil {
		t.Fatal(err)
	}
	domainID64, err := domain.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := hostingpath.DocumentRoot(int(subscriptionID64), int(domainID64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO sites (domain_id, document_root) VALUES (?, ?)`,
		domainID64, canonicalRoot); err != nil {
		t.Fatal(err)
	}
	// Simulate a poisoned legacy row after disabling only the fixture's UPDATE
	// guard. File-manager handlers must ignore this text and send identities.
	if _, err := db.Exec(`DROP TRIGGER trg_sites_document_root_identity_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sites SET document_root = '/etc' WHERE domain_id = ?`, domainID64); err != nil {
		t.Fatal(err)
	}
	return p, int(subscriptionID64), int(domainID64)
}

func TestDomainFilesSendsIdentityAndCanonicalRelativePath(t *testing.T) {
	p, subscriptionID, domainID := newFileManagerPanelFixture(t)
	agent := &fileManagerTestAgent{}
	attachFileManagerTestAgent(t, p, agent)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/files?path="+url.QueryEscape("/nested/./deeper"),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDomainFiles(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.listCalls) != 1 {
		t.Fatalf("list calls = %+v", agent.listCalls)
	}
	call := agent.listCalls[0]
	if call.SubscriptionID != subscriptionID || call.DomainID != domainID ||
		call.Path != "nested/deeper" {
		t.Fatalf("unexpected RPC request: %+v", call)
	}
	if strings.Contains(recorder.Body.String(), "/etc") {
		t.Fatalf("DB document_root leaked into response/RPC: %s", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"/nested/deeper/child.txt"`) {
		t.Fatalf("UI path was not restored: %s", recorder.Body.String())
	}
}

func TestDomainFilesRejectsTraversalBeforeRPC(t *testing.T) {
	p, _, domainID := newFileManagerPanelFixture(t)
	agent := &fileManagerTestAgent{}
	attachFileManagerTestAgent(t, p, agent)

	for _, candidate := range []string{
		"/../sibling/secret",
		"../sibling-prefix/secret",
		"//etc/passwd",
		`..\outside`,
	} {
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/v1/domains/"+strconv.Itoa(domainID)+"/files?path="+url.QueryEscape(candidate),
			nil,
		)
		recorder := httptest.NewRecorder()
		p.handleDomainFiles(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%q status = %d, body=%s", candidate, recorder.Code, recorder.Body.String())
		}
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.listCalls) != 0 {
		t.Fatalf("invalid paths reached agent: %+v", agent.listCalls)
	}
}

func TestDomainFileDownloadUsesSafeDispositionAndRelativeRPC(t *testing.T) {
	p, subscriptionID, domainID := newFileManagerPanelFixture(t)
	agent := &fileManagerTestAgent{}
	attachFileManagerTestAgent(t, p, agent)

	const relativePath = `/reports/weird "name".txt`
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/files/download?path="+url.QueryEscape(relativePath),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDomainFileDownload(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "hello" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}

	disposition := recorder.Header().Get("Content-Disposition")
	if strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("unsafe Content-Disposition: %q", disposition)
	}
	mediaType, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		t.Fatalf("parse Content-Disposition %q: %v", disposition, err)
	}
	if mediaType != "attachment" || params["filename"] != `weird "name".txt` {
		t.Fatalf("unexpected Content-Disposition: %q, %#v", mediaType, params)
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.readCalls) != 1 {
		t.Fatalf("read calls = %+v", agent.readCalls)
	}
	call := agent.readCalls[0]
	if call.SubscriptionID != subscriptionID || call.DomainID != domainID ||
		call.Path != `reports/weird "name".txt` {
		t.Fatalf("unexpected read request: %+v", call)
	}
}

type repeatingSpaceReader struct{}

func (repeatingSpaceReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func TestDecodeFileActionRejectsOversizedAndUnknownInput(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		body := io.NopCloser(io.LimitReader(repeatingSpaceReader{}, maxFileActionRequestBytes+1))
		request := httptest.NewRequest(http.MethodPost, "/", body)
		recorder := httptest.NewRecorder()
		var target map[string]any
		if err := decodeFileAction(recorder, request, &target); err == nil {
			t.Fatal("oversized request succeeded")
		}
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/",
			strings.NewReader(`{"action":"read","absolute_path":"/etc/passwd"}`),
		)
		recorder := httptest.NewRecorder()
		var target struct {
			Action string `json:"action"`
		}
		if err := decodeFileAction(recorder, request, &target); err == nil {
			t.Fatal("unknown field succeeded")
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestNormalizePanelFilePathAndAttachmentDisposition(t *testing.T) {
	for input, want := range map[string]string{
		"":          ".",
		"/":         ".",
		"/a/b":      "a/b",
		"a/./b":     "a/b",
		"/a/../b":   "b",
		"plain.txt": "plain.txt",
	} {
		got, err := normalizePanelFilePath(input)
		if err != nil || got != want {
			t.Errorf("normalizePanelFilePath(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"/../escape", "//absolute", "/a/../../escape", `\windows`} {
		if _, err := normalizePanelFilePath(input); err == nil {
			t.Errorf("normalizePanelFilePath(%q) succeeded", input)
		}
	}

	disposition := safeAttachmentDisposition(`nested/a"; filename=evil.txt`)
	if strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("unsafe disposition: %q", disposition)
	}
}

func TestFileManagerSubscriptionRejectsUnknownDomain(t *testing.T) {
	p := newDNSPanelForTest(t)
	if _, err := p.fileManagerSubscriptionID(context.Background(), 999999); err == nil {
		t.Fatal("unknown domain succeeded")
	}
}
