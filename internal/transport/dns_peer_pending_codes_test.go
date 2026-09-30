package transport

import "testing"

func TestDNSPeerPendingCodesAcceptOnlyReviewedCompositeDetail(t *testing.T) {
	for code, want := range map[string][2]string{
		"dns_peer_inspection_unknown":                          {"dns_peer_inspection_unknown", ""},
		"dns_peer_catalog_transfer_refused":                    {"dns_peer_catalog_transfer_refused", ""},
		"dns_peer_inspection_unknown:named_unavailable":        {"dns_peer_inspection_unknown", "named_unavailable"},
		"dns_peer_inspection_unknown:observation_expired":      {"dns_peer_inspection_unknown", "observation_expired"},
		"dns_peer_inspection_unknown:catalog_transfer_refused": {"", ""},
		"dns_peer_native_unknown:named_unavailable":            {"", ""},
		"dns_peer_inspection_unknown:":                         {"", ""},
		"dns_peer_inspection_unknown:raw stderr":               {"", ""},
		"bind_rndc_unavailable":                                {"", ""},
	} {
		reason, detail, ok := SplitDNSPeerPendingCode(code)
		if ok != (want[0] != "") || reason != want[0] && ok || detail != want[1] && ok || ValidDNSPeerPendingCode(code) != ok {
			t.Fatalf("%q -> %q %q %t", code, reason, detail, ok)
		}
	}
	if DNSPeerPendingInspectionUnknownWithDetail("catalog_transfer_refused") != DNSPeerPendingInspectionUnknown ||
		DNSPeerPendingInspectionUnknownWithDetail("nope") != DNSPeerPendingInspectionUnknown ||
		DNSPeerPendingInspectionUnknownWithDetail("catalog_malformed") != "dns_peer_inspection_unknown:catalog_malformed" {
		t.Fatal("composite construction accepted an unreviewed or typed detail")
	}
}
