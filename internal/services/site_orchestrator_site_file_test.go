package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 at site creation: the creation render uses the Panel's own host-name
// derivation (set8 measured that it did not), a successful creation records
// the vhost's ledger row, and a kept owner file is never removed by the
// orchestrator's compensation.

func TestCreateSiteSendsThePanelsHostNamesAndRecordsTheLedgerRow(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{CreateResponse: transport.CreateSiteResponse{
		Success: true, PHPSocket: "/run/php/php8.3-fpm-site1.sock",
		SiteFile: &transport.SiteFileResult{
			Kind: transport.SiteFileKindNginxVhost, Path: "/etc/nginx/sites-available/names.example.test.conf",
			State: transport.SiteFileAbsent, Outcome: transport.SiteFileOutcomeWritten,
			RenderSHA256: strings.Repeat("b", 64), WrittenSHA256: strings.Repeat("b", 64), FileSHA256: strings.Repeat("c", 64),
		},
	}}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	var derivedFor int
	orchestrator.SetSiteServerNames(func(_ context.Context, domainID int) ([]string, error) {
		derivedFor = domainID
		return []string{"names.example.test", "www.names.example.test"}, nil
	})
	orchestrator.SetReleaseLabel("v0.1.0-test")
	response, err := orchestrator.CreateSite(context.Background(), createSiteTestRequest(subscriptionID, "names.example.test"))
	if err != nil {
		t.Fatal(err)
	}
	if derivedFor != response.DomainID {
		t.Fatalf("host names derived for domain %d, created %d", derivedFor, response.DomainID)
	}
	if got := agent.CreateCalls[0].ServerNames; strings.Join(got, ",") != "names.example.test,www.names.example.test" {
		t.Fatalf("CreateSite server names = %v", got)
	}
	record, found, err := LoadSiteFileRecord(context.Background(), database.GetDB(), response.SiteID, transport.SiteFileKindNginxVhost)
	if err != nil || !found {
		t.Fatalf("ledger row: %v %v", found, err)
	}
	if record.State != transport.SiteFileManagedUnchanged || record.BodySHA256 != strings.Repeat("b", 64) ||
		record.WrittenRelease != "v0.1.0-test" || record.FileFormat != ManagedRenderFormat {
		t.Fatalf("ledger row: %+v", record)
	}
}

func TestCreateSiteWithAKeptOwnerFileRemovesOnlyTheRecords(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			Success: false, ErrorMessage: "site provisioning failed during nginx vhost activation",
			ErrorCode: transport.SiteConfigExists, ErrorDetail: "/etc/nginx/sites-available/kept.example.test.conf",
		},
		DeleteSuccess: true,
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	_, err := orchestrator.CreateSite(context.Background(), createSiteTestRequest(subscriptionID, "kept.example.test"))
	var exists *SiteConfigExistsError
	if !errors.As(err, &exists) || exists.Path != "/etc/nginx/sites-available/kept.example.test.conf" {
		t.Fatalf("error = %v", err)
	}
	if len(agent.DeleteCalls) != 0 {
		t.Fatalf("the Agent was asked to delete the site, which would remove the owner's file: %+v", agent.DeleteCalls)
	}
	assertSiteMetadataCounts(t, database, 0, 0)

	// The Agent's own removal was incomplete: the records stay for the owner.
	agent.CreateResponse.ErrorMessage += "; automatic rollback is incomplete"
	_, err = orchestrator.CreateSite(context.Background(), createSiteTestRequest(subscriptionID, "kept2.example.test"))
	if !errors.As(err, &exists) || !strings.Contains(err.Error(), "retained domain") || len(agent.DeleteCalls) != 0 {
		t.Fatalf("incomplete: %v deletes=%d", err, len(agent.DeleteCalls))
	}
	assertSiteMetadataCounts(t, database, 1, 1)
}
