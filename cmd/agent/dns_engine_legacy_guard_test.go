package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func legacyDNSUnit(name, active, load, unitFile string) dnsUnitState {
	return dnsUnitState{
		Name: name, ActiveState: active, LoadState: load, UnitFileState: unitFile,
	}
}

func TestValidateLegacyPowerDNSUnitStatesBlocksBINDAndMissingTarget(t *testing.T) {
	inactiveBIND := legacyDNSUnit("named.service", "inactive", "loaded", "disabled")
	inactiveAlias := legacyDNSUnit("bind9.service", "inactive", "loaded", "disabled")
	activePDNS := legacyDNSUnit("pdns.service", "active", "loaded", "enabled")
	activeBIND := inactiveBIND
	activeBIND.ActiveState = "active"
	if _, err := validateLegacyPowerDNSUnitStates(
		activeBIND, inactiveAlias, activePDNS, false,
	); err == nil {
		t.Fatal("legacy PowerDNS configure accepted active BIND")
	}
	missingPDNS := legacyDNSUnit("pdns.service", "inactive", "not-found", "")
	if _, err := validateLegacyPowerDNSUnitStates(
		inactiveBIND, inactiveAlias, missingPDNS, false,
	); err == nil {
		t.Fatal("legacy PowerDNS configure accepted an uninstalled target")
	}
}

func TestValidateLegacyPowerDNSUnitStatesAllowsRepairButClusterNeedsActive(t *testing.T) {
	inactiveBIND := legacyDNSUnit("named.service", "inactive", "loaded", "disabled")
	inactiveAlias := legacyDNSUnit("bind9.service", "inactive", "loaded", "disabled")
	inactivePDNS := legacyDNSUnit("pdns.service", "inactive", "loaded", "masked")
	active, err := validateLegacyPowerDNSUnitStates(
		inactiveBIND, inactiveAlias, inactivePDNS, false,
	)
	if err != nil || active {
		t.Fatalf("stopped PowerDNS repair active=%t err=%v", active, err)
	}
	if _, err := validateLegacyPowerDNSUnitStates(
		inactiveBIND, inactiveAlias, inactivePDNS, true,
	); err == nil {
		t.Fatal("cluster mutation accepted inactive PowerDNS")
	}
	activePDNS := inactivePDNS
	activePDNS.ActiveState = "active"
	active, err = validateLegacyPowerDNSUnitStates(
		inactiveBIND, inactiveAlias, activePDNS, true,
	)
	if err != nil || !active {
		t.Fatalf("active sole PowerDNS target active=%t err=%v", active, err)
	}
}

func TestRejectLegacyPublicDNSListenersAllowsOnlyLocalStub(t *testing.T) {
	if err := rejectLegacyPublicDNSListeners(
		"udp UNCONN 0 0 127.0.0.53:53 0.0.0.0:*\n",
	); err != nil {
		t.Fatalf("local resolver stub was rejected: %v", err)
	}
	for _, output := range []string{
		"udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:*\n",
		"tcp LISTEN 0 128 192.0.2.10:53 0.0.0.0:*\n",
		"udp UNCONN 0 0 [::]:53 [::]:*\n",
	} {
		if err := rejectLegacyPublicDNSListeners(output); err == nil {
			t.Fatalf("public DNS listener was accepted: %q", output)
		}
	}
}

func legacyDurableDNSState(engine transport.DNSEngine) dnsEngineStateReceipt {
	state := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: engine, EngineEpoch: 1, SourceRevision: 0,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" + strings.Repeat("a", 64),
		MutationRequestID: strings.Repeat("b", 32),
		MutationOwnerID:   strings.Repeat("c", 32),
	}
	if engine == transport.DNSEngineBIND {
		state.Generation = strings.Repeat("d", 64)
	}
	return state
}

