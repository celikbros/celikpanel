package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	domainPHPTestOwnerID  = 7351
	domainPHPTestSubID    = 7352
	domainPHPTestDomainID = 7353
	domainPHPTestSiteID   = 7354
)

type domainPHPHandlerTestAgent struct {
	verifiedAPTAgentRPCFixture

	mu              sync.Mutex
	instances       []core.ServiceInstance
	listErr         error
	listCalls       int
	poolConfigCalls int
	migrateCalls    int
}

func (a *domainPHPHandlerTestAgent) ListServiceInstances(
	request *transport.ServiceInstancesRequest,
	reply *transport.ServiceInstancesResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listCalls++
	if request.ID != "php-fpm" {
		return fmt.Errorf("unexpected service instance ID %q", request.ID)
	}
	if a.listErr != nil {
		return a.listErr
	}
	reply.Instances = append([]core.ServiceInstance(nil), a.instances...)
	return nil
}

func (a *domainPHPHandlerTestAgent) GetPHPPoolConfig(
	request transport.GetPHPPoolConfigRequest,
	reply *transport.PHPPoolConfig,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.poolConfigCalls++
	if request.Version != "8.3" || request.PoolName != "site7354" {
		return fmt.Errorf("unexpected pool request: %#v", request)
	}
	*reply = transport.PHPPoolConfig{
		Name:        request.PoolName,
		User:        request.PoolName,
		Group:       request.PoolName,
		Listen:      "/var/run/php/php8.3-fpm-site7354.sock",
		ListenOwner: "www-data",
		ListenGroup: "www-data",
		ListenMode:  "0660",
		PM:          "ondemand",
	}
	return nil
}

func (a *domainPHPHandlerTestAgent) MigratePHPPool(
	_ transport.MigratePHPPoolRequest,
	_ *transport.Empty,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.migrateCalls++
	return nil
}

func (a *domainPHPHandlerTestAgent) callCounts() (list, poolConfig, migrate int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listCalls, a.poolConfigCalls, a.migrateCalls
}

type domainPHPHandlerFixture struct {
	panel *Panel
	agent *domainPHPHandlerTestAgent
}

