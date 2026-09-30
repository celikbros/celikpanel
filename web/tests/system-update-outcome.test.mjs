import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

// The failed-update notice takes its primary text from the exact request's
// recovery observation and typed cause. The worker's raw summary is only ever a
// secondary "server reported" line (upd2 finding O1).
const dataModule = (source) => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const compile = (path) => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const recoveryURL = dataModule(compile('../src/lib/recoveryObservation.ts'));
const failureURL = dataModule(compile('../src/lib/systemUpdateFailure.ts'));
const { parseRecoveryObservation, recoveryFailureGuidanceKey } = await import(recoveryURL);
const { withoutInternalTokens, systemUpdatePreflightStop } = await import(failureURL);
const { failedUpdateGuidance, decodePreviousUpdateAttempt, RECOVERY_LOG_COMMAND, preflightSteps } = await import(dataModule(
    compile('../src/lib/systemUpdateOutcome.ts')
        .replace(/from ['"]\.\/recoveryObservation['"]/g, `from '${recoveryURL}'`)
        .replace(/from ['"]\.\/systemUpdateFailure['"]/g, `from '${failureURL}'`)));

const id = 'a'.repeat(32);
const summary = 'reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved detail=';
const observed = (phase, extra = {}) => parseRecoveryObservation({
    schema: 'celikpanel-recovery-status/v1', panel_state: 'ready', request_id: id, observation: 'known', phase,
    terminal_proof: phase === 'recovered' ? 'rollback_verified' : phase === 'succeeded' ? 'update_verified' : 'none',
    reason: { accepted: 'operation_accepted', running: 'update_running', recovering: 'recovery_running', failed: 'update_failed',
        recovery_required: 'recovery_incomplete', succeeded: 'update_verified', recovered: 'rollback_verified' }[phase],
    observed_at: '2026-09-30T15:53:24Z', previous_failure: 'update_failed', ...extra,
}, id);
const input = (overrides = {}) => ({
    targetVersion: 'v0.1.0-alpha.82', previousVersion: 'v0.1.0-alpha.81', message: summary,
    productMessages: [en['panelUpdate.failed'], en['panelUpdate.notAccepted']], reading: false, ...overrides,
});
const render = (catalog, value) => {
    if ('text' in value) return value.text;
    const template = catalog[value.key];
    assert.equal(typeof template, 'string', `missing ${value.key}`);
    return template.replace(/\{(\w+)\}/g, (_, name) => value.vars?.[name] ?? `{${name}}`);
};
const primary = (catalog, guidance) => [guidance.title, ...guidance.lines].map((value) => render(catalog, value));

test('a verified rollback names both versions, the typed cause, who acts and how it resumes', () => {
    for (const [code, causeKey] of [
        ['candidate_panel_startup_check_failed', 'panelUpdate.outcome.cause.candidate_panel_startup_check_failed'],
        ['panel_start_unverified', 'panelUpdate.outcome.cause.panel_start_unverified'],
        [undefined, 'panelUpdate.outcome.cause.generic'],
    ]) {
        const guidance = failedUpdateGuidance(observed('recovered', code ? { failure_code: code } : {}), input());
        assert.equal(guidance.state, 'rolled_back');
        assert.deepEqual(guidance.lines.map((line) => line.key), [
            'panelUpdate.outcome.rolledBack', causeKey, 'panelUpdate.outcome.rolledBackNext', 'panelUpdate.outcome.rolledBackResume',
        ]);
        // upd3 O6: the server's words stay secondary, without internal tokens.
        assert.equal(guidance.serverMessage, 'reviewed updater failed: exit status 1: offline panel database migration failed; its original database and work evidence are preserved');
        for (const catalog of [en, tr]) {
            const text = primary(catalog, guidance).join('\n');
            assert.match(text, /v0\.1\.0-alpha\.82/);
            assert.match(text, /v0\.1\.0-alpha\.81/);
            assert.doesNotMatch(text, /CELIKPANEL_UPDATE_FAILURE|recovery_required|update_failed|\{/);
        }
    }
    const turkish = primary(tr, failedUpdateGuidance(observed('recovered'), input())).join(' ');
    assert.match(turkish, /otomatik olarak v0\.1\.0-alpha\.81 sürümüne döndürüldü/);
    assert.match(turkish, /Sunucuda yapmanız gereken bir şey yok/);
});

test('an unknown cause code keeps the generic cause', () => {
    const parsed = observed('recovered', { failure_code: 'private_detail' });
    assert.equal(parsed.failure_code, undefined);
    const forced = { ...observed('recovered'), failure_code: 'private_detail' };
    for (const value of [parsed, forced]) {
        assert.equal(failedUpdateGuidance(value, input()).lines[1].key, 'panelUpdate.outcome.cause.generic');
    }
    // A later recovery failure hides the update's own cause.
    const later = { ...observed('recovered', { failure_code: 'candidate_panel_startup_check_failed' }), previous_failure: 'recovery_failed' };
    assert.equal(failedUpdateGuidance(later, input()).lines[1].key, 'panelUpdate.outcome.cause.generic');
});

test('recovery still in progress follows the recovery screen texts, never the raw summary', () => {
    const cases = [
        [observed('recovering'), 'recovery.phase.recovering', 'recovery.next.recovering'],
        [observed('failed'), 'recovery.phase.failed', 'recovery.next.failed'],
        [observed('recovery_required'), 'recovery.phase.recovery_required', 'recovery.next.recovery_required'],
        [observed('recovering', { waiting_for: 'starting' }), 'recovery.wait.starting', 'recovery.wait.next'],
        [observed('recovering', { failure_code: 'candidate_panel_startup_check_failed' }), 'recovery.phase.recovering', 'recovery.failure.candidate_panel_startup_check_failed.returning'],
        [observed('recovery_required', { automatic_recovery: 'paused_retry_limit' }), 'recovery.automatic.pausedTitle', 'recovery.automatic.pausedHelp'],
    ];
    for (const [observation, title, first] of cases) {
        const guidance = failedUpdateGuidance(observation, input());
        assert.equal(guidance.state, 'recovery');
        assert.equal(guidance.title.key, title);
        assert.equal(guidance.lines[0].key, first);
        assert.equal(guidance.lines.at(-1).key, 'panelUpdate.outcome.followsRecovery');
        assert.equal(guidance.serverMessage, summary);
        for (const catalog of [en, tr]) {
            assert.doesNotMatch(primary(catalog, guidance).join('\n'), /CELIKPANEL_UPDATE_FAILURE/);
        }
    }
    const paused = failedUpdateGuidance(observed('recovery_required', { automatic_recovery: 'paused_retry_limit' }), input());
    assert.equal(paused.command, RECOVERY_LOG_COMMAND);
    const forward = failedUpdateGuidance(observed('succeeded', { previous_failure: 'update_failed' }), input());
    assert.equal(forward.state, 'succeeded');
    assert.equal(forward.title.key, 'recovery.phase.succeeded');
});

test('without an observation the summary is secondary and the result is stated as unknown', () => {
    const reading = failedUpdateGuidance(null, input({ reading: true }));
    assert.equal(reading.state, 'unknown');
    assert.deepEqual(reading.lines, [{ key: 'panelUpdate.outcome.checking' }]);
    const unknown = failedUpdateGuidance(parseRecoveryObservation({ schema: 'celikpanel-recovery-status/v1', panel_state: 'ready', request_id: id, observation: 'unavailable' }, id), input());
    assert.deepEqual(unknown.lines, [{ key: 'panelUpdate.outcome.unknownResult' }]);
    assert.equal(unknown.title.key, 'panelUpdate.failed');
    assert.equal(unknown.serverMessage, summary);
    // Product text stored instead of a summary is primary and never labelled as the server's.
    const product = failedUpdateGuidance(null, input({ message: en['panelUpdate.notAccepted'] }));
    assert.deepEqual(product.lines, [{ text: en['panelUpdate.notAccepted'] }]);
    assert.equal(product.serverMessage, undefined);
    // A reviewed typed pre-mutation stop keeps its guidance first and the summary second.
    const typed = failedUpdateGuidance(null, input({ typedMessage: en['panelUpdate.packageManagerBusy'] }));
    assert.deepEqual(typed.lines, [{ text: en['panelUpdate.packageManagerBusy'] }]);
    assert.equal(typed.serverMessage, summary);
    const empty = failedUpdateGuidance(null, input({ message: '' }));
    assert.equal(empty.serverMessage, undefined);
});

test('every key the notice can produce exists in both shell catalogues', () => {
    const observations = [null, observed('recovered'), observed('recovered', { failure_code: 'candidate_panel_startup_check_failed' }),
        observed('recovered', { failure_code: 'panel_start_unverified' }), observed('succeeded'),
        observed('recovering', { waiting_for: 'initializing' }), observed('recovering', { waiting_for: 'stopping' }),
        observed('recovering', { failure_code: 'panel_start_unverified' }),
        observed('recovery_required', { automatic_recovery: 'paused_retry_limit' }),
        ...['accepted', 'running', 'recovering', 'failed', 'recovery_required'].map((phase) => observed(phase))];
    for (const observation of observations) {
        for (const reading of [true, false]) {
            const guidance = failedUpdateGuidance(observation, input({ reading }));
            for (const catalog of [en, tr]) primary(catalog, guidance);
        }
    }
    for (const catalog of [en, tr]) assert.match(catalog['panelUpdate.outcome.serverMessage'], /\{message\}/);
});

test('the previous attempt decoder accepts only the closed shape', () => {
    const valid = { request_id: id, phase: 'recovered', failure_code: 'candidate_panel_startup_check_failed', finished_at: '2026-09-30T15:53:24Z' };
    assert.deepEqual(decodePreviousUpdateAttempt(valid), valid);
    assert.deepEqual(decodePreviousUpdateAttempt({ ...valid, failure_code: 'private detail' }),
        { request_id: id, phase: 'recovered', finished_at: '2026-09-30T15:53:24Z' });
    assert.equal(decodePreviousUpdateAttempt({ ...valid, phase: 'failed' }).phase, 'failed');
    for (const bad of [undefined, null, 'x', { ...valid, phase: 'recovering' }, { ...valid, request_id: '../x' },
        { ...valid, finished_at: 'yesterday' }, { ...valid, finished_at: '2026-13-40T99:99:99Z' }]) {
        assert.equal(decodePreviousUpdateAttempt(bad), undefined);
    }
});

test('the mounted tracker renders the typed guidance and keeps the summary secondary', () => {
    const tracker = readFileSync(new URL('../src/components/SystemUpdateOperation.tsx', import.meta.url), 'utf8');
    const message = tracker.slice(tracker.indexOf('const message = pendingReload'), tracker.indexOf('const failureDetails'));
    assert.match(message, /failureGuidance\s*\?\s*outcomeText\(failureGuidance\.lines\[0\]\)/);
    assert.doesNotMatch(message, /systemUpdateFailureMessage|summary/);
    assert.match(tracker, /panelUpdate\.outcome\.serverMessage', \{ message: failureGuidance\.serverMessage \}/);
    // The observation read is a GET of the exact request; it never posts or retries.
    const poll = tracker.slice(tracker.indexOf('const failedRequestID'), tracker.indexOf('const failureGuidance'));
    assert.match(poll, /\/api\/v1\/recovery\/status\?request_id=\$\{failedRequestID\}/);
    assert.doesNotMatch(poll, /method:|\/update\/start|\/update\/abandon/);
});

test('a paused recovery adds the first typed cause before the pause guidance in the notice', () => {
    const withCause = failedUpdateGuidance(observed('recovery_required', { automatic_recovery: 'paused_retry_limit', previous_failure: 'recovery_failed', first_failure_code: 'panel_start_unverified' }), input());
    assert.deepEqual(withCause.lines.slice(0, 2).map((line) => line.key), ['recovery.automatic.cause.panel_start_unverified', 'recovery.automatic.pausedHelp']);
    const without = failedUpdateGuidance(observed('recovery_required', { automatic_recovery: 'paused_retry_limit', previous_failure: 'recovery_failed' }), input());
    assert.equal(without.lines[0].key, 'recovery.automatic.pausedHelp');
    for (const catalog of [en, tr]) {
        const text = primary(catalog, withCause).join('\n');
        assert.match(text, /sudo journalctl -u celikpanel-panel -n 50/);
        primary(catalog, failedUpdateGuidance(observed('recovery_required', { automatic_recovery: 'paused_retry_limit', first_failure_code: 'candidate_panel_startup_check_failed' }), input()));
    }
});

// upd3 F1: a preflight stop changed nothing and nothing follows it. The notice
// says so, names the step, says who acts and that starting again is safe.
const preflightSummary = (step, diagnostic) => `reviewed updater failed: exit status 1: !! CELIKPANEL_UPDATE_FAILURE code=recovery_runtime_preflight_failed state=unchanged reason=recovery runtime preflight step=${step}: ${diagnostic} detail=`;

test('a preflight stop is final, unchanged and safe to start again', () => {
    const message = preflightSummary('panel_database_check', 'Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning');
    assert.deepEqual(systemUpdatePreflightStop(message), { step: 'panel_database_check', diagnostic: 'Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning' });
    const stop = observed('failed', { failure_code: 'recovery_runtime_preflight_failed' });
    assert.equal(stop.failure_code, 'recovery_runtime_preflight_failed');
    assert.equal(recoveryFailureGuidanceKey(stop), 'recovery.failure.recovery_runtime_preflight_failed.stopped');
    for (const [observation, reading] of [[stop, false], [null, true], [null, false], [observed('failed'), false]]) {
        const guidance = failedUpdateGuidance(observation, input({ message, reading }));
        assert.equal(guidance.state, 'unchanged');
        assert.equal(guidance.title.key, 'panelUpdate.outcome.stoppedTitle');
        assert.deepEqual(guidance.lines.map((line) => line.key), ['panelUpdate.outcome.stopped',
            'panelUpdate.outcome.preflightStep.panel_database_check', 'panelUpdate.outcome.stoppedNext', 'panelUpdate.outcome.stoppedResume']);
        assert.equal(guidance.command, undefined);
        assert.equal(guidance.serverMessage, 'Recovery database check: service operations are not idle: SQLite sidecar -wal changed after pinning');
        const english = primary(en, guidance).join('\n');
        assert.match(english, /before the installed version was changed or any service was stopped/);
        assert.match(english, /running v0\.1\.0-alpha\.81 as before/);
        assert.match(english, /Starting the update to v0\.1\.0-alpha\.82 again is safe/);
        assert.doesNotMatch(english, /do not start|must act|check .* again in/i);
        const turkish = primary(tr, guidance).join('\n');
        assert.match(turkish, /v0\.1\.0-alpha\.81 sürümünü eskisi gibi çalıştırıyor/);
        assert.match(turkish, /yeniden başlatmak güvenlidir/);
        for (const text of [english, turkish]) assert.doesNotMatch(text, /CELIKPANEL_UPDATE_FAILURE|recovery_runtime|state=|\{/);
        assert.doesNotMatch(turkish, /\b(The|Reason|Nothing)\b/);
    }
    // The record alone (no parsable summary) still says unchanged, with the generic step.
    const recordOnly = failedUpdateGuidance(stop, input({ message: '' }));
    assert.equal(recordOnly.state, 'unchanged');
    assert.equal(recordOnly.lines[1].key, 'panelUpdate.outcome.preflightStep.generic');
    assert.equal(recordOnly.serverMessage, undefined);
    // An unknown step keeps the generic reason; every known step has both texts.
    assert.equal(failedUpdateGuidance(null, input({ message: preflightSummary('private_step', 'x') })).lines[1].key, 'panelUpdate.outcome.preflightStep.generic');
    for (const step of preflightSteps) {
        for (const catalog of [en, tr]) primary(catalog, failedUpdateGuidance(null, input({ message: preflightSummary(step, 'x') })));
    }
    // A contradicting newer record wins over the summary.
    const recovering = failedUpdateGuidance(observed('recovering'), input({ message }));
    assert.equal(recovering.state, 'recovery');
    // Anything but the exact unchanged outcome is not a preflight stop.
    assert.equal(systemUpdatePreflightStop(message.replace('state=unchanged', 'state=recovery_required')), undefined);
    assert.equal(systemUpdatePreflightStop(summary), undefined);
});

// upd3 F3: while attempts remain the notice says the server retries by itself
// and keeps the first typed cause; owner action appears only at the pause.
test('a scheduled automatic retry keeps the first cause and asks nothing of the owner', () => {
    const retry = observed('recovery_required', { reason: 'recovery_failed', automatic_recovery: 'retry_scheduled', previous_failure: 'recovery_failed', first_failure_code: 'panel_start_unverified' });
    assert.equal(retry.automatic_recovery, 'retry_scheduled');
    assert.equal(retry.first_failure_code, 'panel_start_unverified');
    const guidance = failedUpdateGuidance(retry, input());
    assert.equal(guidance.title.key, 'recovery.automatic.retryTitle');
    assert.deepEqual(guidance.lines.map((line) => line.key), ['recovery.failure.panel_start_unverified.pending',
        'recovery.automatic.retryHelp', 'panelUpdate.outcome.followsRecovery']);
    assert.equal(guidance.command, undefined);
    for (const catalog of [en, tr]) {
        const text = primary(catalog, guidance).join('\n');
        assert.match(text, /sudo journalctl -u celikpanel-panel -n 50/);
        assert.match(text, /30/);
    }
    assert.doesNotMatch(primary(en, guidance).join('\n'), /must act/);
    // The hint is accepted only on a recovery failure record.
    assert.equal(observed('recovery_required', { automatic_recovery: 'retry_scheduled' }).automatic_recovery, undefined);
    // The next attempt after a recovery failure keeps the same first cause.
    const next = observed('recovering', { previous_failure: 'recovery_failed', first_failure_code: 'panel_start_unverified' });
    assert.equal(recoveryFailureGuidanceKey(next), 'recovery.failure.panel_start_unverified.pending');
    assert.equal(failedUpdateGuidance(next, input()).lines[0].key, 'recovery.failure.panel_start_unverified.pending');
    assert.equal(observed('recovering', { first_failure_code: 'panel_start_unverified' }).first_failure_code, undefined);
    // At the pause the renewal line follows the pause help.
    const paused = failedUpdateGuidance(observed('recovery_required', { automatic_recovery: 'paused_retry_limit', previous_failure: 'recovery_failed' }), input());
    assert.deepEqual(paused.lines.slice(0, 2).map((line) => line.key), ['recovery.automatic.pausedHelp', 'recovery.automatic.renewal']);
    for (const catalog of [en, tr]) assert.match(render(catalog, { key: 'recovery.automatic.renewal' }), /Certbot/);
});

// upd3 O6: the secondary line after a verified rollback carries the server's
// words without internal tokens, or is omitted when nothing readable remains.
test('internal tokens are removed from the rolled-back server line', () => {
    assert.equal(withoutInternalTokens('!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason= detail='), '');
    const shortLine = failedUpdateGuidance(observed('recovered'), input({ message: 'reviewed updater failed: !! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason= detail=' }));
    assert.equal(shortLine.serverMessage, 'reviewed updater failed');
    const only = failedUpdateGuidance(observed('recovered'), input({ message: '!! CELIKPANEL_UPDATE_FAILURE code=update_failed state=recovery_required reason= detail=' }));
    assert.equal(only.serverMessage, undefined);
    const turkish = primary(tr, failedUpdateGuidance(observed('recovered'), input())).join('\n');
    assert.doesNotMatch(turkish, /\b(The|update|Cause|Nothing)\b/);
});
