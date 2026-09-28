// A 202 deletion response is a durable pending operation, never a success receipt.
const reviewedDNSPeerReasons = new Set([
    'dns_peer_enrollment_required',
    'dns_peer_enrollment_changed',
    'dns_peer_inspection_unknown',
    'dns_peer_native_unknown',
    'dns_peer_journal_unknown',
    'dns_peer_owner_edit_unknown',
]);

export async function readDomainDeletionPending(response: Response): Promise<string> {
    if (response.status !== 202) return '';
    try {
        const body: unknown = await response.json();
        if (!body || typeof body !== 'object' || Array.isArray(body)) return '';
        const pending = body as Record<string, unknown>;
        if (pending.status !== 'deletion_pending' || pending.stage !== 'dns_cleanup') return '';
        return typeof pending.reason === 'string' && reviewedDNSPeerReasons.has(pending.reason)
            ? pending.reason
            : '';
    } catch {
        return '';
    }
}


export async function readDomainDeletionOutcome(response: Response): Promise<{
    state: 'pending' | 'succeeded' | 'error';
    reason: string;
}> {
    if (response.status === 202) {
        return { state: 'pending', reason: await readDomainDeletionPending(response) };
    }
    if (response.status === 200 || response.status === 204) {
        return { state: 'succeeded', reason: '' };
    }
    return { state: 'error', reason: '' };
}


// null means this GET did not verify an active deletion marker. An empty reason
// means the marker exists but the exact DNS/Agent reason remains unknown.
export async function readSavedDomainDeletionStatus(response: Response): Promise<string | null> {
    if (response.status !== 200) return null;
    try {
        const body: unknown = await response.json();
        if (!body || typeof body !== 'object' || Array.isArray(body)) return null;
        const pending = body as Record<string, unknown>;
        if (pending.status === 'unknown' && pending.stage === 'unknown') return '';
        if (pending.status !== 'deletion_pending') return null;
        if (pending.stage !== 'dns_cleanup') return '';
        return typeof pending.reason === 'string' && reviewedDNSPeerReasons.has(pending.reason)
            ? pending.reason
            : '';
    } catch {
        return null;
    }
}
