package main

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type VersionPairRequest struct{}

type VersionPairResponse struct {
	Commit string
}

type versionPairAgent struct {
	commit string
}

func (a *versionPairAgent) Version(
	_ *VersionPairRequest,
	resp *VersionPairResponse,
) error {
	resp.Commit = a.commit
	return nil
}

func attachVersionPairAgent(t *testing.T, p *Panel, commit string) {
	t.Helper()

	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &versionPairAgent{commit: commit}); err != nil {
		t.Fatalf("register version pair agent: %v", err)
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
		t.Fatalf("connect version pair agent: %v", err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() {
		_ = client.Close()
	})
}

func attachUnreachableVersionPairAgent(
	t *testing.T,
	p *Panel,
	connectErr error,
) {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	go func() {
		_ = serverConn.Close()
	}()
	client := rpc.NewClient(clientConn)
	p.agentClient = transport.NewReconnectingClientWithContextConnector(
		client,
		func(context.Context) (*rpc.Client, error) {
			return nil, connectErr
		},
	)
	t.Cleanup(func() {
		_ = client.Close()
	})
}

func withPanelBuildCommit(t *testing.T, commit string) {
	t.Helper()
	previous := buildCommit
	buildCommit = commit
	t.Cleanup(func() {
		buildCommit = previous
	})
}

func TestRequireMatchingAgentBuildAcceptsExactProductionCommit(t *testing.T) {
	withPanelBuildCommit(t, "release-0123456789abcdef")
	p := &Panel{}
	attachVersionPairAgent(t, p, "release-0123456789abcdef")

	if err := p.requireMatchingAgentBuild(context.Background()); err != nil {
		t.Fatalf("exact panel/agent build pair rejected: %v", err)
	}
}

func TestRequireMatchingAgentBuildFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		agentCommit string
	}{
		{name: "mismatch", agentCommit: "release-fedcba9876543210"},
		{name: "empty agent commit", agentCommit: ""},
		{name: "whitespace agent commit", agentCommit: " \t "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withPanelBuildCommit(t, "release-0123456789abcdef")
			p := &Panel{}
			attachVersionPairAgent(t, p, tt.agentCommit)

			err := p.requireMatchingAgentBuild(context.Background())
			if err == nil {
				t.Fatal("unsafe panel/agent build pair was accepted")
			}
			if !strings.Contains(err.Error(), "panel/agent build mismatch") {
				t.Fatalf("error = %q, want explicit build mismatch", err)
			}
		})
	}
}

func TestRequireMatchingAgentBuildFailsClosedWhenAgentIsUnreachable(t *testing.T) {
	withPanelBuildCommit(t, "release-0123456789abcdef")
	connectErr := errors.New("agent unavailable")
	p := &Panel{}
	attachUnreachableVersionPairAgent(t, p, connectErr)

	err := p.requireMatchingAgentBuild(context.Background())
	if err == nil {
		t.Fatal("unreachable agent was accepted for a production mutation")
	}
	if !errors.Is(err, connectErr) {
		t.Fatalf("error = %v, want wrapped connector error", err)
	}
	if !strings.Contains(err.Error(), "verify panel/agent build pair") {
		t.Fatalf("error = %q, want build-pair verification context", err)
	}
}

func TestRequireMatchingAgentBuildBypassesUnknownDevelopmentBuild(t *testing.T) {
	withPanelBuildCommit(t, "unknown")

	if err := (&Panel{}).requireMatchingAgentBuild(context.Background()); err != nil {
		t.Fatalf("development build should bypass release pairing: %v", err)
	}
}
