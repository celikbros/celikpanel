package transport

import "testing"

func TestDNSPeerPendingCodesAcceptOnlyReviewedCompositeDetail(t *testing.T) {
	for code, want := range map[string][2]string{
		"dns_peer_inspection_unknown":                          {"dns_peer_inspection_unknown", ""},
		"dns_peer_catalog_transfer_refused":                    {"dns_peer_catalog_transfer_refused", ""},
		"dns_peer_proof_internal":                              {"dns_peer_proof_internal", ""},
		"dns_peer_proof_internal:named_unavailable":            {"", ""},
		"dns_peer_proof_timeout":                               {"dns_peer_proof_timeout", ""},
		"dns_peer_proof_timeout:exchange":                      {"", ""},
		"dns_peer_inspection_unknown:named_unavailable":        {"dns_peer_inspection_unknown", "named_unavailable"},
		"dns_peer_inspection_unknown:observation_expired":      {"dns_peer_inspection_unknown", "observation_expired"},
		"dns_peer_inspection_unknown:config_unreviewed":        {"dns_peer_inspection_unknown", "config_unreviewed"},
		"dns_peer_inspection_unknown:catalog_transfer_refused": {"", ""},
		"dns_peer_native_unknown:named_unavailable":            {"", ""},
		"dns_peer_inspection_unknown:":                         {"", ""},
		"dns_peer_inspection_unknown:raw stderr":               {"", ""},
		"bind_rndc_unavailable":                                {"", ""},
		"dns_peer_owner_edit_unknown":                          {"dns_peer_owner_edit_unknown", ""},
		"dns_peer_owner_edit_unknown:producer_catalog":         {"dns_peer_owner_edit_unknown", "producer_catalog"},
		"dns_peer_owner_edit_unknown:zone_answered":            {"dns_peer_owner_edit_unknown", "zone_answered"},
		"dns_peer_owner_edit_unknown:named_unavailable":        {"", ""},
		"dns_peer_inspection_unknown:producer_catalog":         {"", ""},
		"dns_peer_owner_edit_unknown:":                         {"", ""},
		"dns_peer_owner_edit_unknown:serial 1790745288":        {"", ""},
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
	if DNSPeerPendingOwnerEditUnknownWithDetail("nope") != DNSPeerPendingOwnerEditUnknown ||
		DNSPeerPendingOwnerEditUnknownWithDetail("config_unreviewed") != DNSPeerPendingOwnerEditUnknown ||
		DNSPeerPendingOwnerEditUnknownWithDetail("authority") != "dns_peer_owner_edit_unknown:authority" {
		t.Fatal("owner-edit composite accepted an unreviewed check")
	}
}

// Every owner-edit check token round-trips through the durable ledger code
// and is disjoint from the inspector reasons.
func TestDNSPeerOwnerEditChecksRoundTrip(t *testing.T) {
	checks := []string{
		DNSPeerOwnerEditCheckOperationAttempt, DNSPeerOwnerEditCheckEngineState,
		DNSPeerOwnerEditCheckActiveEngine, DNSPeerOwnerEditCheckNativeBinding,
		DNSPeerOwnerEditCheckDeletionReceipt, DNSPeerOwnerEditCheckProducerCatalog,
		DNSPeerOwnerEditCheckCatalogProbe, DNSPeerOwnerEditCheckAuthority,
		DNSPeerOwnerEditCheckTransferObserved, DNSPeerOwnerEditCheckZoneAnswered,
	}
	for _, check := range checks {
		code := DNSPeerPendingOwnerEditUnknownWithDetail(check)
		reason, detail, ok := SplitDNSPeerPendingCode(code)
		if !ok || reason != DNSPeerPendingOwnerEditUnknown || detail != check ||
			ValidDNSPeerInspectorReason(check) || len(code) > 64 {
			t.Fatalf("%q -> %q %q %t", code, reason, detail, ok)
		}
	}
}
