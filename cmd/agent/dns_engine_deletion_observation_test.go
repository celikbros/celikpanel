package main

import (
	"context"
	"errors"
	"testing"
)

func TestObservedDeletedDNSZoneRequiresNoncontradictoryUDPAndTCP(t *testing.T) {
	const source = "192.0.2.10"
	const peer = "192.0.2.11"
	const domain = "gone.example.test"
	negative := dnsSOAProbeResult{
		LocalIP: source, ExactDeletedZone: true, Authoritative: true,
		RCode: dnsRCodeNameError, AuthoritySOAOwners: []string{"example.test"},
	}
	refused := dnsSOAProbeResult{LocalIP: source, RCode: dnsRCodeRefused}
	positive := dnsSOAProbeResult{
		LocalIP: source, Authoritative: true, RCode: dnsRCodeNoError,
		AnswerCount: 1, SOASerials: []uint32{8},
		AnswerSOAOwners: []string{domain},
	}
	cases := []struct {
		name     string
		udp, tcp dnsSOAProbeResult
		probeErr error
		want     dnsDeletedZoneSOAState
		fail     bool
	}{
		{"parent_negative", negative, negative, nil, dnsDeletedZoneParentNegative, false},
		{"parentless_refused", refused, refused, nil, dnsDeletedZoneEmptyRefused, false},
		{"mixed_negative_refused", negative, refused, nil, dnsDeletedZoneEmptyRefused, false},
		{"loaded_positive", positive, refused, nil, dnsDeletedZoneUnknown, true},
		{"loaded_positive_tcp", refused, positive, nil, dnsDeletedZoneUnknown, true},
		{"refused_with_answer", dnsSOAProbeResult{LocalIP: source, RCode: dnsRCodeRefused, AnswerCount: 1}, refused, nil, dnsDeletedZoneUnknown, true},
		{"wrong_source", dnsSOAProbeResult{LocalIP: peer, RCode: dnsRCodeRefused}, refused, nil, dnsDeletedZoneUnknown, true},
		{"network_error", refused, refused, errors.New("private probe detail"), dnsDeletedZoneUnknown, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			probe := func(_ context.Context, network, address, gotDomain string) (dnsSOAProbeResult, error) {
				if address != peer || gotDomain != domain {
					t.Fatalf("probe address=%q domain=%q", address, gotDomain)
				}
				calls++
				if network == "udp" {
					if tc.probeErr != nil {
						return dnsSOAProbeResult{}, tc.probeErr
					}
					return tc.udp, nil
				}
				if network != "tcp" {
					t.Fatalf("network=%q", network)
				}
				return tc.tcp, nil
			}
			got, err := observeDeletedDNSZoneAt(context.Background(), source, peer, domain, probe)
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("state=%v err=%v want=%v fail=%v", got, err, tc.want, tc.fail)
			}
			if err != nil && tc.probeErr != nil && err.Error() == tc.probeErr.Error() {
				t.Fatal("untrusted probe detail leaked")
			}
			if calls == 0 {
				t.Fatal("DNS observation was skipped")
			}
			if tc.want == dnsDeletedZoneEmptyRefused {
				if err := verifyDeletedDNSZoneAt(context.Background(), source, peer, domain, probe); err == nil {
					t.Fatal("empty REFUSED alone was accepted as deletion")
				}
			}
		})
	}
}

func TestParentlessDeletionNeedsNativeProofAfterExactCatalogAndNoTransfer(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: evidence,
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	localAXFR := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			t.Fatal("unexpected local catalog query")
		}
		return dnsCatalogAXFRResult{Serial: evidence.Serial}, nil
	}
	peerCatalog := exactTestPeerCatalogAXFR(evidence)
	peerZone := absentTestPeerZoneAXFR(evidence)
	positive := false
	soa := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
		if address != evidence.PeerIP || (network != "udp" && network != "tcp") {
			t.Fatal("unexpected SOA query")
		}
		if domain == evidence.Domain {
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{evidence.Serial},
			}, nil
		}
		if domain != plan.Changed.Domain {
			t.Fatalf("unexpected zone %q", domain)
		}
		if positive {
			return dnsSOAProbeResult{
				LocalIP: evidence.LocalIP, Authoritative: true,
				RCode: dnsRCodeNoError, AnswerCount: 1,
				AnswerSOAOwners: []string{domain}, SOASerials: []uint32{42},
			}, nil
		}
		return dnsSOAProbeResult{LocalIP: evidence.LocalIP, RCode: dnsRCodeRefused}, nil
	}
	nativeCalls := 0
	native := func(_ context.Context, authority dnsPeerAXFRAuthority, got dnsV3PrimaryPropagationPlan) error {
		nativeCalls++
		if authority.sourceIP != evidence.LocalIP || authority.peerIP != evidence.PeerIP ||
			authority.catalogSerial != evidence.Serial || got.Changed.Domain != plan.Changed.Domain {
			t.Fatal("native proof lost catalog or deletion identity")
		}
		return nil
	}
	check, err := verifyDNSV3PrimaryPropagationCheckWithNativeAt(
		context.Background(), plan, soa, localAXFR, peerCatalog, peerZone, nil,
	)
	if err == nil || check != dnsV3ProofZoneSOA || nativeCalls != 0 {
		t.Fatalf("missing native proof accepted: check=%s err=%v calls=%d", check, err, nativeCalls)
	}
	check, err = verifyDNSV3PrimaryPropagationCheckWithNativeAt(
		context.Background(), plan, soa, localAXFR, peerCatalog, peerZone, native,
	)
	if err != nil || check != dnsV3ProofVerified || nativeCalls != 1 {
		t.Fatalf("native proof rejected: check=%s err=%v calls=%d", check, err, nativeCalls)
	}
	positive = true
	check, err = verifyDNSV3PrimaryPropagationCheckWithNativeAt(
		context.Background(), plan, soa, localAXFR, peerCatalog, peerZone, native,
	)
	if err == nil || check != dnsV3ProofZoneSOA || nativeCalls != 1 {
		t.Fatalf("positive DNS bypassed native gate: check=%s err=%v calls=%d", check, err, nativeCalls)
	}
	positive = false
	refusedNative := func(context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan) error {
		return errors.New("private remote output")
	}
	check, err = verifyDNSV3PrimaryPropagationCheckWithNativeAt(
		context.Background(), plan, soa, localAXFR, peerCatalog, peerZone, refusedNative,
	)
	if err == nil || check != dnsV3ProofNativePeer || err.Error() == "private remote output" {
		t.Fatalf("unverified native result accepted or leaked: check=%s err=%v", check, err)
	}
}
