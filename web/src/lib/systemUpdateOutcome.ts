import { isPreflightStop, recoveryFailureCodes, recoveryFailureGuidanceKey, retryingCauseKey, type RecoveryFailureCode, type RecoveryObservation } from './recoveryObservation';
import { systemUpdatePreflightStop, withoutInternalTokens } from './systemUpdateFailure';

/** Preflight steps with reviewed owner wording; any other step uses the generic line. */
export const preflightSteps = ['panel_database_check', 'agent_ledger_check', 'owner_metadata_probe', 'runtime_selection',
    'runtime_revalidation', 'release_boundary', 'material_support', 'database_support', 'database_metadata'] as const;
type PreflightStep = typeof preflightSteps[number];

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
    | `recovery.automatic.cause.${RecoveryFailureCode}`
    | 'recovery.automatic.pausedHelp'
    | 'recovery.automatic.renewal'
    | 'recovery.automatic.retryTitle'
    | 'recovery.automatic.retryHelp'
    | 'recovery.automatic.inspect'
    | 'recovery.automatic.resume'
    | NonNullable<ReturnType<typeof recoveryFailureGuidanceKey>>;

export type OutcomeText = { key: OutcomeKey; vars?: Record<string, string> } | { text: string };

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
    /** The worker's raw summary, shown only as a secondary line. */
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

function causeKey(code: RecoveryFailureCode | undefined): OutcomeKey {
    return code && (recoveryFailureCodes as readonly string[]).includes(code)
        ? `panelUpdate.outcome.cause.${code}`
        : 'panelUpdate.outcome.cause.generic';
}

function preflightStepKey(step: string | undefined): OutcomeKey {
    return step && (preflightSteps as readonly string[]).includes(step)
        ? `panelUpdate.outcome.preflightStep.${step as PreflightStep}`
        : 'panelUpdate.outcome.preflightStep.generic';
}

export function failedUpdateGuidance(observation: RecoveryObservation | null, input: Input): FailedUpdateGuidance {
    const product = input.message !== '' && input.productMessages.includes(input.message);
    const serverMessage = !product && input.message !== '' ? input.message : undefined;
    const versions = { target: input.targetVersion, previous: input.previousVersion };
    // A preflight stop changed nothing and nothing follows. The typed record is
    // the source; the updater's own unchanged summary counts only while no
    // other recorded outcome contradicts it.
    const preflight = systemUpdatePreflightStop(input.message);
    const uncontradicted = !observation || observation.observation !== 'known'
        || (observation.phase === 'failed' && observation.terminal_proof === 'none' && !observation.failure_code);
    if (isPreflightStop(observation) || (preflight && uncontradicted)) {
        const reported = withoutInternalTokens(preflight ? preflight.diagnostic : input.message);
        return {
            state: 'unchanged',
            title: { key: 'panelUpdate.outcome.stoppedTitle' },
            lines: [
                { key: 'panelUpdate.outcome.stopped', vars: versions },
                { key: preflightStepKey(preflight?.step) },
                { key: 'panelUpdate.outcome.stoppedNext' },
                { key: 'panelUpdate.outcome.stoppedResume', vars: versions },
            ],
            serverMessage: !product && reported ? reported : undefined,
        };
    }
    if (observation?.observation === 'known' && observation.phase === 'recovered'
        && observation.terminal_proof === 'rollback_verified') {
        // The secondary line keeps the server's words without internal tokens.
        const reported = serverMessage ? withoutInternalTokens(serverMessage) : '';
        return {
            state: 'rolled_back',
            title: { key: 'panelUpdate.outcome.rolledBackTitle' },
            lines: [
                { key: 'panelUpdate.outcome.rolledBack', vars: versions },
                { key: causeKey(observation.previous_failure === 'update_failed' ? observation.failure_code : undefined) },
                { key: 'panelUpdate.outcome.rolledBackNext', vars: versions },
                { key: 'panelUpdate.outcome.rolledBackResume', vars: versions },
            ],
            serverMessage: reported || undefined,
        };
    }
    if (observation?.observation === 'known' && observation.phase) {
        const phase = observation.phase;
        const succeeded = phase === 'succeeded' && observation.terminal_proof === 'update_verified';
        const lines: OutcomeText[] = [];
        let title: OutcomeText;
        let command: string | undefined;
        if (observation.automatic_recovery === 'retry_scheduled') {
            // Attempts remain: nothing is asked of the owner before the pause.
            title = { key: 'recovery.automatic.retryTitle' };
            const cause = retryingCauseKey(observation.first_failure_code);
            if (cause) lines.push({ key: cause });
            lines.push({ key: 'recovery.automatic.retryHelp' });
        } else if (observation.automatic_recovery) {
            title = { key: 'recovery.automatic.pausedTitle' };
            if (observation.first_failure_code) lines.push({ key: `recovery.automatic.cause.${observation.first_failure_code}` });
            lines.push({ key: 'recovery.automatic.pausedHelp' }, { key: 'recovery.automatic.renewal' }, { key: 'recovery.automatic.inspect' });
            command = RECOVERY_LOG_COMMAND;
            lines.push({ key: 'recovery.automatic.resume' });
        } else if (observation.waiting_for) {
            title = { key: `recovery.wait.${observation.waiting_for}` };
            lines.push({ key: 'recovery.wait.next' });
        } else {
            title = { key: `recovery.phase.${phase}` };
            lines.push({ key: recoveryFailureGuidanceKey(observation) ?? `recovery.next.${phase}` });
        }
        if (!succeeded) lines.push({ key: 'panelUpdate.outcome.followsRecovery' });
        return { state: succeeded ? 'succeeded' : 'recovery', title, lines, command, serverMessage };
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
