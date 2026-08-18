package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func testPrimaryCatalogHandoffManifest(
	t *testing.T,
) mutationpayload.DNSEngineSwitchManifestCommitment {
	t.Helper()
	zone, err := mutationpayload.CanonicalDNSZoneSyncV3(
		transport.DNSEngineBIND, 4, 7, "primary.test", false, "MASTER",
		testPDNSEngineRecords("primary.test"),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		3, 4, 9, transport.DNSTopologyPaired,
		transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test",
		"192.0.2.20", "ns2.example.test",
		[]transport.DNSEngineSwitchZoneSnapshot{{
			Domain: "primary.test", DesiredGeneration: 7,
			ZoneType: "MASTER", Records: zone.Records,
			ZoneQualifier: zone.Qualifier,
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestPDNSToBINDPrimarySwitchPlanPreservesCatalogSerial(t *testing.T) {
	manifest := testPrimaryCatalogHandoffManifest(t)
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(
		manifest, testPDNSEngineBinding(), 41,
	)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := binddns.RenderTree("/var/cache/bind/celikpanel", plan)
	if err != nil {
		t.Fatal(err)
	}
	if generation.ReceiptValue.Pairing == nil ||
		generation.ReceiptValue.Pairing.CatalogSerial != 41 {
		t.Fatalf("BIND handoff receipt=%+v", generation.ReceiptValue.Pairing)
	}
	if _, err := bindSwitchTreePlanWithPrimaryCatalogSerial(
		manifest, testPDNSEngineBinding(), 0,
	); err == nil {
		t.Fatal("paired primary BIND plan accepted a missing catalog serial")
	}
}

func TestPrimaryCatalogHandoffEvidenceRequiresExactDurableAndLiveState(t *testing.T) {
	manifest := testPrimaryCatalogHandoffManifest(t)
	domain, err := binddns.CatalogDomain(manifest.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: manifest.LocalIP, PeerIP: manifest.PeerIP,
		Domain: domain, Serial: 41,
		Members: []string{"primary.test"}, MemberSerials: []uint32{7},
	}
	soa := func(
		_ context.Context, _, address, queriedDomain string,
	) (dnsSOAProbeResult, error) {
		if address != manifest.LocalIP || queriedDomain != domain {
			return dnsSOAProbeResult{}, errors.New("unexpected SOA target")
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{41},
		}, nil
	}
	axfr := func(
		_ context.Context, address, queriedDomain string,
	) (dnsCatalogAXFRResult, error) {
		if address != manifest.LocalIP || queriedDomain != domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected AXFR target")
		}
		return dnsCatalogAXFRResult{
			Serial: 41, Members: []string{"primary.test"},
		}, nil
	}
	if err := verifyPrimaryCatalogHandoffEvidenceAt(
		context.Background(), evidence, manifest, 41, soa, axfr,
	); err != nil {
		t.Fatal(err)
	}
	stale := evidence
	stale.Serial = 40
	if err := verifyPrimaryCatalogHandoffEvidenceAt(
		context.Background(), stale, manifest, 41, soa, axfr,
	); err == nil {
		t.Fatal("stale durable catalog serial passed handoff proof")
	}
	extra := evidence
	extra.Members = []string{"extra.test", "primary.test"}
	extra.MemberSerials = []uint32{1, 7}
	if reflect.DeepEqual(extra.Members, evidence.Members) {
		t.Fatal("invalid test fixture")
	}
	if err := verifyPrimaryCatalogHandoffEvidenceAt(
		context.Background(), extra, manifest, 41, soa, axfr,
	); err == nil {
		t.Fatal("extra durable catalog member passed handoff proof")
	}
}

func TestLegacyPrimarySourceReceiptDerivesDurableCatalogSerial(t *testing.T) {
	legacy := dnsEngineStateReceipt{
		Schema: dnsEngineStateSchema, Mode: transport.DNSEngineSwitchModeSwitch,
		Engine: transport.DNSEnginePowerDNS, EngineEpoch: 3,
		SourceRevision: 1,
		ManifestQualifier: "dns-engine-switch/v1:sha256:" +
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MutationRequestID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		MutationOwnerID:   "cccccccccccccccccccccccccccccccc",
	}
	serial, err := primaryCatalogSerialBoundBySourceState(legacy, 41)
	if err != nil || serial != 41 {
		t.Fatalf("legacy serial=%d err=%v", serial, err)
	}
	bound := legacy
	bound.PairRole = transport.DNSPairRolePrimary
	bound.PrimaryCatalogSerial = 41
	if serial, err := primaryCatalogSerialBoundBySourceState(bound, 41); err != nil || serial != 41 {
		t.Fatalf("bound serial=%d err=%v", serial, err)
	}
	if serial, err := primaryCatalogSerialBoundBySourceState(bound, 42); err != nil || serial != 42 {
		t.Fatalf("advanced durable serial=%d err=%v", serial, err)
	}
	if _, err := primaryCatalogSerialBoundBySourceState(bound, 40); err == nil {
		t.Fatal("new primary source receipt accepted a durable serial rollback")
	}
	secondary := legacy
	secondary.PairRole = transport.DNSPairRoleSecondary
	if _, err := primaryCatalogSerialBoundBySourceState(secondary, 41); err == nil {
		t.Fatal("secondary receipt entered the primary compatibility path")
	}
}

func TestPrimaryCatalogHandoffEvidenceAcceptsCanonicalEmptyMembership(t *testing.T) {
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		transport.DNSEngineSwitchModeSwitch,
		"", transport.DNSEngineBIND, 0, 1, 0,
		transport.DNSTopologyPaired, transport.DNSPairRolePrimary,
		"192.0.2.10", "ns1.example.test",
		"192.0.2.20", "ns2.example.test", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := binddns.CatalogDomain(manifest.LocalIP)
	if err != nil {
		t.Fatal(err)
	}
	evidence := dnsPrimaryCatalogEvidence{
		LocalIP: manifest.LocalIP, PeerIP: manifest.PeerIP,
		Domain: domain, Serial: 1,
		Members: []string{}, MemberSerials: []uint32{},
	}
	soa := func(
		_ context.Context, _, address, queriedDomain string,
	) (dnsSOAProbeResult, error) {
		if address != manifest.LocalIP || queriedDomain != domain {
			return dnsSOAProbeResult{}, errors.New("unexpected SOA target")
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{1},
		}, nil
	}
	axfr := func(
		_ context.Context, address, queriedDomain string,
	) (dnsCatalogAXFRResult, error) {
		if address != manifest.LocalIP || queriedDomain != domain {
			return dnsCatalogAXFRResult{}, errors.New("unexpected AXFR target")
		}
		return dnsCatalogAXFRResult{Serial: 1, Members: nil}, nil
	}
	if err := verifyPrimaryCatalogHandoffEvidenceAt(
		context.Background(), evidence, manifest, 1, soa, axfr,
	); err != nil {
		t.Fatal(err)
	}
}

func TestManagedPDNSPairIdentityUsesExactDirectionalConfig(t *testing.T) {
	prepareManagedPDNSCatalogConfig(t)
	manifest := testPrimaryCatalogHandoffManifest(t)
	if err := verifyManagedPDNSPairIdentity(manifest); err != nil {
		t.Fatal(err)
	}
	manifest.PeerIP = "192.0.2.21"
	if err := verifyManagedPDNSPairIdentity(manifest); err == nil {
		t.Fatal("PowerDNS pair identity accepted a different peer address")
	}
}
