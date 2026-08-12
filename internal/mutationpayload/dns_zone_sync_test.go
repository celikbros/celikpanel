package mutationpayload

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestCanonicalDNSZoneSyncIsStableDetachedAndMultiplicityPreserving(t *testing.T) {
	first := transport.ZoneRecord{
		Name: "WWW.Example.Test.", Type: "a", Content: "192.0.2.20",
		TTL: 300,
	}
	duplicate := transport.ZoneRecord{
		Name: "example.test", Type: "MX", Content: "mail.example.test",
		TTL: 3600, Prio: 10,
	}
	input := []transport.ZoneRecord{duplicate, first, duplicate}

	commitment, err := CanonicalDNSZoneSync(7, "EXAMPLE.TEST.", false, " master ", input)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := CanonicalDNSZoneSync(
		7, "example.test", false, "MASTER",
		[]transport.ZoneRecord{first, duplicate, duplicate},
	)
	if err != nil {
		t.Fatal(err)
	}
	if commitment.Qualifier != reordered.Qualifier {
		t.Fatalf("equivalent snapshots produced different qualifiers: %q / %q", commitment.Qualifier, reordered.Qualifier)
	}
	if !ValidDNSZoneSyncQualifier(commitment.Qualifier) {
		t.Fatalf("generated qualifier is invalid: %q", commitment.Qualifier)
	}
	if commitment.Domain != "example.test" || commitment.ZoneType != "MASTER" || commitment.Delete {
		t.Fatalf("canonical identity=%#v", commitment)
	}
	if len(commitment.Records) != 3 ||
		commitment.Records[0].Name != "example.test" ||
		commitment.Records[1].Name != "example.test" ||
		commitment.Records[2].Name != "www.example.test" {
		t.Fatalf("canonical records=%#v", commitment.Records)
	}
	input[0].Content = "attacker.example.test"
	if commitment.Records[0].Content != "mail.example.test" {
		t.Fatal("canonical DNS snapshot aliases caller memory")
	}

	withoutDuplicate, err := CanonicalDNSZoneSync(
		7, "example.test", false, "MASTER", []transport.ZoneRecord{first, duplicate},
	)
	if err != nil {
		t.Fatal(err)
	}
	if withoutDuplicate.Qualifier == commitment.Qualifier {
		t.Fatal("duplicate record multiplicity was not committed")
	}
}

