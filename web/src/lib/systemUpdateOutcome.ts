import { isPreflightStop, preflightStopCodes, recoveryFailureCodes, recoveryFailureGuidanceKey, retryingCauseKey, type PausedCauseCode, type RecoveryFailureCode, type RecoveryObservation } from './recoveryObservation';
import { systemUpdatePreflightStop, withoutInternalTokens } from './systemUpdateFailure';

/** Preflight steps with reviewed owner wording; any other step uses the generic line. */
export const preflightSteps = ['panel_database_check', 'agent_ledger_check', 'owner_metadata_probe', 'runtime_selection',
    'runtime_revalidation', 'release_boundary', 'material_support', 'database_support', 'database_metadata',
    'idle_probe', 'agent_idle', 'bind_compatibility', 'application_compatibility', 'bootstrap_state'] as const;
type PreflightStep = typeof preflightSteps[number];
/** Reason classes of a refused update check whose own wording replaces the server's line. */
export const preflightReasonClasses = ['concurrent_write', 'operation_active'] as const;
type PreflightReasonClass = typeof preflightReasonClasses[number];

/**
 * Owner guidance for a finished-with-failure update notice. The primary text
 * comes from typed data (the exact request's recovery observation and its
 * optional typed cause), never from the worker's raw summary. The summary stays
 * available as a secondary "server reported" line only.
 *
 * Başarısız biten güncelleme bildiriminin yönlendirmesi tipli veriden gelir;
 * sunucunun ham özeti yalnız ikincil satırdır.
 */
export type OutcomeKey =
    | 'panelUpdate.failed'
    | 'panelUpdate.outcome.rolledBackTitle'
    | 'panelUpdate.outcome.rolledBack'
    | 'panelUpdate.outcome.cause.candidate_panel_startup_check_failed'
    | 'panelUpdate.outcome.cause.panel_start_unverified'
    | 'panelUpdate.outcome.cause.recovery_runtime_preflight_failed'
    | 'panelUpdate.outcome.stoppedTitle'
    | 'panelUpdate.outcome.stopped'
    | `panelUpdate.outcome.preflightStep.${PreflightStep}`
    | 'panelUpdate.outcome.preflightStep.generic'
    | `panelUpdate.outcome.preflightClass.${PreflightReasonClass}`
    | 'panelUpdate.outcome.stoppedNext'
    | 'panelUpdate.outcome.stoppedResume'
    | 'panelUpdate.outcome.cause.generic'
    | 'panelUpdate.outcome.rolledBackNext'
    | 'panelUpdate.outcome.rolledBackResume'
    | 'panelUpdate.outcome.followsRecovery'
    | 'panelUpdate.outcome.unknownResult'
    | 'panelUpdate.outcome.checking'
    | `recovery.phase.${NonNullable<RecoveryObservation['phase']>}`
    | `recovery.next.${NonNullable<RecoveryObservation['phase']>}`
    | `recovery.wait.${NonNullable<RecoveryObservation['waiting_for']>}`
    | 'recovery.wait.next'
    | 'recovery.automatic.pausedTitle'
    | `recovery.automatic.cause.${PausedCauseCode}`
    | 'recovery.automatic.pausedHelp'
    | 'recovery.automatic.renewal'
    | 'recovery.automatic.renewalOff'
    | 'recovery.automatic.pausingTitle'
    | 'recovery.automatic.pausingHelp'
    | 'recovery.automatic.retryTitle'
    | 'recovery.automatic.retryHelp'
    | 'recovery.automatic.inspect'
    | 'recovery.automatic.resume'
    | NonNullable<ReturnType<typeof recoveryFailureGuidanceKey>>;

/**
 * fallback: a boot-catalogue key shown while the screen catalogue that holds
 * `key` has not arrived (the notice is mounted above the routes).
 */
export type OutcomeText = { key: OutcomeKey; vars?: Record<string, string>; fallback?: OutcomeKey } | { text: string };

export type FailedUpdateGuidance = {
    /**
     * rolled_back: verified; unchanged: stopped in the read-only preflight, final;
     * recovery: the server's recovery is the source; unknown: no usable observation yet.
     */
    state: 'rolled_back' | 'unchanged' | 'recovery' | 'succeeded' | 'unknown';
    title: OutcomeText;
    lines: OutcomeText[];
    /** A fixed owner command to show as code, never server-supplied text. */
    command?: string;
    /**
     * The worker's summary without internal tokens, shown only as a secondary
     * line and only when no translated product summary says the same.
     */
    serverMessage?: string;
};

