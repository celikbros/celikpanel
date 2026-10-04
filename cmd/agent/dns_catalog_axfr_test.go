package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/transport"
)

type catalogAXFRTestRR struct {
	owner string
	type_ uint16
	class uint16
	ttl   uint32
	data  []byte
}

func catalogAXFRTestSOA(
	t *testing.T,
	owner, mname, rname string,
	serial, refresh, retry, expire, minimum uint32,
) catalogAXFRTestRR {
	t.Helper()
	mnameWire, err := encodeDNSName(mname)
	if err != nil {
		t.Fatal(err)
	}
	rnameWire, err := encodeDNSName(rname)
	if err != nil {
		t.Fatal(err)
	}
	data := append(append([]byte{}, mnameWire...), rnameWire...)
	numbers := make([]byte, 20)
	binary.BigEndian.PutUint32(numbers[0:4], serial)
	binary.BigEndian.PutUint32(numbers[4:8], refresh)
	binary.BigEndian.PutUint32(numbers[8:12], retry)
	binary.BigEndian.PutUint32(numbers[12:16], expire)
	binary.BigEndian.PutUint32(numbers[16:20], minimum)
	return catalogAXFRTestRR{
		owner: owner, type_: dnsTypeSOA, class: dnsClassIN,
		ttl: dnsCatalogAXFRTTL, data: append(data, numbers...),
	}
}

func exactCatalogAXFRTestRecords(
	t *testing.T,
	catalog, member string,
) []catalogAXFRTestRR {
	t.Helper()
	soa := func() catalogAXFRTestRR {
		return catalogAXFRTestSOA(
			t, catalog, "invalid", "invalid", 17, 60, 30, 3600, 30,
		)
	}
	invalid, err := encodeDNSName("invalid")
	if err != nil {
		t.Fatal(err)
	}
	target, err := encodeDNSName(member)
	if err != nil {
		t.Fatal(err)
	}
	label, err := binddns.CatalogMemberLabel(member)
	if err != nil {
		t.Fatal(err)
	}
	return []catalogAXFRTestRR{
		soa(),
		{owner: catalog, type_: dnsTypeNS, class: dnsClassIN, ttl: 60, data: invalid},
		{owner: "version." + catalog, type_: dnsTypeTXT, class: dnsClassIN, ttl: 60, data: []byte{1, '2'}},
		{
			owner: label + ".zones." + catalog,
			type_: dnsTypePTR, class: dnsClassIN, ttl: 60, data: target,
		},
		soa(),
	}
}

func buildCatalogAXFRTestMessage(
	t *testing.T,
	id uint16,
	catalog string,
	flags uint16,
	question bool,
	answers, authorities, additionals []catalogAXFRTestRR,
) []byte {
	t.Helper()
	message := make([]byte, 12)
	binary.BigEndian.PutUint16(message[0:2], id)
	binary.BigEndian.PutUint16(message[2:4], flags)
	if question {
		binary.BigEndian.PutUint16(message[4:6], 1)
	}
	binary.BigEndian.PutUint16(message[6:8], uint16(len(answers)))
	binary.BigEndian.PutUint16(message[8:10], uint16(len(authorities)))
	binary.BigEndian.PutUint16(message[10:12], uint16(len(additionals)))
	if question {
		name, err := encodeDNSName(catalog)
		if err != nil {
			t.Fatal(err)
		}
		message = append(message, name...)
		message = append(message, byte(dnsTypeAXFR>>8), byte(dnsTypeAXFR), 0, dnsClassIN)
	}
	appendRecords := func(records []catalogAXFRTestRR) {
		for _, record := range records {
			owner, err := encodeDNSName(record.owner)
			if err != nil {
				t.Fatal(err)
			}
			message = append(message, owner...)
			header := make([]byte, 10)
			binary.BigEndian.PutUint16(header[0:2], record.type_)
			binary.BigEndian.PutUint16(header[2:4], record.class)
			binary.BigEndian.PutUint32(header[4:8], record.ttl)
			binary.BigEndian.PutUint16(header[8:10], uint16(len(record.data)))
			message = append(message, header...)
			message = append(message, record.data...)
		}
	}
	appendRecords(answers)
	appendRecords(authorities)
	appendRecords(additionals)
	return message
}

func frameCatalogAXFRTestMessages(messages ...[]byte) []byte {
	stream := make([]byte, 0)
	for _, message := range messages {
		length := make([]byte, 2)
		binary.BigEndian.PutUint16(length, uint16(len(message)))
		stream = append(stream, length...)
		stream = append(stream, message...)
	}
	return stream
}