func newDomainPHPHandlerFixture(
	t *testing.T,
	instances []core.ServiceInstance,
) domainPHPHandlerFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.sqlite"))
	if err != nil {
		t.Fatalf("open PHP handler test database: %v", err)
	}
	t.Cleanup(database.Close)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES (7351, 'php-owner', 'hash', 'php-owner@example.test', 'customer', 'active');
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES (7352, 7351, 'PHP subscription', 'active');
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES (7353, 7352, 'php.example.test', 'active');
		INSERT INTO sites (
			id, domain_id, document_root, project_type, php_version,
			php_fpm_socket, status
		) VALUES (
			7354, 7353, '/var/www/php.example.test', 'php', '8.3',
			'/var/run/php/php8.3-fpm-site7354.sock', 'active'
		);
	`); err != nil {
		t.Fatalf("seed PHP handler test database: %v", err)
	}

	agent := &domainPHPHandlerTestAgent{instances: instances}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register PHP handler test agent: %v", err)
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
		t.Fatalf("connect PHP handler test agent: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return domainPHPHandlerFixture{
		panel: &Panel{
			db: database,
			agentClient: transport.NewReconnectingClientWithContextConnector(
				client,
				connector,
			),
		},
		agent: agent,
	}
}

func domainPHPHandlerRequest(method, body string, caller *Caller) *http.Request {
	request := httptest.NewRequest(
		method,
		fmt.Sprintf("/api/v1/domains/%d/php", domainPHPTestDomainID),
		strings.NewReader(body),
	)
	return request.WithContext(context.WithValue(request.Context(), callerKey, caller))
}

func domainPHPTestOwnerCaller() *Caller {
	return &Caller{
		ID:          domainPHPTestOwnerID,
		Role:        roleCustomer,
		AccountType: core.AccountTypeAccount,
		CustomerID:  domainPHPTestOwnerID,
	}
}

func TestDomainPHPGetReturnsStableManagedVersionsWithoutMutation(t *testing.T) {
	fixture := newDomainPHPHandlerFixture(t, []core.ServiceInstance{
		{Version: "8.2", Managed: true},
		{Version: "8.10", Managed: true},
		{Version: "8.9", Managed: true},
		{Version: "8.10", Managed: true},
		{Version: "../../8.11", Managed: true},
		{Version: "9.0", Managed: false},
	})
	recorder := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(
		recorder,
		domainPHPHandlerRequest(http.MethodGet, "", domainPHPTestOwnerCaller()),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	var response DomainPHPSettingsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode PHP settings response: %v", err)
	}
	wantVersions := []string{"8.10", "8.9", "8.3", "8.2"}
	if response.AvailableVersions == nil || !reflect.DeepEqual(response.AvailableVersions, wantVersions) {
		t.Fatalf("available versions = %#v, want %#v", response.AvailableVersions, wantVersions)
	}
	if response.PHPVersion != "8.3" || response.PoolConfig == nil {
		t.Fatalf("PHP settings response = %#v", response)
	}
	if list, pool, migrate := fixture.agent.callCounts(); list != 1 || pool != 1 || migrate != 0 {
		t.Fatalf("agent calls = list %d pool %d migrate %d, want 1/1/0", list, pool, migrate)
	}
	var storedVersion, storedSocket string
	if err := fixture.panel.db.GetDB().QueryRow(`
		SELECT php_version, php_fpm_socket FROM sites WHERE id = ?
	`, domainPHPTestSiteID).Scan(&storedVersion, &storedSocket); err != nil {
		t.Fatalf("read stored PHP identity: %v", err)
	}
	if storedVersion != "8.3" || storedSocket != "/var/run/php/php8.3-fpm-site7354.sock" {
		t.Fatalf("GET changed PHP identity to %q/%q", storedVersion, storedSocket)
	}
}

func TestDomainPHPGetAuthorizesBeforeVersionDiscovery(t *testing.T) {
	fixture := newDomainPHPHandlerFixture(t, []core.ServiceInstance{{Version: "8.4", Managed: true}})
	foreignCaller := &Caller{
		ID:          7999,
		Role:        roleCustomer,
		AccountType: core.AccountTypeAccount,
		CustomerID:  7999,
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(
		recorder,
		domainPHPHandlerRequest(http.MethodGet, "", foreignCaller),
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("foreign GET status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	if list, pool, migrate := fixture.agent.callCounts(); list != 0 || pool != 0 || migrate != 0 {
		t.Fatalf("foreign GET reached agent: list %d pool %d migrate %d", list, pool, migrate)
	}
}

func TestDomainPHPGetKeepsKnownCurrentVersionWhenDiscoveryFails(t *testing.T) {
	fixture := newDomainPHPHandlerFixture(t, nil)
	fixture.agent.listErr = fmt.Errorf("PHP discovery unavailable")
	recorder := httptest.NewRecorder()
	fixture.panel.handleDomainSubroute(
		recorder,
		domainPHPHandlerRequest(http.MethodGet, "", domainPHPTestOwnerCaller()),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%q", recorder.Code, recorder.Body.String())
	}
	var response DomainPHPSettingsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode PHP settings response: %v", err)
	}
	wantVersions := []string{"8.3"}
	if !reflect.DeepEqual(response.AvailableVersions, wantVersions) {
		t.Fatalf("available versions = %#v, want %#v", response.AvailableVersions, wantVersions)
	}
	if list, pool, migrate := fixture.agent.callCounts(); list != 1 || pool != 1 || migrate != 0 {
		t.Fatalf("agent calls = list %d pool %d migrate %d, want 1/1/0", list, pool, migrate)
	}
	var storedVersion, storedSocket string
	if err := fixture.panel.db.GetDB().QueryRow(`
		SELECT php_version, php_fpm_socket FROM sites WHERE id = ?
	`, domainPHPTestSiteID).Scan(&storedVersion, &storedSocket); err != nil {
		t.Fatalf("read stored PHP identity: %v", err)
	}
	if storedVersion != "8.3" || storedSocket != "/var/run/php/php8.3-fpm-site7354.sock" {
		t.Fatalf("GET changed PHP identity to %q/%q", storedVersion, storedSocket)
	}
}

func TestDomainPHPPostRejectsUnsafeOrUnavailableVersionBeforeMutation(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		instances     []core.ServiceInstance
		wantListCalls int
	}{
		{
			name:          "malformed path-like version",
			version:       "../../8.4",
			instances:     []core.ServiceInstance{{Version: "8.4", Managed: true}},
			wantListCalls: 0,
		},
		{
			name:          "well-formed unavailable version",
			version:       "8.5",
			instances:     []core.ServiceInstance{{Version: "8.4", Managed: true}},
			wantListCalls: 1,
		},
		{
			name:          "stored version missing from agent",
			version:       "8.3",
			instances:     []core.ServiceInstance{{Version: "8.4", Managed: true}},
			wantListCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDomainPHPHandlerFixture(t, test.instances)
			recorder := httptest.NewRecorder()
			fixture.panel.handleDomainSubroute(
				recorder,
				domainPHPHandlerRequest(
					http.MethodPost,
					fmt.Sprintf(`{"php_version":%q}`, test.version),
					domainPHPTestOwnerCaller(),
				),
			)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("POST status = %d, body=%q", recorder.Code, recorder.Body.String())
			}
			if list, pool, migrate := fixture.agent.callCounts(); list != test.wantListCalls || pool != 0 || migrate != 0 {
				t.Fatalf(
					"agent calls = list %d pool %d migrate %d, want %d/0/0",
					list,
					pool,
					migrate,
					test.wantListCalls,
				)
			}
			var storedVersion, storedSocket string
			if err := fixture.panel.db.GetDB().QueryRow(`
				SELECT php_version, php_fpm_socket FROM sites WHERE id = ?
			`, domainPHPTestSiteID).Scan(&storedVersion, &storedSocket); err != nil {
				t.Fatalf("read stored PHP identity: %v", err)
			}
			if storedVersion != "8.3" || storedSocket != "/var/run/php/php8.3-fpm-site7354.sock" {
				t.Fatalf("rejected POST changed PHP identity to %q/%q", storedVersion, storedSocket)
			}
		})
	}
}

func TestDomainPHPAvailableVersionsKeepsStableEmptyArray(t *testing.T) {
	versions := domainPHPAvailableVersions("legacy", []string{"", "default", "8.3.1"})
	if versions == nil || len(versions) != 0 {
		t.Fatalf("filtered versions = %#v, want non-nil empty slice", versions)
	}
}

func TestDomainPHPAvailableVersionsUsesDeterministicTieBreak(t *testing.T) {
	left := domainPHPAvailableVersions("", []string{"8.3", "08.03", "8.03"})
	right := domainPHPAvailableVersions("", []string{"8.03", "8.3", "08.03"})
	want := []string{"08.03", "8.03", "8.3"}
	if !reflect.DeepEqual(left, want) || !reflect.DeepEqual(right, want) {
		t.Fatalf("tied versions = %#v / %#v, want %#v", left, right, want)
	}
}
