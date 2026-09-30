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
const { parseRecoveryObservation } = await import(recoveryURL);
const { failedUpdateGuidance, decodePreviousUpdateAttempt, RECOVERY_LOG_COMMAND } = await import(dataModule(
    compile('../src/lib/systemUpdateOutcome.ts').replace(/from ['"]\.\/recoveryObservation['"]/g, `from '${recoveryURL}'`)));

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
        assert.equal(guidance.serverMessage, summary);
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
