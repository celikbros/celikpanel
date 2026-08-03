package services

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/transport"
)

type SiteOrchestratorTestDeleteRequest struct {
	ExpectedBuildCommit string
	SiteID              int
	SubscriptionID      int
	DomainID            int
	Domain              string
	Username            string
	PHPVersion          string
	SiteHome            string
}

type SiteOrchestratorTestDeleteResponse struct {
	Success bool
	Error   string
}

type SiteOrchestratorTestAgent struct {
	mu sync.Mutex

	CreateResponse transport.CreateSiteResponse
	CreateRPCError string
	DeleteSuccess  bool
	DeleteError    string
	DeleteRPCError string

	CreateStarted chan struct{}
	CreateBlock   chan struct{}
	startedOnce   sync.Once

	CreateCalls []transport.CreateSiteRequest
	DeleteCalls []SiteOrchestratorTestDeleteRequest
}

func (a *SiteOrchestratorTestAgent) CreateSite(
	req transport.CreateSiteRequest,
	reply *transport.CreateSiteResponse,
) error {
	a.mu.Lock()
	a.CreateCalls = append(a.CreateCalls, req)
	started := a.CreateStarted
	block := a.CreateBlock
	response := a.CreateResponse
	rpcError := a.CreateRPCError
	a.mu.Unlock()

	if started != nil {
		a.startedOnce.Do(func() { close(started) })
	}
	if block != nil {
		<-block
	}
	*reply = response
	if rpcError != "" {
		return errors.New(rpcError)
	}
	return nil
}

func (a *SiteOrchestratorTestAgent) DeleteSite(
	req *SiteOrchestratorTestDeleteRequest,
	reply *SiteOrchestratorTestDeleteResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.DeleteCalls = append(a.DeleteCalls, *req)
	if a.DeleteRPCError != "" {
		return errors.New(a.DeleteRPCError)
	}
	reply.Success = a.DeleteSuccess
	reply.Error = a.DeleteError
	return nil
}

func newSiteOrchestratorFixture(
	t *testing.T,
	agent *SiteOrchestratorTestAgent,
) (*SiteOrchestrator, *paneldb.SQLiteDB, int) {
	t.Helper()

	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)

	result, err := database.GetDB().Exec(
		"INSERT INTO users (username, password_hash, email, role) " +
			"VALUES ('site-owner', 'unused', 'site-owner@example.test', 'customer')")
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	result, err = database.GetDB().Exec(
		"INSERT INTO subscriptions (owner_id, name, status) "+
			"VALUES (?, 'Site tests', 'active')",
		ownerID,
	)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(context.Context) (*rpc.Client, error) {
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client := transport.NewReconnectingClientWithContextConnector(nil, connector)
	orchestrator := NewSiteOrchestrator(database.GetDB(), client, "test-build")
	return orchestrator, database, int(subscriptionID)
}

func createSiteTestRequest(subscriptionID int, domain string) *CreateSiteRequest {
	return &CreateSiteRequest{
		SubscriptionID: subscriptionID,
		Domain:         domain,
		ProjectType:    "php",
		PHPVersion:     "8.3",
		SSLType:        "none",
	}
}

func TestCreateSiteCanStartFailClosedPending(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success:   true,
			PHPSocket: "/run/php/celikpanel-pending.sock",
		},
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	req := createSiteTestRequest(subscriptionID, "pending-import.example.test")
	req.InitialStatus = "pending"

	response, err := orchestrator.CreateSite(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var domainStatus, siteStatus string
	if err := database.GetDB().QueryRow(
		"SELECT status FROM domains WHERE id = ?",
		response.DomainID,
	).Scan(&domainStatus); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow(
		"SELECT status FROM sites WHERE id = ?",
		response.SiteID,
	).Scan(&siteStatus); err != nil {
		t.Fatal(err)
	}
	if domainStatus != "pending" || siteStatus != "pending" {
		t.Fatalf(
			"created statuses = domain %q, site %q; want both pending",
			domainStatus,
			siteStatus,
		)
	}
}

func TestCreateSiteRejectsUnsupportedInitialStatusBeforeMutation(t *testing.T) {
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(
		t,
		&SiteOrchestratorTestAgent{},
	)
	req := createSiteTestRequest(subscriptionID, "bad-status.example.test")
	req.InitialStatus = "active-but-unverified"

	if _, err := orchestrator.CreateSite(context.Background(), req); err == nil {
		t.Fatal("CreateSite accepted an unsupported initial status")
	}

	var count int
	if err := database.GetDB().QueryRow(
		"SELECT COUNT(*) FROM domains WHERE name = ?",
		req.Domain,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unsupported status created %d domain rows", count)
	}
}