func TestCanonicalDNSZoneSyncSeparatesEveryEffectiveField(t *testing.T) {
	record := transport.ZoneRecord{
		Name: "example.test", Type: "MX", Content: "mail.example.test",
		TTL: 3600, Prio: 10,
	}
	base, err := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func() DNSZoneSyncCommitment{
		"generation": func() DNSZoneSyncCommitment {
			got, _ := CanonicalDNSZoneSync(11, "example.test", false, "NATIVE", []transport.ZoneRecord{record})
			return got
		},
		"domain": func() DNSZoneSyncCommitment {
			changed := record
			changed.Name = "other.test"
			changed.Content = "mail.other.test"
			got, _ := CanonicalDNSZoneSync(10, "other.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"zone type": func() DNSZoneSyncCommitment {
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "MASTER", []transport.ZoneRecord{record})
			return got
		},
		"name": func() DNSZoneSyncCommitment {
			changed := record
			changed.Name = "mail.example.test"
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"type": func() DNSZoneSyncCommitment {
			changed := record
			changed.Type = "CNAME"
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"content": func() DNSZoneSyncCommitment {
			changed := record
			changed.Content = "other.example.test"
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"TTL": func() DNSZoneSyncCommitment {
			changed := record
			changed.TTL++
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"priority": func() DNSZoneSyncCommitment {
			changed := record
			changed.Prio++
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
		"disabled": func() DNSZoneSyncCommitment {
			changed := record
			changed.Disabled = true
			got, _ := CanonicalDNSZoneSync(10, "example.test", false, "NATIVE", []transport.ZoneRecord{changed})
			return got
		},
	}
	for name, mutate := range mutations {
		if got := mutate(); got.Qualifier == "" || got.Qualifier == base.Qualifier {
			t.Fatalf("%s change did not change commitment: %#v", name, got)
		}
	}
}

func TestCanonicalDNSZoneSyncDeleteIsExplicitAndEmpty(t *testing.T) {
	deleted, err := CanonicalDNSZoneSync(4, "example.test", true, "NATIVE", nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := CanonicalDNSZoneSync(4, "example.test", true, "NATIVE", []transport.ZoneRecord{})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Qualifier != empty.Qualifier || !deleted.Delete || deleted.Records != nil {
		t.Fatalf("canonical deletion=%#v empty=%#v", deleted, empty)
	}
	if _, err := CanonicalDNSZoneSync(4, "example.test", false, "NATIVE", nil); err == nil {
		t.Fatal("full-zone sync accepted an empty snapshot")
	}
	if _, err := CanonicalDNSZoneSync(4, "example.test", true, "NATIVE", []transport.ZoneRecord{{
		Name: "example.test", Type: "A", Content: "192.0.2.1", TTL: 300,
	}}); err == nil {
		t.Fatal("deletion accepted hidden records")
	}
}

func TestCanonicalDNSZoneSyncCommitsContentByteExactly(t *testing.T) {
	base := transport.ZoneRecord{
		Name: "example.test", Type: "TXT", Content: " value ", TTL: 300,
	}
	withSpaces, err := CanonicalDNSZoneSync(
		1, "example.test", false, "NATIVE", []transport.ZoneRecord{base},
	)
	if err != nil {
		t.Fatalf("legitimate whitespace-bearing PowerDNS content was rejected: %v", err)
	}
	if got := withSpaces.Records[0].Content; got != " value " {
		t.Fatalf("content=%q want byte-exact whitespace", got)
	}
	base.Content = "value"
	withoutSpaces, err := CanonicalDNSZoneSync(
		1, "example.test", false, "NATIVE", []transport.ZoneRecord{base},
	)
	if err != nil {
		t.Fatal(err)
	}
	if withSpaces.Qualifier == withoutSpaces.Qualifier {
		t.Fatal("leading/trailing content bytes were omitted from the commitment")
	}
}

func TestCanonicalDNSZoneSyncRejectsUnsafeSnapshots(t *testing.T) {
	valid := transport.ZoneRecord{
		Name: "example.test", Type: "A", Content: "192.0.2.1", TTL: 300,
	}
	tests := []struct {
		name       string
		generation int64
		domain     string
		zoneType   string
		records    []transport.ZoneRecord
	}{
		{name: "negative generation", generation: -1, domain: "example.test", zoneType: "NATIVE"},
		{name: "invalid domain", domain: "localhost", zoneType: "NATIVE"},
		{name: "missing zone type", domain: "example.test"},
		{name: "secondary zone type", domain: "example.test", zoneType: "SECONDARY"},
		{name: "record outside zone", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "other.test", Type: "A", Content: "192.0.2.1", TTL: 300}}},
		{name: "invalid wildcard", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "www.*.example.test", Type: "A", Content: "192.0.2.1", TTL: 300}}},
		{name: "invalid type", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "A TXT", Content: "value", TTL: 300}}},
		{name: "empty content", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "TXT", TTL: 300}}},
		{name: "content control", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "TXT", Content: "bad\nvalue", TTL: 300}}},
		{name: "negative TTL", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "A", Content: "192.0.2.1", TTL: -1}}},
		{name: "TTL overflow", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "A", Content: "192.0.2.1", TTL: dnsZoneSyncMaxTTL + 1}}},
		{name: "negative priority", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "MX", Content: "mail.example.test", TTL: 300, Prio: -1}}},
		{name: "priority overflow", domain: "example.test", zoneType: "NATIVE", records: []transport.ZoneRecord{{Name: "example.test", Type: "MX", Content: "mail.example.test", TTL: 300, Prio: dnsZoneSyncMaxPriority + 1}}},
		{name: "too many records", domain: "example.test", zoneType: "NATIVE", records: make([]transport.ZoneRecord, dnsZoneSyncMaxRecords+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records := test.records
			if records == nil && test.generation >= 0 && test.domain == "example.test" && test.zoneType == "NATIVE" {
				records = []transport.ZoneRecord{valid}
			}
			if _, err := CanonicalDNSZoneSync(test.generation, test.domain, false, test.zoneType, records); err == nil {
				t.Fatal("unsafe DNS snapshot was accepted")
			}
		})
	}
}

func TestCanonicalDNSZoneSyncRejectsOversizedPayload(t *testing.T) {
	records := make([]transport.ZoneRecord, dnsZoneSyncMaxSnapshotPayloadBytes/dnsZoneSyncMaxRecordContentBytes+1)
	content := strings.Repeat("x", dnsZoneSyncMaxRecordContentBytes)
	for index := range records {
		records[index] = transport.ZoneRecord{
			Name: "example.test", Type: "TXT", Content: content, TTL: 300,
		}
	}
	if _, err := CanonicalDNSZoneSync(1, "example.test", false, "NATIVE", records); err == nil {
		t.Fatal("oversized DNS snapshot payload was accepted")
	}
}

func TestValidDNSZoneSyncQualifierRejectsNonCanonicalValues(t *testing.T) {
	valid := dnsZoneSyncQualifierPrefix + strings.Repeat("a", 64)
	if !ValidDNSZoneSyncQualifier(valid) {
		t.Fatal("canonical qualifier was rejected")
	}
	for _, invalid := range []string{
		"", dnsZoneSyncQualifierPrefix,
		dnsZoneSyncQualifierPrefix + strings.Repeat("a", 63),
		dnsZoneSyncQualifierPrefix + strings.Repeat("A", 64),
		"dns-zone-sync/v2:sha256:" + strings.Repeat("a", 64),
	} {
		if ValidDNSZoneSyncQualifier(invalid) {
			t.Fatalf("invalid qualifier accepted: %q", invalid)
		}
	}
}
