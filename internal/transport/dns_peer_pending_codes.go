package transport

// These codes describe why an exact, locally committed BIND V3 deletion is
// awaiting peer verification. They are observations, never proof of absence.
const (
	DNSPeerPendingEnrollmentRequired = "dns_peer_enrollment_required"
	DNSPeerPendingEnrollmentChanged  = "dns_peer_enrollment_changed"
	DNSPeerPendingInspectionUnknown  = "dns_peer_inspection_unknown"
	DNSPeerPendingNativeUnknown      = "dns_peer_native_unknown"
	DNSPeerPendingJournalUnknown     = "dns_peer_journal_unknown"
	DNSPeerPendingOwnerEditUnknown   = "dns_peer_owner_edit_unknown"
)

func ValidDNSPeerPendingCode(code string) bool {
	switch code {
	case DNSPeerPendingEnrollmentRequired,
		DNSPeerPendingEnrollmentChanged,
		DNSPeerPendingInspectionUnknown,
		DNSPeerPendingNativeUnknown,
		DNSPeerPendingJournalUnknown,
		DNSPeerPendingOwnerEditUnknown:
		return true
	default:
		return false
	}
}
