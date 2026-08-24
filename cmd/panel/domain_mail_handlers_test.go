package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type mailMutationPanelAgent struct {
	verifiedAPTAgentRPCFixture

	mu                sync.Mutex
	addApplied        bool
	forwardingApplied bool
	passwordApplied   bool
	passwordErr       error
	queueActionOK     bool
	addCalls          int
	forwardingCalls   [][]transport.MailForwarding
	passwordCalls     []transport.UpdateMailPasswordRequest
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

func (a *mailMutationPanelAgent) UpdateMailPassword(
	request *transport.UpdateMailPasswordRequest,
	response *transport.MailMutationResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.passwordCalls = append(a.passwordCalls, *request)
	response.Applied = a.passwordApplied
	return a.passwordErr
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

func (a *mailMutationPanelAgent) passwordCallState() []transport.UpdateMailPasswordRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]transport.UpdateMailPasswordRequest(nil), a.passwordCalls...)
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
		db:           database,
		pkgFamilyVal: "apt",
		agentClient: transport.NewReconnectingClientWithContextConnector(
			client,
			connector,
		),
	}, 6103
}

func TestMailMutationsRejectDomainDeletionPending(t *testing.T) {
	agent := &mailMutationPanelAgent{addApplied: true, forwardingApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb)
		VALUES (6201, ?, 'user@example.com', 'managed-by-agent', 100);
		INSERT INTO email_forwardings (id, domain_id, source, destination)
		VALUES (6202, ?, 'alias@example.com', 'archive@other.test');
		INSERT INTO mail_catch_all (domain_id, destination)
		VALUES (?, 'catch@other.test');
		INSERT INTO domain_deletion_operations (domain_id, previous_status)
		VALUES (?, 'active');
		UPDATE domains SET status = 'pending' WHERE id = ?;
	`, domainID, domainID, domainID, domainID, domainID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		method  string
		target  string
		body    string
		handler func(http.ResponseWriter, *http.Request, int)
	}{
		{
			name: "add account", method: http.MethodPost, target: "/mail/accounts",
			body:    `{"address":"new@example.com","password":"long-enough","quota_mb":200}`,
			handler: panel.handleAddEmailAccount,
		},
		{
			name: "delete account", method: http.MethodDelete, target: "/mail/accounts?id=6201",
			handler: panel.handleDeleteEmailAccount,
		},
		{
			name: "update quota", method: http.MethodPut, target: "/mail/accounts",
			body: `{"id":6201,"quota_mb":300}`, handler: panel.handleUpdateEmailQuota,
		},
		{
			name: "update password", method: http.MethodPut, target: "/mail/accounts/password",
			body: `{"id":6201,"new_password":"replacement-password"}`, handler: panel.handleUpdateEmailPassword,
		},
		{
			name: "add forwarding", method: http.MethodPost, target: "/mail/forwardings",
			body:    `{"source":"newalias","destination":"dest@other.test"}`,
			handler: panel.handleAddEmailForwarding,
		},
		{
			name: "delete forwarding", method: http.MethodDelete, target: "/mail/forwardings?id=6202",
			handler: panel.handleDeleteEmailForwarding,
		},
		{
			name: "put catch all", method: http.MethodPut, target: "/mail/catch-all",
			body: `{"destination":"newcatch@other.test"}`, handler: panel.handleMailCatchAll,
		},
		{
			name: "delete catch all", method: http.MethodDelete, target: "/mail/catch-all",
			handler: panel.handleMailCatchAll,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, strings.NewReader(test.body))
			recorder := httptest.NewRecorder()
			test.handler(recorder, request, domainID)
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != errCodeDomainDeletionPending {
				t.Fatalf("error body = %+v", body)
			}
		})
	}

	var quota int
	if err := panel.db.GetDB().QueryRow(
		`SELECT quota_mb FROM email_accounts WHERE id = 6201`,
	).Scan(&quota); err != nil {
		t.Fatal(err)
	}
	var forwardingCount int
	if err := panel.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM email_forwardings WHERE id = 6202`,
	).Scan(&forwardingCount); err != nil {
		t.Fatal(err)
	}
	var catchAll string
	if err := panel.db.GetDB().QueryRow(
		`SELECT destination FROM mail_catch_all WHERE domain_id = ?`, domainID,
	).Scan(&catchAll); err != nil {
		t.Fatal(err)
	}
	agent.mu.Lock()
	addCalls := agent.addCalls
	forwardingCalls := len(agent.forwardingCalls)
	passwordCalls := len(agent.passwordCalls)
	agent.mu.Unlock()
	if quota != 100 || forwardingCount != 1 || catchAll != "catch@other.test" ||
		addCalls != 0 || forwardingCalls != 0 || passwordCalls != 0 {
		t.Fatalf(
			"quota=%d forwarding=%d catchall=%q addCalls=%d forwardingCalls=%d passwordCalls=%d",
			quota, forwardingCount, catchAll, addCalls, forwardingCalls, passwordCalls,
		)
	}
}

