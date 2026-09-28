package servicemutationledger

import "github.com/alicelik/celikpanel/internal/mutationpayload"

// DNSZoneV3PeerOperation describes what a verified ledger says about one
// captured attempt. It is an observation, not authority to start or resume a
// mutation. In particular, a pending attempt requires the ordinary owner
// resume transition before a new peer proof can be requested.
type DNSZoneV3PeerOperation string

const (
	DNSZoneV3PeerOperationMismatch             DNSZoneV3PeerOperation = "mismatch"
	DNSZoneV3PeerOperationActiveApplied        DNSZoneV3PeerOperation = "active-applied"
	DNSZoneV3PeerOperationActiveRecovering     DNSZoneV3PeerOperation = "active-recovering"
	DNSZoneV3PeerOperationPendingRetryRequired DNSZoneV3PeerOperation = "pending-retry-required"
	DNSZoneV3PeerOperationHistoricalPublished  DNSZoneV3PeerOperation = "historical-published"
)

// ClassifyDNSZoneV3PeerOperation matches a peer-proof attempt to the exact
// durable V3 job. Callers must supply the attempt captured before issuing a
// challenge. No clock, host, lock, network or mutable state is consulted here.
// Active results describe only the currently selected committed job. A caller
// must still perform its own live admission and freshness checks.
func ClassifyDNSZoneV3PeerOperation(
	ledger *Ledger, requestID, ownerID, domain, qualifier string, capturedAttempt int,
) DNSZoneV3PeerOperation {
	if capturedAttempt < 1 || !ValidIdentity(requestID) || !ValidIdentity(ownerID) ||
		!ServiceMutationCanonicalFQDN(domain) ||
		!mutationpayload.ValidDNSZoneSyncV3Qualifier(qualifier) ||
		Validate(ledger) != nil {
		return DNSZoneV3PeerOperationMismatch
	}
	job := ledger.Jobs[requestID]
	if job == nil || job.RequestID != requestID || job.OwnerID != ownerID ||
		job.Kind != "dns_zone_sync" || job.Target != domain ||
		job.PackageName != qualifier || job.Attempt != capturedAttempt {
		return DNSZoneV3PeerOperationMismatch
	}
	state, phaseRequestID, phaseDomain, phaseQualifier, err := ParseDNSZoneSyncV3Phase(job.Phase)
	if err != nil || phaseRequestID != requestID || phaseDomain != domain ||
		phaseQualifier != qualifier {
		return DNSZoneV3PeerOperationMismatch
	}
	switch {
	case state == DnsZoneSyncV3Applied && ledger.ActiveRequestID == requestID &&
		(job.Status == StatusRunning || job.Status == StatusCancelling):
		return DNSZoneV3PeerOperationActiveApplied
	case state == DnsZoneSyncV3Recovering && ledger.ActiveRequestID == requestID &&
		job.Status == StatusRunning:
		return DNSZoneV3PeerOperationActiveRecovering
	case state == DnsZoneSyncV3PropagationPending &&
		job.Status == StatusPending && ledger.ActiveRequestID != requestID:
		return DNSZoneV3PeerOperationPendingRetryRequired
	case state == DnsZoneSyncV3Published &&
		job.Status == StatusSucceeded && ledger.ActiveRequestID != requestID:
		return DNSZoneV3PeerOperationHistoricalPublished
	default:
		return DNSZoneV3PeerOperationMismatch
	}
}
