//go:build linux

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

func TestPDNSAdoptionNativeProofRequiresSoleActiveAuthority(t *testing.T) {
	units := []dnsenginerecovery.NativeUnitObservation{
		{Name: "named.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"},
		{Name: "pdns.service", LoadState: "loaded", ActiveState: "active"},
	}
	if err := proveOnlyPDNSActiveUnits(units); err != nil {
		t.Fatalf("single PowerDNS authority rejected: %v", err)
	}
	for _, change := range []struct {
		name string
		edit func([]dnsenginerecovery.NativeUnitObservation)
	}{
		{"named-started", func(v []dnsenginerecovery.NativeUnitObservation) { v[0].ActiveState = "active" }},
		{"bind-alias-started", func(v []dnsenginerecovery.NativeUnitObservation) { v[1].ActiveState = "active" }},
		{"pdns-stopped", func(v []dnsenginerecovery.NativeUnitObservation) { v[2].ActiveState = "inactive" }},
		{"pdns-missing", func(v []dnsenginerecovery.NativeUnitObservation) { v[2].LoadState = "not-found" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := append([]dnsenginerecovery.NativeUnitObservation(nil), units...)
			change.edit(changed)
			if err := proveOnlyPDNSActiveUnits(changed); err == nil {
				t.Fatal("ambiguous native DNS authority accepted")
			}
		})
	}
}

func TestPDNSAdoptionNativeProofRejectsOtherJournalBeforeNativeProbe(t *testing.T) {
	_, err := proveInstalledPDNSAdoptionNative(context.Background(), dnsengineartifact.JournalPolicy{}, dnsengineartifact.SwitchJournalV1{})
	if err == nil || !strings.Contains(err.Error(), "exact PowerDNS adoption journal") {
		t.Fatalf("unexpected journal result: %v", err)
	}
}
