// A 202 deletion response is a durable pending operation, never a success receipt.
const reviewedDNSPeerReasons = new Set([
    'dns_peer_enrollment_required',
    'dns_peer_enrollment_changed',
    'dns_peer_inspection_unknown',
    'dns_peer_native_unknown',
    'dns_peer_journal_unknown',
    'dns_peer_owner_edit_unknown',
    // The secondary's named refused the inspector's local catalog transfer.
    'dns_peer_catalog_transfer_refused',
    // The Agent could not run its own proof of the secondary; no owner
    // change was found. A retry waits for a fixed Agent.
    'dns_peer_proof_internal',
    // A step of the proof of the secondary ran out of its time before the
    // peer's answer was accepted; nothing changed. Retrying now is the action.
    'dns_peer_proof_timeout',
    // Local, not peer: this server's named could not be asked about zone
    // state (no usable rndc key). Same retry action, same pending deletion.
    'bind_rndc_unavailable',
]);

// What the secondary's inspector reported for an incomplete inspection. Only
// these tokens are shown, each through its own translated sentence; the server
// never forwards inspector or SSH output.
const reviewedInspectorDetails = new Set([
    'inspector_policy',
    'named_unavailable',
    'listeners_unverified',
    'catalog_unverified',
    'catalog_transfer_failed',
    'catalog_malformed',
    'observation_expired',
    // PowerDNS only: its configuration matches neither reviewed shape.
    'config_unreviewed',
]);

// Which check of this server's proof found different evidence when the
// deletion stays pending as dns_peer_owner_edit_unknown. Only these tokens are
// shown, each through its own translated sentence; no observed value is sent.
const reviewedOwnerEditChecks = new Set([
    'operation_attempt',
    'engine_state',
    'active_engine',
    'native_binding',
    'deletion_receipt',
    'producer_catalog',
    'catalog_probe',
    'authority',
    'transfer_observed',
    'zone_answered',
]);

// The reviewed detail tokens of each reason that carries one.
const reviewedDetailsByReason: Record<string, Set<string>> = {
    dns_peer_inspection_unknown: reviewedInspectorDetails,
    dns_peer_owner_edit_unknown: reviewedOwnerEditChecks,
};

// The translation key of a reviewed detail sentence, or null.
export function domainDeletionDetailKey(reason: string, detail: string): string | null {
    if (!detail) return null;
    if (reason === 'dns_peer_inspection_unknown' && reviewedInspectorDetails.has(detail)) {
        return `domains.peerInspectorDetail.${detail}`;
    }
    if (reason === 'dns_peer_owner_edit_unknown' && reviewedOwnerEditChecks.has(detail)) {
        return `domains.peerOwnerEditDetail.${detail}`;
    }
    return null;
}

// A verified failure of a non-DNS deletion stage on this server, by stage.
// The deletion stays pending and the same retry action repeats it.
const reviewedStageFailures: Record<string, string> = {
    mail_runtime_cleanup: 'mail_runtime_cleanup_failed',
};

function reviewedStageFailure(pending: Record<string, unknown>): string {
    return typeof pending.stage === 'string' && Object.prototype.hasOwnProperty.call(reviewedStageFailures, pending.stage) &&
        pending.reason === reviewedStageFailures[pending.stage]
        ? pending.reason
        : '';
}

// The translation key for a reviewed reason, or null for the generic text.
export function domainDeletionReasonKey(reason: string): string | null {
    if (!reason) return null;
    if (Object.values(reviewedStageFailures).includes(reason)) return `err.DOMAIN_DELETION_FAILED.${reason}`;
    return `err.DNS_PUBLICATION_FAILED.${reason}`;
}

function reviewedDetail(pending: Record<string, unknown>, reason: string): string {
    const allowed = Object.prototype.hasOwnProperty.call(reviewedDetailsByReason, reason)
        ? reviewedDetailsByReason[reason]
        : undefined;
    return allowed !== undefined && typeof pending.detail === 'string' && allowed.has(pending.detail)
        ? pending.detail
        : '';
}

async function readPendingBody(response: Response): Promise<{ reason: string; detail: string }> {
    if (response.status !== 202) return { reason: '', detail: '' };
    try {
        const body: unknown = await response.json();
        if (!body || typeof body !== 'object' || Array.isArray(body)) return { reason: '', detail: '' };
        const pending = body as Record<string, unknown>;
        if (pending.status === 'deletion_pending' && pending.stage !== 'dns_cleanup') {
            return { reason: reviewedStageFailure(pending), detail: '' };
        }
        if (pending.status !== 'deletion_pending' || pending.stage !== 'dns_cleanup') return { reason: '', detail: '' };
        const reason = typeof pending.reason === 'string' && reviewedDNSPeerReasons.has(pending.reason)
            ? pending.reason
            : '';
        return { reason, detail: reviewedDetail(pending, reason) };
    } catch {
        return { reason: '', detail: '' };
    }
}

export async function readDomainDeletionPending(response: Response): Promise<string> {
    return (await readPendingBody(response)).reason;
}


export async function readDomainDeletionOutcome(response: Response): Promise<{
    state: 'pending' | 'succeeded' | 'error';
    reason: string;
    detail?: string;
}> {
    if (response.status === 202) {
        const { reason, detail } = await readPendingBody(response);
        return detail ? { state: 'pending', reason, detail } : { state: 'pending', reason };
    }
    if (response.status === 200 || response.status === 204) {
        return { state: 'succeeded', reason: '' };
    }
    return { state: 'error', reason: '' };
}


// null means this GET did not verify an active deletion marker. An empty reason
// means the marker exists but the exact DNS/Agent reason remains unknown.
export async function readSavedDomainDeletionState(
    response: Response,
): Promise<{ reason: string; detail: string } | null> {
    if (response.status !== 200) return null;
    try {
        const body: unknown = await response.json();
        if (!body || typeof body !== 'object' || Array.isArray(body)) return null;
        const pending = body as Record<string, unknown>;
        if (pending.status === 'unknown' && pending.stage === 'unknown') return { reason: '', detail: '' };
        // A recorded, verified stage failure: the marker exists and the same
        // deletion can be retried; an unreviewed failure shows the generic text.
        if (pending.status === 'failed') return { reason: reviewedStageFailure(pending), detail: '' };
        if (pending.status !== 'deletion_pending') return null;
        if (pending.stage !== 'dns_cleanup') return { reason: '', detail: '' };
        const reason = typeof pending.reason === 'string' && reviewedDNSPeerReasons.has(pending.reason)
            ? pending.reason
            : '';
        return { reason, detail: reviewedDetail(pending, reason) };
    } catch {
        return null;
    }
}

export async function readSavedDomainDeletionStatus(response: Response): Promise<string | null> {
    const state = await readSavedDomainDeletionState(response);
    return state === null ? null : state.reason;
}
