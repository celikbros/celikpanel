// The setup page's final check, mounted (owner report 2026-10-10, Debian
// server): the page said "This operation stopped ... A required check needs
// attention" while its last step read "In progress", and the reason was folded
// under "Checks and how to continue". The run had been stopped by reopening its
// plan (server_setup_plan_revised) while its final check waited for the mail
// host's reverse DNS. D-024: the reason, who acts and the next action come
// before the step list, unfolded; a stopped step is not in progress; waiting for
// the owner, an unmet prerequisite, a failure and an unknown result differ.
//
// Kurulum sayfasinin son denetimi, gercek katalogla ve iki dilde.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';
import { enServerScreens } from '../src/i18n/screens/server/en.ts';
import { trServerScreens } from '../src/i18n/screens/server/tr.ts';

const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataURL = text => `data:text/javascript;base64,${Buffer.from(text).toString('base64')}`;
const compile = path => ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const setupURL = dataURL(compile('../src/lib/serverSetup.ts'));
const remoteURL = dataURL(compile('../src/lib/remoteDNS.ts'));
const componentLibURL = dataURL(compile('../src/lib/serverSetupComponents.ts').replace("from './serverSetup'", `from '${setupURL}'`));
const operationURL = dataURL(compile('../src/lib/serverSetupOperation.ts').replace("from './serverSetup'", `from '${setupURL}'`));
const dnsEnglishURL = dataURL(compile('../src/i18n/setupDNS/en.ts'));
const dnsTurkishURL = dataURL(compile('../src/i18n/setupDNS/tr.ts'));
const dnsCopyURL = dataURL(compile('../src/i18n/setupDNS.ts').replace("from './setupDNS/en'", `from '${dnsEnglishURL}'`).replace("from './setupDNS/tr'", `from '${dnsTurkishURL}'`));
const guidanceURL = dataURL(compile('../src/lib/serverSetupGuidance.ts'));
const handoverURL = dataURL(compile('../src/lib/panelHandover.ts'));
const catalogues = { en: { ...en, ...enScreens, ...enServerScreens }, tr: { ...tr, ...trScreens, ...trServerScreens } };
const stub = dataURL(`
import React from '${reactURL}';
const fill = (text, vars) => String(text).replace(/\\{(\\w+)\\}/g, (all, name) => vars && vars[name] !== undefined ? String(vars[name]) : all);
export const useAuth = () => ({ role: 'admin', user: { username: 'admin' }, logout() {} });
export const useI18n = () => ({ locale: globalThis.finalCheck.locale, t: (key, vars) => fill(globalThis.finalCheck.catalogue[key] ?? key, vars), screensReady: true, screensFailed: false });
export const Navigate = props => React.createElement('redirect', props);
export const Link = props => React.createElement('a', { ...props, href: props.to });
export const useServerSetup = () => globalThis.finalCheck.context;
export const ServerSetupShell = props => React.createElement('main', null, props.navigation, props.children);
export const Button = props => React.createElement('button', props);
export const Spinner = () => React.createElement('span', null, 'loading');
export const ArrowRight = () => null, Check = () => null, Circle = () => null, Loader2 = () => null;
export const ServerSetupDNSConnection = () => null;
export const ServerSetupSteps = () => null;
export const ServerSetupComponents = () => null;
export const useSetupComponentCatalog = () => ({ catalog: null, failed: false, reload() {} });
export const ServerSetupChoice = () => null, ServerSetupManualAction = () => null;
export const inputClass = '';
export const AddressLink = props => React.createElement('a', { href: props.href }, props.address);
`);
const source = compile('../src/components/ServerSetup.tsx').replace(/from ['"]([^'"]+)['"]/g, (_, path) => {
    const url = path === 'react' ? reactURL : path.endsWith('/remoteDNS') ? remoteURL : path.endsWith('/serverSetupComponents') ? componentLibURL
        : path.endsWith('/setupDNS') ? dnsCopyURL : path.endsWith('/panelHandover') ? handoverURL : path.endsWith('/serverSetupGuidance') ? guidanceURL
            : path.endsWith('/serverSetupOperation') ? operationURL : path.endsWith('/serverSetup') ? setupURL : stub;
    return `from '${url}'`;
});
const { ServerSetup } = await import(dataURL(`import React from '${reactURL}';\n${source}`));

