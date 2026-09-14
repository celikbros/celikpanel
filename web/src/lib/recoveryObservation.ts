export const UPDATE_MARKER_KEY = 'celikpanel.system-update-operation.v1';
export const recoveryPhases = ['accepted', 'running', 'recovering', 'recovered', 'succeeded', 'failed', 'recovery_required'] as const;
export const recoveryReasons = ['operation_accepted', 'update_running', 'update_failed', 'recovery_running', 'recovery_failed', 'recovery_incomplete', 'update_verified', 'rollback_verified', 'observation_unavailable'] as const;
export type RecoveryReason = typeof recoveryReasons[number];
export type RecoveryFailureReason = 'update_failed' | 'recovery_failed' | 'recovery_incomplete';
export type RecoveryObservation = {
    request_id: string; observation: 'known' | 'unavailable'; panel_state: 'starting' | 'ready';
    phase?: typeof recoveryPhases[number]; terminal_proof: 'none' | 'update_verified' | 'rollback_verified';
    reason: RecoveryReason; observed_at?: string; previous_failure?: RecoveryFailureReason;
};

/** Browser storage supplies an ID hint only. Its outcome and message are never trusted. */
export function savedRecoveryRequestId(raw: string | null): string | null {
    if (!raw || raw.length > 8192) return null;
    try {
        const value = JSON.parse(raw);
        const marker = value?.state_version === 1 && ['active', 'terminal', 'reload'].includes(value.phase) ? value.marker : value;
        return marker?.marker_version === 1 && typeof marker.request_id === 'string' && /^[a-f0-9]{32}$/.test(marker.request_id) ? marker.request_id : null;
    } catch { return null; }
}

export function parseRecoveryObservation(raw: unknown, requestId: string): RecoveryObservation {
    if (!/^[a-f0-9]{32}$/.test(requestId) || !raw || typeof raw !== 'object') throw new Error('invalid recovery observation');
    const value = raw as Record<string, unknown>;
    if (value.schema !== 'celikpanel-recovery-status/v1' || value.request_id !== requestId
        || !['starting', 'ready'].includes(String(value.panel_state))) throw new Error('wrong recovery identity');
    const base = { request_id: requestId, panel_state: value.panel_state as 'starting' | 'ready' };
    if (value.observation === 'unavailable') return { ...base, observation: 'unavailable', terminal_proof: 'none', reason: 'observation_unavailable' };
    if (value.observation !== 'known' || !recoveryPhases.includes(value.phase as never)
        || !recoveryReasons.includes(value.reason as never) || typeof value.observed_at !== 'string'
        || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(value.observed_at)
        || !Number.isFinite(Date.parse(value.observed_at))
        || new Date(value.observed_at).toISOString().replace('.000Z', 'Z') !== value.observed_at) throw new Error('invalid recovery observation');
    const phaseReason: Record<string, string[]> = { accepted: ['operation_accepted'], running: ['update_running'], recovering: ['recovery_running'],
        failed: ['update_failed'], recovery_required: ['recovery_failed', 'recovery_incomplete'], succeeded: ['update_verified'], recovered: ['rollback_verified'] };
    if (!phaseReason[String(value.phase)]?.includes(String(value.reason))) throw new Error('inconsistent recovery reason');
    const proof = value.phase === 'succeeded' ? 'update_verified' : value.phase === 'recovered' ? 'rollback_verified' : 'none';
    if (value.terminal_proof !== proof) throw new Error('unproved recovery outcome');
    if (value.previous_failure !== undefined && !['update_failed', 'recovery_failed', 'recovery_incomplete'].includes(String(value.previous_failure))) throw new Error('invalid previous failure');
    return { ...base, observation: 'known', phase: value.phase as RecoveryObservation['phase'], terminal_proof: proof,
        reason: value.reason as RecoveryReason, observed_at: value.observed_at, previous_failure: value.previous_failure as RecoveryFailureReason | undefined };
}

/** Missing/stale reads never erase a verified terminal proof or a known failure. */
export function reconcileRecoveryObservation(previous: RecoveryObservation | null, next: RecoveryObservation): { record: RecoveryObservation | null; unavailable: boolean } {
    if (previous?.request_id !== next.request_id) previous = null;
    if (next.observation === 'unavailable') return { record: previous, unavailable: true };
    if (previous) {
        const stale = Date.parse(next.observed_at || '') < Date.parse(previous.observed_at || '');
        if (previous.terminal_proof !== 'none') return { record: previous, unavailable: stale || next.terminal_proof !== previous.terminal_proof || next.phase !== previous.phase };
        if (stale) return { record: previous, unavailable: true };
        const failure = previous.previous_failure || (['update_failed', 'recovery_failed', 'recovery_incomplete'].includes(previous.reason) ? previous.reason as RecoveryFailureReason : undefined);
        if (failure && !next.previous_failure) next = { ...next, previous_failure: failure };
    }
    return { record: next, unavailable: false };
}
