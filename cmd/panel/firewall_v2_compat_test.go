package main

import (
	"context"
	"net"
	"net/rpc"
	"strings"
	"sync"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

type legacyOnlyFirewallAgent struct {
	durableMutationRPCFixture

	mu          sync.Mutex
	legacyCalls int
}

func (a *legacyOnlyFirewallAgent) FirewallStatus(
	_ *transport.Empty,
	out *FirewallStatusResp,
) error {
	*out = FirewallStatusResp{Enabled: true, EngineAvailable: true}
	return nil
}

func (a *legacyOnlyFirewallAgent) InstalledServiceIDsStrict(
	_ *transport.Empty,
	out *[]string,
) error {
	*out = nil
	return nil
}

func (a *legacyOnlyFirewallAgent) ApplyFirewall(
	_ *transport.ApplyFirewallRequest,
	out *FirewallStatusResp,
) error {
	a.mu.Lock()
	a.legacyCalls++
	a.mu.Unlock()
	*out = FirewallStatusResp{Error: "legacy path reached"}
	return nil
}

func attachLegacyOnlyFirewallAgent(
	t *testing.T,
	panel *Panel,
	agent *legacyOnlyFirewallAgent,
) {
	t.Helper()
	panel.pkgFamilyVal = "apt"
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
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func TestSyncFirewallNewPanelOldAgentFailsClosedWithoutV1Fallback(t *testing.T) {
	agent := &legacyOnlyFirewallAgent{}
	panel := &Panel{}
	attachLegacyOnlyFirewallAgent(t, panel, agent)

	err := panel.syncFirewall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ApplyFirewallV2") {
		t.Fatalf("mixed-version error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.legacyCalls != 0 {
		t.Fatalf("legacy ApplyFirewall fallback calls = %d", agent.legacyCalls)
	}
}
