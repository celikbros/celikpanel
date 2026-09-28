package main

import (
	"context"
	"errors"
	"testing"
)

func TestFreshPDNSV3LocalDeletionRequiresBoundNegativeOnBothTransports(t *testing.T) {
	const address = "192.0.2.10"
	const domain = "gone.example.test"
	negative := dnsSOAProbeResult{
		LocalIP: address, ExactDeletedZone: true, Authoritative: true,
		RCode: dnsRCodeNameError, AuthoritySOAOwners: []string{"example.test"},
	}
	refused := dnsSOAProbeResult{LocalIP: address, RCode: dnsRCodeRefused}
	positive := dnsSOAProbeResult{
		LocalIP: address, Authoritative: true, RCode: dnsRCodeNoError,
		AnswerCount: 1, SOASerials: []uint32{42},
		AnswerSOAOwners: []string{domain},
	}
	for _, tc := range []struct {
		name     string
		udp, tcp dnsSOAProbeResult
		probeErr error
		wantErr  bool
	}{
		{"parent-negative", negative, negative, nil, false},
		{"parentless-refused", refused, refused, nil, false},
		{"mixed-negative-refused", negative, refused, nil, false},
		{"positive-udp", positive, refused, nil, true},
		{"positive-tcp", refused, positive, nil, true},
		{"refused-with-answer", dnsSOAProbeResult{LocalIP: address, RCode: dnsRCodeRefused, AnswerCount: 1}, refused, nil, true},
		{"wrong-source", dnsSOAProbeResult{LocalIP: "192.0.2.11", RCode: dnsRCodeRefused}, refused, nil, true},
		{"probe-error", refused, refused, errors.New("private DNS detail"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			probe := func(_ context.Context, network, gotAddress, gotDomain string) (dnsSOAProbeResult, error) {
				if gotAddress != address || gotDomain != domain {
					t.Fatalf("unexpected proof identity %s %s", gotAddress, gotDomain)
				}
				calls++
				if network == "udp" {
					if tc.probeErr != nil {
						return dnsSOAProbeResult{}, tc.probeErr
					}
					return tc.udp, nil
				}
				if network != "tcp" {
					t.Fatalf("unexpected network %s", network)
				}
				return tc.tcp, nil
			}
			err := verifyFreshPDNSV3DeletedZoneAt(context.Background(), address, domain, probe)
			if (err != nil) != tc.wantErr {
				t.Fatalf("proof err=%v wantErr=%v", err, tc.wantErr)
			}
			if calls == 0 {
				t.Fatal("DNS proof was skipped")
			}
			if tc.probeErr != nil && err != nil && err.Error() == tc.probeErr.Error() {
				t.Fatal("private probe detail leaked")
			}
		})
	}
}
