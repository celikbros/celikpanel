export const UPDATE_MARKER_KEY = 'celikpanel.system-update-operation.v1';
export const recoveryPhases = ['accepted', 'running', 'recovering', 'recovered', 'succeeded', 'failed', 'recovery_required'] as const;
export const recoveryReasons = ['operation_accepted', 'update_running', 'update_failed', 'recovery_running', 'recovery_failed', 'recovery_incomplete', 'update_verified', 'rollback_verified', 'observation_unavailable'] as const;
export const recoveryWaitReasons = ['initializing', 'starting', 'stopping'] as const;
/** Optional typed cause of the update's own failure. Unknown values are ignored (generic text). */
export const recoveryFailureCodes = ['candidate_panel_startup_check_failed', 'panel_start_unverified', 'recovery_runtime_preflight_failed', 'update_preflight_refused'] as const;
/** Causes written only for a request that stopped before changing the installed version. */
export const preflightStopCodes = ['recovery_runtime_preflight_failed', 'update_preflight_refused'] as const;
export type RecoveryFailureCode = typeof recoveryFailureCodes[number];
/** A cause that can precede a retry or a pause: a refused update check never does. */
export type PausedCauseCode = Exclude<RecoveryFailureCode, 'update_preflight_refused'>;
export type RecoveryReason = typeof recoveryReasons[number];
export type RecoveryFailureReason = 'update_failed' | 'recovery_failed' | 'recovery_incomplete';
export type RecoveryObservation = {
    request_id: string; observation: 'known' | 'unavailable'; panel_state: 'starting' | 'ready';
    phase?: typeof recoveryPhases[number]; terminal_proof: 'none' | 'update_verified' | 'rollback_verified';
    waiting_for?: typeof recoveryWaitReasons[number];
    /**
     * paused_retry_limit: all automatic attempts used; retry_scheduled: the timer admits another one;
     * pause_pending: the last admitted attempt failed and the next timer run records the pause.
     */
    automatic_recovery?: 'paused_retry_limit' | 'retry_scheduled' | 'pause_pending';
    /** At the pause: whether Certbot renewal was on before the update paused it. Absent: not recorded. */
    renewal_before_update?: 'on' | 'off';
    failure_code?: RecoveryFailureCode;
    /** The update's first typed cause, kept between automatic attempts and at the pause. */
    first_failure_code?: PausedCauseCode;
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

/**
 * Presentation hint only: this browser's own note that the saved operation ended
 * as a verified update. It never asserts a server outcome. It only keeps a
 * finished update from being shown as if it explained an unrelated access check.
 */
export function savedRecoveryFinished(raw: string | null): boolean {
    if (!raw || raw.length > 8192) return false;
    try {
        const value = JSON.parse(raw);
        return value?.state_version === 1 && value.phase === 'terminal' && value.outcome === 'succeeded';
    } catch { return false; }
}

/**
 * Presentation hint only: this browser started an update and has not recorded
 * its end (the tracker's own record is still `active`). It never asserts a
 * server outcome. It only lets a Panel that stopped answering be explained by
 * the restart an update makes, instead of by the license (2026-10-10).
 * Yalnizca sunum ipucu: bu tarayici bir guncelleme baslatti ve sonunu kaydetmedi.
 */
export function savedUpdateUnfinished(raw: string | null): boolean {
    if (!raw || raw.length > 8192) return false;
    try {
        const value = JSON.parse(raw);
        return value?.state_version === 1 && value.phase === 'active' && savedRecoveryRequestId(raw) !== null;
    } catch { return false; }
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
    const waiting = value.phase === 'recovering' && recoveryWaitReasons.includes(value.waiting_for as never) ? value.waiting_for as RecoveryObservation['waiting_for'] : undefined;
    const automatic = value.phase === 'recovery_required' && value.automatic_recovery === 'paused_retry_limit' ? 'paused_retry_limit'
        : value.phase === 'recovery_required' && value.reason === 'recovery_failed' && value.automatic_recovery === 'retry_scheduled' ? 'retry_scheduled'
        : value.phase === 'recovery_required' && value.reason === 'recovery_failed' && value.automatic_recovery === 'pause_pending' ? 'pause_pending' : undefined;
    const renewal = automatic === 'paused_retry_limit' && (value.renewal_before_update === 'on' || value.renewal_before_update === 'off') ? value.renewal_before_update as 'on' | 'off' : undefined;
    // Only meaningful while the update's own failure is the latest recorded one.
    const failureCode = value.previous_failure === 'update_failed' && recoveryFailureCodes.includes(value.failure_code as never) ? value.failure_code as RecoveryFailureCode : undefined;
    // Only meaningful between automatic attempts or at the pause; unknown values are ignored.
    const betweenAttempts = automatic || (value.phase === 'recovering' && value.previous_failure === 'recovery_failed');
    const firstFailureCode = betweenAttempts && recoveryFailureCodes.includes(value.first_failure_code as never) && value.first_failure_code !== 'update_preflight_refused' ? value.first_failure_code as PausedCauseCode : undefined;
    return { ...base, observation: 'known', waiting_for: waiting, automatic_recovery: automatic, renewal_before_update: renewal, failure_code: failureCode, first_failure_code: firstFailureCode, phase: value.phase as RecoveryObservation['phase'], terminal_proof: proof,
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
        if (failure && !next.previous_failure) next = { ...next, previous_failure: failure, failure_code: failure === 'update_failed' ? previous.failure_code : undefined };
    }
    return { record: next, unavailable: false };
}

/**
 * Reviewed guidance for a typed update cause, matching the owner CLI. The start
 * check fails before completion is marked, so that failure is returned to the
 * previous release; a panel that did not come up after the switch is completed
 * forward only. Waits and paused recovery keep their own guidance.
 */
export type RecoveryFailureGuidanceKey = 'recovery.failure.candidate_panel_startup_check_failed.returning'
    | 'recovery.failure.candidate_panel_startup_check_failed.recovered' | 'recovery.failure.panel_start_unverified.pending'
    | 'recovery.failure.recovery_runtime_preflight_failed.stopped';
export function recoveryFailureGuidanceKey(observation: RecoveryObservation): RecoveryFailureGuidanceKey | undefined {
    if (observation.observation !== 'known' || observation.waiting_for || observation.automatic_recovery) return undefined;
    // The next automatic attempt runs after an earlier one failed: keep the
    // update's first typed cause as the reference point (never the preflight).
    if (observation.phase === 'recovering' && observation.terminal_proof === 'none' && observation.previous_failure === 'recovery_failed') {
        return retryingCauseKey(observation.first_failure_code);
    }
    if (observation.previous_failure !== 'update_failed') return undefined;
    const pending = observation.terminal_proof === 'none' && (observation.phase === 'failed' || observation.phase === 'recovering');
    // Written only when the update stopped before changing anything: nothing follows.
    if (preflightStopCodes.includes(observation.failure_code as never)) {
        return observation.phase === 'failed' && observation.terminal_proof === 'none' ? 'recovery.failure.recovery_runtime_preflight_failed.stopped' : undefined;
    }
    if (observation.failure_code === 'candidate_panel_startup_check_failed') {
        if (observation.phase === 'recovered' && observation.terminal_proof === 'rollback_verified') return 'recovery.failure.candidate_panel_startup_check_failed.recovered';
        if (pending) return 'recovery.failure.candidate_panel_startup_check_failed.returning';
    }
    if (observation.failure_code === 'panel_start_unverified' && pending) return 'recovery.failure.panel_start_unverified.pending';
    return undefined;
}

/** The pending guidance for the update's first typed cause while automatic attempts continue. */
export function retryingCauseKey(code: RecoveryFailureCode | undefined): RecoveryFailureGuidanceKey | undefined {
    if (code === 'candidate_panel_startup_check_failed') return 'recovery.failure.candidate_panel_startup_check_failed.returning';
    if (code === 'panel_start_unverified') return 'recovery.failure.panel_start_unverified.pending';
    return undefined;
}

/** A failed record whose typed cause says the update stopped in a read-only check before changing anything. */
export function isPreflightStop(observation: RecoveryObservation | null): boolean {
    return observation?.observation === 'known' && observation.phase === 'failed' && observation.terminal_proof === 'none'
        && observation.previous_failure === 'update_failed' && preflightStopCodes.includes(observation.failure_code as never);
}
