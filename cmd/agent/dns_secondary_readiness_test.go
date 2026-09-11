package main

import (
	"context"
	"errors"
	"testing"
)

func TestDNSSecondaryReadinessRequiresCurrentCatalogAndEveryTransferredMember(t *testing.T) {
	const local, peer, domain = "192.0.2.20", "192.0.2.10", "catalog-c000020a.celikpanel.invalid"
	for _, kind := range []string{"ready", "missing_catalog", "stale_catalog", "missing_member", "stale_member", "untrusted_transfer", "moving_catalog", "tcp_mismatch"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			axfr := func(_ context.Context, source, address, name string) (dnsCatalogAXFRResult, error) {
				if source != local || address != peer || name != domain {
					t.Fatalf("unbound transfer %s %s %s", source, address, name)
				}
				calls++
				if kind == "untrusted_transfer" {
					return dnsCatalogAXFRResult{}, errors.New("denied")
				}
				serial := uint32(9)
				if kind == "moving_catalog" && calls > 1 {
					serial++
				}
				return dnsCatalogAXFRResult{Serial: serial, Members: []string{"customer.example.test"}}, nil
			}
			soa := func(_ context.Context, network, address, name string) (dnsSOAProbeResult, error) {
				serial := uint32(41)
				if name == domain {
					serial = 9
				}
				if address == local {
					if (kind == "missing_catalog" && name == domain) || (kind == "missing_member" && name != domain) {
						return dnsSOAProbeResult{}, errors.New("not transferred")
					}
					if (kind == "stale_catalog" && name == domain) || (kind == "stale_member" && name != domain) || (kind == "tcp_mismatch" && network == "tcp") {
						serial--
					}
				}
				return dnsSOAProbeResult{Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial}}, nil
			}
			err := verifyDNSSecondaryPairReadyAt(context.Background(), local, peer, soa, axfr)
			if (err == nil) != (kind == "ready") {
				t.Fatalf("ready=%v error=%v", err == nil, err)
			}
		})
	}
}