const draft = { purpose: 'web', panel_domain: 'panel.example.com', mail_hostname: 'mail.example.com', dns_mode: 'external', remote_dns_connection_id: '', dns_engine: 'bind', dns_role: 'primary', ns1: '', ns2: '', local_ip: '', peer_ip: '', peer_ns: '', node_version: '', database: 'mariadb' };
const steps = [
    { id: 'svc', kind: 'service', target: 'postfix', status: 'succeeded' },
    { id: 'verify', kind: 'verify', target: 'web', status: 'pending' },
];
const ptrCheck = { id: 'mail_identity', state: 'action_required', code: 'mail_identity_required', reason: 'reverse_dns_mismatch', vars: { hostname: 'mail.example.com', ip: '203.0.113.42', ptr: 'static.42.provider.example' } };
const run = (extra) => ({ id: 'c'.repeat(32), request_id: 'b'.repeat(32), plan_id: 'a'.repeat(32), phase: 'verification', steps, ...extra });
const scenarios = {
    // The owner's state: the plan was reopened while the final check waited for PTR.
    reopened: { snapshot: 'draft', execution: run({ status: 'failed', error: { code: 'server_setup_plan_revised', message: 'The administrator reopened the plan. Completed host changes remain in place.' }, checks: [ptrCheck] }) },
    waitingOwner: { snapshot: 'waiting', execution: run({ status: 'waiting', checks: [ptrCheck] }) },
    prerequisite: { snapshot: 'waiting', execution: run({ status: 'waiting', checks: [{ id: 'mail_delivery', state: 'action_required', code: 'mail_delivery_required' }] }) },
    unknown: { snapshot: 'waiting', execution: run({ status: 'waiting', checks: [{ id: 'mail_identity', state: 'unknown', code: 'mail_identity_unavailable' }] }) },
    failed: { snapshot: 'draft', execution: run({ status: 'failed', phase: 'firewall', steps: [{ id: 'fw', kind: 'firewall', target: 'nftables', status: 'failed' }, { id: 'verify', kind: 'verify', target: 'web', status: 'pending' }], error: { code: 'server_setup_firewall_failed', message: 'firewall apply failed' } }) },
};

const originals = { fetch: globalThis.fetch, window: globalThis.window, localStorage: globalThis.localStorage, sessionStorage: globalThis.sessionStorage };
async function mount(name, locale) {
    const scenario = scenarios[name];
    const state = { version: 1, revision: 3, origin: 'fresh', status: scenario.snapshot, required: true, draft, checks: [] };
    globalThis.finalCheck = { locale, catalogue: catalogues[locale], context: { snapshot: state, accept() {}, async reload() { return state; } } };
    const store = new Map();
    globalThis.localStorage = { getItem: key => store.get(key) ?? null, setItem: (key, value) => store.set(key, value), removeItem: key => store.delete(key) };
    globalThis.sessionStorage = { getItem: () => null, setItem() {}, removeItem() {} };
    globalThis.window = { setTimeout, clearTimeout, setInterval, clearInterval, location: { hostname: 'panel.example.com', port: '2083', reload() {} } };
    const calls = [];
    globalThis.fetch = async (url, options) => {
        calls.push({ url, method: options?.method || 'GET' });
        if (String(url).includes('/setup/operation')) return Response.json(scenario.execution);
        return Response.json(state);
    };
    let tree;
    await act(async () => { tree = Renderer.create(React.createElement(ServerSetup)); });
    return { tree, calls };
}
async function unmount(tree) {
    await act(async () => tree.unmount());
    for (const [key, value] of Object.entries(originals)) { if (value === undefined) delete globalThis[key]; else globalThis[key] = value; }
    delete globalThis.finalCheck;
}
// The page's text in document order, so "before the list" can be measured.
function textOf(node) {
    if (node === null || node === undefined || typeof node === 'boolean') return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(textOf).join(' ');
    return textOf(node.children);
}
const before = (page, text, list) => { const at = page.indexOf(text); assert.ok(at >= 0 && at < list, 'missing or after the step list: ' + text); };
const insideDetails = (tree, instance) => { for (let node = instance.parent; node; node = node.parent) if (node.type === 'details') return true; return false; };
const verifyState = tree => tree.root.findAll(node => node.props && node.props['data-step-state'] !== undefined).at(-1).props['data-step-state'];

