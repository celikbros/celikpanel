package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The Agent's typed refusal (native finding P3) reaches the Panel as a typed
// error with the blocking directory; the refused site's records are removed
// as for any other Agent failure.
func TestCreateSiteCarriesTheHostingRootRefusal(t *testing.T) {
	block := transport.HostingRootBlock{Directory: "/var/www", Mode: "0750", Owner: "root:celikpanel", Account: "http"}
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{
			ErrorMessage: "site refused before any change",
			ErrorCode:    transport.HostingRootNotTraversable,
			HostingRoot:  &block,
		},
		DeleteSuccess: true,
	}
	orchestrator, database, subscriptionID := newSiteOrchestratorFixture(t, agent)
	response, err := orchestrator.CreateSite(context.Background(), createSiteTestRequest(subscriptionID, "p3.example.test"))
	if response != nil || err == nil {
		t.Fatalf("CreateSite = %#v, %v", response, err)
	}
	var refusal *HostingRootNotTraversableError
	if !errors.As(err, &refusal) || refusal.Block != block {
		t.Fatalf("error = %v, want the typed refusal for %+v", err, block)
	}
	if !strings.Contains(err.Error(), "hosting_root_not_traversable: /var/www (mode 0750") {
		t.Fatalf("error text = %q", err.Error())
	}
	assertSiteMetadataCounts(t, database, 0, 0)
}

// Without the code (an older Agent) the answer stays the ordinary failure.
func TestCreateSiteWithoutHostingRootCodeStaysGeneric(t *testing.T) {
	agent := &SiteOrchestratorTestAgent{
		CreateResponse: transport.CreateSiteResponse{ErrorMessage: "site provisioning failed during document root creation"},
		DeleteSuccess:  true,
	}
	orchestrator, _, subscriptionID := newSiteOrchestratorFixture(t, agent)
	_, err := orchestrator.CreateSite(context.Background(), createSiteTestRequest(subscriptionID, "old.example.test"))
	var refusal *HostingRootNotTraversableError
	if err == nil || errors.As(err, &refusal) {
		t.Fatalf("error = %v", err)
	}
}