func TestUpdateEmailPasswordSuccessUsesStoredIdentityAndAuditsWithoutSecret(t *testing.T) {
	agent := &mailMutationPanelAgent{passwordApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb)
		VALUES (6401, ?, 'User@example.com', 'sentinel-hash', 100)
	`, domainID); err != nil {
		t.Fatal(err)
	}
	previousCommit := buildCommit
	buildCommit = "mail-password-panel-test"
	t.Cleanup(func() { buildCommit = previousCommit })

	request := httptest.NewRequest(http.MethodPut, "/mail/accounts/password",
		strings.NewReader(`{"id":6401,"new_password":"replacement-password"}`))
	request = request.WithContext(context.WithValue(request.Context(), callerKey, &Caller{
		ID: 6101, Role: roleAdmin, AccountType: core.AccountTypeAccount,
	}))
	response := httptest.NewRecorder()
	panel.handleUpdateEmailPassword(response, request, domainID)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", got)
	}
	if body := response.Body.String(); strings.Contains(body, "replacement-password") ||
		strings.Contains(strings.ToLower(body), "user@example.com") {
		t.Fatalf("sensitive identity leaked in response: %s", body)
	}
	calls := agent.passwordCallState()
	if len(calls) != 1 || calls[0].ExpectedBuildCommit != "mail-password-panel-test" ||
		calls[0].Email != "user@example.com" ||
		calls[0].NewPassword != "replacement-password" {
		t.Fatalf("password calls=%+v", calls)
	}
	var passwordHash string
	if err := panel.db.GetDB().QueryRow(
		`SELECT password_hash FROM email_accounts WHERE id = 6401`,
	).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash != "sentinel-hash" {
		t.Fatalf("database password hash changed to %q", passwordHash)
	}
	var action, resourceType string
	var resourceID, userID int
	if err := panel.db.GetDB().QueryRow(`
		SELECT action, resource_type, resource_id, user_id
		FROM audit_logs ORDER BY id DESC LIMIT 1
	`).Scan(&action, &resourceType, &resourceID, &userID); err != nil {
		t.Fatal(err)
	}
	if action != "mail.account.password.rotate" || resourceType != "email_account" ||
		resourceID != 6401 || userID != 6101 {
		t.Fatalf("audit=%q/%q/%d user=%d", action, resourceType, resourceID, userID)
	}
	if strings.Contains(strings.ToLower(action), "user@example.com") ||
		strings.Contains(action, "replacement-password") {
		t.Fatalf("sensitive identity leaked in audit action: %q", action)
	}
}

func mailPasswordInvalidBody(t *testing.T, name, fallback string) string {
	t.Helper()
	var password string
	switch name {
	case "carriage return":
		password = "valid-pass" + string(rune(13)) + "word"
	case "line feed":
		password = "valid-pass" + string(rune(10)) + "word"
	case "nul":
		password = "valid-pass" + string(rune(0)) + "word"
	default:
		return fallback
	}
	body, err := json.Marshal(map[string]any{
		"id":           6401,
		"new_password": password,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestUpdateEmailPasswordRejectsInvalidBodiesBeforeAgent(t *testing.T) {
	agent := &mailMutationPanelAgent{passwordApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb)
		VALUES (6401, ?, 'user@example.com', 'sentinel-hash', 100)
	`, domainID); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"id":6401,`},
		{name: "unknown field", body: `{"id":6401,"new_password":"replacement-password","email":"other@example.com"}`},
		{name: "trailing value", body: `{"id":6401,"new_password":"replacement-password"} {}`},
		{name: "missing id", body: `{"new_password":"replacement-password"}`},
		{name: "short password", body: `{"id":6401,"new_password":"short"}`},
		{name: "long password", body: `{"id":6401,"new_password":"` + strings.Repeat("x", transport.MaxMailboxPasswordBytes+1) + `"}`},
		{name: "carriage return"},
		{name: "line feed"},
		{name: "nul"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/mail/accounts/password",
				strings.NewReader(mailPasswordInvalidBody(t, test.name, test.body)))
			panel.handleUpdateEmailPassword(response, request, domainID)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control=%q, want no-store", got)
			}
		})
	}
	if calls := agent.passwordCallState(); len(calls) != 0 {
		t.Fatalf("agent received invalid requests: %+v", calls)
	}
}

func TestUpdateEmailPasswordHidesUnknownAndCrossDomainAccounts(t *testing.T) {
	agent := &mailMutationPanelAgent{passwordApplied: true}
	panel, domainID := newMailMutationPanel(t, agent)
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (6402, 6102, 'other.test', 'active');
		INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb)
		VALUES (6403, 6402, 'user@other.test', 'sentinel-hash', 100)
	`); err != nil {
		t.Fatal(err)
	}
	var firstBody string
	for index, accountID := range []int{6403, 999999} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/mail/accounts/password",
			strings.NewReader(`{"id":`+strconv.Itoa(accountID)+`,"new_password":"replacement-password"}`))
		panel.handleUpdateEmailPassword(response, request, domainID)
		if response.Code != http.StatusNotFound {
			t.Fatalf("id=%d status=%d body=%s", accountID, response.Code, response.Body.String())
		}
		if index == 0 {
			firstBody = response.Body.String()
		} else if response.Body.String() != firstBody {
			t.Fatalf("cross-domain and unknown responses differ: %q vs %q", firstBody, response.Body.String())
		}
	}
	if calls := agent.passwordCallState(); len(calls) != 0 {
		t.Fatalf("agent received hidden account requests: %+v", calls)
	}
}

