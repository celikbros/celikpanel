package main

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/transport"
)

// These fallbacks are used by older clients without the translated reason key.
// Each sentence identifies the current uncertainty, actor, action and how the
// same accepted publication can resume. No peer output or credential is shown.
func dnsPeerPendingEnglish(code string) string {
	switch code {
	case transport.DNSPeerPendingEnrollmentRequired:
		return "The DNS change is saved on this server, but the secondary has not been shown to have removed the zone: no inspection access is set up between the two servers. The administrators of both servers set it up once with the dns-peer-enroll owner tool (add --engine pdns to each command when the secondary runs PowerDNS): primary-prepare here, secondary-install and secondary-host-key on the secondary, then primary-activate here. After that, retry the same publication; it continues from where it stopped. Nothing retries by itself."
	case transport.DNSPeerPendingEnrollmentChanged:
		return "The DNS change is saved, but peer inspection enrollment could not be trusted. This server's administrator must check its peer identity and credential enrollment, then retry this same publication."
	case transport.DNSPeerPendingInspectionUnknown:
		return "The DNS change is saved, but authenticated secondary inspection could not complete. This server's administrator and the secondary owner must check the pinned SSH channel and inspector access, then retry this same publication."
	case transport.DNSPeerPendingNativeUnknown:
		return "The DNS change is saved, but the secondary's native secondary DNS state did not prove the deleted zone absent. The secondary owner must inspect its loaded zones and transfer state, then retry this same publication."
	case transport.DNSPeerPendingJournalUnknown:
		return "The DNS change is saved, but its private peer challenge cannot be reconciled. This server's administrator must review the retained challenge and exact operation record; only then retry this same publication."
	case transport.DNSPeerPendingOwnerEditUnknown:
		return "The DNS change is saved, but local or peer evidence changed during verification. This server's administrator must reconcile the accepted operation with native DNS configuration: compare the catalog zone and zone serials on both servers, then retry this same publication."
	case transport.DNSPeerPendingCatalogTransferRefused:
		return "The DNS change is saved, but the secondary has not been shown to have removed the zone: the secondary's named refused the inspector's local transfer of the catalog zone (allow-transfer). The secondary's owner allows that transfer from loopback on the secondary. A secondary whose DNS CelikPanel set up with this release already allows it; on one set up by an earlier release it arrives when CelikPanel next writes that server's DNS configuration. On a secondary you run without CelikPanel, add 127.0.0.1 and ::1 to the catalog zone's allow-transfer in named's configuration and reload named. Then retry the same publication; it continues from where it stopped. Nothing retries by itself."
	case transport.DNSPeerPendingProofInternal:
		return "The DNS change is saved on this server, but the deletion is not verified yet: this server could not run its own check of the secondary. No change by either server's owner was found, and nothing on either server needs to be undone. The server owner checks the CelikPanel Agent log on this server with sudo journalctl -u celikpanel-agent | grep peer and reports those lines to CelikPanel. Retrying does not help until the CelikPanel Agent is updated with a fix; after that update, use “Retry this deletion” to retry the same publication; it continues from where it stopped. Until then the deletion stays pending and DNS answers are unaffected. Nothing retries by itself."
	default:
		return "The DNS change is saved, but paired deletion is unverified. The server administrator must inspect the exact DNS operation and secondary state, then retry this same publication."
	}
}

// dnsPeerInspectorDetailEnglish is the product sentence for one reviewed
// reason the secondary's inspector reported. It is shown after the reason's
// own text; it never repeats inspector, SSH or native command output.
func dnsPeerInspectorDetailEnglish(detail string) string {
	switch detail {
	case transport.DNSPeerInspectorReasonPolicy:
		return "The secondary's inspector reported that its owner policy is missing or does not match this request; the secondary's owner checks it with dns-peer-enroll secondary-status."
	case transport.DNSPeerInspectorReasonNamedUnavailable:
		return "The secondary's inspector reported that named is not running under its standard service and configuration, or that its configuration could not be read."
	case transport.DNSPeerInspectorReasonListenersUnverified:
		return "The secondary's inspector reported that named's DNS or control (rndc) listeners do not match the enrolled address and loopback."
	case transport.DNSPeerInspectorReasonCatalogUnverified:
		return "The secondary's inspector reported that named does not hold the primary's catalog zone as a subscribed secondary zone."
	case transport.DNSPeerInspectorReasonCatalogTransferFailed:
		return "The secondary's inspector reported that the local transfer of the catalog zone did not complete."
	case transport.DNSPeerInspectorReasonCatalogMalformed:
		return "The secondary's inspector reported that the transferred catalog zone does not have the expected content."
	case transport.DNSPeerInspectorReasonObservationExpired:
		return "The secondary's inspector reported that the request expired before it finished; check that both servers' clocks are correct."
	case transport.DNSPeerInspectorReasonConfigUnreviewed:
		return "The secondary's inspector reported that PowerDNS is not running with a configuration it recognises (the panel's own or the documented panel-free one)."
	default:
		return ""
	}
}

