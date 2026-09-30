package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// testPDNSV3SourceState is the managed PowerDNS primary's engine state
// receipt; every primary propagation plan carries its source receipt.
func testPDNSV3SourceState() dnsEngineStateReceipt {
	return dnsEngineStateReceipt{
		Mode: transport.DNSEngineSwitchModeSwitch, Engine: transport.DNSEnginePowerDNS,
		EngineEpoch: 1, PairRole: transport.DNSPairRolePrimary,
		PairLocalIP: "192.0.2.10", PairPeerIP: "192.0.2.20",
	}
}

func testPDNSPrimaryPropagationEvidence(
	catalogSerial uint32,
	members []string,
	memberSerials []uint32,
) dnsPrimaryCatalogEvidence {
	return dnsPrimaryCatalogEvidence{
		LocalIP: "192.0.2.10", PeerIP: "192.0.2.20",
		Domain: "catalog-c000020a.celikpanel.invalid",
		Serial: catalogSerial, Members: members, MemberSerials: memberSerials,
	}
}

func exactTestPeerCatalogAXFR(
	evidence dnsPrimaryCatalogEvidence,
) dnsBoundCatalogAXFRProbe {
	return func(
		_ context.Context, source, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		if source != evidence.LocalIP || address != evidence.PeerIP ||
			domain != evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected peer catalog AXFR")
		}
		return dnsCatalogAXFRResult{
			Serial:  evidence.Serial,
			Members: append([]string(nil), evidence.Members...),
		}, nil
	}
}

func absentTestPeerZoneAXFR(
	evidence dnsPrimaryCatalogEvidence,
) dnsBoundZoneAXFRProbe {
	return func(
		_ context.Context, source, address, _ string,
	) (dnsZoneAXFRState, error) {
		if source != evidence.LocalIP || address != evidence.PeerIP {
			return dnsZoneAXFRIndeterminate, errors.New("unexpected peer zone AXFR")
		}
		return dnsZoneAXFRNoTransfer, nil
	}
}

func TestPreparePDNSV3PropagationCommandsAreDirectionalAndOrdered(t *testing.T) {
	for _, test := range []struct {
		name string
		plan pdnsV3PropagationPlan
		want []string
	}{
		{
			name: "add",
			plan: pdnsV3PropagationPlan{
				Primary: true, State: testPDNSV3SourceState(),
				Evidence: testPDNSPrimaryPropagationEvidence(
					2, []string{"example.test"}, []uint32{41},
				),
				Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
			},
			want: []string{
				"purge example.test$",
				"purge catalog-c000020a.celikpanel.invalid$",
				"notify-host catalog-c000020a.celikpanel.invalid 192.0.2.20:53",
				"notify-host example.test 192.0.2.20:53",
			},
		},
		{
			name: "record-only change",
			plan: pdnsV3PropagationPlan{
				Primary: true, State: testPDNSV3SourceState(),
				Evidence: testPDNSPrimaryPropagationEvidence(
					2, []string{"example.test"}, []uint32{42},
				),
				Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 42},
			},
			want: []string{
				"purge example.test$",
				"purge catalog-c000020a.celikpanel.invalid$",
				"notify-host catalog-c000020a.celikpanel.invalid 192.0.2.20:53",
				"notify-host example.test 192.0.2.20:53",
			},
		},
		{
			name: "delete",
			plan: pdnsV3PropagationPlan{
				Primary: true, State: testPDNSV3SourceState(),
				Evidence: testPDNSPrimaryPropagationEvidence(3, nil, nil),
				Changed:  expectedDNSZoneAuthority{Domain: "example.test", Delete: true},
			},
			want: []string{
				"purge example.test$",
				"purge catalog-c000020a.celikpanel.invalid$",
				"notify-host catalog-c000020a.celikpanel.invalid 192.0.2.20:53",
			},
		},
		{
			name: "standalone remains local-only",
			plan: pdnsV3PropagationPlan{
				Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
			},
			want: []string{"purge example.test$"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var commands []string
			run := func(_ context.Context, args ...string) error {
				commands = append(commands, strings.Join(args, " "))
				return nil
			}
			if err := preparePDNSV3PropagationAt(
				context.Background(), test.plan, run,
			); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(commands, test.want) {
				t.Fatalf("commands=%q want=%q", commands, test.want)
			}
		})
	}
}

