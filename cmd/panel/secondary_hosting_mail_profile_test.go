package main

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// This executes the real setup child admission and complete mail-profile
// runner after the same reviewed setup has explicitly bound its publisher.
// The agent is a deterministic RPC fixture; real SMTP/IMAP delivery is outside
// this test. The separate two-guest fixture proves real DNS transfer and HTTP.
func TestSecondaryHostingBoundPublisherExecutesMailProfiles(t *testing.T) {
	for _, profile := range []string{"webmail", "protected-mail"} {
		for _, revoked := range []bool{false, true} {
			name := profile + "/ready"
			if revoked {
				name = profile + "/revoked-before-child"
			}
			t.Run(name, func(t *testing.T) {
				f := newSecondaryHostingFixture(t)
				f.wait(t)
				if response := f.bind(t, f.id); response.Code != http.StatusAccepted {
					t.Fatal(response.Body.String())
				}
				ctx := context.Background()
				execution, err := f.f.panel.latestServerSetupExecution(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if progressed, err := f.f.panel.advanceServerSetupExecution(f.plan, execution); err != nil || !progressed {
					t.Fatalf("explicit publisher binding did not advance: %v %v", progressed, err)
				}
				before, err := readDNSEngineDBState(ctx, f.f.database.GetDB())
				if err != nil || before.PairRole != transport.DNSPairRoleSecondary {
					t.Fatalf("fixture is not a secondary: %+v %v", before, err)
				}
				if mode, err := f.f.panel.setupDNSManagementMode(ctx); err != nil || mode != setupDNSModeExisting {
					t.Fatalf("publisher was not bound as hosting authority: %s %v", mode, err)
				}
				var child *serverSetupExecutionStep
				for index := range execution.Steps {
					if execution.Steps[index].Kind == "mail_profile" && execution.Steps[index].Target == profile {
						child = &execution.Steps[index]
						break
					}
				}
				if child == nil {
					t.Fatal("review did not contain the expected mail profile")
				}
				agent := &mailProfileTestAgent{serviceOperationTestAgent: f.f.agent, agentCommit: buildCommit}
				attachMailProfileTestAgent(t, f.f.panel, agent)
				f.f.panel.pkgFamilyVal = "apt"
				f.f.panel.webmailReadinessProbe = func(context.Context) bool { return true }
				seedInstalledServices(agent.serviceOperationTestAgent, "pdns")
				if revoked {
					if _, err := f.f.database.GetDB().Exec(`UPDATE remote_dns_connections SET status='revoked' WHERE id=?`, f.id); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := f.f.panel.runServerSetupStep(ctx, f.plan, child); err != nil {
					t.Fatalf("setup could not admit the reviewed child: %v", err)
				}
				wantStatus := serviceOperationSucceeded
				if revoked {
					wantStatus = serviceOperationFailed
				}
				op, _ := waitForServiceOperation(t, f.f.panel, f.f.userID, child.OperationID, wantStatus)
				if revoked {
					if op.Error == nil || len(agent.callsSnapshot()) != 0 || agent.installCalls.Load() != 0 {
						t.Fatal("revoked publisher admitted mail host mutations")
					}
				} else {
					if done, err := f.f.panel.runServerSetupStep(ctx, f.plan, child); err != nil || !done || child.OperationID != op.ID {
						t.Fatalf("mail child receipt could not reconcile: done=%v err=%v", done, err)
					}
					definition, _ := mailProfileByID(profile)
					for _, service := range definition.Services {
						if !installedForTest(agent.serviceOperationTestAgent, service) {
							t.Fatalf("mail runner did not install %s", service)
						}
					}
					if len(agent.hostnameRequestsSnapshot()) != 0 || f.f.panel.setting(ctx, settingMailHostname) != f.plan.Draft.MailHostname {
						t.Fatal("mail profile changed OS hostname or lost its reviewed mail identity")
					}
					calls := agent.callsSnapshot()
					stack, submission, tls := false, false, false
					for _, call := range calls {
						stack = stack || call.Name == "mail-stack"
						submission = submission || call.Name == "submission"
						tls = tls || call.Name == "mail-tls"
					}
					if !stack || !submission || !tls {
						t.Fatalf("mail runner omitted final configuration: %+v", calls)
					}
				}
				after, err := readDNSEngineDBState(ctx, f.f.database.GetDB())
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("mail child changed secondary authority: before=%+v after=%+v err=%v", before, after, err)
				}
				for _, table := range []string{"pdns_domains", "dns_zone_sync_state"} {
					var count int
					if err := f.f.database.GetDB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
						t.Fatalf("mail child acquired local DNS publication through %s: count=%d err=%v", table, count, err)
					}
				}
			})
		}
	}
}
