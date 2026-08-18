package main

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testBINDV3Snapshot(
	t *testing.T,
	domain string,
	generation int64,
	serial uint32,
	deleted bool,
) binddns.ZoneSnapshot {
	t.Helper()
	var records []transport.ZoneRecord
	if !deleted {
		records = testPDNSEngineRecords(domain)
		records[0].Content = "ns1.example.net hostmaster." + domain + " " +
			strconv.FormatUint(uint64(serial), 10) +
			" 10800 3600 604800 3600"
	}
	commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 1, generation, domain, deleted,
		"MASTER", records,
	)
	if err != nil {
		t.Fatal(err)
	}
	return binddns.ZoneSnapshot{
		DesiredGeneration: commitment.DesiredGeneration,
		Domain:            commitment.Domain,
		Delete:            commitment.Delete,
		Qualifier:         commitment.Qualifier,
		MutationRequestID: testMutationRequestID,
		MutationOwnerID:   testMutationOwnerID,
		Records:           commitment.Records,
	}
}

func testBINDV3PrimaryTree(
	t *testing.T,
	catalogSerial uint32,
	zones ...binddns.ZoneSnapshot,
) (binddns.VerifiedTree, binddns.Generation) {
	t.Helper()
	generation, err := binddns.RenderManifest(
		"/var/lib/celikpanel/bind",
		binddns.Manifest{
			EngineEpoch: 1,
			Pairing: &binddns.Pairing{
				Role:    binddns.PairRolePrimary,
				LocalIP: "192.0.2.10", LocalNS: "ns1.example.test",
				PeerIP: "192.0.2.20", PeerNS: "ns2.example.test",
			},
			PrimaryCatalogSerial: catalogSerial,
			Zones:                zones,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(generation.Zones)+1)
	for _, zone := range generation.Zones {
		files["zones/"+zone.FileName] = zone.Data
	}
	files[generation.ReceiptValue.Pairing.CatalogFile] = generation.Catalog.Data
	tree, err := binddns.VerifyTree(generation.Receipt, generation.Config, files)
	if err != nil {
		t.Fatal(err)
	}
	return tree, generation
}

func exactBINDV3LocalCatalog(
	evidence dnsPrimaryCatalogEvidence,
) dnsCatalogAXFRProbe {
	return func(
		_ context.Context, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		if address != evidence.LocalIP || domain != evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected local catalog AXFR")
		}
		return dnsCatalogAXFRResult{
			Serial: evidence.Serial, Members: append([]string(nil), evidence.Members...),
		}, nil
	}
}

func exactBINDV3SOA(
	evidence dnsPrimaryCatalogEvidence,
) dnsZoneSOAProbe {
	return func(
		_ context.Context, network, address, domain string,
	) (dnsSOAProbeResult, error) {
		if network != "udp" && network != "tcp" {
			return dnsSOAProbeResult{}, errors.New("unexpected DNS network")
		}
		serial := evidence.Serial
		if domain == evidence.Domain {
			if address != evidence.PeerIP {
				return dnsSOAProbeResult{}, errors.New("unexpected catalog SOA address")
			}
		} else {
			found := false
			for index, member := range evidence.Members {
				if member == domain {
					serial = evidence.MemberSerials[index]
					found = true
					break
				}
			}
			if !found || (address != evidence.LocalIP && address != evidence.PeerIP) {
				return dnsSOAProbeResult{}, errors.New("unexpected member SOA target")
			}
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{serial},
		}, nil
	}
}