func TestCreateSiteSocketUpdateFailureCompensatesAgentAndMetadata(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success:   true,
			PHPSocket: "/run/php/celikpanel-test.sock",
		},
		DeleteSuccess: true,
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	if _, err := database.GetDB().Exec(
		"CREATE TRIGGER fail_site_socket_update " +
			"BEFORE UPDATE OF php_fpm_socket ON sites " +
			"BEGIN SELECT RAISE(ABORT, 'injected socket update failure'); END",
	); err != nil {
		t.Fatal(err)
	}

	response, err := orchestrator.CreateSite(
		context.Background(),
		createSiteTestRequest(subscriptionID, "socket-failure.example.test"),
	)
	if err == nil {
		t.Fatal("CreateSite succeeded after its final metadata update failed")
	}
	if response != nil {
		t.Fatalf("CreateSite response = %#v, want nil on compensated failure", response)
	}
	for _, want := range []string{
		"failed to record PHP-FPM socket",
		"injected socket update failure",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSite error = %v, want %q", err, want)
		}
	}

	agent.mu.Lock()
	createCalls := append([]transport.CreateSiteRequest(nil), agent.CreateCalls...)
	deleteCalls := append([]SiteOrchestratorTestDeleteRequest(nil), agent.DeleteCalls...)
	agent.mu.Unlock()
	if len(createCalls) != 1 || len(deleteCalls) != 1 {
		t.Fatalf(
			"agent calls create=%d delete=%d, want one compensating delete",
			len(createCalls),
			len(deleteCalls),
		)
	}
	deleted := deleteCalls[0]
	if deleted.SiteID == 0 ||
		deleted.SubscriptionID != createCalls[0].SubscriptionID ||
		deleted.DomainID != createCalls[0].DomainID ||
		deleted.Domain != "socket-failure.example.test" ||
		deleted.Username != SiteUsername(deleted.Domain) ||
		deleted.PHPVersion != "8.3" ||
		deleted.SiteHome != filepath.Dir(createCalls[0].DocumentRoot) {
		t.Fatalf("compensating DeleteSite request = %#v", deleted)
	}

	assertSiteMetadataCounts(t, database, 0, 0)
}

func TestCreateSiteReportsMetadataRollbackFailure(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success:      false,
			ErrorMessage: "injected agent create failure",
		},
		DeleteSuccess: true,
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	if _, err := database.GetDB().Exec(
		"CREATE TRIGGER fail_site_metadata_rollback " +
			"BEFORE DELETE ON sites " +
			"BEGIN SELECT RAISE(ABORT, 'injected metadata rollback failure'); END",
	); err != nil {
		t.Fatal(err)
	}

	_, err := orchestrator.CreateSite(
		context.Background(),
		createSiteTestRequest(subscriptionID, "rollback-failure.example.test"),
	)
	if err == nil {
		t.Fatal("CreateSite swallowed its rollback failure")
	}
	for _, want := range []string{
		"site creation failed: injected agent create failure",
		"rollback site metadata",
		"injected metadata rollback failure",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSite error = %v, want %q", err, want)
		}
	}
	assertSiteMetadataCounts(t, database, 1, 1)
}

func TestCreateSiteRetainsMetadataWhenAgentRollbackFails(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success:      false,
			ErrorMessage: "injected agent create failure",
		},
		DeleteSuccess: false,
		DeleteError:   "injected agent cleanup failure",
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)

	_, err := orchestrator.CreateSite(
		context.Background(),
		createSiteTestRequest(subscriptionID, "agent-rollback-failure.example.test"),
	)
	if err == nil {
		t.Fatal("CreateSite swallowed its agent rollback failure")
	}
	for _, want := range []string{
		"site creation failed: injected agent create failure",
		"rollback agent site: injected agent cleanup failure",
		"retained domain",
		"because agent cleanup was not confirmed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CreateSite error = %v, want %q", err, want)
		}
	}
	assertSiteMetadataCounts(t, database, 1, 1)
}

func TestCreateSiteRequestCancellationUsesIndependentCompensationContext(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success:   true,
			PHPSocket: "/run/php/late.sock",
		},
		DeleteSuccess: true,
		CreateStarted: make(chan struct{}),
		CreateBlock:   make(chan struct{}),
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	t.Cleanup(func() { close(agent.CreateBlock) })

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := orchestrator.CreateSite(
			ctx,
			createSiteTestRequest(subscriptionID, "cancelled.example.test"),
		)
		result <- err
	}()

	select {
	case <-agent.CreateStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("agent CreateSite call did not start")
	}
	cancel()

	var err error
	select {
	case err = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("CreateSite did not honor request cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateSite error = %v, want context.Canceled", err)
	}

	agent.mu.Lock()
	deleteCalls := len(agent.DeleteCalls)
	agent.mu.Unlock()
	if deleteCalls != 1 {
		t.Fatalf("compensating DeleteSite calls = %d, want 1", deleteCalls)
	}
	assertSiteMetadataCounts(t, database, 0, 0)
}

func assertSiteMetadataCounts(
	t *testing.T,
	database *paneldb.SQLiteDB,
	wantDomains, wantSites int,
) {
	t.Helper()
	var domains, sites int
	if err := database.GetDB().QueryRow("SELECT COUNT(*) FROM domains").Scan(&domains); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().QueryRow("SELECT COUNT(*) FROM sites").Scan(&sites); err != nil {
		t.Fatal(err)
	}
	if domains != wantDomains || sites != wantSites {
		t.Fatalf(
			"metadata counts domains=%d sites=%d, want domains=%d sites=%d",
			domains,
			sites,
			wantDomains,
			wantSites,
		)
	}
}