func TestReadDNSCatalogAXFRAcceptsExactMultiMessageTransfer(t *testing.T) {
	const id = uint16(0x4217)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	const member = "example.test"
	records := exactCatalogAXFRTestRecords(t, catalog, member)
	messages := [][]byte{
		buildCatalogAXFRTestMessage(t, id, catalog, dnsResponseQR|dnsResponseAA, true, records[:1], nil, nil),
		buildCatalogAXFRTestMessage(t, id, catalog, dnsResponseQR|dnsResponseAA, false, records[1:3], nil, nil),
		// RFC 5936 permits a subsequent message to repeat the exact question.
		buildCatalogAXFRTestMessage(t, id, catalog, dnsResponseQR|dnsResponseAA, true, records[3:], nil, nil),
	}
	result, err := readDNSCatalogAXFR(
		bytes.NewReader(frameCatalogAXFRTestMessages(messages...)), id, catalog,
	)
	if err != nil || result.Serial != 17 ||
		len(result.Members) != 1 || result.Members[0] != member {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReadDNSCatalogAXFRRejectsSplitDuplicateAndMissingBaseRecords(t *testing.T) {
	const id = uint16(0x4218)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	records := exactCatalogAXFRTestRecords(t, catalog, "example.test")
	for _, test := range []struct {
		name    string
		batches [][]catalogAXFRTestRR
	}{
		{name: "duplicate NS", batches: [][]catalogAXFRTestRR{records[:2], {records[1], records[2], records[3], records[4]}}},
		{name: "duplicate version", batches: [][]catalogAXFRTestRR{records[:3], {records[2], records[3], records[4]}}},
		{name: "duplicate PTR member hash", batches: [][]catalogAXFRTestRR{records[:4], {records[3], records[4]}}},
		{name: "missing NS", batches: [][]catalogAXFRTestRR{{records[0], records[2]}, {records[3], records[4]}}},
		{name: "missing version", batches: [][]catalogAXFRTestRR{records[:2], {records[3], records[4]}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages := make([][]byte, 0, len(test.batches))
			for index, batch := range test.batches {
				messages = append(messages, buildCatalogAXFRTestMessage(
					t, id, catalog, dnsResponseQR|dnsResponseAA, index == 0,
					batch, nil, nil,
				))
			}
			if _, err := readDNSCatalogAXFR(
				bytes.NewReader(frameCatalogAXFRTestMessages(messages...)), id, catalog,
			); err == nil {
				t.Fatal("noncanonical split transfer was accepted")
			}
		})
	}
}

func TestParseDNSCatalogAXFRRejectsPropertiesAndUnknownTypes(t *testing.T) {
	const id = uint16(0x4219)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	label, err := binddns.CatalogMemberLabel("example.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, owner string
		type_       uint16
		data        []byte
	}{
		{"CVE group quoted", "group." + label + ".zones." + catalog, dnsTypeTXT, []byte{3, '"', '2', '"'}},
		{"CVE group malformed", "group." + label + ".zones." + catalog, dnsTypeTXT, []byte{8, 'x'}},
		{"coo property", "coo." + catalog, dnsTypePTR, []byte{0}},
		{"unique property", "unique." + label + ".zones." + catalog, dnsTypeTXT, []byte{1, 'x'}},
		{"custom property", "custom." + label + ".zones." + catalog, dnsTypeTXT, []byte{1, 'x'}},
		{"APL", catalog, dnsTypeAPL, []byte{0}},
		{"A", catalog, 1, []byte{192, 0, 2, 1}},
		{"CNAME", catalog, 5, []byte{0}},
		{"MX", catalog, 15, []byte{0, 0, 0}},
		{"AAAA", catalog, 28, make([]byte, 16)},
		{"unknown", catalog, 65000, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			records := exactCatalogAXFRTestRecords(t, catalog, "example.test")
			injected := catalogAXFRTestRR{
				owner: test.owner, type_: test.type_, class: dnsClassIN,
				ttl: 60, data: test.data,
			}
			records = append(records[:4], injected, records[4])
			message := buildCatalogAXFRTestMessage(
				t, id, catalog, dnsResponseQR|dnsResponseAA, true, records, nil, nil,
			)
			if _, _, err := parseDNSCatalogAXFRMessage(message, id, catalog); err == nil {
				t.Fatal("unsupported catalog property/type was accepted")
			}
		})
	}
}

func TestParseDNSCatalogAXFRRequiresExactManagedRecords(t *testing.T) {
	const id = uint16(0x4220)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, []catalogAXFRTestRR)
	}{
		{"zero serial", func(t *testing.T, r []catalogAXFRTestRR) {
			r[0] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 0, 60, 30, 3600, 30)
		}},
		{"mismatched serial", func(t *testing.T, r []catalogAXFRTestRR) {
			r[4] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 18, 60, 30, 3600, 30)
		}},
		{"SOA MNAME", func(t *testing.T, r []catalogAXFRTestRR) {
			r[0] = catalogAXFRTestSOA(t, catalog, "ns.example", "invalid", 17, 60, 30, 3600, 30)
		}},
		{"SOA RNAME", func(t *testing.T, r []catalogAXFRTestRR) {
			r[0] = catalogAXFRTestSOA(t, catalog, "invalid", "hostmaster.example", 17, 60, 30, 3600, 30)
		}},
		{"SOA timers", func(t *testing.T, r []catalogAXFRTestRR) {
			r[0] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 17, 60, 30, 86400, 30)
		}},
		{"NS target", func(t *testing.T, r []catalogAXFRTestRR) {
			r[1].data, _ = encodeDNSName("ns.example")
		}},
		{"quoted version", func(_ *testing.T, r []catalogAXFRTestRR) { r[2].data = []byte{3, '"', '2', '"'} }},
		{"multiple TXT strings", func(_ *testing.T, r []catalogAXFRTestRR) { r[2].data = []byte{1, '2', 0} }},
		{"TTL", func(_ *testing.T, r []catalogAXFRTestRR) { r[3].ttl = 61 }},
		{"class", func(_ *testing.T, r []catalogAXFRTestRR) { r[2].class = 3 }},
		{"hash mismatch", func(_ *testing.T, r []catalogAXFRTestRR) {
			r[3].owner = "00000000000000000000000000000000000000000000000000000000.zones." + catalog
		}},
		{"short hash label", func(_ *testing.T, r []catalogAXFRTestRR) {
			r[3].owner = strings.Repeat("a", 55) + ".zones." + catalog
		}},
		{"long hash label", func(_ *testing.T, r []catalogAXFRTestRR) {
			r[3].owner = strings.Repeat("a", 57) + ".zones." + catalog
		}},
		{"uppercase hash", func(_ *testing.T, r []catalogAXFRTestRR) {
			r[3].owner = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.zones." + catalog
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			records := exactCatalogAXFRTestRecords(t, catalog, "example.test")
			test.mutate(t, records)
			message := buildCatalogAXFRTestMessage(
				t, id, catalog, dnsResponseQR|dnsResponseAA, true, records, nil, nil,
			)
			if _, _, err := parseDNSCatalogAXFRMessage(message, id, catalog); err == nil {
				t.Fatal("noncanonical managed record was accepted")
			}
		})
	}
}

