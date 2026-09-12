package main

import (
	"context"
	"testing"
)

func TestServerSetupApplicationWithoutDatabaseCanVerifyServices(t *testing.T) {
	f, state := setupOperationFixture(t)
	a := attachSetupComponentsAgent(t, f)
	a.versions = []string{"22.14.0"}
	seedInstalledServices(f.agent, "nginx", "certbot", "nftables")
	f.agent.active["nginx"] = true
	draft := state.Draft
	draft.Purpose, draft.NodeVersion, draft.PanelDomain = "application", "22.14.0", ""
	for _, database := range []string{"none", "mariadb"} {
		draft.Database = database
		checks, err := f.panel.serverSetupCompletionChecks(context.Background(), draft)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, check := range checks {
			if check.ID == "services" {
				found = true
				if (check.State == "ready") != (database == "none") {
					t.Fatalf("database %s: unexpected services readiness %+v", database, check)
				}
			}
		}
		if !found {
			t.Fatal("services check missing")
		}
	}
}
