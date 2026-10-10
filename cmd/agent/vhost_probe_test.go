package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// D-031 step 1b, second round: only a request that asks for it gets the
// validation probe, and the probe asks for exactly the names a certificate
// request for this render validates.
func TestTheValidationProbeIsAskedOnlyWhenRequested(t *testing.T) {
	data := services.VhostData{
		Domain:             "probe.example",
		ServerNames:        []string{"probe.example", "www.probe.example", "alias.example"},
		ACMEChallengeNames: []string{"mail.probe.example", "alias.example"},
	}
	if item := managedVhostItemFor(&transport.ApplyVhostRequest{}, data, "body"); item.Probe != nil {
		t.Fatalf("probe without a request: %+v", item.Probe)
	}
	item := managedVhostItemFor(&transport.ApplyVhostRequest{ProbeValidation: true}, data, "body")
	if item.Probe == nil ||
		strings.Join(item.Probe.SiteNames, ",") != "probe.example,www.probe.example,alias.example" ||
		strings.Join(item.Probe.ExtraNames, ",") != "mail.probe.example" {
		t.Fatalf("probe = %+v", item.Probe)
	}
}