func TestBINDV3PrimaryPropagationPlanCoversAddUpdateAndDelete(t *testing.T) {
	for _, test := range []struct {
		name          string
		generation    int64
		zoneSerial    uint32
		catalogSerial uint32
		deleted       bool
	}{
		{name: "add", generation: 1, zoneSerial: 41, catalogSerial: 7},
		{name: "update", generation: 2, zoneSerial: 42, catalogSerial: 7},
		{name: "last-member-delete", generation: 3, catalogSerial: 8, deleted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			zone := testBINDV3Snapshot(
				t, "example.test", test.generation, test.zoneSerial, test.deleted,
			)
			tree, _ := testBINDV3PrimaryTree(t, test.catalogSerial, zone)
			plan, primary, err := bindV3PrimaryPropagationPlan(tree, zone.Domain)
			if err != nil || !primary {
				t.Fatalf("primary=%v plan=%+v err=%v", primary, plan, err)
			}
			if plan.Changed.Delete != test.deleted ||
				plan.Changed.Serial != test.zoneSerial ||
				plan.Evidence.Serial != test.catalogSerial {
				t.Fatalf("unexpected plan=%+v", plan)
			}
			if test.deleted {
				if len(plan.Evidence.Members) != 0 || len(plan.Evidence.MemberSerials) != 0 {
					t.Fatalf("deleted plan retained members=%+v", plan.Evidence)
				}
			} else if !reflect.DeepEqual(plan.Evidence.Members, []string{"example.test"}) ||
				!reflect.DeepEqual(plan.Evidence.MemberSerials, []uint32{test.zoneSerial}) {
				t.Fatalf("live plan evidence=%+v", plan.Evidence)
			}
		})
	}
}

func TestBINDV3PrimaryProofRequiresSourceBoundExactPeerCatalog(t *testing.T) {
	tree, _ := testBINDV3PrimaryTree(
		t, 7, testBINDV3Snapshot(t, "example.test", 2, 42, false),
	)
	plan, primary, err := bindV3PrimaryPropagationPlan(tree, "example.test")
	if err != nil || !primary {
		t.Fatal(err)
	}
	peerCalls := 0
	peerCatalog := func(
		_ context.Context, source, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		peerCalls++
		if source != plan.Evidence.LocalIP || address != plan.Evidence.PeerIP ||
			domain != plan.Evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("peer AXFR was not source-bound")
		}
		return dnsCatalogAXFRResult{
			Serial:  plan.Evidence.Serial,
			Members: append([]string(nil), plan.Evidence.Members...),
		}, nil
	}
	if err := verifyDNSV3PrimaryPropagationAt(
		context.Background(), plan, exactBINDV3SOA(plan.Evidence),
		exactBINDV3LocalCatalog(plan.Evidence), peerCatalog,
		absentTestPeerZoneAXFR(plan.Evidence),
	); err != nil || peerCalls != 1 {
		t.Fatalf("exact BIND proof calls=%d err=%v", peerCalls, err)
	}
	stalePeerCatalog := func(
		context.Context, string, string, string,
	) (dnsCatalogAXFRResult, error) {
		return dnsCatalogAXFRResult{
			Serial:  plan.Evidence.Serial - 1,
			Members: append([]string(nil), plan.Evidence.Members...),
		}, nil
	}
	if err := verifyDNSV3PrimaryPropagationAt(
		context.Background(), plan, exactBINDV3SOA(plan.Evidence),
		exactBINDV3LocalCatalog(plan.Evidence), stalePeerCatalog,
		absentTestPeerZoneAXFR(plan.Evidence),
	); err == nil {
		t.Fatal("stale source-bound peer catalog passed BIND propagation proof")
	}
}

