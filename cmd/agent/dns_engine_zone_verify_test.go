package main

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestVerifyBINDPairingAuthorityRequiresExactCatalogAndMemberProof(t *testing.T) {
	for _, test := range []struct {
		name    string
		pairing binddns.PairingReceipt
		wantErr bool
	}{
		{
			name: "primary exact local catalog",
			pairing: binddns.PairingReceipt{
				Role: binddns.PairRolePrimary, LocalCatalog: "catalog-c000020a.celikpanel.invalid",
				CatalogSerial: 7,
			},
		},
		{
			name: "secondary exact peer catalog received locally",
			pairing: binddns.PairingReceipt{
				Role: binddns.PairRoleSecondary, PeerIP: "192.0.2.20",
				PeerCatalog: "catalog-c0000214.celikpanel.invalid", CatalogSerial: 1,
			},
		},
		{
			name: "udp tcp mismatch",
			pairing: binddns.PairingReceipt{
				Role: binddns.PairRolePrimary, LocalCatalog: "catalog-c000020a.celikpanel.invalid",
				CatalogSerial: 7,
			},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			probe := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
				calls++
				if address == "" || domain == "" {
					return dnsSOAProbeResult{}, errors.New("missing proof identity")
				}
				serial := uint32(7)
				if test.pairing.Role == binddns.PairRoleSecondary {
					serial = 11
				}
				return dnsSOAProbeResult{
					Authoritative: true, RCode: dnsRCodeNoError,
					SOASerials: []uint32{serial},
				}, nil
			}
			axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
				calls++
				if address == "" || domain == "" {
					return dnsCatalogAXFRResult{}, errors.New("missing catalog identity")
				}
				serial := test.pairing.CatalogSerial
				if test.pairing.Role == binddns.PairRoleSecondary {
					serial = 11
				}
				if test.wantErr {
					serial++
				}
				return dnsCatalogAXFRResult{Serial: serial, Members: []string{}}, nil
			}
			err := verifyBINDPairingAuthorityAt(
				context.Background(), binddns.Receipt{
					Pairing: &test.pairing, Zones: []binddns.ZoneReceipt{},
				}, "192.0.2.10", probe, axfr,
			)
			if test.wantErr && err == nil {
				t.Fatal("mismatched catalog proof was accepted")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("exact catalog proof rejected: %v", err)
			}
			if calls == 0 {
				t.Fatal("catalog proof did not query the authority")
			}
		})
	}
}

func TestVerifyBINDSecondaryProvesEveryCatalogMemberOnPeerAndLocal(t *testing.T) {
	const (
		peerIP  = "192.0.2.20"
		localIP = "192.0.2.10"
		catalog = "catalog-c0000214.celikpanel.invalid"
		member  = "example.test"
	)
	pairing := binddns.PairingReceipt{
		Role: binddns.PairRoleSecondary, PeerIP: peerIP,
		PeerCatalog: catalog, CatalogSerial: 1,
	}
	for _, test := range []struct {
		name        string
		localSerial uint32
		wantErr     bool
	}{
		{name: "exact member", localSerial: 41},
		{name: "stale local member", localSerial: 40, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			memberCalls := map[string]int{}
			probe := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
				if domain == catalog {
					return dnsSOAProbeResult{
						Authoritative: true, RCode: dnsRCodeNoError,
						SOASerials: []uint32{9},
					}, nil
				}
				if domain != member || (address != peerIP && address != localIP) {
					return dnsSOAProbeResult{}, errors.New("unexpected member proof")
				}
				memberCalls[address+"/"+network]++
				serial := uint32(41)
				if address == localIP {
					serial = test.localSerial
				}
				return dnsSOAProbeResult{
					Authoritative: true, RCode: dnsRCodeNoError,
					SOASerials: []uint32{serial},
				}, nil
			}
			axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
				if address != peerIP || domain != catalog {
					return dnsCatalogAXFRResult{}, errors.New("unexpected catalog proof")
				}
				return dnsCatalogAXFRResult{Serial: 9, Members: []string{member}}, nil
			}
			proofCtx := context.Background()
			if test.wantErr {
				var cancel context.CancelFunc
				proofCtx, cancel = context.WithTimeout(proofCtx, 20*time.Millisecond)
				defer cancel()
			}
			err := verifyBINDPairingAuthorityAt(
				proofCtx, binddns.Receipt{Pairing: &pairing},
				localIP, probe, axfr,
			)
			if test.wantErr && err == nil {
				t.Fatal("stale local catalog member was accepted")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("exact catalog member was rejected: %v", err)
			}
			for _, key := range []string{
				peerIP + "/udp", peerIP + "/tcp",
				localIP + "/udp", localIP + "/tcp",
			} {
				if memberCalls[key] != 1 {
					t.Fatalf("member proof calls=%v; want one %s proof", memberCalls, key)
				}
			}
		})
	}
}

