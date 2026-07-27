package main

import (
	"strings"
	"testing"
)

func TestDeleteSiteRejectsBuildMismatchBeforePrivilegedMutation(t *testing.T) {
	previousCommit := buildCommit
	buildCommit = "agent-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	var response DeleteSiteResponse
	if err := (&Agent{}).DeleteSite(
		&DeleteSiteRequest{
			ExpectedBuildCommit: "panel-other-commit",
			SiteID:              42,
			Domain:              "example.test",
			SiteHome:            "/var/www/celikpanel/subscriptions/7/sites/42",
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if response.Success {
		t.Fatal("mismatched build deleted a site")
	}
	if !strings.Contains(response.Error, "build mismatch") {
		t.Fatalf("DeleteSite error = %q, want explicit build mismatch", response.Error)
	}
}