func TestBINDV3LastMemberDeleteProofIsNonVacuous(t *testing.T) {
	tree, _ := testBINDV3PrimaryTree(
		t, 8, testBINDV3Snapshot(t, "gone.example.test", 3, 0, true),
	)
	plan, primary, err := bindV3PrimaryPropagationPlan(tree, "gone.example.test")
	if err != nil || !primary || len(plan.Evidence.Members) != 0 {
		t.Fatalf("primary=%v plan=%+v err=%v", primary, plan, err)
	}
	localCalls, peerCatalogCalls, catalogSOACalls, zoneCalls := 0, 0, 0, 0
	localCatalog := func(
		ctx context.Context, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		localCalls++
		return exactBINDV3LocalCatalog(plan.Evidence)(ctx, address, domain)
	}
	peerCatalog := func(
		_ context.Context, source, address, domain string,
	) (dnsCatalogAXFRResult, error) {
		peerCatalogCalls++
		if source != plan.Evidence.LocalIP || address != plan.Evidence.PeerIP ||
			domain != plan.Evidence.Domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected peer catalog identity")
		}
		return dnsCatalogAXFRResult{Serial: 8, Members: []string{}}, nil
	}
	soa := func(
		_ context.Context, network, address, domain string,
	) (dnsSOAProbeResult, error) {
		catalogSOACalls++
		if address != plan.Evidence.PeerIP || domain != plan.Evidence.Domain ||
			(network != "udp" && network != "tcp") {
			return dnsSOAProbeResult{}, errors.New("unexpected catalog SOA identity")
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{8},
		}, nil
	}
	zoneAXFR := func(
		_ context.Context, source, address, domain string,
	) (dnsZoneAXFRState, error) {
		zoneCalls++
		if source != plan.Evidence.LocalIP || address != plan.Evidence.PeerIP ||
			domain != plan.Changed.Domain {
			return dnsZoneAXFRIndeterminate, errors.New("unexpected deleted-zone identity")
		}
		return dnsZoneAXFRAbsent, nil
	}
	if err := verifyDNSV3PrimaryPropagationAt(
		context.Background(), plan, soa, localCatalog, peerCatalog, zoneAXFR,
	); err != nil {
		t.Fatal(err)
	}
	if localCalls != 1 || peerCatalogCalls != 1 || catalogSOACalls != 2 || zoneCalls != 1 {
		t.Fatalf(
			"local=%d peer=%d soa=%d zone=%d",
			localCalls, peerCatalogCalls, catalogSOACalls, zoneCalls,
		)
	}
	if err := verifyDNSV3PrimaryPropagationAt(
		context.Background(), plan, soa, localCatalog, peerCatalog,
		func(context.Context, string, string, string) (dnsZoneAXFRState, error) {
			return dnsZoneAXFRPresent, nil
		},
	); err == nil {
		t.Fatal("last deleted BIND member still served by peer passed")
	}
	zoneCalls = 0
	if err := verifyDNSV3PrimaryPropagationAt(
		context.Background(), plan, soa, localCatalog,
		func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
			return dnsCatalogAXFRResult{Serial: 7, Members: []string{}}, nil
		},
		zoneAXFR,
	); err == nil || zoneCalls != 0 {
		t.Fatalf("stale catalog err=%v deleted-zone calls=%d", err, zoneCalls)
	}
}

func TestPrepareBINDV3NotificationsAreBoundedOrderedAndIdempotent(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		name := "live"
		serial := uint32(42)
		if deleted {
			name = "delete"
			serial = 0
		}
		t.Run(name, func(t *testing.T) {
			tree, _ := testBINDV3PrimaryTree(
				t, 8, testBINDV3Snapshot(t, "example.test", 3, serial, deleted),
			)
			plan, _, err := bindV3PrimaryPropagationPlan(tree, "example.test")
			if err != nil {
				t.Fatal(err)
			}
			var commands []string
			run := func(ctx context.Context, args ...string) error {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > dnsProbeTimeout {
					t.Fatalf("unbounded rndc context: %v %v", ok, deadline)
				}
				commands = append(commands, strings.Join(args, " "))
				return nil
			}
			if err := prepareBINDV3PrimaryPropagationAt(
				context.Background(), plan, run,
			); err != nil {
				t.Fatal(err)
			}
			want := []string{"notify " + plan.Evidence.Domain}
			if !deleted {
				want = append(want, "notify example.test")
			}
			if !reflect.DeepEqual(commands, want) {
				t.Fatalf("commands=%q want=%q", commands, want)
			}
		})
	}
}

