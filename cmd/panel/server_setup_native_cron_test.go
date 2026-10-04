package main

import (
	"context"
	"slices"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/transport"
)

func setupPlanHasCronStep(plan serverSetupPlan) bool {
	for _, step := range plan.Steps {
		if step.Kind == "service" && step.Target == core.NativeCronServiceID {
			return true
		}
	}
	return false
}

// upd1 (1 Oct 2026): a web_mail server set up through the wizard had no cron,
// so its Scheduled tasks screen could not work. Profiles that host sites now
// prepare native cron through the ordinary component step; profiles without
// sites leave cron alone.
func TestServerSetupNativeCronFollowsSiteHostingProfiles(t *testing.T) {
	for _, test := range []struct {
		draft serverSetupDraft
		want  bool
	}{
		{serverSetupDraft{Purpose: "web"}, true},
		{serverSetupDraft{Purpose: "web_mail"}, true},
		{serverSetupDraft{Purpose: "application"}, true},
		{serverSetupDraft{Purpose: "dns"}, false},
		{serverSetupDraft{Purpose: "custom"}, false},
		{serverSetupDraft{Purpose: "custom", Customization: &serverSetupCustomization{Components: []string{"nginx"}}}, true},
		{serverSetupDraft{Purpose: "custom", Customization: &serverSetupCustomization{Components: []string{"phpmyadmin"}}}, true},
		{serverSetupDraft{Purpose: "custom", Customization: &serverSetupCustomization{Components: []string{"mariadb", "redis"}}}, false},
		{serverSetupDraft{Purpose: "custom", Customization: &serverSetupCustomization{Components: []string{}}}, false},
		{serverSetupDraft{Purpose: "dns", Customization: &serverSetupCustomization{Components: []string{}}}, false},
	} {
		if got := serverSetupNeedsNativeCron(test.draft); got != test.want {
			t.Errorf("%+v: needs cron = %v, want %v", test.draft, got, test.want)
		}
	}
	for _, purpose := range []string{"web", "web_mail"} {
		t.Run(purpose, func(t *testing.T) {
			f, state := setupOperationFixture(t)
			state.Draft.Purpose = purpose
			caps := []string{transport.AgentCapabilityMailHostCertificateV1, transport.AgentCapabilityMailTLSSyncV2, transport.AgentCapabilityFirewallApplyV2, transport.AgentCapabilityPanelCertificateIssueV2}
			f.agent.versionCapabilities = &caps
			if purpose == "web_mail" {
				state.Draft.MailHostname = "mail.example.test"
			}
			plan, err := f.panel.buildServerSetupPlan(context.Background(), state, serviceOperationActor{UserID: f.userID})
			if err != nil {
				t.Fatal(err)
			}
			if !setupPlanHasCronStep(plan) {
				t.Fatalf("%s plan does not prepare native cron: %+v", purpose, plan.Steps)
			}
			for _, blocker := range plan.Blockers {
				if blocker == "server_setup_service_unsupported:cron" || blocker == "server_setup_service_unknown" {
					t.Fatalf("native cron blocked the plan: %v", plan.Blockers)
				}
			}
			if f.agent.installCalls.Load() != 0 {
				t.Fatal("review installed cron")
			}
		})
	}
}

// An existing cron is the owner's: the reviewed plan keeps it and adds no
// install step (D-022).
func TestServerSetupKeepsInstalledNativeCron(t *testing.T) {
	f, state := setupOperationFixture(t)
	seedInstalledServices(f.agent, core.NativeCronServiceID)
	plan := saveSetupPlanForTest(t, f, state)
	if setupPlanHasCronStep(plan) {
		t.Fatalf("installed cron was planned again: %+v", plan.Steps)
	}

	custom := customSetupPlanForTest(t, f, state, "nginx")
	if setupPlanHasCronStep(custom) {
		t.Fatalf("installed cron was planned again in a custom plan: %+v", custom.Steps)
	}
	index := slices.IndexFunc(custom.Components, func(c serverSetupPlanComponent) bool { return c.ID == core.NativeCronServiceID })
	if index < 0 || !custom.Components[index].Installed || !custom.Components[index].Required || custom.Components[index].Selected {
		t.Fatalf("custom review must list the kept cron as an installed requirement: %+v", custom.Components)
	}
}

func TestServerSetupCustomPlanAddsNativeCronOnlyForSites(t *testing.T) {
	f, state := setupOperationFixture(t)
	withSites := customSetupPlanForTest(t, f, state, "nginx")
	if !setupPlanHasCronStep(withSites) {
		t.Fatalf("custom site plan does not prepare cron: %+v", withSites.Steps)
	}
	index := slices.IndexFunc(withSites.Components, func(c serverSetupPlanComponent) bool { return c.ID == core.NativeCronServiceID })
	if index < 0 || withSites.Components[index].Installed || !withSites.Components[index].Required {
		t.Fatalf("custom review must list cron as a required component to install: %+v", withSites.Components)
	}
	withoutSites := customSetupPlanForTest(t, f, state, "mariadb")
	if setupPlanHasCronStep(withoutSites) || slices.ContainsFunc(withoutSites.Components, func(c serverSetupPlanComponent) bool { return c.ID == core.NativeCronServiceID }) {
		t.Fatalf("a plan without sites changed cron: %+v", withoutSites)
	}
	// Cron is added by the profile, never selectable on its own, so an old
	// saved selection cannot name it and the catalogue does not list it.
	if slices.Contains(serverSetupSelectableComponents, core.NativeCronServiceID) {
		t.Fatal("cron must not be an individually selectable setup component")
	}
}
