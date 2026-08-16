//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestInspectLegacyPowerDNSDurableAuthorityUsesPersistedState(t *testing.T) {
	for _, test := range []struct {
		name      string
		engine    transport.DNSEngine
		wantError bool
	}{
		{name: "PowerDNS active", engine: transport.DNSEnginePowerDNS},
		{name: "BIND remains authoritative while stopped", engine: transport.DNSEngineBIND, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := mutationTestRoot(t)
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", root)
			encoded, err := encodeDNSEngineState(legacyDurableDNSState(test.engine))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dnsEngineStatePath(), encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			err = inspectLegacyPowerDNSDurableAuthorityOnHost(false)
			if test.wantError && err == nil {
				t.Fatal("persisted BIND authority unexpectedly allowed PowerDNS")
			}
			if !test.wantError && err != nil {
				t.Fatalf("persisted PowerDNS authority rejected: %v", err)
			}
		})
	}
}

func TestInspectLegacyPowerDNSDurableAuthorityJournalTakesPrecedence(t *testing.T) {
	journal := testBINDSwitchJournal(t)
	encodedJournal, err := encodeDNSEngineSwitchJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dnsEngineSwitchJournalPath(), encodedJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	encodedState, err := encodeDNSEngineState(
		legacyDurableDNSState(transport.DNSEnginePowerDNS),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(serviceMutationStateDirectory(), "dns-engine-state.json"),
		encodedState, 0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := inspectLegacyPowerDNSDurableAuthorityOnHost(false); err == nil {
		t.Fatal("active persisted switch journal unexpectedly allowed PowerDNS")
	}
}