func TestValidateLegacyPowerDNSDurableAuthority(t *testing.T) {
	pdns := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	bind := legacyDurableDNSState(transport.DNSEngineBIND)
	for _, test := range []struct {
		name            string
		state           dnsEngineStateReceipt
		stateExists     bool
		journalExists   bool
		requireResolved bool
		wantError       bool
	}{
		{name: "exact PowerDNS state", state: pdns, stateExists: true},
		{
			name:  "stopped BIND state remains authoritative",
			state: bind, stateExists: true, wantError: true,
		},
		{
			name:          "active switch journal blocks PowerDNS",
			journalExists: true, wantError: true,
		},
		{name: "legacy unresolved compatibility"},
		{
			name: "resolved state required", requireResolved: true,
			wantError: true,
		},
		{
			name: "invalid persisted PowerDNS state",
			state: dnsEngineStateReceipt{
				Schema: dnsEngineStateSchema, Engine: transport.DNSEnginePowerDNS,
			},
			stateExists: true, wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLegacyPowerDNSDurableAuthority(
				test.state, test.stateExists, test.journalExists,
				test.requireResolved,
			)
			if test.wantError && err == nil {
				t.Fatal("unsafe durable authority unexpectedly accepted")
			}
			if !test.wantError && err != nil {
				t.Fatalf("safe durable authority rejected: %v", err)
			}
		})
	}
}

func TestLegacyPowerDNSDurableAuthorityPrecedesRuntimeInspection(t *testing.T) {
	for _, test := range []struct {
		name          string
		durable       func(bool) error
		requireActive bool
	}{
		{
			name: "BIND state with stopped units and no listener",
			durable: func(requireResolved bool) error {
				return validateLegacyPowerDNSDurableAuthority(
					legacyDurableDNSState(transport.DNSEngineBIND),
					true, false, requireResolved,
				)
			},
		},
		{
			name: "active switch journal",
			durable: func(requireResolved bool) error {
				return validateLegacyPowerDNSDurableAuthority(
					dnsEngineStateReceipt{}, false, true, requireResolved,
				)
			},
			requireActive: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldDurable := legacyPowerDNSDurableAuthorityCheck
			oldRuntime := legacyPowerDNSRuntimeSafetyCheck
			legacyPowerDNSDurableAuthorityCheck = test.durable
			runtimeCalls := 0
			legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
				runtimeCalls++
				return nil
			}
			t.Cleanup(func() {
				legacyPowerDNSDurableAuthorityCheck = oldDurable
				legacyPowerDNSRuntimeSafetyCheck = oldRuntime
			})
			if err := requireLegacyPowerDNSMutationSafe(
				context.Background(), test.requireActive,
			); err == nil {
				t.Fatal("unsafe durable authority unexpectedly reached runtime")
			}
			if runtimeCalls != 0 {
				t.Fatalf("durable rejection performed %d runtime inspections", runtimeCalls)
			}
		})
	}
}

func TestLegacyPowerDNSDurableAuthorityAllowsExactPDNSAndIntentionalLegacy(t *testing.T) {
	oldDurable := legacyPowerDNSDurableAuthorityCheck
	oldRuntime := legacyPowerDNSRuntimeSafetyCheck
	stateExists := true
	legacyPowerDNSDurableAuthorityCheck = func(requireResolved bool) error {
		if stateExists {
			return validateLegacyPowerDNSDurableAuthority(
				legacyDurableDNSState(transport.DNSEnginePowerDNS),
				true, false, requireResolved,
			)
		}
		return validateLegacyPowerDNSDurableAuthority(
			dnsEngineStateReceipt{}, false, false, requireResolved,
		)
	}
	runtimeCalls := 0
	legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
		runtimeCalls++
		return nil
	}
	t.Cleanup(func() {
		legacyPowerDNSDurableAuthorityCheck = oldDurable
		legacyPowerDNSRuntimeSafetyCheck = oldRuntime
	})
	if err := requireLegacyPowerDNSMutationSafe(
		context.Background(), true,
	); err != nil {
		t.Fatalf("exact active PowerDNS state rejected: %v", err)
	}
	stateExists = false
	if err := requireLegacyPowerDNSMutationSafe(
		context.Background(), false,
	); err != nil {
		t.Fatalf("legacy unresolved compatibility rejected: %v", err)
	}
	if err := requireLegacyPowerDNSMutationSafeWithAuthority(
		context.Background(), true, true,
	); err == nil {
		t.Fatal("exact-authority operation accepted unresolved legacy state")
	}
	if runtimeCalls != 2 {
		t.Fatalf("runtime inspections=%d want=2", runtimeCalls)
	}
}
