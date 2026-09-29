package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Decision D (2026-09-30): a standalone snapshot carries neither pair_ready
// nor secondary_ready.
func TestDNSEngineStandaloneSnapshotHasNoPairFields(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	attachDNSEngineTestAgent(t, panel, agent)
	installBINDForReinstallTest(t, panel, agent)
	snapshot, err := panel.dnsEngineSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	if strings.Contains(string(raw), "secondary_ready") || strings.Contains(string(raw), "pair_ready") {
		t.Fatalf("standalone snapshot carries pair fields: %s", raw)
	}
}
