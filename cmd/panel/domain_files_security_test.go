package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type fileMutationTestAgent struct {
	writeSuccess bool
	lastWrite    transport.WriteFileRequest
}

func (a *fileMutationTestAgent) WriteFile(req *transport.WriteFileRequest, reply *bool) error {
	a.lastWrite = *req
	*reply = a.writeSuccess
	return nil
}

func newFileMutationTestPanel(t *testing.T, agent *fileMutationTestAgent) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register file test agent: %v", err)
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
		t.Fatalf("connect file test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{agentClient: transport.NewReconnectingClientWithContextConnector(client, connector)}
}

func TestSiteDocrootRejectsForeignStoredPath(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	if _, err := panel.db.GetDB().Exec(
		`UPDATE sites SET document_root = '/var/www/foreign' WHERE domain_id = ?`,
		domainID,
	); err != nil {
		t.Fatalf("set foreign document root: %v", err)
	}

	if _, err := panel.siteDocroot(context.Background(), domainID); err == nil {
		t.Fatal("foreign stored document root was accepted")
	}
}

func TestSiteDocrootRejectsCanonicalEquivalentStoredPath(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	if _, err := panel.db.GetDB().Exec(
		`UPDATE sites SET document_root = document_root || '/.' WHERE domain_id = ?`,
		domainID,
	); err != nil {
		t.Fatalf("set non-canonical document root: %v", err)
	}

	if _, err := panel.siteDocroot(context.Background(), domainID); err == nil {
		t.Fatal("canonical-equivalent stored document root was accepted")
	}
}

func TestSiteFileScopeLoadsSubscriptionThroughDomain(t *testing.T) {
	panel, domainID := newDomainAliasFixture(t)
	var wantSubscriptionID int
	if err := panel.db.GetDB().QueryRow(
		`SELECT subscription_id FROM domains WHERE id = ?`,
		domainID,
	).Scan(&wantSubscriptionID); err != nil {
		t.Fatalf("load fixture subscription: %v", err)
	}

	scope, err := panel.siteFileScope(context.Background(), domainID)
	if err != nil {
		t.Fatalf("load site file scope: %v", err)
	}
	if scope.SubscriptionID != wantSubscriptionID || scope.DomainID != domainID {
		t.Fatalf("unexpected immutable scope: %+v", scope)
	}
}

func TestFileMutationFalseResultIsServerError(t *testing.T) {
	panel := newFileMutationTestPanel(t, &fileMutationTestAgent{writeSuccess: false})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/1/files",
		strings.NewReader(`{"action":"write","content":"x"}`),
	)

	panel.handleFileAction(recorder, request, "index.html", domainFileScope{
		SubscriptionID: 7,
		DomainID:       1,
		Root:           "/site",
	})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}

func TestFileRenameRejectsSiblingPrefixEscape(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/1/files",
		strings.NewReader(`{"action":"rename","new_path":"../site-escape/file"}`),
	)

	new(Panel).handleFileAction(recorder, request, "file", domainFileScope{
		SubscriptionID: 7,
		DomainID:       1,
		Root:           "/site",
	})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestFileUploadRejectsPathAsFileName(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/1/files",
		strings.NewReader(`{"action":"upload","file_name":"../escape","content":""}`),
	)

	new(Panel).handleFileAction(recorder, request, "", domainFileScope{
		SubscriptionID: 7,
		DomainID:       1,
		Root:           "/site",
	})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestFileMutationPassesImmutableSiteIdentityAndRelativePath(t *testing.T) {
	agent := &fileMutationTestAgent{writeSuccess: true}
	panel := newFileMutationTestPanel(t, agent)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/domains/19/files",
		strings.NewReader(`{"action":"write","content":"x"}`),
	)

	panel.handleFileAction(recorder, request, "assets/index.html", domainFileScope{
		SubscriptionID: 7,
		DomainID:       19,
		Root:           "/var/www/celikpanel/subscriptions/7/sites/19/public_html",
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if agent.lastWrite.SubscriptionID != 7 || agent.lastWrite.DomainID != 19 || agent.lastWrite.Path != "assets/index.html" {
		t.Fatalf("unexpected write identity: %+v", agent.lastWrite)
	}
}