// dnsPeerOwnerEditDetailEnglish names, for dns_peer_owner_edit_unknown, the
// one check of the Agent's proof that found different evidence. It is shown
// after the reason's own text and never carries an observed value; the
// Agent log has the recorded and observed values.
func dnsPeerOwnerEditDetailEnglish(check string) string {
	switch check {
	case transport.DNSPeerOwnerEditCheckOperationAttempt:
		return "What differed: this server's record of which attempt is running this deletion changed during the check."
	case transport.DNSPeerOwnerEditCheckEngineState:
		return "What differed: this server's saved DNS engine state changed during the check."
	case transport.DNSPeerOwnerEditCheckActiveEngine:
		return "What differed: which DNS service runs on this server changed during the check; only the panel's DNS server may be running."
	case transport.DNSPeerOwnerEditCheckNativeBinding:
		return "What differed: the DNS server process on this server, its database or its configuration files changed during the check (for example the service was restarted or its configuration was edited)."
	case transport.DNSPeerOwnerEditCheckDeletionReceipt:
		return "What differed: this server's saved record of this deletion no longer matches the accepted change."
	case transport.DNSPeerOwnerEditCheckProducerCatalog:
		return "What differed: this server's catalog zone changed in a way the DNS server does not make by itself (its member zones, their serials or the catalog's identity)."
	case transport.DNSPeerOwnerEditCheckCatalogProbe:
		return "What differed: after the secondary's inspection, the catalog zone served by this server or by the secondary no longer matched this server's saved catalog."
	case transport.DNSPeerOwnerEditCheckAuthority:
		return "What differed: after the secondary's inspection, the catalog zone answered for a different pair of servers or a different catalog."
	case transport.DNSPeerOwnerEditCheckTransferObserved:
		return "What differed: after the secondary's inspection, the secondary did not refuse a transfer of the deleted zone, or that check did not complete."
	case transport.DNSPeerOwnerEditCheckZoneAnswered:
		return "What differed: after the secondary's inspection, the secondary's answer for the deleted zone was no longer the plain refusal seen before, or the query did not complete."
	default:
		return ""
	}
}

// dnsPeerPendingDetailEnglish selects the detail sentence by the reason the
// detail refines: an inspector reason or an owner-edit check.
func dnsPeerPendingDetailEnglish(reason, detail string) string {
	switch reason {
	case transport.DNSPeerPendingInspectionUnknown:
		return dnsPeerInspectorDetailEnglish(detail)
	case transport.DNSPeerPendingOwnerEditUnknown:
		return dnsPeerOwnerEditDetailEnglish(detail)
	default:
		return ""
	}
}

// dnsPeerPendingGuidance splits a verified pending code into the reviewed
// reason, its optional reviewed detail (inspector reason or owner-edit check)
// and the English fallback.
func dnsPeerPendingGuidance(code string) (reason, detail, message string, ok bool) {
	reason, detail, ok = transport.SplitDNSPeerPendingCode(code)
	if !ok {
		return "", "", "", false
	}
	message = dnsPeerPendingEnglish(reason)
	if sentence := dnsPeerPendingDetailEnglish(reason, detail); sentence != "" {
		message += " " + sentence
	} else {
		detail = ""
	}
	return reason, detail, message, true
}

// dnsPeerPendingAPIError recognizes only a reviewed code carried by the
// verified pending operation. Unknown data must use the generic fallback.
func dnsPeerPendingAPIError(err error) (apiErrorBody, bool) {
	var pending *dnsZoneV3PropagationPendingError
	if !errors.As(err, &pending) || !pending.Exact {
		return apiErrorBody{}, false
	}
	reason, detail, message, ok := dnsPeerPendingGuidance(pending.Code)
	if !ok {
		return apiErrorBody{}, false
	}
	return apiErrorBody{
		Code: errCodeDNSPublicationFailed, Reason: reason, Detail: detail,
		Error: message,
	}, true
}

// dnsPublicationFailureReasonError is a verified local publication failure
// with a reviewed cause from the Agent (transport.ValidDNSPublicationFailure-
// Reason). The change was not applied (or was rolled back); retrying the same
// accepted publication continues it. A lost RPC response loses the reason and
// falls back to the generic failure text.
type dnsPublicationFailureReasonError struct {
	Reason string
}

func (e *dnsPublicationFailureReasonError) Error() string {
	return "agent reported a verified DNS publication failure: " + e.Reason
}

// dnsPublicationFailureEnglish is the fallback for clients without the
// translated reason key. Reason, actor, action and resumption come first; no
// key material or tool output is shown.
func dnsPublicationFailureEnglish(reason string) string {
	switch reason {
	case transport.DNSPublicationFailureBINDRNDCUnavailable:
		return "The DNS change is saved, but this server could not prove the zone was removed: named cannot be asked about zone state because no usable rndc key is present. The server owner fixes this on this server: run sudo rndc-confgen -a, then sudo systemctl restart named. The restart briefly interrupts DNS answers from this server; on Debian the unit is also named and the key is /etc/bind/rndc.key. Then use “Retry this deletion” to retry the same publication. Until then the deletion stays pending, nothing else is changed and DNS answers are unaffected."
	default:
		return "The DNS change is saved, but it could not be published or verified. The server administrator must inspect the exact DNS operation, then retry this same publication."
	}
}

// dnsPublicationGuidanceAPIError recognizes a reviewed peer pending code or a
// reviewed local failure reason; anything else keeps the caller's generic text.
func dnsPublicationGuidanceAPIError(err error) (apiErrorBody, bool) {
	if guidance, ok := dnsPeerPendingAPIError(err); ok {
		return guidance, true
	}
	var failure *dnsPublicationFailureReasonError
	if !errors.As(err, &failure) || !transport.ValidDNSPublicationFailureReason(failure.Reason) {
		return apiErrorBody{}, false
	}
	return apiErrorBody{
		Code: errCodeDNSPublicationFailed, Reason: failure.Reason,
		Error: dnsPublicationFailureEnglish(failure.Reason),
	}, true
}