// PowerDNS 4.9.17 queues a port-less notify-host address as "<ip>:0" and
// then treats the port-53 answer as spurious (PowerDNS issue 13576), so the
// operator notify must carry the explicit DNS port in the form its parser
// accepts: "<ipv4>:53", never the bare address.
func TestPDNSNotifyHostTargetCarriesExplicitDNSPort(t *testing.T) {
	for _, test := range []struct{ peer, want string }{
		{"192.0.2.11", "192.0.2.11:53"},
		{"192.0.2.20", "192.0.2.20:53"},
		{"2001:db8::11", "[2001:db8::11]:53"},
	} {
		if got := pdnsNotifyHostTarget(test.peer); got != test.want {
			t.Fatalf("pdnsNotifyHostTarget(%q)=%q want %q", test.peer, got, test.want)
		}
	}
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(),
		Evidence: testPDNSPrimaryPropagationEvidence(
			2, []string{"example.test"}, []uint32{41},
		),
		Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
	}
	var targets []string
	run := func(_ context.Context, args ...string) error {
		if len(args) > 0 && args[0] == "notify-host" {
			if len(args) != 3 {
				t.Fatalf("notify-host argv=%q", args)
			}
			targets = append(targets, args[2])
		}
		return nil
	}
	if err := preparePDNSV3PropagationAt(context.Background(), plan, run); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(targets, []string{"192.0.2.20:53", "192.0.2.20:53"}) {
		t.Fatalf("notify-host targets=%q", targets)
	}
}

func TestPreparePDNSV3PropagationNotifyFailureIsStaticAndStops(t *testing.T) {
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(),
		Evidence: testPDNSPrimaryPropagationEvidence(
			2, []string{"example.test"}, []uint32{41},
		),
		Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
	}
	var commands []string
	run := func(_ context.Context, args ...string) error {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		if strings.HasPrefix(command, "notify-host catalog-") {
			return errors.New("sensitive subprocess detail")
		}
		return nil
	}
	err := preparePDNSV3PropagationAt(context.Background(), plan, run)
	if err == nil || err.Error() != "PowerDNS paired catalog notification failed" {
		t.Fatalf("unexpected bounded error=%v", err)
	}
	want := []string{
		"purge example.test$",
		"purge catalog-c000020a.celikpanel.invalid$",
		"notify-host catalog-c000020a.celikpanel.invalid 192.0.2.20:53",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands after failure=%q want=%q", commands, want)
	}
}

func TestPDNSV3PropagationRecoveryRepeatsNotificationsIdempotently(t *testing.T) {
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(),
		Evidence: testPDNSPrimaryPropagationEvidence(
			2, []string{"example.test"}, []uint32{41},
		),
		Changed: expectedDNSZoneAuthority{Domain: "example.test", Serial: 41},
	}
	var commands []string
	run := func(_ context.Context, args ...string) error {
		commands = append(commands, strings.Join(args, " "))
		return nil
	}
	axfr := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{
			Serial: 2, Members: []string{"example.test"},
		}, nil
	}
	peerReady := false
	soa := func(_ context.Context, _, address, domain string) (dnsSOAProbeResult, error) {
		serial := uint32(2)
		if domain == "example.test" {
			serial = 41
			if address == plan.Evidence.PeerIP && !peerReady {
				serial = 40
			}
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{serial},
		}, nil
	}
	if err := preparePDNSV3PropagationAt(context.Background(), plan, run); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan, soa, axfr,
		exactTestPeerCatalogAXFR(plan.Evidence),
		absentTestPeerZoneAXFR(plan.Evidence),
	); err == nil {
		t.Fatal("stale first transfer unexpectedly passed")
	}
	peerReady = true
	if err := preparePDNSV3PropagationAt(context.Background(), plan, run); err != nil {
		t.Fatal(err)
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan, soa, axfr,
		exactTestPeerCatalogAXFR(plan.Evidence),
		absentTestPeerZoneAXFR(plan.Evidence),
	); err != nil {
		t.Fatalf("recovery proof did not converge: %v", err)
	}
	if len(commands) != 8 ||
		!reflect.DeepEqual(commands[:4], commands[4:]) {
		t.Fatalf("recovery commands are not an exact idempotent retry: %q", commands)
	}
}

