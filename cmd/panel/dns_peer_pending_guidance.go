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
		return "The DNS change is saved on this server, but the secondary has not been shown to have removed the zone: no inspection access is set up between the two servers. The administrators of both servers set it up once with the dns-peer-enroll owner tool: primary-prepare here, secondary-install and secondary-host-key on the secondary, then primary-activate here. After that, retry the same publication; it continues from where it stopped. Nothing retries by itself."
	case transport.DNSPeerPendingEnrollmentChanged:
		return "The DNS change is saved, but peer inspection enrollment could not be trusted. This server's administrator must check its peer identity and credential enrollment, then retry this same publication."
	case transport.DNSPeerPendingInspectionUnknown:
		return "The DNS change is saved, but authenticated secondary inspection could not complete. This server's administrator and the secondary owner must check the pinned SSH channel and inspector access, then retry this same publication."
	case transport.DNSPeerPendingNativeUnknown:
		return "The DNS change is saved, but the secondary's native secondary DNS state did not prove the deleted zone absent. The secondary owner must inspect its loaded zones and transfer state, then retry this same publication."
	case transport.DNSPeerPendingJournalUnknown:
		return "The DNS change is saved, but its private peer challenge cannot be reconciled. This server's administrator must review the retained challenge and exact operation record; only then retry this same publication."
	case transport.DNSPeerPendingOwnerEditUnknown:
		return "The DNS change is saved, but local or peer evidence changed during verification. This server's administrator must reconcile the accepted operation with native DNS configuration, then retry this same publication."
	default:
		return "The DNS change is saved, but paired deletion is unverified. The server administrator must inspect the exact DNS operation and secondary state, then retry this same publication."
	}
}

// dnsPeerPendingAPIError recognizes only a reviewed code carried by the
// verified pending operation. Unknown data must use the generic fallback.
func dnsPeerPendingAPIError(err error) (apiErrorBody, bool) {
	var pending *dnsZoneV3PropagationPendingError
	if !errors.As(err, &pending) || !pending.Exact || !transport.ValidDNSPeerPendingCode(pending.Code) {
		return apiErrorBody{}, false
	}
	return apiErrorBody{
		Code: errCodeDNSPublicationFailed, Reason: pending.Code,
		Error: dnsPeerPendingEnglish(pending.Code),
	}, true
}
