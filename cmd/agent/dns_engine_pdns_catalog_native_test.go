package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestPDNSCatalogBaseAcceptsOnlyMeasuredNativeRDATA(t *testing.T) {
	expected, err := canonicalPDNSCatalogBaseRecords("192.0.2.10", 1790542951, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !equalPDNSCatalogBaseRecordsWithNativeRDATA(expected, expected) {
		t.Fatal("staged catalog rejected")
	}
	native := append([]transport.ZoneRecord(nil), expected...)
	for i := range native {
		switch native[i].Type {
		case "SOA":
			native[i].Content = "invalid invalid 1790542951 60 30 3600 30"
		case "NS":
			native[i].Content = "invalid."
		}
	}
	if !equalPDNSCatalogBaseRecordsWithNativeRDATA(native, expected) {
		t.Fatal("measured mixed native RDATA rejected")
	}
	normalizedNS := append([]transport.ZoneRecord(nil), native...)
	for i := range normalizedNS {
		if normalizedNS[i].Type == "NS" {
			normalizedNS[i].Content = "invalid"
		}
	}
	if !equalPDNSCatalogBaseRecordsWithNativeRDATA(normalizedNS, expected) {
		t.Fatal("measured fully normalized native RDATA rejected")
	}
	for _, edit := range []func([]transport.ZoneRecord){
		func(rows []transport.ZoneRecord) { rows[0].TTL++ },
		func(rows []transport.ZoneRecord) {
			for i := range rows {
				if rows[i].Type == "SOA" {
					rows[i].Content = "foreign invalid 1790542951 60 30 3600 30"
				}
			}
		},
		func(rows []transport.ZoneRecord) {
			for i := range rows {
				if rows[i].Type == "NS" {
					rows[i].Content = "foreign"
				}
			}
		},
		func(rows []transport.ZoneRecord) {
			for i := range rows {
				if rows[i].Type == "SOA" {
					rows[i].Content = "invalid invalid 2 60 30 3600 30"
				}
			}
		},
	} {
		changed := append([]transport.ZoneRecord(nil), native...)
		edit(changed)
		if equalPDNSCatalogBaseRecordsWithNativeRDATA(changed, expected) {
			t.Fatal("foreign native catalog edit accepted")
		}
	}
}
