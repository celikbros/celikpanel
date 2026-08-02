package main

import (
	"context"
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
	"github.com/alicelik/celikpanel/internal/transport"
)

type mailMutationPanelAgent struct {
	mu                sync.Mutex
	addApplied        bool
	forwardingApplied bool
	queueActionOK     bool
	addCalls          int
	forwardingCalls   [][]transport.MailForwarding
}

func (a *mailMutationPanelAgent) AddMailAccount(
	_ *transport.MailAccount,
	response *transport.MailMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.addCalls++
	response.Applied = a.addApplied
	return nil
}

func (a *mailMutationPanelAgent) UpdateMailForwarding(
	request *transport.UpdateMailForwardingRequest,
	response *transport.MailMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copyState := append([]transport.MailForwarding(nil), request.Forwardings...)
	a.forwardingCalls = append(a.forwardingCalls, copyState)
	response.Applied = a.forwardingApplied
	return nil
}

func (a *mailMutationPanelAgent) PostfixQueueAction(
	_ *core.PostfixActionRequest,
	response *bool,
) error {
	*response = a.queueActionOK
	return nil
}

func (a *mailMutationPanelAgent) addCallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.addCalls
}

func (a *mailMutationPanelAgent) lastForwardingState() []transport.MailForwarding {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.forwardingCalls) == 0 {
		return nil
	}
	return append([]transport.MailForwarding(nil), a.forwardingCalls[len(a.forwardingCalls)-1]...)
}

func newMailMutationPanel(t *testing.T, agent *mailMutationPanelAgent) (*Panel, int) {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role)
		VALUES (6101, 'mail-admin', 'x', 'mail-admin@example.test', 'admin');
		INSERT INTO subscriptions (id, owner_id, name, max_email_accounts, status)
		VALUES (6102, 6101, 'Mail subscription', 50, 'active');
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (6103, 6102, 'example.com', 'active');
	`); err != nil {
		t.Fatalf("seed mail fixture: %v", err)
	}

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register mail test agent: %v", err)
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
		t.Fatalf("connect mail test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Panel{
		db: database,
		agentClient: transport.NewReconnectingClientWithContextConnector(
			client,
			connector,
		),
	}, 6103
}

func TestAddEmailAccountRejectsCrossTenantAddressBeforeAgent(t *testing.T) {
	agent := &mailMutationPanelAgent{addApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	request := httptest.NewRequest(http.MethodPost, "/mail/accounts",
		strings.NewReader(`{"address":"user@evil.test","password":"long-enough","quota_mb":100}`))
	response := httptest.NewRecorder()

	panel.handleAddEmailAccount(response, request, domainID)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if calls := agent.addCallCount(); calls != 0 {
		t.Fatalf("agent add calls = %d, want 0", calls)
	}
	var count int
	if err := panel.db.GetDB().QueryRow("SELECT COUNT(*) FROM email_accounts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("persisted account count = %d", count)
	}
}

func TestAddEmailAccountRollsBackWhenAgentDoesNotConfirm(t *testing.T) {
	agent := &mailMutationPanelAgent{addApplied: false}
	panel, domainID := newMailMutationPanel(t, agent)
	request := httptest.NewRequest(http.MethodPost, "/mail/accounts",
		strings.NewReader(`{"address":"user","password":"long-enough","quota_mb":100}`))
	response := httptest.NewRecorder()

	panel.handleAddEmailAccount(response, request, domainID)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if calls := agent.addCallCount(); calls != 1 {
		t.Fatalf("agent add calls = %d, want 1", calls)
	}
	var count int
	if err := panel.db.GetDB().QueryRow("SELECT COUNT(*) FROM email_accounts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unconfirmed account remained in database: %d", count)
	}
}

func TestAddEmailForwardingRollsBackWhenAgentDoesNotConfirm(t *testing.T) {
	agent := &mailMutationPanelAgent{forwardingApplied: false}
	panel, domainID := newMailMutationPanel(t, agent)
	request := httptest.NewRequest(http.MethodPost, "/mail/forwardings",
		strings.NewReader(`{"source":"sales","destination":"team@example.net"}`))
	response := httptest.NewRecorder()

	panel.handleAddEmailForwarding(response, request, domainID)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var count int
	if err := panel.db.GetDB().QueryRow("SELECT COUNT(*) FROM email_forwardings").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unconfirmed forwarding remained in database: %d", count)
	}
}

func TestForwardingPublishPreservesExplicitAndCatchAllState(t *testing.T) {
	agent := &mailMutationPanelAgent{forwardingApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO email_forwardings (domain_id, source, destination)
		VALUES (?, 'existing@example.com', 'archive@example.net');
		INSERT INTO mail_catch_all (domain_id, destination)
		VALUES (?, 'catch@example.net');
	`, domainID, domainID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mail/forwardings",
		strings.NewReader(`{"source":"sales","destination":"team@example.net"}`))
	response := httptest.NewRecorder()

	panel.handleAddEmailForwarding(response, request, domainID)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	state := agent.lastForwardingState()
	want := map[string]string{
		"existing@example.com": "archive@example.net",
		"sales@example.com":    "team@example.net",
		"@example.com":         "catch@example.net",
	}
	if len(state) != len(want) {
		t.Fatalf("published forwarding count = %d, state = %+v", len(state), state)
	}
	for _, forwarding := range state {
		if want[forwarding.Source] != forwarding.Destination {
			t.Fatalf("unexpected forwarding state: %+v", state)
		}
		delete(want, forwarding.Source)
	}
	if len(want) != 0 {
		t.Fatalf("missing forwarding state: %+v", want)
	}
}

func TestPostfixQueueActionRequiresPositiveAgentConfirmation(t *testing.T) {
	agent := &mailMutationPanelAgent{queueActionOK: false}
	panel, _ := newMailMutationPanel(t, agent)
	request := httptest.NewRequest(http.MethodPost, "/mail/queue",
		strings.NewReader(`{"action":"flush"}`))
	response := httptest.NewRecorder()

	panel.handlePostfixQueue(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
