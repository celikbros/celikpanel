package main

import (
	"context"
	"slices"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServerSetupProfileDNSMatrixPreservesIdentityAndServiceScope(t *testing.T) {
	for _, purpose := range []string{"web", "web_mail", "dns"} {
		for _, mode := range []string{"local", "external"} {
			for _, engine := range []string{"bind", "pdns"} {
				for _, role := range []string{"primary", "secondary"} {
					t.Run(purpose+"/"+mode+"/"+engine+"/"+role, func(t *testing.T) {
						f, state := setupOperationFixture(t)
						caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
						f.agent.versionCapabilities = &caps
						state.Draft.Purpose, state.Draft.DNSMode = purpose, mode
						state.Draft.DNSEngine, state.Draft.DNSRole = engine, role
						state.Draft.NS1, state.Draft.NS2 = "ns1.example.test", "ns2.example.test"
						state.Draft.LocalIP, state.Draft.PeerIP = "72.62.38.15", "2.25.80.4"
						state.Draft.PeerNS = state.Draft.NS2
						if role == "secondary" {
							state.Draft.PeerNS = state.Draft.NS1
							state.Draft.LocalIP, state.Draft.PeerIP = state.Draft.PeerIP, state.Draft.LocalIP
						}
						// A stale hidden mail field must not add mail to web or DNS profiles.
						state.Draft.MailHostname = "mail.frankfurt.celikhost.com"
						plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
						if err != nil {
							t.Fatal(err)
						}
						wantBlocked := (purpose == "dns" && mode != "local") || (purpose != "dns" && mode == "local" && role == "secondary")
						if plan.CanStart == wantBlocked {
							t.Fatalf("unexpected plan decision: %v", plan.Blockers)
						}
						if plan.HostnameChange != "" {
							t.Fatal("setup planned an OS rename")
						}
						mail := false
						for _, step := range plan.Steps {
							mail = mail || step.Kind == "mail_profile" || step.Kind == "mail_certificate"
							if purpose == "dns" && step.Kind == "service" && !slices.Contains([]string{"nftables", "certbot"}, step.Target) {
								t.Fatalf("DNS-only plan installs unrelated service: %+v", step)
							}
						}
						if mail != (purpose == "web_mail") {
							t.Fatal("mail scope does not follow purpose")
						}
						for _, port := range []int{25, 110, 143, 465, 587, 993, 995} {
							if purpose != "web_mail" && slices.Contains(plan.TCPPorts, port) {
								t.Fatalf("unrelated mail port %d", port)
							}
						}
						if (slices.Contains(plan.TCPPorts, 53) && slices.Contains(plan.UDPPorts, 53)) != (mode == "local") {
							t.Fatal("DNS ports do not follow ownership mode")
						}
						if f.agent.installCalls.Load() != 0 || len(f.agent.capturedMutationEvents()) != 0 {
							t.Fatal("review mutated host")
						}
					})
				}
			}
		}
	}
}
