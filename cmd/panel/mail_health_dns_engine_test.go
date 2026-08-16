package main

import (
	"context"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestMailHealthDNSSECIgnoresStandbyPowerDNSForActiveBIND(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgentForEngine(
		t, panel, agent, transport.DNSEngineBIND,
	)

	if panel.mailDNSSECSecured(context.Background(), "bind-mail.example") {
		t.Fatal("active BIND was reported secured from standby PowerDNS state")
	}
	agent.mu.Lock()
	calls := agent.dnssecStatusCalls
	agent.mu.Unlock()
	if calls != 0 {
		t.Fatalf("active BIND queried standby PowerDNS DNSSEC %d time(s)", calls)
	}
}

func TestMailHealthDNSSECUsesExactActivePowerDNS(t *testing.T) {
	panel := newDNSPanelForTest(t)
	agent := &strictDNSRPCAgent{}
	attachStrictDNSRPCAgent(t, panel, agent)

	if !panel.mailDNSSECSecured(context.Background(), "pdns-mail.example") {
		t.Fatal("exact active PowerDNS DNSSEC state was not reported")
	}
	agent.mu.Lock()
	calls := agent.dnssecStatusCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("active PowerDNS DNSSEC calls=%d, want 1", calls)
	}
}