export const RECOVERY_LOG_COMMAND = 'sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50';

type Input = {
    targetVersion: string;
    previousVersion: string;
    /** The stored terminal message: either the server's summary or product text. */
    message: string;
    /** Product texts this browser may have stored instead of a server summary. */
    productMessages: readonly string[];
    /** Reviewed typed guidance derived from a summary (for example package_manager_busy). */
    typedMessage?: string;
    /** True until the first observation read for this request has settled. */
    reading: boolean;
};

// A refused update check never rolls back or pauses, so it has no cause text
// of its own there; the generic cause is shown if a record ever says so.
function causeKey(code: RecoveryFailureCode | undefined): OutcomeKey {
    return code && code !== 'update_preflight_refused' && (recoveryFailureCodes as readonly string[]).includes(code)
        ? `panelUpdate.outcome.cause.${code as PausedCauseCode}`
        : 'panelUpdate.outcome.cause.generic';
}

// The refused-check steps and the reason classes live in the server screen
// catalogue (the boot catalogue stays small); until it has arrived the notice
// shows the boot catalogue's generic reason.
function preflightReasonLine(preflight: { step: string; reasonClass?: string } | undefined): OutcomeText {
    const generic: OutcomeKey = 'panelUpdate.outcome.preflightStep.generic';
    if (preflight?.reasonClass && (preflightReasonClasses as readonly string[]).includes(preflight.reasonClass)) {
        return { key: `panelUpdate.outcome.preflightClass.${preflight.reasonClass as PreflightReasonClass}`, fallback: generic };
    }
    return preflight?.step && (preflightSteps as readonly string[]).includes(preflight.step)
        ? { key: `panelUpdate.outcome.preflightStep.${preflight.step as PreflightStep}`, fallback: generic }
        : { key: generic };
}

