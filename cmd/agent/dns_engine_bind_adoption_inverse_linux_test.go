//go:build linux

package main

import (
	"context"
	"testing"
)

func TestBINDAdoptionAcceptsExactAuthoritativeZeroSOASerial(t *testing.T) {
	original := probeDNSZoneSOA
	t.Cleanup(func() { probeDNSZoneSOA = original })
	probeDNSZoneSOA = func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
		if network != "udp" || address != "192.0.2.10" || domain != "owner.test" {
			t.Fatalf("unexpected probe %s %s %s", network, address, domain)
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: 0, AnswerCount: 1,
			SOASerials: []uint32{0}, AnswerSOAOwners: []string{"owner.test"},
		}, nil
	}
	serial, err := probeBINDAdoptionSourceSOA("192.0.2.10")(context.Background(), "owner.test", "udp")
	if err != nil || serial != 0 {
		t.Fatalf("valid serial zero rejected: serial=%d err=%v", serial, err)
	}
}

func TestBINDIndependentAdoptionAcceptsOnlyExactRunningDebianAliasPreimages(t *testing.T) {
	named := dnsUnitState{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	active := dnsUnitState{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"}
	absent := dnsUnitState{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive"}
	for _, alias := range []dnsUnitState{active, absent} {
		if err := validateBINDIndependentAdoptionUnitPreimage(map[string]dnsUnitState{
			"named.service": named, "bind9.service": alias,
		}); err != nil {
			t.Fatalf("supported alias %#v refused: %v", alias, err)
		}
	}
	for _, alias := range []dnsUnitState{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "enabled"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "active"},
		{Name: "bind9.service", LoadState: "not-found", ActiveState: "inactive", UnitFileState: "disabled"},
	} {
		if err := validateBINDIndependentAdoptionUnitPreimage(map[string]dnsUnitState{
			"named.service": named, "bind9.service": alias,
		}); err == nil {
			t.Fatalf("unsupported alias %#v admitted", alias)
		}
	}
}
