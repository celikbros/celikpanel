package main

import (
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPowerDNSManagedForBackendReadinessRequiresExactLegacyProof(t *testing.T) {
	ready := transport.DNSBackendRuntimeState{
		Engine: transport.DNSEnginePowerDNS, Installed: true,
		Running: true, Unit: "pdns.service",
	}
	missingState := func() error { return nil }
	switchJournal := func() error {
		return validateLegacyPowerDNSDurableAuthority(
			dnsEngineStateReceipt{}, false, true, false,
		)
	}

	for _, test := range []struct {
		name               string
		state              dnsEngineStateReceipt
		stateExists        bool
		runtime            transport.DNSBackendRuntimeState
		managedConfigError error
		exactActiveProof   func() error
		wantManaged        bool
		wantConfigCalls    int
		wantActiveCalls    int
	}{
		{
			name:    "registration-only managed PowerDNS is adoptable",
			runtime: ready, exactActiveProof: missingState, wantManaged: true,
			wantConfigCalls: 1, wantActiveCalls: 1,
		},
		{
			name:  "existing exact PowerDNS receipt remains managed",
			state: legacyDurableDNSState(transport.DNSEnginePowerDNS), stateExists: true,
			runtime: ready, exactActiveProof: func() error { return nil }, wantManaged: true,
			wantConfigCalls: 1, wantActiveCalls: 1,
		},
		{
			name:    "manual PowerDNS configuration stays unmanaged",
			runtime: ready, managedConfigError: errors.New("not panel-managed"),
			exactActiveProof: missingState, wantConfigCalls: 1,
		},
		{
			name:    "unsafe runtime ownership stays unmanaged",
			runtime: ready, exactActiveProof: func() error {
				return errors.New("BIND or another process owns port 53")
			},
			wantConfigCalls: 1, wantActiveCalls: 1,
		},
		{
			name:  "BIND receipt keeps standby PowerDNS unmanaged",
			state: legacyDurableDNSState(transport.DNSEngineBIND), stateExists: true,
			runtime: ready, exactActiveProof: func() error { return nil },
		},
		{
			name:    "active switch journal keeps PowerDNS unmanaged",
			runtime: ready, exactActiveProof: switchJournal,
			wantConfigCalls: 1, wantActiveCalls: 1,
		},
		{
			name: "stopped PowerDNS cannot be managed",
			runtime: transport.DNSBackendRuntimeState{
				Engine: transport.DNSEnginePowerDNS, Installed: true,
				Unit: "pdns.service",
			},
			exactActiveProof: missingState,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			configCalls, activeCalls := 0, 0
			managed := powerDNSManagedForBackendReadiness(
				test.state,
				test.stateExists,
				test.runtime,
				func() error {
					configCalls++
					return test.managedConfigError
				},
				func() error {
					activeCalls++
					return test.exactActiveProof()
				},
			)
			if managed != test.wantManaged ||
				configCalls != test.wantConfigCalls ||
				activeCalls != test.wantActiveCalls {
				t.Fatalf(
					"managed=%v configCalls=%d activeCalls=%d",
					managed, configCalls, activeCalls,
				)
			}
		})
	}
}

func TestPowerDNSManagedForBackendReadinessRejectsMissingProofs(t *testing.T) {
	runtimeState := transport.DNSBackendRuntimeState{
		Engine: transport.DNSEnginePowerDNS, Installed: true,
		Running: true, Unit: "pdns.service",
	}
	if powerDNSManagedForBackendReadiness(dnsEngineStateReceipt{}, false, runtimeState, nil, func() error { return nil }) ||
		powerDNSManagedForBackendReadiness(dnsEngineStateReceipt{}, false, runtimeState, func() error { return nil }, nil) {
		t.Fatal("readiness accepted an incomplete managed PowerDNS proof")
	}
}

func TestExactActiveDNSBackendManagedRejectsIncompleteOrConflictingRuntime(t *testing.T) {
	ready := transport.DNSBackendRuntimeState{
		Installed: true, Running: true,
	}
	for _, test := range []struct {
		name     string
		runtime  transport.DNSBackendRuntimeState
		evidence bool
		proof    func() error
		want     bool
	}{
		{name: "exact active authority", runtime: ready, evidence: true, proof: func() error { return nil }, want: true},
		{name: "missing immutable evidence", runtime: ready, proof: func() error { return nil }},
		{name: "stopped service", runtime: transport.DNSBackendRuntimeState{Installed: true}, evidence: true, proof: func() error { return nil }},
		{name: "mixed or incomplete listeners", runtime: ready, evidence: true, proof: func() error { return errors.New("listener conflict") }},
		{name: "missing runtime proof", runtime: ready, evidence: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := exactActiveDNSBackendManaged(
				test.runtime, test.evidence, test.proof,
			); got != test.want {
				t.Fatalf("managed=%v want=%v", got, test.want)
			}
		})
	}
}
