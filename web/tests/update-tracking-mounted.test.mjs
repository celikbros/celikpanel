// The update notice and the update card say the same thing about a followed
// update (owner report 2026-10-10, Debian server: the card showed
// v0.1.0-alpha.82 as the current version while the corner notice still said
// "The update is being applied" at T+02:03). The Agent's record stays running
// after the new Panel has started, until the updater exits and its final proof
// passes; the Panel's status says "verifying" in that window. A failed read is
// unknown, never "being applied"; a finished record leaves no notice. The
// dialog's time is local time named with its zone, not labelled UTC.
//
// Guncelleme bildirimi ve karti ayni seyi soyler; saat yerel ve dilimiyle.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import TestRenderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { enServerScreens } from '../src/i18n/screens/server/en.ts';
import { trServerScreens } from '../src/i18n/screens/server/tr.ts';
import { systemUpdateClockTime, systemUpdateOperationPhase, systemUpdateTrackingText, decodeSystemUpdatePhase } from '../src/lib/systemUpdateTracking.ts';

const require = createRequire(import.meta.url);
const moduleURL = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
function compileURL(relativePath, replacements = []) {
    let compiled = ts.transpileModule(readFileSync(new URL(relativePath, import.meta.url), 'utf8'), {
        compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020, jsx: ts.JsxEmit.ReactJSX },
    }).outputText;
    for (const [pattern, replacement] of replacements) compiled = compiled.replace(pattern, replacement);
    return moduleURL(compiled);
}
const reactURL = pathToFileURL(require.resolve('react')).href;
const jsxRuntimeURL = pathToFileURL(require.resolve('react/jsx-runtime')).href;
const catalogues = { en: { ...en, ...enServerScreens }, tr: { ...tr, ...trServerScreens } };
const fill = (text, vars) => String(text).replace(/\{(\w+)\}/g, (all, name) => vars?.[name] ?? all);
const iconsURL = moduleURL(`import React from '${reactURL}'; const Icon = props => React.createElement('i', props);
export const AlertTriangle = Icon, DownloadCloud = Icon, Loader2 = Icon, RefreshCw = Icon;`);
const uiURL = moduleURL(`import React from '${reactURL}'; export function Button({ children, ...props }) { return React.createElement('button', props, children); }`);
const i18nURL = moduleURL(`const fill = (text, vars) => String(text).replace(/\\{(\\w+)\\}/g, (all, name) => vars && vars[name] !== undefined ? String(vars[name]) : all);
export function useI18n() { const c = globalThis.__trackingCatalogue; return { t: (key, vars) => fill(c.strings[key] ?? key, vars), locale: c.locale }; }`);
const apiErrorURL = moduleURL(`export async function readApiError() { return {}; } export function apiErrorText() { return 'api-error'; }`);
const operationURL = moduleURL(`
export function createSystemUpdateRequestID() { return 'mounted-request-id'; }
export function systemUpdateResponseHint() { return 'response-error'; }
export function useSystemUpdateOperation() { return globalThis.__panelUpdateOperation; }
export function validUpdateTarget() { return true; }
export function validUpdateVersion() { return true; }`);
const admissionURL = moduleURL(`
export function unverifiedHostMutationReadiness() { return { ready: false, code: 'HOST_MUTATION_UNAVAILABLE', reason: 'state_unverified' }; }
export function fetchHostMutationReadiness() { return Promise.resolve({ ready: true }); }
export async function runHostMutationAdmission(read, onReady) { const r = await read(); if (r.ready) await onReady(); return r; }`);
const outcomeURL = compileURL('../src/lib/systemUpdateOutcome.ts', [
    [/from ['"]\.\/recoveryObservation['"]/g, `from '${compileURL('../src/lib/recoveryObservation.ts')}'`],
    [/from ['"]\.\/systemUpdateFailure['"]/g, `from '${compileURL('../src/lib/systemUpdateFailure.ts')}'`],
]);
const { PanelUpdateCard } = await import(compileURL('../src/components/PanelUpdateCard.tsx', [
    [/from ['"]\.\.\/lib\/systemUpdateOutcome['"]/g, `from '${outcomeURL}'`],
    [/from ['"]react['"]/g, `from '${reactURL}'`],
    [/from ['"]react\/jsx-runtime['"]/g, `from '${jsxRuntimeURL}'`],
    [/from ['"]lucide-react['"]/g, `from '${iconsURL}'`],
    [/from ['"]\.\/ui['"]/g, `from '${uiURL}'`],
    [/from ['"]\.\.\/i18n['"]/g, `from '${i18nURL}'`],
    [/from ['"]\.\.\/lib\/apiError['"]/g, `from '${apiErrorURL}'`],
    [/from ['"]\.\.\/lib\/panelUpdateAdmission['"]/g, `from '${admissionURL}'`],
    [/from ['"]\.\/SystemUpdateOperation['"]/g, `from '${operationURL}'`],
]));

const flush = async () => { for (let i = 0; i < 4; i += 1) await Promise.resolve(); };
function textOf(node) {
    if (node === null || node === undefined || typeof node === 'boolean') return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(textOf).join(' ');
    return textOf(node.children);
}
async function mountCard(locale, tracking, runningVersion = 'v0.1.0-alpha.82') {
    globalThis.__trackingCatalogue = { locale, strings: catalogues[locale] };
    globalThis.__panelUpdateOperation = { active: tracking !== null, start: async () => ({ kind: 'accepted' }), tracking };
    const reads = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (path, options) => {
        reads.push({ path: String(path), method: options?.method || 'GET' });
        if (String(path) === '/api/v1/panel/version') return { ok: true, json: async () => ({ version: runningVersion, commit: 'f'.repeat(40) }) };
        throw new Error('unexpected fetch ' + path);
    };
    let renderer;
    await act(async () => { renderer = TestRenderer.create(React.createElement(PanelUpdateCard)); await flush(); });
    return { renderer, reads, restore: () => { globalThis.fetch = originalFetch; } };
}

const tTo = locale => (key, vars) => fill(catalogues[locale][key] ?? key, vars);

test('the status phase is typed: verifying only for a running record on the installed target', () => {
    assert.equal(systemUpdateOperationPhase({ status: 'running', phase: 'verifying' }), 'verifying');
    assert.equal(systemUpdateOperationPhase({ status: 'running' }), 'applying');
    assert.equal(systemUpdateOperationPhase({ status: 'queued' }), 'queued');
    assert.equal(systemUpdateOperationPhase(null), 'unknown', 'a read that failed is unknown');
    assert.equal(systemUpdateOperationPhase(undefined), 'unknown');
    assert.equal(decodeSystemUpdatePhase('verifying'), 'verifying');
    assert.equal(decodeSystemUpdatePhase('done'), undefined);
});

for (const locale of ['en', 'tr']) {
    const c = catalogues[locale];
    const t = tTo(locale);

    test(`${locale}: during verification the notice and the card both say installed and being verified`, async () => {
        const notice = systemUpdateTrackingText({ version: 'v0.1.0-alpha.82', phase: 'verifying' }, t('panelUpdate.running'), t);
        assert.equal(notice.title, c['panelUpdate.tracking.verifyingTitle']);
        assert.equal(notice.message, fill(c['panelUpdate.tracking.verifying'], { version: 'v0.1.0-alpha.82' }));
        assert.notEqual(notice.message, c['panelUpdate.running'], 'not "being applied"');
        const { renderer, reads, restore } = await mountCard(locale, { version: 'v0.1.0-alpha.82', phase: 'verifying' });
        try {
            const text = textOf(renderer.toJSON());
            assert.ok(text.includes('v0.1.0-alpha.82'));
            assert.ok(text.includes(c['panelUpdate.card.installedVerifying']), 'beside the current version');
            assert.ok(text.includes(fill(c['panelUpdate.card.verifying'], { version: 'v0.1.0-alpha.82' })));
            assert.equal(renderer.root.findByProps({ 'data-update-phase': 'verifying' }).props.role, 'status');
            assert.ok(reads.every(read => read.method === 'GET'));
        } finally { restore(); await act(async () => renderer.unmount()); }
    });

    test(`${locale}: a read that failed is said as unknown with its reason, in the notice and on the card`, async () => {
        const hint = c['panelUpdate.connectionLost'];
        const notice = systemUpdateTrackingText({ version: 'v0.1.0-alpha.82', phase: 'unknown' }, hint, t);
        assert.equal(notice.title, null);
        assert.equal(notice.message, c['panelUpdate.tracking.unknown']);
        assert.equal(notice.hint, hint);
        const { renderer, restore } = await mountCard(locale, { version: 'v0.1.0-alpha.82', phase: 'unknown' }, 'v0.1.0-alpha.81');
        try {
            const text = textOf(renderer.toJSON());
            assert.ok(text.includes(fill(c['panelUpdate.card.unknown'], { version: 'v0.1.0-alpha.82' })));
            assert.ok(!text.includes(c['panelUpdate.running']) && !text.includes(c['panelUpdate.card.installedVerifying']));
        } finally { restore(); await act(async () => renderer.unmount()); }
    });

    test(`${locale}: a finished record leaves neither the notice nor the card's progress line`, async () => {
        // The provider follows a record only while its outcome is unknown; a
        // finished one gives tracking null and the notice is not drawn.
        const { renderer, restore } = await mountCard(locale, null);
        try {
            assert.equal(renderer.root.findAll(node => node.props && node.props['data-update-phase'] !== undefined).length, 0);
            const text = textOf(renderer.toJSON());
            assert.ok(text.includes('v0.1.0-alpha.82') && !text.includes(c['panelUpdate.card.installedVerifying']));
        } finally { restore(); await act(async () => renderer.unmount()); }
    });

    test(`${locale}: before any read the notice says it is reading, and an applying record keeps its own sentence`, () => {
        const reading = systemUpdateTrackingText({ version: 'v0.1.0-alpha.82', phase: 'reading' }, c['panelUpdate.tracking.reading'], t);
        assert.equal(reading.message, c['panelUpdate.tracking.reading']);
        const applying = systemUpdateTrackingText({ version: 'v0.1.0-alpha.82', phase: 'applying' }, c['panelUpdate.running'], t);
        assert.deepEqual(applying, { title: null, message: c['panelUpdate.running'], hint: null });
    });
}

test('the provider wires the typed phase, a reading placeholder and the finished-record rule', () => {
    const tracker = readFileSync(new URL('../src/components/SystemUpdateOperation.tsx', import.meta.url), 'utf8');
    assert.match(tracker, /phase: systemUpdateOperationPhase\(outcome\.operation\)/);
    assert.match(tracker, /message: message \?\? t\('panelUpdate\.tracking\.reading'\)/);
    assert.match(tracker, /const followed = marker !== null && provisional === null && pendingReload === null\s*&& requiredReloadMarker === null && displayedTerminal === null \? marker : null;/);
    assert.match(tracker, /tracking,\n/);
    // The dialog's time: no "UTC" label on a local time.
    assert.doesNotMatch(tracker, />UTC</);
    assert.match(tracker, /\{t\('panelUpdate\.lastRead'\)\}<\/dt>/);
    assert.match(tracker, /systemUpdateClockTime\(view\.lastAttemptAt, locale\)/);
});

test('the dialog time is the local time with its zone, in the interface language', () => {
    const at = Date.UTC(2026, 9, 10, 13, 8, 52);
    const english = systemUpdateClockTime(at, 'en');
    const turkish = systemUpdateClockTime(at, 'tr');
    assert.equal(english, new Date(at).toLocaleTimeString('en-US', { timeZoneName: 'short' }));
    assert.equal(turkish, new Date(at).toLocaleTimeString('tr-TR', { timeZoneName: 'short' }));
    // A zone name is part of the value, so a local time is never shown bare.
    assert.notEqual(english, new Date(at).toLocaleTimeString('en-US'));
    assert.equal(en['panelUpdate.lastRead'], 'Last read');
    assert.equal(tr['panelUpdate.lastRead'], 'Son okuma');
});

// The wording decided on 2026-10-10: while the update is verified nobody has to
// act; while its state is unknown, a second update is not to be started.
// Dogrulama surerken kimse bir sey yapmaz; durum bilinmezken ikinci guncelleme baslatilmaz.
test('verifying says nobody has to act, and an unknown state says not to start another update', () => {
    for (const key of ['panelUpdate.tracking.verifying', 'panelUpdate.card.verifying']) {
        assert.match(catalogues.en[key], /You do not need to do anything\.$/, key);
        assert.match(catalogues.tr[key], /Bir şey yapmanız gerekmiyor\.$/, key);
    }
    assert.match(catalogues.en['panelUpdate.card.unknown'], /Do not start another update\.$/);
    assert.match(catalogues.tr['panelUpdate.card.unknown'], /İkinci bir güncelleme başlatmayın\.$/);
    for (const key of ['panelUpdate.card.applying', 'panelUpdate.card.verifying']) assert.doesNotMatch(catalogues.tr[key], /denetle/, key);
});
