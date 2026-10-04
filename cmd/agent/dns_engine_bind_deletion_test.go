package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBINDTopLevelDeletionRequiresAuthenticatedAbsenceAndBothDNSProtocols(t *testing.T) {
	const domain = "s1-kill.test"
	status := func(_ context.Context, got string) ([]byte, error) {
		if got != domain {
			t.Fatalf("zonestatus domain=%q", got)
		}
		return []byte("rndc: 'zonestatus' failed: not found\nno matching zone '" + domain + "' in any view\n"), errors.New("exit status 1")
	}
	var networks []string
	refused := func(_ context.Context, network, address, got string) (dnsSOAProbeResult, error) {
		if address != "192.0.2.11" || got != domain {
			t.Fatalf("probe address=%q domain=%q", address, got)
		}
		networks = append(networks, network)
		return dnsSOAProbeResult{RCode: dnsRCodeRefused}, nil
	}
	if err := verifyBINDV3AuthoritiesAt(context.Background(), "192.0.2.11",
		[]expectedDNSZoneAuthority{{Domain: domain, Delete: true}}, refused, status); err != nil {
		t.Fatal(err)
	}
	if strings.Join(networks, ",") != "udp,tcp" {
		t.Fatalf("networks=%v", networks)
	}
}

func TestBINDDeletionRejectsUnprovenAbsenceAndRemainingAuthority(t *testing.T) {
	const domain = "s1-kill.test"
	goodStatus := func(context.Context, string) ([]byte, error) {
		return []byte("rndc: 'zonestatus' failed: not found\nno matching zone '" + domain + "' in any view\n"), errors.New("exit status 1")
	}
	goodProbe := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
		return dnsSOAProbeResult{RCode: dnsRCodeRefused}, nil
	}
	for _, tc := range []struct {
		name   string
		status bindZoneStatusProbe
		probe  dnsZoneSOAProbe
	}{
		{"different-zone", func(context.Context, string) ([]byte, error) {
			return []byte("rndc: 'zonestatus' failed: not found\nno matching zone 'other.test' in any view\n"), errors.New("exit status 1")
		}, goodProbe},
		{"control-transport-failure", func(context.Context, string) ([]byte, error) {
			return []byte("rndc: connection refused\n"), errors.New("exit status 1")
		}, goodProbe},
		{"no-control-error", func(context.Context, string) ([]byte, error) {
			return []byte("rndc: 'zonestatus' failed: not found\nno matching zone '" + domain + "' in any view\n"), nil
		}, goodProbe},
		{"old-zone-still-served", goodStatus, func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
			return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, AnswerCount: 1, SOASerials: []uint32{41}}, nil
		}},
		{"refused-with-records", goodStatus, func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
			return dnsSOAProbeResult{RCode: dnsRCodeRefused, AnswerCount: 1}, nil
		}},
		{"dns-unreachable", goodStatus, func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
			return dnsSOAProbeResult{}, errors.New("network unavailable")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyBINDDeletedZoneOnce(context.Background(), "192.0.2.11", domain, tc.probe, tc.status); err == nil {
				t.Fatal("unproved deletion accepted")
			}
		})
	}
}

func TestBINDDeletionWaitsForReloadConvergence(t *testing.T) {
	const domain = "s1-kill.test"
	checks := 0
	status := func(context.Context, string) ([]byte, error) {
		checks++
		if checks == 1 {
			return []byte("name: s1-kill.test\ntype: primary\n"), nil
		}
		return []byte("rndc: 'zonestatus' failed: not found\nno matching zone '" + domain + "' in any view\n"), errors.New("exit status 1")
	}
	probe := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
		return dnsSOAProbeResult{RCode: dnsRCodeRefused}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := verifyBINDDeletedZoneAt(ctx, "192.0.2.11", domain, probe, status); err != nil {
		t.Fatal(err)
	}
	if checks < 2 {
		t.Fatalf("zonestatus checks=%d, want convergence check", checks)
	}
}