func TestBINDV3PeerProofFailureKeepsTargetForRecoverZoneRetry(t *testing.T) {
	previousTree, previousGeneration := testBINDV3PrimaryTree(
		t, 7, testBINDV3Snapshot(t, "example.test", 1, 41, false),
	)
	targetTree, targetGeneration := testBINDV3PrimaryTree(
		t, 7, testBINDV3Snapshot(t, "example.test", 2, 42, false),
	)
	stateGeneration := previousGeneration.ID
	pointerGeneration := targetGeneration.ID
	if err := applyVerifiedBINDV3GenerationAt(
		context.Background(), 1, targetTree,
		previousTree.CurrentReceipt(), targetTree.CurrentReceipt(),
		func(context.Context) error { return nil },
		nil,
		func() error {
			stateGeneration = targetGeneration.ID
			return nil
		},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	proofCalls := 0
	var notifications []string
	run := func(_ context.Context, args ...string) error {
		notifications = append(notifications, strings.Join(args, " "))
		return nil
	}
	complete := func(context.Context, dnsV3PrimaryPropagationPlan) error {
		proofCalls++
		if proofCalls == 1 {
			return errors.New("peer catalog is stale")
		}
		return nil
	}
	if err := completeManagedBINDV3PropagationAt(
		context.Background(), targetTree, "example.test", run, complete,
	); err == nil {
		t.Fatal("stale peer unexpectedly completed the initial publication")
	}
	if pointerGeneration != targetGeneration.ID || stateGeneration != targetGeneration.ID {
		t.Fatalf("peer failure rolled back target pointer/state: %s/%s", pointerGeneration, stateGeneration)
	}
	if err := completeManagedBINDV3PropagationAt(
		context.Background(), targetTree, "example.test", run, complete,
	); err != nil {
		t.Fatalf("RecoverZone retry did not converge: %v", err)
	}
	if proofCalls != 2 || len(notifications) != 4 ||
		!reflect.DeepEqual(notifications[:2], notifications[2:]) {
		t.Fatalf("proofCalls=%d notifications=%q", proofCalls, notifications)
	}
}

func TestBINDV3LocalApplyRollbackUsesOnlyExactPriorTreeAndState(t *testing.T) {
	priorTree, _ := testBINDV3PrimaryTree(
		t, 7, testBINDV3Snapshot(t, "example.test", 1, 41, false),
	)
	targetTree, _ := testBINDV3PrimaryTree(
		t, 7, testBINDV3Snapshot(t, "example.test", 2, 42, false),
	)
	priorReceipt := priorTree.CurrentReceipt()
	targetReceipt := targetTree.CurrentReceipt()
	_, priorBytes, _ := priorTree.Zone("example.test")
	_, targetBytes, _ := targetTree.Zone("example.test")
	if reflect.DeepEqual(priorBytes, targetBytes) {
		t.Fatal("rollback fixture did not change the served zone")
	}
	state := []byte("exact-prior-state")
	stateBefore := append([]byte(nil), state...)
	targetVerifications, rollbackVerifications := 0, 0
	commitFailure := errors.New("state commit lost durability")
	err := applyVerifiedBINDV3GenerationAt(
		context.Background(), 1, targetTree, priorReceipt, targetReceipt,
		func(context.Context) error {
			targetVerifications++
			return nil
		},
		nil,
		func() error {
			state = []byte("partially-written-target-state")
			return commitFailure
		},
		nil,
	)
	if !errors.Is(err, commitFailure) {
		t.Fatalf("target apply error=%v", err)
	}
	err = applyVerifiedBINDV3GenerationAt(
		context.Background(), 2, priorTree, priorReceipt, targetReceipt,
		func(context.Context) error {
			targetVerifications++
			return errors.New("target verifier must not run during rollback")
		},
		func(
			ctx context.Context, tree binddns.VerifiedTree, previous binddns.Receipt,
		) error {
			rollbackVerifications++
			if !reflect.DeepEqual(state, stateBefore) {
				return errors.New("prior state was not restored before rollback proof")
			}
			return verifyRestoredBINDV3GenerationAt(
				ctx, tree, previous,
				func(_ context.Context, expected []expectedDNSZoneAuthority) error {
					if !reflect.DeepEqual(expected, []expectedDNSZoneAuthority{{
						Domain: "example.test", Serial: 41,
					}}) {
						return errors.New("prior local authority differs")
					}
					return nil
				},
				func(_ context.Context, readyTree binddns.VerifiedTree) (bool, error) {
					return reflect.DeepEqual(readyTree.CurrentReceipt(), priorReceipt), nil
				},
			)
		},
		func() error {
			return errors.New("target state commit ran during rollback")
		},
		func() error {
			state = append([]byte(nil), stateBefore...)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if targetVerifications != 1 || rollbackVerifications != 1 ||
		!reflect.DeepEqual(state, stateBefore) {
		t.Fatalf(
			"target=%d rollback=%d state=%q",
			targetVerifications, rollbackVerifications, state,
		)
	}
	if err := verifyRestoredBINDV3GenerationAt(
		context.Background(), priorTree, priorReceipt,
		func(context.Context, []expectedDNSZoneAuthority) error { return nil },
		func(context.Context, binddns.VerifiedTree) (bool, error) {
			return false, errors.New("peer already serves a higher serial")
		},
	); err == nil {
		t.Fatal("rollback claimed exact prior authority after peer advanced")
	}
}