func TestUpdateEmailPasswordRequiresPositiveAgentConfirmationAndSanitizesErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		applied bool
		err     error
	}{
		{name: "not applied"},
		{
			name: "agent error",
			err:  errors.New("leaked user@example.com and replacement-password"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent := &mailMutationPanelAgent{passwordApplied: test.applied, passwordErr: test.err}
			panel, domainID := newMailMutationPanel(t, agent)
			if _, err := panel.db.GetDB().Exec(`
				INSERT INTO email_accounts (id, domain_id, address, password_hash, quota_mb)
				VALUES (6401, ?, 'user@example.com', 'sentinel-hash', 100)
			`, domainID); err != nil {
				t.Fatal(err)
			}
			var logs strings.Builder
			previousWriter := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previousWriter) })

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/mail/accounts/password",
				strings.NewReader(`{"id":6401,"new_password":"replacement-password"}`))
			panel.handleUpdateEmailPassword(response, request, domainID)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "replacement-password") ||
				strings.Contains(strings.ToLower(response.Body.String()), "user@example.com") ||
				strings.Contains(logs.String(), "replacement-password") ||
				strings.Contains(strings.ToLower(logs.String()), "user@example.com") {
				t.Fatalf("sensitive value leaked: body=%q logs=%q", response.Body.String(), logs.String())
			}
			var auditCount int
			if err := panel.db.GetDB().QueryRow(
				`SELECT COUNT(*) FROM audit_logs WHERE action = 'mail.account.password.rotate'`,
			).Scan(&auditCount); err != nil {
				t.Fatal(err)
			}
			if auditCount != 0 {
				t.Fatalf("failed rotation wrote %d success audits", auditCount)
			}
		})
	}
}

func TestLoadAllForwardingsExcludesDeletingSourcesButPreservesTargetDestinations(t *testing.T) {
	panel, deletingDomainID := newMailMutationPanel(t, &mailMutationPanelAgent{})
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (6301, 6102, 'other.test', 'active');
		INSERT INTO email_forwardings (domain_id, source, destination)
		VALUES
			(?, 'alias@example.com', 'outside@other.test'),
			(6301, 'keep@other.test', 'archive@example.com');
		INSERT INTO mail_catch_all (domain_id, destination)
		VALUES
			(?, 'catch-outside@other.test'),
			(6301, 'catch@example.com');
		INSERT INTO domain_deletion_operations (domain_id, previous_status)
		VALUES (?, 'active');
		UPDATE domains SET status = 'pending' WHERE id = ?;
	`, deletingDomainID, deletingDomainID, deletingDomainID, deletingDomainID); err != nil {
		t.Fatal(err)
	}
	forwardings, err := loadAllForwardings(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if len(forwardings) != 2 {
		t.Fatalf("forwardings = %+v", forwardings)
	}
	got := map[string]string{}
	for _, forwarding := range forwardings {
		got[forwarding.Source] = forwarding.Destination
	}
	if got["keep@other.test"] != "archive@example.com" ||
		got["@other.test"] != "catch@example.com" {
		t.Fatalf("forwardings = %+v", forwardings)
	}
	if _, exists := got["alias@example.com"]; exists {
		t.Fatalf("deleting source leaked into snapshot: %+v", forwardings)
	}
	if _, exists := got["@example.com"]; exists {
		t.Fatalf("deleting catch-all leaked into snapshot: %+v", forwardings)
	}
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
