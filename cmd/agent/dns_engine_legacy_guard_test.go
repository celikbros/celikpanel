package main

import "testing"

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
