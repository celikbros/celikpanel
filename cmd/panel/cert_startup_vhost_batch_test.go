package main

import (
	"context"
	"net"
	"net/rpc"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/transport"
)

type StartupVhostBatchRequest struct {
	ExpectedBuildCommit string                 `json:"expected_build_commit"`
	Vhosts              []applyVhostRPCRequest `json:"vhosts"`
}

type StartupVhostBatchResponse struct {
	Applied int    `json:"applied"`
	Error   string `json:"error,omitempty"`
}

type StartupVhostBatchVersionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type startupVhostBatchAgent struct {
	mu              sync.Mutex
	commit          string
	calls           [][]applyVhostRPCRequest
	appliedOverride *int
	responseError   string
	expectedCommits []string
}

func (a *startupVhostBatchAgent) Version(
	_ *struct{},
	response *StartupVhostBatchVersionResponse,
) error {
	response.Version = "test"
	response.Commit = a.commit
	return nil
}

func (a *startupVhostBatchAgent) ApplyVhosts(
	request *StartupVhostBatchRequest,
	response *StartupVhostBatchResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := append([]applyVhostRPCRequest(nil), request.Vhosts...)
	a.calls = append(a.calls, copied)
	a.expectedCommits = append(
		a.expectedCommits,
		request.ExpectedBuildCommit,
	)
	response.Error = a.responseError
	if a.appliedOverride != nil {
		response.Applied = *a.appliedOverride
	} else if response.Error == "" {
		response.Applied = len(request.Vhosts)
	}
	return nil
}

type StartupLegacyApplyVhostRequest struct{}

type StartupLegacyApplyVhostResponse struct{}

type startupLegacyVhostAgent struct {
	mu         sync.Mutex
	applyCalls int
}

func (a *startupLegacyVhostAgent) ApplyVhost(
	_ *StartupLegacyApplyVhostRequest,
	_ *StartupLegacyApplyVhostResponse,
) error {
	a.mu.Lock()
	a.applyCalls++
	a.mu.Unlock()
	return nil
}

func TestStartupHostedVhostsAreSubmittedAsOneBatch(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	firstID := addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"first-startup-vhost.example",
		"static",
	)
	secondID := addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"second-startup-vhost.example",
		"static",
	)
	addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"dns-only-startup-vhost.example",
		"dnsonly",
	)

	agent := &startupVhostBatchAgent{}
	attachStartupVhostBatchAgent(t, panel, agent)

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		10,
	)
	if err != nil {
		t.Fatalf("reconcile startup vhost batch: %v", err)
	}
	if applied != 2 {
		t.Fatalf("applied startup vhosts = %d, want 2", applied)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.calls) != 1 {
		t.Fatalf("ApplyVhosts calls = %d, want exactly one", len(agent.calls))
	}
	if len(agent.calls[0]) != 2 {
		t.Fatalf("batched vhosts = %d, want 2", len(agent.calls[0]))
	}
	if agent.calls[0][0].DomainID != firstID ||
		agent.calls[0][1].DomainID != secondID {
		t.Fatalf(
			"batch domain identities = [%d %d], want [%d %d]",
			agent.calls[0][0].DomainID,
			agent.calls[0][1].DomainID,
			firstID,
			secondID,
		)
	}
}

func TestStartupHostedVhostBatchCarriesExpectedAgentBuild(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"build-bound-startup-vhost.example",
		"static",
	)
	agent := &startupVhostBatchAgent{commit: "paired-release-commit"}
	attachStartupVhostBatchAgent(t, panel, agent)

	previousCommit := buildCommit
	buildCommit = "paired-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		10,
	)
	if err != nil {
		t.Fatalf("build-bound startup batch: %v", err)
	}
	if applied != 1 {
		t.Fatalf("build-bound startup batch applied %d vhosts", applied)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.expectedCommits) != 1 ||
		agent.expectedCommits[0] != "paired-release-commit" {
		t.Fatalf(
			"startup batch expected commits = %#v",
			agent.expectedCommits,
		)
	}
}

func TestStartupHostedVhostLimitRefusesPartialBatch(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	for _, domain := range []string{
		"one-limit-vhost.example",
		"two-limit-vhost.example",
	} {
		addStartupHostedDomain(
			t,
			panel,
			subscriptionID,
			domain,
			"static",
		)
	}
	agent := &startupVhostBatchAgent{}
	attachStartupVhostBatchAgent(t, panel, agent)

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		1,
	)
	if err == nil || !strings.Contains(err.Error(), "safe startup limit 1") {
		t.Fatalf("over-limit startup batch error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("over-limit startup batch applied %d vhosts", applied)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.calls) != 0 {
		t.Fatalf("over-limit startup sent a partial batch: %#v", agent.calls)
	}
}

