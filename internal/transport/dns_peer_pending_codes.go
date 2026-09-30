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
	// DNSPeerPendingProofInternal: the Agent could not run its own proof of
	// the secondary (an internal precondition of the proof failed, for
	// example its source engine receipt could not select the catalog
	// probes). No owner change was observed; the owner changed nothing.
	// Retrying does not help until the Agent is corrected.
	DNSPeerPendingProofInternal = "dns_peer_proof_internal"
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
	// DNSPeerInspectorReasonConfigUnreviewed: the PowerDNS inspector found the
	// daemon's configuration in none of its reviewed shapes (the documented
	// panel-free one or a CelikPanel-managed secondary), or could not read it
	// safely. Only pdns-peer-inspect reports it.
	DNSPeerInspectorReasonConfigUnreviewed = "config_unreviewed"
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
		DNSPeerInspectorReasonObservationExpired,
		DNSPeerInspectorReasonConfigUnreviewed:
		return true
	default:
		return false
	}
}

// Reviewed names of the check that found different evidence when the Agent
// keeps a deletion pending as dns_peer_owner_edit_unknown (D-024). Each names
// one bounded comparison of the native peer proof; none carries an observed
// value. The Agent logs the recorded and observed values separately.
const (
	// The durable ledger's attempt of this operation changed or was lost.
	DNSPeerOwnerEditCheckOperationAttempt = "operation_attempt"
	// This server's DNS engine state receipt changed or was unreadable.
	DNSPeerOwnerEditCheckEngineState = "engine_state"
	// Another DNS service became active, or the managed one stopped.
	DNSPeerOwnerEditCheckActiveEngine = "active_engine"
	// The DNS daemon's process, database file or managed runtime
	// configuration changed during the check.
	DNSPeerOwnerEditCheckNativeBinding = "native_binding"
	// The exact durable deletion receipt of this operation changed.
	DNSPeerOwnerEditCheckDeletionReceipt = "deletion_receipt"
	// The producer catalog differs from the recorded evidence other than by
	// an admitted PowerDNS daemon re-stamp.
	DNSPeerOwnerEditCheckProducerCatalog = "producer_catalog"
	// After the inspection, the catalog served by this server or the
	// secondary no longer matched the recorded evidence.
	DNSPeerOwnerEditCheckCatalogProbe = "catalog_probe"
	// After the inspection, the catalog pair answered for another identity.
	DNSPeerOwnerEditCheckAuthority = "authority"
	// After the inspection, the secondary did not refuse the deleted zone's
	// transfer, or the check did not complete.
	DNSPeerOwnerEditCheckTransferObserved = "transfer_observed"
	// After the inspection, the secondary's answer for the deleted zone was
	// not the empty REFUSED seen before, or the query did not complete.
	DNSPeerOwnerEditCheckZoneAnswered = "zone_answered"
)

// ValidDNSPeerOwnerEditCheck reports a reviewed owner-edit check token.
func ValidDNSPeerOwnerEditCheck(check string) bool {
	switch check {
	case DNSPeerOwnerEditCheckOperationAttempt,
		DNSPeerOwnerEditCheckEngineState,
		DNSPeerOwnerEditCheckActiveEngine,
		DNSPeerOwnerEditCheckNativeBinding,
		DNSPeerOwnerEditCheckDeletionReceipt,
		DNSPeerOwnerEditCheckProducerCatalog,
		DNSPeerOwnerEditCheckCatalogProbe,
		DNSPeerOwnerEditCheckAuthority,
		DNSPeerOwnerEditCheckTransferObserved,
		DNSPeerOwnerEditCheckZoneAnswered:
		return true
	default:
		return false
	}
}

// DNSPeerPendingOwnerEditUnknownWithDetail returns the composite code naming
// the check that found different evidence, for example
// "dns_peer_owner_edit_unknown:producer_catalog". An unreviewed check keeps
// the plain reason.
func DNSPeerPendingOwnerEditUnknownWithDetail(check string) string {
	if !ValidDNSPeerOwnerEditCheck(check) {
		return DNSPeerPendingOwnerEditUnknown
	}
	return DNSPeerPendingOwnerEditUnknown + dnsPeerPendingDetailSeparator + check
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
// detail of a pending code: an inspector reason for
// dns_peer_inspection_unknown, or an owner-edit check for
// dns_peer_owner_edit_unknown. ok is false for anything unreviewed.
func SplitDNSPeerPendingCode(code string) (reason, detail string, ok bool) {
	reason, detail, composite := strings.Cut(code, dnsPeerPendingDetailSeparator)
	if !composite {
		return code, "", validDNSPeerPendingReason(code)
	}
	switch reason {
	case DNSPeerPendingInspectionUnknown:
		if !ValidDNSPeerInspectorReason(detail) ||
			detail == DNSPeerInspectorReasonCatalogTransferRefused {
			return "", "", false
		}
	case DNSPeerPendingOwnerEditUnknown:
		if !ValidDNSPeerOwnerEditCheck(detail) {
			return "", "", false
		}
	default:
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
		DNSPeerPendingCatalogTransferRefused,
		DNSPeerPendingProofInternal:
		return true
	default:
		return false
	}
}

// ValidDNSPeerPendingCode accepts a reviewed reason, the reviewed
// inspection_unknown composite carrying one reviewed inspector detail, or the
// owner_edit_unknown composite carrying one reviewed check.
func ValidDNSPeerPendingCode(code string) bool {
	_, _, ok := SplitDNSPeerPendingCode(code)
	return ok
}
