package main

import (
	"context"
	"errors"
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

func TestDNSPort53ConflictParserAllowsScopedLocalStubs(t *testing.T) {
	output := "udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:((\"systemd-resolve\",pid=13,fd=4))\n" +
		"tcp LISTEN 0 4096 [fe80::1%eth0]:53 [::]:* users:((\"systemd-resolve\",pid=13,fd=5))"
	if hasUnrelatedPublicDNSListener(output, false, false) {
		t.Fatal("scoped loopback and link-local resolver stubs were rejected")
	}
}

func TestDNSPort53ConflictParserAllowsOnlyDeclaredEngineOwners(t *testing.T) {
	const (
		bindTCP = `tcp LISTEN 0 128 192.0.2.10:53 0.0.0.0:* users:(("named",pid=10,fd=1))`
		pdnsUDP = `udp UNCONN 0 0 [2001:db8::10]:53 [::]:* users:(("pdns_server",pid=11,fd=2))`
		foreign = `udp UNCONN 0 0 0.0.0.0:53 0.0.0.0:* users:(("dnsmasq",pid=12,fd=3))`
		local   = `udp UNCONN 0 0 127.0.0.53:53 0.0.0.0:* users:(("systemd-resolve",pid=13,fd=4))`
	)
	for _, test := range []struct {
		name                 string
		output               string
		allowBIND, allowPDNS bool
		wantConflict         bool
	}{
		{name: "local stub", output: local},
		{name: "declared BIND", output: bindTCP, allowBIND: true},
		{name: "undeclared BIND", output: bindTCP, wantConflict: true},
		{name: "declared PowerDNS", output: pdnsUDP, allowPDNS: true},
		{name: "undeclared PowerDNS", output: pdnsUDP, wantConflict: true},
		{name: "foreign listener", output: foreign, allowBIND: true, wantConflict: true},
		{name: "missing owner evidence", output: "tcp LISTEN 0 128 *:53 *:*", allowBIND: true, wantConflict: true},
		{name: "mixed owner output", output: bindTCP + "\n" + foreign, allowBIND: true, wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := hasUnrelatedPublicDNSListener(
				test.output, test.allowBIND, test.allowPDNS,
			); got != test.wantConflict {
				t.Fatalf("conflict=%v want=%v", got, test.wantConflict)
			}
		})
	}
}

func TestDNSPort53PreMutationGuardRejectsBeforeAnyMutationCallback(t *testing.T) {
	previous := dnsPort53ConflictCheck
	t.Cleanup(func() { dnsPort53ConflictCheck = previous })
	probeCalls, mutationCalls := 0, 0
	dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) {
		probeCalls++
		return true, nil
	}
	err := runDNSPort53PreMutationGuard(context.Background(), true, func() error {
		mutationCalls++
		return nil
	})
	if err == nil || probeCalls != 1 || mutationCalls != 0 {
		t.Fatalf("err=%v probeCalls=%d mutationCalls=%d", err, probeCalls, mutationCalls)
	}
	dnsPort53ConflictCheck = func(context.Context, bool, bool) (bool, error) {
		probeCalls++
		return false, nil
	}
	if err := runDNSPort53PreMutationGuard(context.Background(), true, func() error {
		mutationCalls++
		return nil
	}); err != nil || mutationCalls != 1 {
		t.Fatalf("safe callback err=%v mutationCalls=%d", err, mutationCalls)
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
	secondary := pdns
	secondary.PairRole = transport.DNSPairRoleSecondary
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
		{name: "secondary PowerDNS remains readable", state: secondary, stateExists: true},
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

func TestValidateLegacyPowerDNSMutationAuthorityRejectsSecondaryOnly(t *testing.T) {
	standalone := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	primary := standalone
	primary.PairRole = transport.DNSPairRolePrimary
	primary.PrimaryCatalogSerial = 7
	secondary := standalone
	secondary.PairRole = transport.DNSPairRoleSecondary
	for _, test := range []struct {
		name      string
		state     dnsEngineStateReceipt
		wantError bool
	}{
		{name: "standalone", state: standalone},
		{name: "primary", state: primary},
		{name: "secondary", state: secondary, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLegacyPowerDNSMutationAuthority(
				test.state, true, false, true,
			)
			if test.wantError && (err == nil || err.Error() != dnsSecondaryWriteDeniedError) {
				t.Fatalf("secondary mutation authority error=%v", err)
			}
			if !test.wantError && err != nil {
				t.Fatalf("write-authoritative PowerDNS rejected: %v", err)
			}
		})
	}
}

func TestLegacyPowerDNSReadSafetyKeepsSecondaryManagedWithoutWriteAuthority(t *testing.T) {
	secondary := legacyDurableDNSState(transport.DNSEnginePowerDNS)
	secondary.PairRole = transport.DNSPairRoleSecondary
	oldDurable := legacyPowerDNSDurableAuthorityCheck
	oldMutation := legacyPowerDNSMutationAuthorityCheck
	oldRuntime := legacyPowerDNSRuntimeSafetyCheck
	durableCalls, mutationCalls, runtimeCalls := 0, 0, 0
	legacyPowerDNSDurableAuthorityCheck = func(requireResolved bool) error {
		durableCalls++
		return validateLegacyPowerDNSDurableAuthority(
			secondary, true, false, requireResolved,
		)
	}
	legacyPowerDNSMutationAuthorityCheck = func(bool) error {
		mutationCalls++
		return errors.New("readiness must not request write authority")
	}
	legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
		runtimeCalls++
		return nil
	}
	t.Cleanup(func() {
		legacyPowerDNSDurableAuthorityCheck = oldDurable
		legacyPowerDNSMutationAuthorityCheck = oldMutation
		legacyPowerDNSRuntimeSafetyCheck = oldRuntime
	})
	if err := requireLegacyPowerDNSReadSafe(context.Background(), true); err != nil {
		t.Fatalf("secondary read safety rejected: %v", err)
	}
	if durableCalls != 1 || mutationCalls != 0 || runtimeCalls != 1 {
		t.Fatalf(
			"read proof calls durable=%d mutation=%d runtime=%d",
			durableCalls, mutationCalls, runtimeCalls,
		)
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
			oldMutation := legacyPowerDNSMutationAuthorityCheck
			oldRuntime := legacyPowerDNSRuntimeSafetyCheck
			legacyPowerDNSMutationAuthorityCheck = test.durable
			runtimeCalls := 0
			legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
				runtimeCalls++
				return nil
			}
			t.Cleanup(func() {
				legacyPowerDNSMutationAuthorityCheck = oldMutation
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
	oldMutation := legacyPowerDNSMutationAuthorityCheck
	oldRuntime := legacyPowerDNSRuntimeSafetyCheck
	stateExists := true
	legacyPowerDNSMutationAuthorityCheck = func(requireResolved bool) error {
		if stateExists {
			return validateLegacyPowerDNSMutationAuthority(
				legacyDurableDNSState(transport.DNSEnginePowerDNS),
				true, false, requireResolved,
			)
		}
		return validateLegacyPowerDNSMutationAuthority(
			dnsEngineStateReceipt{}, false, false, requireResolved,
		)
	}
	runtimeCalls := 0
	legacyPowerDNSRuntimeSafetyCheck = func(context.Context, bool) error {
		runtimeCalls++
		return nil
	}
	t.Cleanup(func() {
		legacyPowerDNSMutationAuthorityCheck = oldMutation
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