for (const locale of ['en', 'tr']) {
    const c = catalogues[locale];
    test(`${locale}: a run stopped by reopening its plan says so, names the reverse DNS it waited for, and no step is in progress`, async () => {
        const { tree, calls } = await mount('reopened', locale);
        try {
            const page = textOf(tree.toJSON());
            const list = page.indexOf(c['setup.kind.verify']);
            assert.ok(page.includes(c['setup.guide.revisedTitle']), 'the heading names why it stopped');
            assert.ok(!page.includes(c['setup.guide.failedTitle']) && !page.includes(c['setup.guide.failed']), 'not said as a failure');
            assert.ok(!page.includes(c['setup.blocker.unknown']), 'no generic "a required check needs attention"');
            const ptr = c['setup.check.mailIdentity.reverseDNS'].replace('{ip}', '203.0.113.42').replace('{ptr}', 'static.42.provider.example').replaceAll('{hostname}', 'mail.example.com').replaceAll('{ip}', '203.0.113.42');
            assert.ok(page.includes(ptr), 'the PTR sentence with the observed values');
            for (const key of ['setup.guide.revised', 'setup.guide.revisedChecks', 'setup.guide.revisedNext']) {
                assert.ok(page.indexOf(c[key]) >= 0 && page.indexOf(c[key]) < list, `${key} before the step list`);
            }
            before(page, ptr, list);
            assert.ok(page.indexOf(c['setup.guide.revisedChecks']) < page.indexOf(ptr), 'the sentence that introduces the check comes first');
            assert.ok(page.indexOf(ptr) < page.indexOf(c['setup.guide.revisedNext']), 'the next action follows the reason');
            const line = tree.root.findByProps({ 'data-check': 'mail_identity' });
            assert.equal(insideDetails(tree, line), false, 'the reason is not folded away');
            assert.equal(verifyState(tree), 'stopped');
            assert.ok(page.includes(c['setup.stepState.stopped']) && !page.includes(c['setup.operation.running']));
            // Technical details keep only the technical line.
            const details = tree.root.findAllByType('details').map(item => textOf(item.children.map(child => child.children ?? child)));
            assert.ok(details.some(text => text.includes('The administrator reopened the plan.')));
            assert.ok(calls.every(call => call.method === 'GET'), 'reading the page changes nothing');
        } finally { await unmount(tree); }
    });

    test(`${locale}: a final check waiting for the owner names the reverse DNS, who sets it and how setup resumes, above the list`, async () => {
        const { tree } = await mount('waitingOwner', locale);
        try {
            const page = textOf(tree.toJSON());
            const list = page.indexOf(c['setup.kind.verify']);
            before(page, c['setup.guide.verificationWaiting'], list);
            before(page, c['setup.guide.verificationResume'], list);
            assert.ok(page.includes('203.0.113.42') && page.indexOf('static.42.provider.example') < list);
            assert.equal(insideDetails(tree, tree.root.findByProps({ 'data-check': 'mail_identity' })), false);
            assert.equal(verifyState(tree), 'waitingOwner');
            assert.ok(page.includes(c['setup.stepState.waitingOwner']));
            assert.equal(tree.root.findAllByType('details').some(item => textOf(item.children.map(child => child.children ?? child)).includes(c['setup.guide.more'])), false, 'no folded "Checks and how to continue"');
        } finally { await unmount(tree); }
    });

    test(`${locale}: an unmet prerequisite without a typed reason keeps its own sentence and its help, unfolded`, async () => {
        const { tree } = await mount('prerequisite', locale);
        try {
            const page = textOf(tree.toJSON());
            const list = page.indexOf(c['setup.kind.verify']);
            assert.ok(page.indexOf(c['setup.blocker.mailDelivery']) >= 0 && page.indexOf(c['setup.blocker.mailDelivery']) < list);
            before(page, c['setup.guide.mailDelivery'], list);
            assert.equal(verifyState(tree), 'waitingRequirement');
            assert.ok(page.includes(c['setup.verifyWaiting']));
        } finally { await unmount(tree); }
    });

    test(`${locale}: a check that could not be read is said as unknown, not as missing`, async () => {
        const { tree } = await mount('unknown', locale);
        try {
            const page = textOf(tree.toJSON());
            const unknown = c['setup.check.notRead'].replace('{check}', c['setup.check.name.mail_identity']);
            assert.ok(page.indexOf(unknown) >= 0 && page.indexOf(unknown) < page.indexOf(c['setup.kind.verify']));
            assert.equal(verifyState(tree), 'unknown');
            assert.ok(page.includes(c['setup.stepState.unknown']));
        } finally { await unmount(tree); }
    });

    test(`${locale}: a verified step failure is said as failed, and the final check as not started`, async () => {
        const { tree } = await mount('failed', locale);
        try {
            const page = textOf(tree.toJSON());
            assert.ok(page.includes(c['setup.guide.failedTitle']));
            const states = tree.root.findAll(node => node.props && node.props['data-step-state'] !== undefined).map(node => node.props['data-step-state']);
            assert.deepEqual(states, ['failed', 'notStarted']);
            assert.ok(page.includes(c['setup.stepState.failed']) && page.includes(c['setup.stepState.notStarted']));
            assert.ok(!page.includes(c['setup.operation.running']));
        } finally { await unmount(tree); }
    });
}
