package main

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

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