func TestVerifyPDNSV3PropagationProvesZeroMemberCatalog(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(7, nil, nil)
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(), Evidence: evidence,
		Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected AXFR")
		}
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
	}
	catalogCalls := 0
	soa := func(_ context.Context, _, address, domain string) (dnsSOAProbeResult, error) {
		if address != evidence.PeerIP {
			return dnsSOAProbeResult{}, errors.New("unexpected address")
		}
		if domain == evidence.Domain {
			catalogCalls++
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{7},
			}, nil
		}
		if domain == plan.Changed.Domain {
			return dnsSOAProbeResult{
				LocalIP:          evidence.LocalIP,
				ExactDeletedZone: true, Authoritative: true, RCode: dnsRCodeNameError,
				AuthoritySOAOwners: []string{"example.test"},
			}, nil
		}
		return dnsSOAProbeResult{}, errors.New("unexpected zone")
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan, soa, axfr,
		exactTestPeerCatalogAXFR(plan.Evidence),
		absentTestPeerZoneAXFR(plan.Evidence),
	); err != nil {
		t.Fatal(err)
	}
	if catalogCalls != 2 {
		t.Fatalf("zero-member peer catalog calls=%d want=2", catalogCalls)
	}
}

func TestVerifyPDNSV3PropagationRejectsStalePeerCatalogSerial(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(7, nil, nil)
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(), Evidence: evidence,
		Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	axfr := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
	}
	soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		if domain == evidence.Domain {
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{6},
			}, nil
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNameError,
			AuthoritySOAOwners: []string{"example.test"},
		}, nil
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan, soa, axfr,
		exactTestPeerCatalogAXFR(plan.Evidence),
		absentTestPeerZoneAXFR(plan.Evidence),
	); err == nil {
		t.Fatal("stale peer catalog serial passed propagation proof")
	}
}

func TestVerifyPDNSV3PropagationRejectsDeletedMemberStillServed(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(), Evidence: evidence,
		Changed: expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	axfr := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: 8, Members: []string{}}, nil
	}
	soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		if domain == evidence.Domain {
			return dnsSOAProbeResult{
				Authoritative: true, RCode: dnsRCodeNoError,
				SOASerials: []uint32{8},
			}, nil
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{41}, AnswerSOAOwners: []string{domain},
		}, nil
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan, soa, axfr,
		exactTestPeerCatalogAXFR(plan.Evidence),
		func(
			context.Context, string, string, string,
		) (dnsZoneAXFRState, error) {
			return dnsZoneAXFRPresent, nil
		},
	); err == nil {
		t.Fatal("deleted member still served by peer passed propagation proof")
	}
}

func TestVerifyPDNSV3DeletionProofIsEngineNeutralAndSourceBound(t *testing.T) {
	for _, engines := range []string{
		"bind-to-bind",
		"bind-to-pdns",
		"pdns-to-bind",
		"pdns-to-pdns",
	} {
		t.Run(engines, func(t *testing.T) {
			evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
			plan := pdnsV3PropagationPlan{
				Primary: true, State: testPDNSV3SourceState(), Evidence: evidence,
				Changed: expectedDNSZoneAuthority{
					Domain: "gone.example.test", Delete: true,
				},
			}
			localAXFR := func(
				_ context.Context, address, domain string,
			) (dnsCatalogAXFRResult, error) {
				if address != evidence.LocalIP || domain != evidence.Domain {
					return dnsCatalogAXFRResult{}, errors.New("unexpected local AXFR")
				}
				return dnsCatalogAXFRResult{Serial: 8, Members: []string{}}, nil
			}
			peerCatalogCalls := 0
			peerCatalogAXFR := func(
				_ context.Context, source, address, domain string,
			) (dnsCatalogAXFRResult, error) {
				peerCatalogCalls++
				if source != evidence.LocalIP || address != evidence.PeerIP ||
					domain != evidence.Domain {
					return dnsCatalogAXFRResult{}, errors.New("unexpected peer catalog AXFR")
				}
				return dnsCatalogAXFRResult{Serial: 8, Members: []string{}}, nil
			}
			soaCalls := 0
			soa := func(
				_ context.Context, network, address, domain string,
			) (dnsSOAProbeResult, error) {
				soaCalls++
				if address != evidence.PeerIP ||
					(network != "udp" && network != "tcp") {
					return dnsSOAProbeResult{}, errors.New("unexpected SOA proof")
				}
				if domain == evidence.Domain {
					return dnsSOAProbeResult{
						Authoritative: true, RCode: dnsRCodeNoError,
						SOASerials: []uint32{8},
					}, nil
				}
				if domain != plan.Changed.Domain {
					return dnsSOAProbeResult{}, errors.New("unexpected deleted-zone SOA proof")
				}
				return dnsSOAProbeResult{
					LocalIP:          evidence.LocalIP,
					ExactDeletedZone: true, Authoritative: true,
					RCode:              dnsRCodeNameError,
					AuthoritySOAOwners: []string{"example.test"},
				}, nil
			}
			zoneCalls := 0
			zoneAXFR := func(
				_ context.Context, source, address, domain string,
			) (dnsZoneAXFRState, error) {
				zoneCalls++
				if source != evidence.LocalIP || address != evidence.PeerIP ||
					domain != plan.Changed.Domain {
					return dnsZoneAXFRIndeterminate, errors.New("unexpected zone AXFR")
				}
				return dnsZoneAXFRNoTransfer, nil
			}
			if err := verifyPDNSV3PropagationAt(
				context.Background(), plan, soa, localAXFR,
				peerCatalogAXFR, zoneAXFR,
			); err != nil {
				t.Fatal(err)
			}
			if peerCatalogCalls != 1 || soaCalls != 4 || zoneCalls != 1 {
				t.Fatalf(
					"catalog=%d soa=%d zone=%d",
					peerCatalogCalls, soaCalls, zoneCalls,
				)
			}
		})
	}
}