export function failedUpdateGuidance(observation: RecoveryObservation | null, input: Input): FailedUpdateGuidance {
    const product = input.message !== '' && input.productMessages.includes(input.message);
    // upd4 O7: never the raw line; omitted when a reviewed translation says it.
    const serverMessage = !product && !input.typedMessage && input.message !== '' ? withoutInternalTokens(input.message) || undefined : undefined;
    const versions = { target: input.targetVersion, previous: input.previousVersion };
    // A preflight stop changed nothing and nothing follows. The typed record is
    // the source; the updater's own unchanged summary counts only while no
    // other recorded outcome contradicts it.
    const preflight = systemUpdatePreflightStop(input.message);
    const uncontradicted = !observation || observation.observation !== 'known'
        || (observation.phase === 'failed' && observation.terminal_proof === 'none' && !observation.failure_code);
    if (isPreflightStop(observation) || (preflight && uncontradicted)) {
        const reported = withoutInternalTokens(preflight ? preflight.diagnostic : input.message);
        // A translated reason class says what happened and what to do: the
        // server's line is then not shown (upd4 O7).
        const translated = !!preflight?.reasonClass && (preflightReasonClasses as readonly string[]).includes(preflight.reasonClass);
        return {
            state: 'unchanged',
            title: { key: 'panelUpdate.outcome.stoppedTitle' },
            lines: [
                { key: 'panelUpdate.outcome.stopped', vars: versions },
                preflightReasonLine(preflight),
                { key: 'panelUpdate.outcome.stoppedNext' },
                { key: 'panelUpdate.outcome.stoppedResume', vars: versions },
            ],
            serverMessage: !product && !translated && reported ? reported : undefined,
        };
    }
    if (observation?.observation === 'known' && observation.phase === 'recovered'
        && observation.terminal_proof === 'rollback_verified') {
        // The secondary line keeps the server's words without internal tokens.
        return {
            state: 'rolled_back',
            title: { key: 'panelUpdate.outcome.rolledBackTitle' },
            lines: [
                { key: 'panelUpdate.outcome.rolledBack', vars: versions },
                { key: causeKey(observation.previous_failure === 'update_failed' ? observation.failure_code : undefined) },
                { key: 'panelUpdate.outcome.rolledBackNext', vars: versions },
                { key: 'panelUpdate.outcome.rolledBackResume', vars: versions },
            ],
            serverMessage,
        };
    }
    if (observation?.observation === 'known' && observation.phase) {
        const phase = observation.phase;
        const succeeded = phase === 'succeeded' && observation.terminal_proof === 'update_verified';
        const lines: OutcomeText[] = [];
        let title: OutcomeText;
        let command: string | undefined;
        const pausing = observation.automatic_recovery === 'pause_pending';
        if (observation.automatic_recovery === 'retry_scheduled' || pausing) {
            // Attempts remain, or the last one is finishing: nothing is asked of
            // the owner before the pause (with its retry command) is recorded.
            title = { key: pausing ? 'recovery.automatic.pausingTitle' : 'recovery.automatic.retryTitle' };
            const cause = retryingCauseKey(observation.first_failure_code);
            if (cause) lines.push({ key: cause });
            lines.push({ key: pausing ? 'recovery.automatic.pausingHelp' : 'recovery.automatic.retryHelp' });
        } else if (observation.automatic_recovery) {
            title = { key: 'recovery.automatic.pausedTitle' };
            if (observation.first_failure_code) lines.push({ key: `recovery.automatic.cause.${observation.first_failure_code}` });
            lines.push({ key: 'recovery.automatic.pausedHelp' },
                { key: observation.renewal_before_update === 'off' ? 'recovery.automatic.renewalOff' : 'recovery.automatic.renewal' },
                { key: 'recovery.automatic.inspect' });
            command = RECOVERY_LOG_COMMAND;
            lines.push({ key: 'recovery.automatic.resume' });
        } else if (observation.waiting_for) {
            title = { key: `recovery.wait.${observation.waiting_for}` };
            lines.push({ key: 'recovery.wait.next' });
        } else {
            title = { key: `recovery.phase.${phase}` };
            lines.push({ key: recoveryFailureGuidanceKey(observation) ?? `recovery.next.${phase}` });
        }
        // upd4 O9: a verified update shows no failure, and the server's
        // failure line of the earlier attempt is not shown.
        if (!succeeded) lines.push({ key: 'panelUpdate.outcome.followsRecovery' });
        return { state: succeeded ? 'succeeded' : 'recovery', title, lines, command, serverMessage: succeeded ? undefined : serverMessage };
    }
    const lines: OutcomeText[] = [];
    if (input.typedMessage) lines.push({ text: input.typedMessage });
    else if (product) lines.push({ text: input.message });
    else lines.push({ key: input.reading ? 'panelUpdate.outcome.checking' : 'panelUpdate.outcome.unknownResult' });
    return { state: 'unknown', title: { key: 'panelUpdate.failed' }, lines, serverMessage };
}

/** The update check's optional previous_attempt, for the exact offered target. */
export type PreviousUpdateAttempt = {
    request_id: string;
    phase: 'failed' | 'recovered';
    failure_code?: RecoveryFailureCode;
    finished_at: string;
};

/** The attempt stopped before changing anything installed (either typed preflight stop). */
export function previousAttemptStopped(attempt: PreviousUpdateAttempt | undefined): boolean {
    return !!attempt && (preflightStopCodes as readonly string[]).includes(attempt.failure_code ?? '');
}

/**
 * Accepts only the closed shape the Panel sends. Anything else is ignored, as an
 * older Panel's absent field is: the notice is guidance, never a start gate.
 */
export function decodePreviousUpdateAttempt(value: unknown): PreviousUpdateAttempt | undefined {
    if (!value || typeof value !== 'object') return undefined;
    const attempt = value as Record<string, unknown>;
    if (typeof attempt.request_id !== 'string' || !/^[a-f0-9]{32}$/.test(attempt.request_id)
        || (attempt.phase !== 'failed' && attempt.phase !== 'recovered')
        || typeof attempt.finished_at !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(attempt.finished_at)
        || !Number.isFinite(Date.parse(attempt.finished_at))) return undefined;
    const code = (recoveryFailureCodes as readonly unknown[]).includes(attempt.failure_code)
        ? attempt.failure_code as RecoveryFailureCode
        : undefined;
    return {
        request_id: attempt.request_id,
        phase: attempt.phase,
        ...(code ? { failure_code: code } : {}),
        finished_at: attempt.finished_at,
    };
}
