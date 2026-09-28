//go:build linux

package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

func TestPDNSTargetStageUnitClassifierV4(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}},
	}
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
	}
	if active, err := classifyPDNSTargetStageUnitsV4(j, units); err != nil || !active {
		t.Fatalf("source preimage rejected: %v", err)
	}
	units[0].ActiveState, units[1].ActiveState = "inactive", "inactive"
	units[0].UnitFileState, units[1].UnitFileState = "disabled", "disabled"
	if active, err := classifyPDNSTargetStageUnitsV4(j, units); err != nil || active {
		t.Fatalf("operation-stopped source rejected: %v", err)
	}
	units[0].UnitFileState = "enabled"
	if active, err := classifyPDNSTargetStageUnitsV4(j, units); err != nil || active {
		t.Fatalf("partly re-enabled source rejected: %v", err)
	}
	units[1].UnitFileState = "enabled"
	if active, err := classifyPDNSTargetStageUnitsV4(j, units); err != nil || active {
		t.Fatalf("re-enabled source before start rejected: %v", err)
	}
	units[2].ActiveState = "active"
	if _, err := classifyPDNSTargetStageUnitsV4(j, units); err == nil {
		t.Fatal("started target admitted as pre-activation checkpoint")
	}
	units[2].ActiveState = "inactive"
	units[0].UnitFileState = "masked"
	if _, err := classifyPDNSTargetStageUnitsV4(j, units); err == nil {
		t.Fatal("foreign source unit state admitted")
	}
}

func TestPDNSTargetStageReplayAfterCandidateUnlinkV4(t *testing.T) {
	checkpoint := func(phase string, staged, renamed, absent, enabled, active, before, unitsExact bool) dnsenginerecovery.PDNSTargetStageState {
		t.Helper()
		state, err := classifyPDNSTargetStageCheckpointV4(phase, staged, renamed, absent, enabled, active, before, unitsExact)
		if err != nil {
			t.Fatalf("checkpoint rejected: %v", err)
		}
		return state
	}
	if got := checkpoint(dnsengineartifact.SwitchPhaseRollingBack, false, false, true, false, false, false, false); got != dnsenginerecovery.PDNSTargetStageNeedsRestore {
		t.Fatalf("absent candidate with mixed config and stopped BIND must resume: %v", got)
	}
	if got := checkpoint(dnsengineartifact.SwitchPhaseRollingBack, false, false, true, false, true, true, true); got != dnsenginerecovery.PDNSTargetStageRestored {
		t.Fatalf("fully restored source rejected: %v", got)
	}
	if got := checkpoint(dnsengineartifact.SwitchPhaseRollingBack, false, true, false, false, false, false, false); got != dnsenginerecovery.PDNSTargetStageRenamed {
		t.Fatalf("exact renamed pre-start target was not classified separately: %v", got)
	}
	for _, tc := range []struct {
		phase                                                        string
		staged, renamed, absent, enabled, active, before, unitsExact bool
	}{
		{dnsengineartifact.SwitchPhaseRollingBack, false, false, false, false, false, false, false},
		{dnsengineartifact.SwitchPhaseRollingBack, true, false, true, false, false, false, false},
		{dnsengineartifact.SwitchPhaseRolledBack, false, false, true, false, false, false, false},
		{dnsengineartifact.SwitchPhaseTargetStarted, true, false, false, false, false, false, false},
		{dnsengineartifact.SwitchPhaseRollingBack, false, true, false, false, true, false, false},
	} {
		if _, err := classifyPDNSTargetStageCheckpointV4(tc.phase, tc.staged, tc.renamed, tc.absent, tc.enabled, tc.active, tc.before, tc.unitsExact); err == nil {
			t.Fatalf("unsafe checkpoint admitted: %+v", tc)
		}
	}
}

func TestPDNSTargetStageEnabledOnlyWithDurableEnableIntentV4(t *testing.T) {
	classify := func(phase string, renamed, enabled, sourceActive bool) (dnsenginerecovery.PDNSTargetStageState, error) {
		return classifyPDNSTargetStageCheckpointV4(phase, false, renamed, false, enabled, sourceActive, false, false)
	}
	state, err := classify(dnsengineartifact.SwitchPhaseRollingBackTargetEnable, true, true, false)
	if err != nil || state != dnsenginerecovery.PDNSTargetStageRenamedEnabled {
		t.Fatalf("exact enabled and inactive cut rejected: %v %v", state, err)
	}
	for _, tc := range []struct {
		phase                          string
		renamed, enabled, sourceActive bool
	}{
		{dnsengineartifact.SwitchPhaseRollingBack, true, true, false},
		{dnsengineartifact.SwitchPhaseRollingBackTargetEnable, false, true, false},
		{dnsengineartifact.SwitchPhaseRollingBackTargetEnable, true, true, true},
		{dnsengineartifact.SwitchPhaseRolledBack, true, true, false},
	} {
		if _, err := classify(tc.phase, tc.renamed, tc.enabled, tc.sourceActive); err == nil {
			t.Fatalf("unsafe enabled target admitted: %+v", tc)
		}
	}
}

func TestPDNSTargetEnabledUnitRequiresRollbackEnableIntentV4(t *testing.T) {
	j := dnsengineartifact.SwitchJournalV1{
		SourceUnitsBefore: []dnsengineartifact.UnitSnapshot{
			{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
			{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		},
		TargetUnitsBefore: []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}},
	}
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"},
	}
	if _, err := classifyPDNSTargetStageUnitsV4(j, units); err == nil {
		t.Fatal("enabled target admitted without durable enable intent")
	}
	j.Phase = dnsengineartifact.SwitchPhaseRollingBackTargetEnable
	if active, err := classifyPDNSTargetStageUnitsV4(j, units); err != nil || active {
		t.Fatalf("frozen enabled and inactive target rejected: %v %v", active, err)
	}
	units[2].ActiveState = "active"
	if _, err := classifyPDNSTargetStageUnitsV4(j, units); err == nil {
		t.Fatal("started target admitted by enable intent")
	}
}

func TestNoPublicDNSPort53ListenersV4AllowsOnlyCanonicalLocalResolver(t *testing.T) {
	local := []byte("udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:((\"systemd-resolve\",pid=368,fd=18))\n" +
		"tcp LISTEN 0 4096 127.0.0.54:53 0.0.0.0:* users:((\"systemd-resolve\",pid=368,fd=21))\n")
	if err := requireNoPublicDNSPort53ListenersV4(local); err != nil {
		t.Fatalf("canonical local resolver refused: %v", err)
	}
	for _, rows := range [][]byte{
		[]byte("udp UNCONN 0 0 192.0.2.10:53 0.0.0.0:* users:((\"named\",pid=777,fd=5))\n"),
		[]byte("tcp LISTEN 0 4096 0.0.0.0:53 0.0.0.0:* users:((\"pdns_server\",pid=777,fd=5))\n"),
		[]byte("arbitrary-output\n"),
	} {
		if err := requireNoPublicDNSPort53ListenersV4(rows); err == nil {
			t.Fatalf("unsafe port-53 inventory admitted: %q", rows)
		}
	}
}
