//go:build linux

package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

func TestRolledBackInactiveTargetUnitSelectsOnlyFrozenTarget(t *testing.T) {
	cases := []struct {
		name    string
		status  dnsenginerecovery.EvidenceStatus
		phase   string
		inverse dnsenginerecovery.NativeInverseKind
		units   []dnsengineartifact.UnitSnapshot
		want    string
	}{
		{"bind", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInverseBINDSwitch, []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "inactive"}}, "named.service"},
		{"pdns", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInversePDNSSwitch, []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}}, "pdns.service"},
		{"active preimage", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInverseBINDSwitch, []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "active"}}, ""},
		{"wrong phase", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRollingBack, dnsenginerecovery.NativeInverseBINDSwitch, []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "inactive"}}, ""},
		{"wrong status", dnsenginerecovery.EvidenceActive, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInverseBINDSwitch, []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "inactive"}}, ""},
		{"adoption", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInverseBINDRunningAdoption, []dnsengineartifact.UnitSnapshot{{Name: "named.service", ActiveState: "inactive"}}, ""},
		{"wrong target", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInverseBINDSwitch, []dnsengineartifact.UnitSnapshot{{Name: "pdns.service", ActiveState: "inactive"}}, ""},
		{"missing target", dnsenginerecovery.EvidenceTerminalRolledBack, dnsengineartifact.SwitchPhaseRolledBack, dnsenginerecovery.NativeInversePDNSSwitch, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := dnsenginerecovery.SwitchEvidence{
				Journal:     dnsengineartifact.SwitchJournalV1{Phase: tc.phase, TargetUnitsBefore: tc.units},
				Observation: dnsenginerecovery.EvidenceObservation{Status: tc.status, InverseKind: tc.inverse},
			}
			got, ok := rolledBackInactiveTargetUnit(evidence)
			if got != tc.want || ok != (tc.want != "") {
				t.Fatalf("target = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}
