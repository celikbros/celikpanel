package binddns

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func testZoneRecords(domain, address string) []transport.ZoneRecord {
	return []transport.ZoneRecord{
		{Name: domain, Type: "SOA", Content: "ns1.example.net hostmaster." + domain + " 2026081601 10800 3600 604800 3600", TTL: 3600},
		{Name: domain, Type: "NS", Content: "ns1.example.net", TTL: 3600},
		{Name: domain, Type: "A", Content: address, TTL: 300},
	}
}

func TestRenderZoneSupportedRecordsAndPrioritySemantics(t *testing.T) {
	records := []transport.ZoneRecord{
		{Name: "example.com", Type: "SOA", Content: "ns1.example.net hostmaster.example.com 2026081601 10800 3600 604800 3600", TTL: 3600},
		{Name: "example.com", Type: "NS", Content: "NS1.EXAMPLE.NET.", TTL: 3600},
		{Name: "example.com", Type: "A", Content: "192.0.2.10", TTL: 300},
		{Name: "ipv6.example.com", Type: "AAAA", Content: "2001:0DB8::1", TTL: 300},
		{Name: "www.example.com", Type: "CNAME", Content: "EXAMPLE.COM.", TTL: 300},
		{Name: "example.com", Type: "MX", Content: "mail.example.com", TTL: 3600, Prio: 10},
		{Name: "example.com", Type: "TXT", Content: `"v=spf1 " "-all"`, TTL: 300},
		{Name: "_sip._tcp.example.com", Type: "SRV", Content: "5 443 sip.example.com", TTL: 300, Prio: 20},
		{Name: "example.com", Type: "CAA", Content: `0 ISSUE "letsencrypt.org"`, TTL: 300},
		{Name: "_25._tcp.mail.example.com", Type: "TLSA", Content: "3 0 1 0123456789ABCDEF", TTL: 300},
		{Name: "disabled.example.com", Type: "A", Content: "192.0.2.99", TTL: 300, Disabled: true},
	}
	rendered, err := RenderZone("example.com", records)
	if err != nil {
		t.Fatalf("RenderZone: %v", err)
	}
	text := string(rendered.Data)
	for _, line := range []string{
		"example.com.\t300\tIN\tA\t192.0.2.10",
		"ipv6.example.com.\t300\tIN\tAAAA\t2001:db8::1",
		"www.example.com.\t300\tIN\tCNAME\texample.com.",
		"example.com.\t3600\tIN\tMX\t10 mail.example.com.",
		"example.com.\t300\tIN\tTXT\t\"v=spf1 -all\"",
		"_sip._tcp.example.com.\t300\tIN\tSRV\t20 5 443 sip.example.com.",
		"example.com.\t300\tIN\tCAA\t0 issue \"letsencrypt.org\"",
		"_25._tcp.mail.example.com.\t300\tIN\tTLSA\t3 0 1 0123456789abcdef",
		"example.com.\t3600\tIN\tSOA\tns1.example.net. hostmaster.example.com. 2026081601 10800 3600 604800 3600",
	} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("rendered zone missing %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, "192.0.2.99") {
		t.Fatalf("disabled record leaked into zone:\n%s", text)
	}
	if rendered.TotalRecords != len(records) || rendered.EnabledRecords != len(records)-1 {
		t.Fatalf("record counts = %d/%d", rendered.EnabledRecords, rendered.TotalRecords)
	}
}

func TestRenderZonePreservesDuplicateMultiplicityAndDeterminism(t *testing.T) {
	record := transport.ZoneRecord{Name: "dup.example.com", Type: "A", Content: "192.0.2.44", TTL: 60}
	first, err := RenderZone("example.com", []transport.ZoneRecord{record, record})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderZone("example.com", []transport.ZoneRecord{record, record})
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Data) != string(second.Data) || first.RecordsSHA256 != second.RecordsSHA256 {
		t.Fatal("same duplicate snapshot was not deterministic")
	}
	if got := strings.Count(string(first.Data), "dup.example.com."); got != 2 {
		t.Fatalf("duplicate multiplicity = %d, want 2", got)
	}
}

func TestDisabledRecordIsOmittedButBoundIntoHash(t *testing.T) {
	base := []transport.ZoneRecord{{Name: "example.com", Type: "A", Content: "192.0.2.1", TTL: 60}}
	without, err := RenderZone("example.com", base)
	if err != nil {
		t.Fatal(err)
	}
	with, err := RenderZone("example.com", append(base,
		transport.ZoneRecord{Name: "hidden.example.com", Type: "A", Content: "192.0.2.2", TTL: 60, Disabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(with.Data), "hidden") {
		t.Fatal("disabled record was rendered")
	}
	if without.RecordsSHA256 == with.RecordsSHA256 {
		t.Fatal("disabled record did not affect receipt hash")
	}
}

func TestTXTCanonicalSegmentation(t *testing.T) {
	value := strings.Repeat("a", 300)
	rendered, err := RenderZone("example.com", []transport.ZoneRecord{{
		Name: "example.com", Type: "TXT", Content: value, TTL: 60,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := `"` + strings.Repeat("a", 255) + `" "` + strings.Repeat("a", 45) + `"`
	if !strings.Contains(string(rendered.Data), want) {
		t.Fatalf("TXT was not split at 255 bytes: %s", rendered.Data)
	}
}

func TestRenderZoneRejectsUnsafeOrAmbiguousRecords(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		record transport.ZoneRecord
	}{
		{"noncanonical domain", "Example.com", transport.ZoneRecord{Name: "example.com", Type: "A", Content: "192.0.2.1", TTL: 1}},
		{"owner traversal", "example.com", transport.ZoneRecord{Name: "evil.test", Type: "A", Content: "192.0.2.1", TTL: 1}},
		{"owner injection", "example.com", transport.ZoneRecord{Name: "x;zone.example.com", Type: "A", Content: "192.0.2.1", TTL: 1}},
		{"unsupported type", "example.com", transport.ZoneRecord{Name: "example.com", Type: "NAPTR", Content: "x", TTL: 1}},
		{"bad IPv4", "example.com", transport.ZoneRecord{Name: "example.com", Type: "A", Content: "192.0.2.1; include x", TTL: 1}},
		{"target injection", "example.com", transport.ZoneRecord{Name: "www.example.com", Type: "CNAME", Content: "example.com; zone x", TTL: 1}},
		{"TXT quote injection", "example.com", transport.ZoneRecord{Name: "example.com", Type: "TXT", Content: `hello"; zone "evil`, TTL: 1}},
		{"CAA backslash", "example.com", transport.ZoneRecord{Name: "example.com", Type: "CAA", Content: `0 issue "bad\\value"`, TTL: 1}},
		{"TLSA odd hex", "example.com", transport.ZoneRecord{Name: "_25._tcp.example.com", Type: "TLSA", Content: "3 0 1 abc", TTL: 1}},
		{"SOA off apex", "example.com", transport.ZoneRecord{Name: "x.example.com", Type: "SOA", Content: "ns.example.net hostmaster.example.com 1 2 3 4 5", TTL: 1}},
		{"hidden priority", "example.com", transport.ZoneRecord{Name: "example.com", Type: "A", Content: "192.0.2.1", TTL: 1, Prio: 10}},
		{"control", "example.com", transport.ZoneRecord{Name: "example.com", Type: "TXT", Content: "a\nb", TTL: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RenderZone(test.domain, []transport.ZoneRecord{test.record}); err == nil {
				t.Fatal("unsafe record was accepted")
			}
		})
	}
}