func TestVerifyBINDPrimaryProvesPowerDNSSecondaryMembers(t *testing.T) {
	const (
		peerIP  = "192.0.2.20"
		localIP = "192.0.2.10"
		catalog = "catalog-c000020a.celikpanel.invalid"
		member  = "example.test"
	)
	pairing := binddns.PairingReceipt{
		Role: binddns.PairRolePrimary, PeerIP: peerIP,
		LocalCatalog: catalog, CatalogSerial: 12,
	}
	probe := func(_ context.Context, network, address, domain string) (dnsSOAProbeResult, error) {
		if domain != member || (address != peerIP && address != localIP) ||
			(network != "udp" && network != "tcp") {
			return dnsSOAProbeResult{}, errors.New("unexpected mixed-engine proof")
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError,
			SOASerials: []uint32{73},
		}, nil
	}
	axfr := func(_ context.Context, address, domain string) (dnsCatalogAXFRResult, error) {
		if address != localIP || domain != catalog {
			return dnsCatalogAXFRResult{}, errors.New("unexpected local catalog proof")
		}
		return dnsCatalogAXFRResult{Serial: 12, Members: []string{member}}, nil
	}
	err := verifyBINDPairingAuthorityAt(
		context.Background(),
		binddns.Receipt{
			Pairing: &pairing,
			Zones:   []binddns.ZoneReceipt{{Domain: member}},
		},
		localIP, probe, axfr,
	)
	if err != nil {
		t.Fatalf("BIND primary to PowerDNS secondary proof rejected: %v", err)
	}
}