func TestParseDNSCatalogAXFRRejectsNonAnswerSurface(t *testing.T) {
	const id = uint16(0x4221)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	records := exactCatalogAXFRTestRecords(t, catalog, "example.test")
	auxiliary := records[1]
	for _, test := range []struct {
		name        string
		flags       uint16
		question    bool
		authorities []catalogAXFRTestRR
		additionals []catalogAXFRTestRR
	}{
		{name: "AA absent", flags: dnsResponseQR, question: true},
		{name: "unexpected RD", flags: dnsResponseQR | dnsResponseAA | 0x0100, question: true},
		{name: "first question absent", flags: dnsResponseQR | dnsResponseAA},
		{name: "authority", flags: dnsResponseQR | dnsResponseAA, question: true, authorities: []catalogAXFRTestRR{auxiliary}},
		{name: "additional", flags: dnsResponseQR | dnsResponseAA, question: true, additionals: []catalogAXFRTestRR{auxiliary}},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := buildCatalogAXFRTestMessage(
				t, id, catalog, test.flags, test.question,
				records, test.authorities, test.additionals,
			)
			if _, _, err := parseDNSCatalogAXFRMessage(message, id, catalog); err == nil {
				t.Fatal("non-answer transfer surface was accepted")
			}
		})
	}
}

