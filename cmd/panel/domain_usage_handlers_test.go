package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"strconv"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

type SiteUsageTestRequest struct {
	SubscriptionID int
	DomainID       int
	Domain         string
	SiteHome       string
}

type SiteUsageTestResponse struct {
	DiskBytes         int64
	TrafficMonthBytes int64
	Error             string
}

type siteUsageTestAgent struct {
	mu    sync.Mutex
	calls []SiteUsageTestRequest
}

func (a *siteUsageTestAgent) SiteUsage(
	req *SiteUsageTestRequest,
	resp *SiteUsageTestResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, *req)
	resp.DiskBytes = 4096
	resp.TrafficMonthBytes = 8192
	return nil
}

func attachSiteUsageTestAgent(t *testing.T, p *Panel, agent *siteUsageTestAgent) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register site-usage test agent: %v", err)
	}
	go server.ServeConn(serverConn)
	client := rpc.NewClient(clientConn)
	p.agentClient = transport.NewReconnectingClient(client)
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverConn.Close()
	})
}

func newSiteUsagePanelFixture(t *testing.T) (*Panel, int, int) {
	t.Helper()
	p := newDNSPanelForTest(t)
	database := p.db.GetDB()

	userResult, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('usage-owner', 'hash', 'usage-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	subscriptionResult, err := database.Exec(`
		INSERT INTO subscriptions (owner_id, name) VALUES (?, 'Usage test')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID64, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	domainResult, err := database.Exec(`
		INSERT INTO domains (subscription_id, name)
		VALUES (?, 'usage.example.test')`, subscriptionID64)
	if err != nil {
		t.Fatal(err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	documentRoot, err := hostingpath.DocumentRoot(int(subscriptionID64), int(domainID64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO sites (domain_id, document_root) VALUES (?, ?)`,
		domainID64, documentRoot); err != nil {
		t.Fatal(err)
	}
	return p, int(subscriptionID64), int(domainID64)
}

func TestDomainUsageSendsIdentityInsteadOfFilesystemPath(t *testing.T) {
	p, subscriptionID, domainID := newSiteUsagePanelFixture(t)
	agent := &siteUsageTestAgent{}
	attachSiteUsageTestAgent(t, p, agent)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/domains/"+strconv.Itoa(domainID)+"/usage",
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDomainUsage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.calls) != 1 {
		t.Fatalf("usage calls = %+v", agent.calls)
	}
	call := agent.calls[0]
	if call.SubscriptionID != subscriptionID ||
		call.DomainID != domainID ||
		call.Domain != "usage.example.test" {
		t.Fatalf("unexpected RPC request: %+v", call)
	}
	if call.SiteHome != "" {
		t.Fatalf("filesystem path crossed the panel-agent RPC: %q", call.SiteHome)
	}

	var diskBytes, trafficBytes int64
	if err := p.db.GetDB().QueryRow(`
		SELECT disk_usage_bytes, traffic_month_bytes
		FROM sites WHERE domain_id = ?`, domainID).Scan(&diskBytes, &trafficBytes); err != nil {
		t.Fatal(err)
	}
	if diskBytes != 4096 || trafficBytes != 8192 {
		t.Fatalf("cached usage = (%d, %d)", diskBytes, trafficBytes)
	}
}