func TestStartupHostedVhostBatchFailsClosedOnBuildMismatch(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"mismatch-startup-vhost.example",
		"static",
	)
	agent := &startupVhostBatchAgent{commit: "agent-other-commit"}
	attachStartupVhostBatchAgent(t, panel, agent)

	previousCommit := buildCommit
	buildCommit = "panel-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		10,
	)
	if err == nil || !strings.Contains(err.Error(), "build mismatch") {
		t.Fatalf("mismatched startup batch error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("mismatched startup batch applied %d vhosts", applied)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.calls) != 0 {
		t.Fatalf("mismatched build reached ApplyVhosts: %#v", agent.calls)
	}
}

func TestStartupHostedVhostBatchDoesNotFallbackToLegacyPerDomainRPC(
	t *testing.T,
) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"legacy-agent-startup-vhost.example",
		"static",
	)
	agent := &startupLegacyVhostAgent{}
	attachStartupVhostBatchAgent(t, panel, agent)

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		10,
	)
	if err == nil || !strings.Contains(err.Error(), "ApplyVhosts") {
		t.Fatalf("legacy agent startup batch error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("legacy agent startup batch applied %d vhosts", applied)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.applyCalls != 0 {
		t.Fatalf(
			"startup silently fell back to %d per-domain ApplyVhost calls",
			agent.applyCalls,
		)
	}
}

func TestStartupHostedVhostBatchRejectsInconsistentAppliedCount(t *testing.T) {
	panel, subscriptionID := newStartupVhostBatchFixture(t)
	addStartupHostedDomain(
		t,
		panel,
		subscriptionID,
		"inconsistent-startup-vhost.example",
		"static",
	)
	reported := 0
	agent := &startupVhostBatchAgent{appliedOverride: &reported}
	attachStartupVhostBatchAgent(t, panel, agent)

	applied, err := panel.reconcileHostedVhostsAtStartupWithLimit(
		context.Background(),
		10,
	)
	if err == nil || !strings.Contains(err.Error(), "reported 0") {
		t.Fatalf("inconsistent startup batch error = %v", err)
	}
	if applied != 0 {
		t.Fatalf("inconsistent response returned %d applied vhosts", applied)
	}
}

func newStartupVhostBatchFixture(t *testing.T) (*Panel, int) {
	t.Helper()
	panel := newDNSPanelForTest(t)
	database := panel.db.GetDB()
	userResult, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('startup-vhost-owner', 'hash',
		        'startup-vhost-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert startup vhost owner: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("startup vhost owner id: %v", err)
	}
	subscriptionResult, err := database.Exec(`
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'Startup vhost batch')`, userID)
	if err != nil {
		t.Fatalf("insert startup vhost subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("startup vhost subscription id: %v", err)
	}
	return panel, int(subscriptionID)
}

func addStartupHostedDomain(
	t *testing.T,
	panel *Panel,
	subscriptionID int,
	name string,
	projectType string,
) int {
	t.Helper()
	result, err := panel.db.GetDB().Exec(`
		INSERT INTO domains (subscription_id, name)
		VALUES (?, ?)`, subscriptionID, name)
	if err != nil {
		t.Fatalf("insert startup vhost domain %s: %v", name, err)
	}
	domainID64, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("startup vhost domain id %s: %v", name, err)
	}
	domainID := int(domainID64)
	documentRoot, err := hostingpath.DocumentRoot(subscriptionID, domainID)
	if err != nil {
		t.Fatalf("derive startup vhost document root: %v", err)
	}
	if projectType == "dnsonly" {
		documentRoot = ""
	}
	if _, err := panel.db.GetDB().Exec(`
		INSERT INTO sites (domain_id, document_root, project_type)
		VALUES (?, ?, ?)`, domainID, documentRoot, projectType); err != nil {
		t.Fatalf("insert startup vhost site %s: %v", name, err)
	}
	return domainID
}

func attachStartupVhostBatchAgent(
	t *testing.T,
	panel *Panel,
	agent any,
) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
	socketFile, err := os.CreateTemp("", "cp-startup-vhosts-*.sock")
	if err != nil {
		t.Fatalf("reserve startup vhost agent socket: %v", err)
	}
	socketPath := socketFile.Name()
	if err := socketFile.Close(); err != nil {
		t.Fatalf("close startup vhost socket placeholder: %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("remove startup vhost socket placeholder: %v", err)
	}
	listener, err := transport.ListenAgent(socketPath)
	if err != nil {
		t.Fatalf("listen startup vhost agent: %v", err)
	}
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		_ = listener.Close()
		t.Fatalf("register startup vhost agent: %v", err)
	}
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go server.ServeConn(connection)
		}
	}()
	connector := func(ctx context.Context) (*rpc.Client, error) {
		dialer := net.Dialer{}
		connection, dialErr := dialer.DialContext(ctx, "unix", socketPath)
		if dialErr != nil {
			return nil, dialErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = connection.Close()
			return nil, ctxErr
		}
		return rpc.NewClient(connection), nil
	}
	rawClient, err := connector(context.Background())
	if err != nil {
		_ = listener.Close()
		t.Fatalf("connect startup vhost agent: %v", err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(
		rawClient,
		connector,
	)
	t.Cleanup(func() {
		_ = rawClient.Close()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
}