func TestVerifyPDNSPairingAuthoritySupportsMixedPeers(t *testing.T) {
	previousSOA := probeDNSZoneSOA
	previousAXFR := probeDNSCatalogAXFR
	previousLocal := dnsPairLocalProofAddress
	dnsPairLocalProofAddress = func() (string, error) { return "192.0.2.10", nil }
	probeDNSCatalogAXFR = func(_ context.Context, address, _ string) (dnsCatalogAXFRResult, error) {
		if address != "192.0.2.10" && address != "192.0.2.20" {
			t.Fatalf("catalog address=%q", address)
		}
		return dnsCatalogAXFRResult{Serial: 11, Members: []string{"example.test"}}, nil
	}
	probeDNSZoneSOA = func(_ context.Context, _, _, domain string) (dnsSOAProbeResult, error) {
		serial := uint32(2026081601)
		if strings.HasPrefix(domain, "catalog-") {
			serial = 11
		}
		return dnsSOAProbeResult{
			Authoritative: true, RCode: dnsRCodeNoError, SOASerials: []uint32{serial},
		}, nil
	}
	t.Cleanup(func() {
		probeDNSZoneSOA = previousSOA
		probeDNSCatalogAXFR = previousAXFR
		dnsPairLocalProofAddress = previousLocal
	})
	for _, role := range []string{
		transport.DNSPairRolePrimary,
		transport.DNSPairRoleSecondary,
	} {
		t.Run(role, func(t *testing.T) {
			zones := []transport.DNSEngineSwitchZoneSnapshot(nil)
			if role == transport.DNSPairRolePrimary {
				zones = []transport.DNSEngineSwitchZoneSnapshot{{
					Domain: "example.test", Records: testPDNSEngineRecords("example.test"),
				}}
			}
			manifest := mutationpayload.DNSEngineSwitchManifestCommitment{
				Topology: transport.DNSTopologyPaired, PairRole: role,
				LocalIP: "192.0.2.10", PeerIP: "192.0.2.20", Zones: zones,
			}
			if err := verifyPDNSPairingAuthority(context.Background(), manifest); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func testNegativeSOAResponse(
	t *testing.T,
	domain, authorityOwner string,
	rcode int,
) dnsSOAProbeResult {
	t.Helper()
	query, id, err := buildDNSZoneSOAQuery(domain)
	if err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 12)
	binary.BigEndian.PutUint16(response[0:2], id)
	binary.BigEndian.PutUint16(
		response[2:4], uint16(dnsResponseQR|dnsResponseAA|rcode),
	)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[8:10], 1)
	response = append(response, query[12:]...)
	owner, err := encodeDNSName(authorityOwner)
	if err != nil {
		t.Fatal(err)
	}
	response = append(response, owner...)
	response = append(response, 0, dnsTypeSOA, 0, dnsClassIN, 0, 0, 0, 60)
	mname, _ := encodeDNSName("ns1.example.net")
	rname, _ := encodeDNSName("hostmaster.example.net")
	rdata := append(append([]byte{}, mname...), rname...)
	numbers := make([]byte, 20)
	binary.BigEndian.PutUint32(numbers[0:4], 2026081601)
	rdata = append(rdata, numbers...)
	response = append(response, byte(len(rdata)>>8), byte(len(rdata)))
	response = append(response, rdata...)
	result, err := parseDNSZoneSOAResponse(response, id, domain)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func testCatalogAXFRMessage(t *testing.T, foreignPTR bool) ([]byte, uint16, string) {
	t.Helper()
	catalog := "catalog-c000020a.celikpanel.invalid"
	query, id, err := buildDNSCatalogAXFRQuery(catalog)
	if err != nil {
		t.Fatal(err)
	}
	message := make([]byte, 12)
	binary.BigEndian.PutUint16(message[0:2], id)
	binary.BigEndian.PutUint16(message[2:4], dnsResponseQR|dnsResponseAA)
	binary.BigEndian.PutUint16(message[4:6], 1)
	binary.BigEndian.PutUint16(message[6:8], 3)
	message = append(message, query[12:]...)
	appendRecord := func(owner string, recordType uint16, data []byte) {
		encodedOwner, encodeErr := encodeDNSName(owner)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		message = append(message, encodedOwner...)
		header := make([]byte, 10)
		binary.BigEndian.PutUint16(header[0:2], recordType)
		binary.BigEndian.PutUint16(header[2:4], dnsClassIN)
		binary.BigEndian.PutUint32(header[4:8], 60)
		binary.BigEndian.PutUint16(header[8:10], uint16(len(data)))
		message = append(message, header...)
		message = append(message, data...)
	}
	soa := func(serial uint32) []byte {
		mname, _ := encodeDNSName("ns1.example.test")
		rname, _ := encodeDNSName("hostmaster.example.test")
		data := append(append([]byte{}, mname...), rname...)
		numbers := make([]byte, 20)
		binary.BigEndian.PutUint32(numbers[:4], serial)
		return append(data, numbers...)
	}
	appendRecord(catalog, dnsTypeSOA, soa(9))
	ptrOwner := "member.zones." + catalog
	if foreignPTR {
		ptrOwner = "member.other." + catalog
	}
	member, _ := encodeDNSName("example.test")
	appendRecord(ptrOwner, dnsTypePTR, member)
	appendRecord(catalog, dnsTypeSOA, soa(9))
	return message, id, catalog
}

func TestParseDNSCatalogAXFRBindsSOAEnvelopeAndMembers(t *testing.T) {
	message, id, catalog := testCatalogAXFRMessage(t, false)
	serials, members, err := parseDNSCatalogAXFRMessage(message, id, catalog)
	if err != nil || len(serials) != 2 || serials[0] != 9 || serials[1] != 9 ||
		len(members) != 1 || members[0] != "example.test" {
		t.Fatalf("serials=%v members=%v err=%v", serials, members, err)
	}
	foreign, foreignID, foreignCatalog := testCatalogAXFRMessage(t, true)
	if _, _, err := parseDNSCatalogAXFRMessage(
		foreign, foreignID, foreignCatalog,
	); err == nil {
		t.Fatal("catalog PTR outside the member namespace was accepted")
	}
}

func TestExpectedDNSZoneAuthoritiesRequiresOneEnabledApexSOA(t *testing.T) {
	zones := []transport.DNSEngineSwitchZoneSnapshot{{
		Domain: "example.test", ZoneType: "NATIVE",
		Records: testPDNSEngineRecords("example.test"),
	}}
	expected, err := expectedDNSZoneAuthorities(zones)
	if err != nil || len(expected) != 1 || expected[0].Serial != 2026081601 {
		t.Fatalf("expected=%+v err=%v", expected, err)
	}
	zones[0].Records[0].Disabled = true
	if _, err := expectedDNSZoneAuthorities(zones); err == nil {
		t.Fatal("zone without an enabled SOA was accepted")
	}
}

func TestParseDNSZoneSOAResponseRejectsWrongSerialShape(t *testing.T) {
	query, id, err := buildDNSZoneSOAQuery("example.test")
	if err != nil {
		t.Fatal(err)
	}
	question := query[12:]
	response := make([]byte, 12)
	binary.BigEndian.PutUint16(response[0:2], id)
	binary.BigEndian.PutUint16(response[2:4], dnsResponseQR|dnsResponseAA)
	binary.BigEndian.PutUint16(response[4:6], 1)
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response, question...)
	response = append(response, 0xc0, 0x0c, 0, dnsTypeSOA, 0, dnsClassIN, 0, 0, 0, 60)
	mname, _ := encodeDNSName("ns1.example.net")
	rname, _ := encodeDNSName("hostmaster.example.test")
	rdata := append(append([]byte{}, mname...), rname...)
	numbers := make([]byte, 20)
	binary.BigEndian.PutUint32(numbers[0:4], 2026081601)
	rdata = append(rdata, numbers...)
	response = append(response, byte(len(rdata)>>8), byte(len(rdata)))
	response = append(response, rdata...)
	result, err := parseDNSZoneSOAResponse(response, id, "example.test")
	if err != nil || !result.Authoritative || len(result.SOASerials) != 1 || result.SOASerials[0] != 2026081601 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	response[len(response)-1] = 1 // numeric tail remains structurally valid; serial is unchanged.
	if _, err := parseDNSZoneSOAResponse(response[:len(response)-1], id, "example.test"); err == nil {
		t.Fatal("truncated SOA response was accepted")
	}
}

func TestExpectedDNSZoneAuthorityFromVerifiedBINDBytes(t *testing.T) {
	receipt := binddns.ZoneReceipt{Domain: "example.test"}
	data := []byte(strings.Join([]string{
		"; Managed by CelikPanel. DO NOT EDIT.",
		"$ORIGIN example.test.",
		"example.test.\t3600\tIN\tSOA\tns1.example.test. hostmaster.example.test. 2026081601 3600 600 1209600 300",
		"example.test.\t3600\tIN\tNS\tns1.example.test.",
		"",
	}, "\n"))
	expected, err := expectedDNSZoneAuthorityFromBINDTree(receipt, data)
	if err != nil || expected.Domain != "example.test" || expected.Delete ||
		expected.Serial != 2026081601 {
		t.Fatalf("expected=%+v err=%v", expected, err)
	}
	if _, err := expectedDNSZoneAuthorityFromBINDTree(receipt, []byte("tampered")); err == nil {
		t.Fatal("unsupported BIND master-file bytes were accepted")
	}
	receipt.Delete = true
	if _, err := expectedDNSZoneAuthorityFromBINDTree(receipt, []byte{}); err == nil {
		t.Fatal("deletion receipt with non-nil bytes was accepted")
	}
	expected, err = expectedDNSZoneAuthorityFromBINDTree(receipt, nil)
	if err != nil || !expected.Delete {
		t.Fatalf("delete expected=%+v err=%v", expected, err)
	}
}

func TestDeletedChildAcceptsAuthoritativeParentNegativeOverUDPAndTCP(t *testing.T) {
	domain := "mail.example.test"
	expected := []expectedDNSZoneAuthority{{Domain: domain, Delete: true}}
	for _, rcode := range []int{dnsRCodeNameError, dnsRCodeNoError} {
		result := testNegativeSOAResponse(t, domain, "Example.Test", rcode)
		if len(result.AuthoritySOAOwners) != 1 ||
			result.AuthoritySOAOwners[0] != "example.test" {
			t.Fatalf("authority SOA owner was not canonical: %+v", result)
		}
		var networks []string
		err := verifyDNSZoneAuthoritiesAt(
			context.Background(), "192.0.2.10", expected,
			func(_ context.Context, network, _, queried string) (dnsSOAProbeResult, error) {
				networks = append(networks, network)
				if queried != domain {
					t.Fatalf("queried domain=%q", queried)
				}
				return result, nil
			},
		)
		if err != nil {
			t.Fatalf("rcode=%d parent negative rejected: %v", rcode, err)
		}
		if strings.Join(networks, ",") != "udp,tcp" {
			t.Fatalf("rcode=%d networks=%v", rcode, networks)
		}
	}
}

func TestDeletedChildRejectsChildApexAuthoritativeSOA(t *testing.T) {
	domain := "mail.example.test"
	for _, rcode := range []int{dnsRCodeNameError, dnsRCodeNoError} {
		result := testNegativeSOAResponse(t, domain, domain, rcode)
		if validDeletedDNSZoneProof(domain, result) {
			t.Fatalf("rcode=%d child-apex negative authority was accepted", rcode)
		}
	}
	if !validDeletedDNSZoneProof(domain, dnsSOAProbeResult{RCode: dnsRCodeRefused}) {
		t.Fatal("non-authoritative REFUSED deletion proof regressed")
	}
}
