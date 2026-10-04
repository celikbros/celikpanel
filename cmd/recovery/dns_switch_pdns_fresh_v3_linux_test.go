//go:build linux

package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

func TestFreshPDNSRecoveryTargetLoadV3(t *testing.T) {
	masked := dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}
	disabled := dnsengineartifact.UnitSnapshot{Name: "pdns.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled"}
	cases := []struct {
		name   string
		seen   dnsenginerecovery.NativeUnitObservation
		before dnsengineartifact.UnitSnapshot
		want   bool
	}{
		{"sealed-owned-mask", dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}, masked, true},
		{"loaded-after-enable", dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}, masked, true},
		{"unowned-mask", dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked"}, disabled, false},
		{"runtime-mask", dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "inactive", UnitFileState: "masked-runtime"}, masked, false},
		{"active-mask", dnsenginerecovery.NativeUnitObservation{Name: "pdns.service", LoadState: "masked", ActiveState: "active", UnitFileState: "masked"}, masked, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := freshPDNSRecoveryTargetLoadV3(tc.seen, tc.before); got != tc.want {
				t.Fatalf("accepted=%t, want %t", got, tc.want)
			}
		})
	}
}
