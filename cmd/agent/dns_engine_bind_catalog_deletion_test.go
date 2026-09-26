package main

import (
	"context"
	"errors"
	"testing"
)

func TestBINDCatalogDeletionProvesLocalTopLevelAbsenceWithoutRNDC(t *testing.T) {
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.11", PeerIP: "192.0.2.10",
		Domain: "catalog.example.test", Serial: 2,
	}
	calls := 0
	soa := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
		if address != evidence.LocalIP || domain != "s1-kill.test" {
			t.Fatalf("SOA address=%q domain=%q", address, domain)
		}
		calls++
		return dnsSOAProbeResult{RCode: dnsRCodeRefused}, nil
	}
	axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			t.Fatalf("AXFR address=%q domain=%q", address, domain)
		}
		return dnsCatalogAXFRResult{Serial: 2}, nil
	}
	if err := verifyBINDCatalogDeletionAt(context.Background(), evidence.LocalIP, "s1-kill.test",
		evidence, soa, axfr); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("SOA protocol checks=%d, want UDP and TCP", calls)
	}
}

func TestBINDCatalogDeletionRejectsStaleCatalogAndStillServedZone(t *testing.T) {
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.11", PeerIP: "192.0.2.10",
		Domain: "catalog.example.test", Serial: 2,
	}
	refused := func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
		return dnsSOAProbeResult{RCode: dnsRCodeRefused}, nil
	}
	tests := []struct {
		name string
		soa  dnsZoneSOAProbe
		axfr dnsCatalogAXFRProbe
	}{
		{"old-catalog", refused, func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
			return dnsCatalogAXFRResult{Serial: 1, Members: []string{"s1-kill.test"}}, nil
		}},
		{"catalog-unreachable", refused, func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
			return dnsCatalogAXFRResult{}, errors.New("AXFR refused")
		}},
		{"zone-still-authoritative", func(context.Context, string, string, string) (dnsSOAProbeResult, error) {
			return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, AnswerCount: 1, SOASerials: []uint32{1}}, nil
		}, func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
			return dnsCatalogAXFRResult{Serial: 2}, nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyBINDCatalogDeletionOnce(context.Background(), evidence.LocalIP,
				"s1-kill.test", evidence, tc.soa, tc.axfr); err == nil {
				t.Fatal("unproved local deletion accepted")
			}
		})
	}
	evidence.Members = []string{"s1-kill.test"}
	evidence.MemberSerials = []uint32{1}
	if err := verifyBINDCatalogDeletionAt(context.Background(), evidence.LocalIP, "s1-kill.test",
		evidence, refused, func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
			return dnsCatalogAXFRResult{Serial: 2, Members: []string{"s1-kill.test"}}, nil
		}); err == nil {
		t.Fatal("durable catalog still contains deleted member")
	}
}
