package transport

import "strings"

// These codes describe why an exact, locally committed BIND V3 deletion is
// awaiting peer verification. They are observations, never proof of absence.
const (
	DNSPeerPendingEnrollmentRequired = "dns_peer_enrollment_required"
	DNSPeerPendingEnrollmentChanged  = "dns_peer_enrollment_changed"
	DNSPeerPendingInspectionUnknown  = "dns_peer_inspection_unknown"
	DNSPeerPendingNativeUnknown      = "dns_peer_native_unknown"
	DNSPeerPendingJournalUnknown     = "dns_peer_journal_unknown"
	DNSPeerPendingOwnerEditUnknown   = "dns_peer_owner_edit_unknown"
	// DNSPeerPendingCatalogTransferRefused: the authenticated inspector on the
	// secondary reported that its named refused the local (loopback) AXFR of
	// the primary's catalog zone. The secondary's owner allows it.
	DNSPeerPendingCatalogTransferRefused = "dns_peer_catalog_transfer_refused"
)

// Reviewed reasons the owner-enrolled secondary inspector may report for an
// incomplete observation. They travel as a fixed token, never as inspector,
// SSH or native command output, and each maps to a product-authored sentence.
const (
	DNSPeerInspectorReasonPolicy                 = "inspector_policy"
	DNSPeerInspectorReasonNamedUnavailable       = "named_unavailable"
	DNSPeerInspectorReasonListenersUnverified    = "listeners_unverified"
	DNSPeerInspectorReasonCatalogUnverified      = "catalog_unverified"
	DNSPeerInspectorReasonCatalogTransferRefused = "catalog_transfer_refused"
	DNSPeerInspectorReasonCatalogTransferFailed  = "catalog_transfer_failed"
	DNSPeerInspectorReasonCatalogMalformed       = "catalog_malformed"
	DNSPeerInspectorReasonObservationExpired     = "observation_expired"
)

// ValidDNSPeerInspectorReason reports a reviewed inspector reason token.
func ValidDNSPeerInspectorReason(reason string) bool {
	switch reason {
	case DNSPeerInspectorReasonPolicy,
		DNSPeerInspectorReasonNamedUnavailable,
		DNSPeerInspectorReasonListenersUnverified,
		DNSPeerInspectorReasonCatalogUnverified,
		DNSPeerInspectorReasonCatalogTransferRefused,
		DNSPeerInspectorReasonCatalogTransferFailed,
		DNSPeerInspectorReasonCatalogMalformed,
		DNSPeerInspectorReasonObservationExpired:
		return true
	default:
		return false
	}
}

// dnsPeerPendingDetailSeparator joins dns_peer_inspection_unknown with one
// reviewed inspector reason in the durable ledger error_code, for example
// "dns_peer_inspection_unknown:named_unavailable". Readers that predate it
// treat the composite as an unreviewed code and fall back to generic text.
const dnsPeerPendingDetailSeparator = ":"

// DNSPeerPendingInspectionUnknownWithDetail returns the composite code for an
// incomplete inspection whose authenticated inspector named a reviewed
// reason. The transfer refusal has its own typed code and is not a detail.
func DNSPeerPendingInspectionUnknownWithDetail(reason string) string {
	if !ValidDNSPeerInspectorReason(reason) ||
		reason == DNSPeerInspectorReasonCatalogTransferRefused {
		return DNSPeerPendingInspectionUnknown
	}
	return DNSPeerPendingInspectionUnknown + dnsPeerPendingDetailSeparator + reason
}

// SplitDNSPeerPendingCode returns the reviewed reason and optional reviewed
// inspector detail of a pending code. ok is false for anything unreviewed.
func SplitDNSPeerPendingCode(code string) (reason, detail string, ok bool) {
	reason, detail, composite := strings.Cut(code, dnsPeerPendingDetailSeparator)
	if !composite {
		return code, "", validDNSPeerPendingReason(code)
	}
	if reason != DNSPeerPendingInspectionUnknown || !ValidDNSPeerInspectorReason(detail) ||
		detail == DNSPeerInspectorReasonCatalogTransferRefused {
		return "", "", false
	}
	return reason, detail, true
}

func validDNSPeerPendingReason(code string) bool {
	switch code {
	case DNSPeerPendingEnrollmentRequired,
		DNSPeerPendingEnrollmentChanged,
		DNSPeerPendingInspectionUnknown,
		DNSPeerPendingNativeUnknown,
		DNSPeerPendingJournalUnknown,
		DNSPeerPendingOwnerEditUnknown,
		DNSPeerPendingCatalogTransferRefused:
		return true
	default:
		return false
	}
}

// ValidDNSPeerPendingCode accepts a reviewed reason, or the reviewed
// inspection_unknown composite carrying one reviewed inspector detail.
func ValidDNSPeerPendingCode(code string) bool {
	_, _, ok := SplitDNSPeerPendingCode(code)
	return ok
}