func TestVerifyPDNSV3DeletionNeverProbesZoneWithoutPeerCatalogAuthority(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := pdnsV3PropagationPlan{
		Primary: true, State: testPDNSV3SourceState(), Evidence: evidence,
		Changed: expectedDNSZoneAuthority{
			Domain: "gone.example.test", Delete: true,
		},
	}
	localAXFR := func(
		context.Context, string, string,
	) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: 8, Members: []string{}}, nil
	}
	peerCatalogAXFR := func(
		context.Context, string, string, string,
	) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{}, errors.New("peer transfer ACL unavailable")
	}
	zoneCalled := false
	zoneAXFR := func(
		context.Context, string, string, string,
	) (dnsZoneAXFRState, error) {
		zoneCalled = true
		return dnsZoneAXFRNoTransfer, nil
	}
	if err := verifyPDNSV3PropagationAt(
		context.Background(), plan,
		func(
			context.Context, string, string, string,
		) (dnsSOAProbeResult, error) {
			return dnsSOAProbeResult{}, nil
		},
		localAXFR, peerCatalogAXFR, zoneAXFR,
	); err == nil || zoneCalled {
		t.Fatalf("err=%v zoneCalled=%v", err, zoneCalled)
	}
}

func TestCompleteDNSV3DeletionNamesLastFailedProofWithoutProbeDetails(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: evidence,
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	localAXFR := func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{Serial: evidence.Serial}, nil
	}
	for _, test := range []struct {
		name  string
		check dnsV3ProofCheck
	}{
		{name: "catalog", check: dnsV3ProofCatalogPair},
		{name: "transfer", check: dnsV3ProofZoneTransfer},
		{name: "soa", check: dnsV3ProofZoneSOA},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			soa := func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
				if domain == evidence.Domain {
					return dnsSOAProbeResult{
						Authoritative: true, RCode: dnsRCodeNoError,
						SOASerials: []uint32{evidence.Serial},
					}, nil
				}
				return dnsSOAProbeResult{}, errors.New("secret-provider-token")
			}
			peerCatalog := exactTestPeerCatalogAXFR(evidence)
			if test.check == dnsV3ProofCatalogPair {
				peerCatalog = func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
					return dnsCatalogAXFRResult{}, errors.New("secret-provider-token")
				}
			}
			peerZone := absentTestPeerZoneAXFR(evidence)
			if test.check == dnsV3ProofZoneTransfer {
				peerZone = func(context.Context, string, string, string) (dnsZoneAXFRState, error) {
					return dnsZoneAXFRPresent, errors.New("secret-provider-token")
				}
			}
			err := completeDNSV3PrimaryPropagationAt(
				ctx, plan, soa, localAXFR, peerCatalog, peerZone,
			)
			if err == nil || !strings.Contains(err.Error(), "check="+string(test.check)) ||
				!strings.Contains(err.Error(), "retry verification of the same operation") ||
				strings.Contains(err.Error(), "secret-provider-token") {
				t.Fatalf("unsafe or unactionable pending guidance: %v", err)
			}
		})
	}
}
