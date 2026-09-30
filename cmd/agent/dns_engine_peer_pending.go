package main

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

// dnsPeerPendingError carries only a reviewed code. Probe, SSH and peer output
// must never become a durable owner-facing message.
type dnsPeerPendingError struct{ code string }

func (e *dnsPeerPendingError) Error() string { return e.code }

func pendingBINDPeer(code string) error {
	if !transport.ValidDNSPeerPendingCode(code) {
		code = transport.DNSPeerPendingNativeUnknown
	}
	return &dnsPeerPendingError{code: code}
}

func pendingDNSPeerCode(err error) string {
	var peer *dnsPeerPendingError
	if errors.As(err, &peer) && transport.ValidDNSPeerPendingCode(peer.code) {
		return peer.code
	}
	return ""
}

// peerCurrentPendingCodeAt keeps proved enrollment drift separate from an
// changed operation or local DNS evidence. The callbacks run in that order so
// no later observation can replace the first failed boundary. Their raw errors
// must never become a durable or owner-facing peer reason.
func peerCurrentPendingCodeAt(attempt, enrollment, local func() error) error {
	if attempt == nil || enrollment == nil || local == nil {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	if attempt() != nil {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	if enrollment() != nil {
		return pendingBINDPeer(transport.DNSPeerPendingEnrollmentChanged)
	}
	if local() != nil {
		return pendingBINDPeer(transport.DNSPeerPendingOwnerEditUnknown)
	}
	return nil
}

// inspectionPendingCode classifies an incomplete authenticated inspection by
// the reviewed reason token its forced command reported, if any. A refused
// local catalog transfer has its own typed reason (the secondary's owner can
// act on it); any other reviewed reason rides as the detail of
// dns_peer_inspection_unknown; no reason keeps the plain code.
func inspectionPendingCode(reason string) string {
	if reason == transport.DNSPeerInspectorReasonCatalogTransferRefused {
		return transport.DNSPeerPendingCatalogTransferRefused
	}
	return transport.DNSPeerPendingInspectionUnknownWithDetail(reason)
}

func dnsZoneV3PendingLedgerCode(code string) string {
	if transport.ValidDNSPeerPendingCode(code) {
		return code
	}
	return "dns_zone_v3_propagation_pending"
}
