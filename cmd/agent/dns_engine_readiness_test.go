package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
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

func TestStandbyDNSBackendManagedRequiresExactOwnership(t *testing.T) {
	stopped := transport.DNSBackendRuntimeState{Installed: true}
	pdns := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	if !powerDNSStandbyManagedForBackendReadiness(
		stopped, pdns, true, func() error { return nil },
	) {
		t.Fatal("exact stopped PowerDNS ownership was not recognized")
	}
	if powerDNSStandbyManagedForBackendReadiness(
		stopped, pdns, false, func() error { return nil },
	) || powerDNSStandbyManagedForBackendReadiness(
		stopped, pdns, true, func() error { return errors.New("tampered config") },
	) {
		t.Fatal("unowned or tampered PowerDNS standby was accepted")
	}
	bind := legacyDurableDNSState(transport.DNSEngineBIND)
	if !bindStandbyManagedForBackendReadiness(
		stopped, bind, true, bind.EngineEpoch, bind.Generation,
	) {
		t.Fatal("exact stopped BIND ownership was not recognized")
	}
	if bindStandbyManagedForBackendReadiness(
		stopped, bind, true, bind.EngineEpoch, strings.Repeat("e", 64),
	) || bindStandbyManagedForBackendReadiness(
		transport.DNSBackendRuntimeState{Installed: true, Running: true},
		bind, true, bind.EngineEpoch, bind.Generation,
	) {
		t.Fatal("stale or running BIND standby was accepted")
	}
}

func TestInstallOwnedStandbyRequiresExactHostPackageReceipt(t *testing.T) {
	stopped := transport.DNSBackendRuntimeState{Installed: true}
	receipt := dnsEngineInstallOwnershipReceipt{
		Schema:            dnsEngineInstallOwnershipSchema,
		Engine:            transport.DNSEngineBIND,
		PackageManager:    string(hostplatform.PackageManagerAPT),
		Packages:          []string{"bind9"},
		MissingBefore:     []string{"bind9"},
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("b", 32),
		MutationOwnerID:   strings.Repeat("c", 32),
	}
	if !installOwnedStandbyManagedForBackendReadiness(
		stopped, receipt, true, transport.DNSEngineBIND,
		hostplatform.PackageManagerAPT, []string{"bind9"},
	) {
		t.Fatal("exact panel install ownership was not recognized")
	}
	if installOwnedStandbyManagedForBackendReadiness(
		stopped, receipt, false, transport.DNSEngineBIND,
		hostplatform.PackageManagerAPT, []string{"bind9"},
	) || installOwnedStandbyManagedForBackendReadiness(
		stopped, receipt, true, transport.DNSEngineBIND,
		hostplatform.PackageManagerPacman, []string{"bind"},
	) {
		t.Fatal("missing or cross-platform install ownership was accepted")
	}
	if installOwnershipFallbackAllowed(true, nil) ||
		installOwnershipFallbackAllowed(false, errors.New("tampered receipt")) ||
		!installOwnershipFallbackAllowed(false, nil) {
		t.Fatal("install ownership fallback did not fail closed around engine ownership")
	}
	managed := bindStandbyManagedForBackendReadiness(
		stopped, legacyDurableDNSState(transport.DNSEngineBIND), true,
		1, strings.Repeat("f", 64),
	)
	if !managed && installOwnershipFallbackAllowed(true, nil) {
		managed = installOwnedStandbyManagedForBackendReadiness(
			stopped, receipt, true, transport.DNSEngineBIND,
			hostplatform.PackageManagerAPT, []string{"bind9"},
		)
	}
	if managed {
		t.Fatal("stale engine ownership fell through to install ownership")
	}
}
