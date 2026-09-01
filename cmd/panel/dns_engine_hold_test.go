package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// A stuck transaction and an intruder look identical on the wire: both leave a
// running engine with Managed=false. They are opposite diagnoses — one is
// "our own change system is held, recover it", the other is "someone installed
// a DNS server behind our back, investigate the host" — and sending an operator
// after the wrong one is how the Boston afternoon disappeared. The mutation hold
// is what tells them apart.
func TestUnmanagedEngineReadsAsHeldWhenMutationsAreHeld(t *testing.T) {
	state := dnsEngineDBState{ActiveEngine: transport.DNSEnginePowerDNS}
	runtimes := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Installed: true, Running: true, Managed: false,
		},
		transport.DNSEngineBIND: {Engine: transport.DNSEngineBIND},
	}

	// No hold: an unclaimed running engine is genuinely a foreign one.
	_, entries := deriveDNSEnginePresentation(state, runtimes, nil, "")
	if code := detailCodeFor(t, entries, transport.DNSEnginePowerDNS); code != "unmanaged_dns_detected" {
		t.Fatalf("without a hold the engine must read as foreign, got %q", code)
	}

	// Held: the same wire state now has a different cause, and the screen must
	// say so rather than accusing the host of an intruder.
	_, entries = deriveDNSEnginePresentation(
		state, runtimes, nil, transport.MutationHoldLedgerAmbiguous,
	)
	if code := detailCodeFor(t, entries, transport.DNSEnginePowerDNS); code != "mutations_held" {
		t.Fatalf("with a hold the engine must read as held, got %q", code)
	}
}

// The same correction applies to an installed-but-stopped engine: it is the
// state a half-finished install leaves, and it is reported through the same
// unmanaged branch.
func TestInstalledStandbyReadsAsHeldWhenMutationsAreHeld(t *testing.T) {
	state := dnsEngineDBState{}
	runtimes := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Installed: true, Managed: false,
		},
		transport.DNSEngineBIND: {Engine: transport.DNSEngineBIND},
	}
	_, entries := deriveDNSEnginePresentation(
		state, runtimes, nil, transport.MutationHoldLedgerUnavailable,
	)
	if code := detailCodeFor(t, entries, transport.DNSEnginePowerDNS); code != "mutations_held" {
		t.Fatalf("an installed engine under a hold must read as held, got %q", code)
	}
}

// A hold must not rewrite states that have nothing to do with ownership: an
// engine the panel does own still reads as active, and a port conflict is still
// a port conflict. Over-applying the correction would hide real diagnoses.
func TestMutationHoldDoesNotRewriteUnrelatedStates(t *testing.T) {
	state := dnsEngineDBState{ActiveEngine: transport.DNSEnginePowerDNS}
	owned := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Installed: true, Running: true, Managed: true,
		},
		transport.DNSEngineBIND: {Engine: transport.DNSEngineBIND},
	}
	_, entries := deriveDNSEnginePresentation(
		state, owned, nil, transport.MutationHoldLedgerAmbiguous,
	)
	for _, entry := range entries {
		if entry.ID != transport.DNSEnginePowerDNS {
			continue
		}
		if entry.Status != "active" {
			t.Fatalf("an owned running engine stays active under a hold, got %q", entry.Status)
		}
		if entry.DetailCode != "" {
			t.Fatalf("an active engine carries no detail code, got %q", entry.DetailCode)
		}
	}

	conflicting := map[transport.DNSEngine]transport.DNSBackendRuntimeState{
		transport.DNSEnginePowerDNS: {
			Engine: transport.DNSEnginePowerDNS, Installed: true, Running: true, Managed: true,
		},
		transport.DNSEngineBIND: {
			Engine: transport.DNSEngineBIND, Installed: true, Running: true,
		},
	}
	_, entries = deriveDNSEnginePresentation(
		state, conflicting, nil, transport.MutationHoldLedgerAmbiguous,
	)
	for _, entry := range entries {
		if entry.Status == "conflict" && entry.DetailCode != "port_53_conflict" {
			t.Fatalf("a port conflict must survive a hold, got %q", entry.DetailCode)
		}
	}
}

// The readiness validator must carry the hold through untouched: a hold dropped
// in validation is a hold the panel never sees.
func TestReadinessValidationCarriesTheMutationHold(t *testing.T) {
	response := transport.DNSBackendReadinessResponse{
		Engines: []transport.DNSBackendRuntimeState{
			{Engine: transport.DNSEnginePowerDNS},
			{Engine: transport.DNSEngineBIND},
		},
		MutationHold: transport.MutationHoldLedgerAmbiguous,
	}
	_, _, hold, err := validateDNSBackendReadiness(response)
	if err != nil {
		t.Fatalf("valid readiness must validate: %v", err)
	}
	if hold != transport.MutationHoldLedgerAmbiguous {
		t.Fatalf("hold = %q, want %q", hold, transport.MutationHoldLedgerAmbiguous)
	}

	response.MutationHold = ""
	if _, _, hold, err = validateDNSBackendReadiness(response); err != nil || hold != "" {
		t.Fatalf("an accepting agent must report no hold, got %q / %v", hold, err)
	}
}

func detailCodeFor(t *testing.T, entries []dnsEngineEntry, id transport.DNSEngine) string {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id {
			return entry.DetailCode
		}
	}
	t.Fatalf("engine %q is missing from the presentation", id)
	return ""
}
