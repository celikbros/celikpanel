package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestParseDomainLogQueryDefaultsAndForwardsTimeRange(t *testing.T) {
	query, err := parseDomainLogQuery(url.Values{
		"filter":     {"warning"},
		"start_time": {"2026-08-02T12:00:00Z"},
		"end_time":   {"2026-08-02T13:00:00+00:00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if query.lines != 100 || query.filter != "warning" ||
		query.startTime != "2026-08-02T12:00:00Z" || query.endTime != "2026-08-02T13:00:00+00:00" {
		t.Fatalf("unexpected query: %#v", query)
	}
}

func TestParseDomainLogQueryRejectsAmbiguousOrUnboundedInputs(t *testing.T) {
	tests := []url.Values{
		{"lines": {"0"}},
		{"lines": {"5001"}},
		{"lines": {"not-a-number"}},
		{"filter": {strings.Repeat("x", 1025)}},
		{"start_time": {"2026-08-02 12:00:00"}},
		{
			"start_time": {"2026-08-02T13:00:00Z"},
			"end_time":   {"2026-08-02T12:00:00Z"},
		},
	}
	for _, values := range tests {
		if query, err := parseDomainLogQuery(values); err == nil {
			t.Fatalf("unexpectedly accepted %#v as %#v", values, query)
		}
	}
}