// The PTR label and TTLs were captured from a native PowerDNS 4.9.17
// producer AXFR; only this explicitly selected producer policy admits them.
func TestCatalogAXFRProbeSelectionUsesProducerEngine(t *testing.T) {
	oldBIND, oldBoundBIND := probeDNSCatalogAXFR, probeDNSBoundCatalogAXFR
	oldPDNS, oldBoundPDNS := probeDNSPDNSCatalogAXFR, probeDNSBoundPDNSCatalogAXFR
	t.Cleanup(func() {
		probeDNSCatalogAXFR, probeDNSBoundCatalogAXFR = oldBIND, oldBoundBIND
		probeDNSPDNSCatalogAXFR, probeDNSBoundPDNSCatalogAXFR = oldPDNS, oldBoundPDNS
	})
	calls := make([]string, 0, 4)
	probeDNSCatalogAXFR = func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		calls = append(calls, "bind-local")
		return dnsCatalogAXFRResult{}, nil
	}
	probeDNSBoundCatalogAXFR = func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
		calls = append(calls, "bind-peer")
		return dnsCatalogAXFRResult{}, nil
	}
	probeDNSPDNSCatalogAXFR = func(context.Context, string, string) (dnsCatalogAXFRResult, error) {
		calls = append(calls, "pdns-local")
		return dnsCatalogAXFRResult{}, nil
	}
	probeDNSBoundPDNSCatalogAXFR = func(context.Context, string, string, string) (dnsCatalogAXFRResult, error) {
		calls = append(calls, "pdns-peer")
		return dnsCatalogAXFRResult{}, nil
	}
	for _, tc := range []struct {
		engine transport.DNSEngine
		want   string
	}{
		{transport.DNSEngineBIND, "bind"}, {transport.DNSEnginePowerDNS, "pdns"},
	} {
		local, peer, err := catalogAXFRProbesForSourceEngine(tc.engine)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = local(context.Background(), "192.0.2.10", "catalog.example")
		_, _ = peer(context.Background(), "192.0.2.10", "192.0.2.11", "catalog.example")
		if got := calls[len(calls)-2:]; got[0] != tc.want+"-local" || got[1] != tc.want+"-peer" {
			t.Fatalf("producer %s routed to %v", tc.engine, got)
		}
	}
	if _, _, err := catalogAXFRProbesForSourceEngine(""); err == nil {
		t.Fatal("unknown producer selected a catalog parser")
	}
}

func TestPowerDNSCatalogAXFRProducerIsExplicitAndBounded(t *testing.T) {
	const id = uint16(0x4260)
	const catalog = "catalog-c000020a.celikpanel.invalid"
	const member = "s1-kill.test"
	records := exactCatalogAXFRTestRecords(t, catalog, member)
	records[0] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 1790542951, 60, 30, 3600, 30)
	records[4] = records[0]
	records[2].ttl = 0
	records[3].ttl = 0
	records[3].owner = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones." + catalog
	parse := func(records []catalogAXFRTestRR, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
		msg := buildCatalogAXFRTestMessage(t, id, catalog, dnsResponseQR|dnsResponseAA, true, records, nil, nil)
		return readDNSCatalogAXFRWithProducer(bytes.NewReader(frameCatalogAXFRTestMessages(msg)), id, catalog, producer)
	}
	got, err := parse(records, dnsCatalogAXFRPowerDNS)
	if err != nil || got.Serial != 1790542951 || len(got.Members) != 1 || got.Members[0] != member {
		t.Fatalf("PowerDNS result=%+v err=%v", got, err)
	}
	if _, err := parse(records, dnsCatalogAXFRBIND); err == nil {
		t.Fatal("BIND producer policy accepted PowerDNS catalog")
	}
	for _, test := range []struct {
		name   string
		change func([]catalogAXFRTestRR) []catalogAXFRTestRR
	}{
		{"outside zones namespace", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[3].owner = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuus.other." + catalog
			return r
		}},
		{"foreign catalog", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[3].owner = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones.foreign.invalid"
			return r
		}},
		{"short label", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[3].owner = "lf5eijnqp9ob8kmq5mv0vhjaevtcfuu.zones." + catalog
			return r
		}},
		{"non-base32hex label", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[3].owner = "wf5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones." + catalog
			return r
		}},
		{"duplicate owner", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			return append(r[:4], append([]catalogAXFRTestRR{r[3]}, r[4:]...)...)
		}},
		{"duplicate member different owner", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			extra := r[3]
			extra.owner = "af5eijnqp9ob8kmq5mv0vhjaevtcfuus.zones." + catalog
			return append(r[:4], append([]catalogAXFRTestRR{extra}, r[4:]...)...)
		}},
		{"wrong property TTL", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[3].ttl = 60
			return r
		}},
		{"wrong serial", func(r []catalogAXFRTestRR) []catalogAXFRTestRR {
			r[4] = catalogAXFRTestSOA(t, catalog, "invalid", "invalid", 1790542952, 60, 30, 3600, 30)
			return r
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			altered := append([]catalogAXFRTestRR(nil), records...)
			if _, err := parse(test.change(altered), dnsCatalogAXFRPowerDNS); err == nil {
				t.Fatal("noncanonical PowerDNS catalog accepted")
			}
		})
	}
}
