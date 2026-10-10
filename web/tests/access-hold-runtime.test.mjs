import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

// Owner report, 2026-10-08, on a released panel: "When I leave the page for a
// while it goes to 'License status could not be checked'. When I come back it
// returns to the page, but not where I left it." The license was fine.
//
// What these tests hold: a gate replaces the screen only on a KNOWN negative. An
// unknown or merely not yet refreshed state keeps the mounted page, with what
// the owner typed, and makes it unreachable until the server confirms access.
// Component tests only; timer throttling and focus events need a browser.
//
// Sahip bildirimi, 8 Ekim 2026: sayfadan ayrilinca "Lisans durumu kontrol
// edilemedi" ekrani geliyor, donunce sayfa kaldigi yerde olmuyordu. Bu testler,
// ekrani yalnizca BILINEN olumsuz sonucun degistirdigini sabitler.
const require = createRequire(import.meta.url);
const reactURL = pathToFileURL(require.resolve('react')).href;
const dataModule = source => 'data:text/javascript;base64,' + Buffer.from(source).toString('base64');
const source = path => readFileSync(new URL(path, import.meta.url), 'utf8');
const compile = path => ts.transpileModule(source(path), { compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText;
const link = (path, resolve) => dataModule(`import React from '${reactURL}';\n` + compile(path)
  .replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => `from '${specifier === 'react' ? reactURL : resolve(specifier)}'`)
  .replace(/import\(['"]([^'"]+)['"]\)/g, (_, specifier) => `import('${resolve(specifier)}')`));

const stub = dataModule(`import React from '${reactURL}';
 const fixture = () => globalThis.holdTest;
 export const useI18n = () => ({ t: key => key, locale: 'en', screensReady: fixture().screensReady !== false, screensFailed: false });
 export const useAuth = () => fixture().auth;
 export const useNavigate = () => fixture().navigate;
 export const useLocation = () => ({ pathname: fixture().pathname || '/settings' });
 export const api = { me: signal => fixture().me(signal), demoAccounts: async () => [] };
 export const createPortal = node => node;
 export const Button = props => React.createElement('button', props);
 export const Spinner = () => null;
 export const Dialog = props => React.createElement('dialog', { id: props.id, 'aria-busy': props.busy }, props.title, React.createElement('p', null, props.description), props.children, props.actions);
 export const RecoveryStatus = props => React.createElement('section', { 'data-operation': props.username, unfinishedOnly: props.unfinishedOnly });
 export const RecoveryAccess = props => React.createElement('aside', { cause: props.cause, checking: props.checking, user: props.user, onRetry: props.onRetry }, 'recovery');
 export const LicenseLockScreen = props => React.createElement('aside', { ...props, cause: 'lock' }, 'locked');
 export const usePanelHandover = (username, enabled) => (enabled && fixture().handover) || null;
 export const handoverAddress = host => 'https://' + host;
 export const PanelAddressHint = () => null, BrandMark = () => null, ThemeSwitcher = () => null, SkinSwitcher = () => null, LanguageSwitcher = () => null;
 export const ShieldCheck = () => null, Users = () => null, User = () => null, Eye = () => null, EyeOff = () => null;
 export const useAccessGuidance = () => null;
 export const AddressLink = props => React.createElement('a', { href: props.href }, props.address);
`);
const accessURL = dataModule(compile('../src/lib/accessObservation.ts'));
// The first-read quiet time, the update hint and the error reader are the real ones (2026-10-10).
const quietURL = link('../src/lib/quietRead.ts', () => stub);
const observationURL = dataModule(compile('../src/lib/recoveryObservation.ts'));
const apiErrorURL = dataModule(compile('../src/lib/apiError.ts'));
const real = (specifier, otherwise) => specifier.endsWith('/quietRead') ? quietURL : specifier.endsWith('/recoveryObservation') ? observationURL
  : specifier.endsWith('/apiError') ? apiErrorURL : otherwise(specifier);
const guidanceURL = link('../src/components/AccessGuidance.tsx', () => stub);
const loaderURL = link('../src/lib/accessGuidance.ts', specifier => specifier.endsWith('/AccessGuidance') ? guidanceURL : stub);
const withGuidance = specifier => real(specifier, item => item.endsWith('/accessGuidance') ? loaderURL : stub);
const holdURL = link('../src/components/AccessHold.tsx', withGuidance);
const bareHoldURL = link('../src/components/AccessHold.tsx', specifier => real(specifier, () => stub));
const onboardingURL = link('../src/components/LicenseOnboarding.tsx', specifier => real(specifier, item => item.endsWith('/accessObservation') ? accessURL : item.endsWith('/AccessHold') ? holdURL : stub));
const loginURL = link('../src/components/Login.tsx', withGuidance);
const noticeURL = link('../src/components/UpdateReloadNotice.tsx', withGuidance);
const bareNoticeURL = link('../src/components/UpdateReloadNotice.tsx', () => stub);
const sessionURL = link('../src/auth/usePanelSession.ts', () => stub);
// The interception sends every call through the request identity (D-029); the
// gate is tested with the real one.
const identityURL = dataModule(compile('../src/lib/requestIdentity.ts'));
const app = source('../src/App.tsx');
const gateURL = dataModule(`import React, { useCallback, useState, useEffect, useLayoutEffect, useRef, Suspense } from '${reactURL}';
 import { usePanelSession } from '${sessionURL}';
 import { AccessHold } from '${holdURL}';
 import { Login } from '${loginURL}';
 import { sendIdentified } from '${identityURL}';
 const fixture = () => globalThis.holdTest;
 const RecoveryAccess = props => React.createElement('aside', { cause: props.cause, checking: props.checking, user: props.user }, 'recovery');
 const AuthProvider = ({ user, onLogout, children }) => { fixture().logout = onLogout; return React.createElement('section', { 'data-user': user.username }, children); };
 const LicenseOnboarding = ({ children, suspended }) => React.createElement('article', { suspended }, children);
 const PageLoading = () => null, ComponentOperationProvider = ({ children }) => children, ServerSetupGate = ({ children }) => children;
 const AppRoutes = () => React.createElement(fixture().Routes);
 const publishSystemUpdateAuthentication = value => fixture().published.push(value);
 const shouldApplyUnauthorizedResponse = (request, current) => request === current;
 ${ts.transpileModule(app.slice(app.indexOf('function AuthGate()'), app.indexOf('function StandaloneRecovery(')) + '\nexport { AuthGate };', { compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText}`);

const { AccessHold, ACCESS_HOLD_QUIET_MS, ACCESS_HOLD_PROLONGED_MS } = await import(holdURL);
const { AccessHold: BareAccessHold } = await import(bareHoldURL);
const { LicenseOnboarding } = await import(onboardingURL);
const { Login } = await import(loginURL);
const { UpdateReloadNotice, UPDATE_RELOAD_EVENT, UPDATE_RELOAD_DELAY_MS } = await import(noticeURL);
const { UpdateReloadNotice: BareUpdateReloadNotice } = await import(bareNoticeURL);
const { loadAccessGuidance } = await import(loaderURL);
const { AuthGate } = await import(gateURL);
const guidance = await loadAccessGuidance();

const realTimeout = setTimeout;
const originalFetch = globalThis.fetch;
const admin = { username: 'admin', effective_role: 'admin' };
const available = state => Response.json({ schema: 'celikpanel-panel-availability/v1', state });
let tree, timers, reloads, history;
globalThis.HTMLElement = class {};
globalThis.document = Object.assign(new EventTarget(), { visibilityState: 'visible', activeElement: null, body: {} });
globalThis.window = Object.assign(new EventTarget(), {
  clearTimeout, setInterval, clearInterval,
  // The quiet time, the half minute and the reload delay are fired by hand.
  setTimeout: (fn, ms) => { if ([ACCESS_HOLD_QUIET_MS, ACCESS_HOLD_PROLONGED_MS, UPDATE_RELOAD_DELAY_MS].includes(ms)) { const timer = { fn, ms, live: true }; timers.push(timer); return timer; } return realTimeout(fn, ms); },
  location: { port: '', reload() { reloads++; }, assign() { history.push('assign'); }, replace() { history.push('replace'); } },
  history: { pushState() { history.push('pushState'); }, replaceState() { history.push('replaceState'); } },
});
window.clearTimeout = timer => { if (timer && typeof timer === 'object' && 'live' in timer) timer.live = false; else clearTimeout(timer); };
Object.defineProperty(window, 'fetch', { get: () => globalThis.fetch, set: value => { globalThis.fetch = value; } });

function fixture(extra = {}) {
  timers = []; reloads = 0; history = []; tree = undefined;
  globalThis.holdTest = { auth: { role: 'admin', user: admin }, navigations: [], published: [], navigate(...args) { globalThis.holdTest.navigations.push(args); return true; }, ...extra };
  document.visibilityState = 'visible';
}
async function clean() {
  if (tree) await act(async () => tree.unmount());
  tree = undefined; globalThis.fetch = originalFetch; delete globalThis.holdTest;
}
const fire = async ms => { const due = timers.filter(timer => timer.live && timer.ms === ms); for (const timer of due) timer.live = false; await act(async () => { for (const timer of due) timer.fn(); }); return due.length; };
const show = async state => { document.visibilityState = state; await act(async () => document.dispatchEvent(new Event('visibilitychange'))); };
const text = () => JSON.stringify(tree.toJSON());
const held = () => tree.root.findAllByProps({ 'data-access-hold': 'blocked' }).filter(node => typeof node.type === 'string');
const layers = () => tree.root.findAllByType('dialog');
const button = label => tree.root.findAllByType('button').find(node => [node.props.children].flat().includes(label));

// A page with something the owner did to it: typed input, a selected tab, an open dialogue.
let mounts;
function Page() {
  const [typed, setTyped] = React.useState('');
  const [tab, setTab] = React.useState('general');
  const [open, setOpen] = React.useState(false);
  React.useEffect(() => { mounts++; }, []);
  return React.createElement('main', { 'data-tab': tab },
    React.createElement('input', { value: typed, onChange: event => setTyped(event.target.value) }),
    React.createElement('nav', { onSelect: setTab }),
    React.createElement('summary', { onToggle: () => setOpen(true) }),
    open && React.createElement('form', null, 'open dialogue'));
}
async function useThePage() {
  await act(async () => tree.root.findByType('input').props.onChange({ target: { value: 'mail.example.com' } }));
  await act(async () => tree.root.findByType('nav').props.onSelect('dns'));
  await act(async () => tree.root.findByType('summary').props.onToggle());
}
function assertPageAsLeft(message) {
  assert.equal(tree.root.findByType('input').props.value, 'mail.example.com', `${message}: typed input`);
  assert.equal(tree.root.findByType('main').props['data-tab'], 'dns', `${message}: selected tab`);
  assert.equal(tree.root.findAllByType('form').length, 1, `${message}: open dialogue`);
  assert.equal(mounts, 1, `${message}: the page was never rebuilt`);
}
const access = (seconds = 60) => Response.json({ can_use_panel: true, valid_until: Math.floor(Date.now() / 1000) + seconds, state: 'active', observation: 'known' });

test('the wording exists in both languages, in the screen half, and says reason, who acts and how the page resumes', () => {
  const keys = ['licenseTitle', 'licenseHelp', 'availabilityTitle', 'availabilityHelp', 'authTitle', 'authHelp', 'waitingHelp', 'resume', 'prolonged', 'sessionEnded', 'updateReload', 'updateReloadTitle', 'updateTitle', 'updateHelp'].map(name => `accessHold.${name}`);
  const value = (file, key) => source(`../src/i18n/${file}.ts`).match(new RegExp(`'${key.replace('.', '\\.')}': "([^"]+)",`))?.[1];
  for (const key of keys) {
    assert.ok(value('screens/en', key) && value('screens/tr', key), `${key} is missing from the screen half`);
    for (const shell of ['en', 'tr']) assert.equal(value(shell, key), undefined, `${key} must not grow the boot payload`);
  }
  // Unknown is not a verdict on the license or the session.
  assert.match(value('screens/en', 'accessHold.licenseHelp'), /does not mean your license is missing or expired/);
  assert.match(value('screens/tr', 'accessHold.licenseHelp'), /lisansınızın eksik veya süresinin dolmuş olduğu anlamına gelmez/);
  assert.match(value('screens/en', 'accessHold.authHelp'), /does not mean you were signed out/);
  assert.match(value('screens/tr', 'accessHold.authHelp'), /oturumunuzun kapatıldığı anlamına gelmez/);
  // Who acts (nobody yet), what happens by itself, how work resumes, what is blocked meanwhile.
  assert.match(value('screens/en', 'accessHold.resume'), /do not need to do anything yet.*checks again by itself.*continues where it was, with what you typed.*nothing on this page can be changed/s);
  assert.match(value('screens/tr', 'accessHold.resume'), /bir şey yapmanız gerekmiyor.*kendiliğinden yeniden kontrol eder.*yazdıklarınızla birlikte kaldığı yerden devam eder.*değişiklik yapılamaz/s);
  // The re-check runs every 5 s for a license read and every 10 s for the session, so no interval is stated.
  for (const file of ['screens/en', 'screens/tr']) assert.doesNotMatch(value(file, 'accessHold.resume'), /\d|every few|saniye/, file);
  // Reload is offered with its cost.
  assert.match(value('screens/en', 'accessHold.prolonged'), /keep waiting.*reload CelikPanel.*discards anything you typed/s);
  assert.match(value('screens/tr', 'accessHold.prolonged'), /Beklemeyi sürdürebilirsiniz.*yeniden yükleyebilirsiniz.*yazıp kaydetmediğiniz her şeyi siler/s);
  assert.match(value('screens/en', 'accessHold.sessionEnded'), /session ended.*Sign in to return to the page you were on.*not kept/s);
  assert.match(value('screens/tr', 'accessHold.sessionEnded'), /Oturumunuz sona erdi.*dönmek için giriş yapın.*korunmadı/s);
  assert.match(value('screens/en', 'accessHold.updateReload'), /most likely because CelikPanel was updated.*reloads in a moment/s);
  assert.match(value('screens/tr', 'accessHold.updateReload'), /büyük olasılıkla.*güncellendi.*birazdan yeniden yüklenir/s);
  // The reload says what it costs.
  assert.match(value('screens/en', 'accessHold.updateReload'), /Anything you typed on this page and did not save is lost\.$/);
  assert.match(value('screens/tr', 'accessHold.updateReload'), /yazıp kaydetmediğiniz her şey kaybolur\.$/);
  // No state borrows a verdict it does not have.
  for (const key of keys.slice(0, 9)) for (const file of ['screens/en', 'screens/tr']) assert.doesNotMatch(value(file, key), /activate|renew|etkinleştir|yenileyin/i, key);
  // The copy function maps each cause to its own reason and keeps "starting" on the shell's reviewed text.
  const t = key => key;
  assert.deepEqual(guidance.accessHoldCopy(t, 'license', false), { title: 'accessHold.licenseTitle', help: 'accessHold.licenseHelp', resume: 'accessHold.resume', prolonged: 'accessHold.prolonged' });
  assert.equal(guidance.accessHoldCopy(t, 'auth', false).title, 'accessHold.authTitle');
  assert.equal(guidance.accessHoldCopy(t, 'availability', false).help, 'accessHold.availabilityHelp');
  assert.deepEqual(guidance.accessHoldCopy(t, 'update', false), { title: 'accessHold.updateTitle', help: 'accessHold.updateHelp', resume: 'accessHold.resume', prolonged: 'accessHold.prolonged' });
  assert.deepEqual([guidance.accessHoldCopy(t, 'starting', false).title, guidance.accessHoldCopy(t, 'starting', false).help], ['recovery.startingTitle', 'recovery.startingHelp']);
  assert.deepEqual(guidance.accessHoldCopy(t, 'license', true), { title: 'recovery.checkingTitle', help: 'accessHold.waitingHelp', resume: '', prolonged: 'accessHold.prolonged' });
});

test('a held page stays mounted and unreachable; a prompt answer shows nothing, a slow one says checking, a failed one explains', async () => {
  fixture(); mounts = 0; let retries = 0;
  const render = props => React.createElement(AccessHold, { cause: 'license', user: admin, onRetry: () => { retries++; }, ...props }, React.createElement(Page));
  try {
    await act(async () => { tree = Renderer.create(render({ active: false, checking: false })); });
    await useThePage();
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    const wrapper = tree.root.findByProps({ className: 'contents' });
    assert.equal(wrapper.props.inert, undefined);

    // Access became unknown and a read is in flight: unreachable at once, nothing drawn.
    await act(async () => tree.update(render({ active: true, checking: true })));
    assert.equal(held().length, 1);
    assert.equal(wrapper.props.inert, '', 'the pages are inert');
    assert.equal(wrapper.props['aria-hidden'], true);
    assert.equal(layers().length, 0, 'a read that may answer promptly is not announced');
    assertPageAsLeft('quiet hold');
    // The read answers in time: the hold ends without ever having been shown.
    await act(async () => tree.update(render({ active: false, checking: false })));
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    assert.equal(await fire(ACCESS_HOLD_QUIET_MS), 0, 'the quiet timer of a finished hold is cancelled');

    // The read is slow: after the quiet time the layer says "checking", never "could not".
    await act(async () => tree.update(render({ active: true, checking: true })));
    assert.equal(await fire(ACCESS_HOLD_QUIET_MS), 1);
    assert.equal(layers().length, 1);
    assert.ok(text().includes('recovery.checkingTitle') && text().includes('accessHold.waitingHelp'), text());
    for (const absent of ['accessHold.licenseTitle', 'accessHold.licenseHelp', 'accessHold.resume', 'data-operation']) assert.ok(!text().includes(absent), absent);
    // Nothing more to say yet: the dialogue has no body, so no empty band is drawn between two hairlines.
    assert.equal(layers()[0].findAllByProps({ role: 'status' }).length, 0, 'an empty status region still makes a body');
    assert.equal(button('recovery.checking').props.loading, true);

    // The read answered without confirming access: reason, nobody acts yet, how the page resumes.
    await act(async () => tree.update(render({ active: true, checking: false })));
    for (const present of ['accessHold.licenseTitle', 'accessHold.licenseHelp', 'accessHold.resume']) assert.ok(text().includes(present), present);
    assert.ok(!text().includes('recovery.licenseTitle') && !text().includes('accessHold.prolonged'));
    assert.equal(layers()[0].findAllByProps({ role: 'status' }).length, 1);
    assert.equal(button('app.reload'), undefined, 'reload is not suggested while waiting is the right thing');
    assert.equal(tree.root.findByProps({ 'data-operation': 'admin' }).props.unfinishedOnly, true, 'only an unfinished operation may be drawn');
    await act(async () => button('recovery.retry').props.onClick());
    assert.equal(retries, 1);
    // The automatic read repeats; it is not drawn as busy unless the owner asked.
    await act(async () => tree.update(render({ active: true, checking: true })));
    assert.equal(button('recovery.checking').props.loading, true, 'the read the owner asked for is shown');
    await act(async () => tree.update(render({ active: true, checking: false })));
    await act(async () => tree.update(render({ active: true, checking: true })));
    assert.equal(button('recovery.retry').props.loading, false, 'a background read does not flicker the control');
    await act(async () => tree.update(render({ active: true, checking: false })));

    // Still unknown after the bounded time: the recovery page's actions, without leaving the page.
    assert.equal(await fire(ACCESS_HOLD_PROLONGED_MS), 1);
    assert.ok(text().includes('accessHold.prolonged'));
    await act(async () => button('app.reload').props.onClick());
    assert.equal(reloads, 1);
    assert.equal(held().length, 1);
    assertPageAsLeft('explained hold');

    // Leaving the tab draws nothing; returning starts one read and is quiet again.
    await show('hidden');
    assert.equal(layers().length, 0); assert.equal(held().length, 1);
    retries = 0;
    await act(async () => { document.visibilityState = 'visible'; document.dispatchEvent(new Event('visibilitychange')); tree.update(render({ active: true, checking: true })); });
    assert.equal(retries, 1, 'returning reads once');
    assert.equal(layers().length, 0, 'and says nothing while that read is in flight');
    await act(async () => tree.update(render({ active: false, checking: false })));
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    assertPageAsLeft('after the hold');
  } finally { await clean(); }
});

test('an unknown session shows no saved operation; an outer hold silences an inner one; missing wording falls back to neutral shell text', async () => {
  fixture(); mounts = 0;
  try {
    await act(async () => { tree = Renderer.create(React.createElement(AccessHold, { active: true, checking: false, cause: 'auth', user: null, onRetry() {} }, React.createElement(Page))); });
    assert.ok(text().includes('accessHold.authTitle') && text().includes('accessHold.authHelp'));
    assert.equal(tree.root.findAllByProps({ 'data-operation': 'admin' }).length, 0, 'unknown authentication reveals no administrator observation');
    await clean(); fixture();
    await act(async () => { tree = Renderer.create(React.createElement(AccessHold, { active: true, silent: true, checking: false, cause: 'license', user: admin, onRetry() {} }, React.createElement(Page))); });
    assert.equal(held().length, 1); assert.equal(layers().length, 0);
    await clean(); fixture();
    // The wording never arrived, or the screen copy is not ready: block, and say only that access is being checked.
    for (const [Component, extra] of [[BareAccessHold, {}], [AccessHold, { screensReady: false }]]) {
      fixture(extra);
      await act(async () => { tree = Renderer.create(React.createElement(Component, { active: true, checking: false, cause: 'license', user: admin, onRetry() {} }, React.createElement(Page))); });
      assert.equal(held().length, 1);
      assert.ok(text().includes('recovery.checkingTitle') && text().includes('recovery.checkingHelp'), text());
      assert.ok(!text().includes('accessHold.'), text());
      assert.ok(button('recovery.retry'));
      await clean();
    }
    // The planned restart during setup keeps its own explanation over the wizard.
    fixture({ handover: { host: 'panel.example.com', elsewhere: true } });
    await act(async () => { tree = Renderer.create(React.createElement(AccessHold, { active: true, checking: false, cause: 'starting', user: admin, onRetry() {} }, React.createElement(Page))); });
    assert.ok(text().includes('recovery.handoverTitle') && text().includes('recovery.handoverHelp') && !text().includes('accessHold.resume'), text());
    assert.equal(tree.root.findByType('a').props.href, 'https://panel.example.com/setup');
  } finally { await clean(); }
});

test('events aimed at held pages are stopped and focus is taken out again, for a browser without inert', async () => {
  fixture(); mounts = 0;
  const node = Object.assign(new EventTarget(), { contains: target => target?.inside === true });
  const render = active => React.createElement(AccessHold, { active, checking: true, cause: 'license', user: admin, onRetry() {} }, React.createElement(Page));
  const deliver = type => { const event = new Event(type, { cancelable: true }); node.dispatchEvent(event); return event.defaultPrevented; };
  try {
    await act(async () => { tree = Renderer.create(render(false), { createNodeMock: element => element.props.className === 'contents' ? node : null }); });
    for (const type of ['click', 'keydown', 'submit', 'input', 'paste']) assert.equal(deliver(type), false, `${type} is delivered to a page that is not held`);
    await act(async () => tree.update(render(true)));
    for (const type of ['click', 'dblclick', 'mousedown', 'pointerdown', 'keydown', 'submit', 'input', 'paste', 'drop']) assert.equal(deliver(type), true, `${type} must not reach a held page`);
    // Focus that lands inside the held pages is removed; focus elsewhere is left alone.
    let blurred = 0;
    const inside = Object.assign(new globalThis.HTMLElement(), { inside: true, blur() { blurred++; } });
    const outside = Object.assign(new globalThis.HTMLElement(), { inside: false, blur() { blurred += 100; } });
    for (const target of [inside, outside]) { const event = new Event('focusin'); Object.defineProperty(event, 'target', { value: target }); document.dispatchEvent(event); }
    assert.equal(blurred, 1);
    await act(async () => tree.update(render(false)));
    assert.equal(deliver('click'), false, 'released with the hold');
  } finally { await clean(); }
});

test('a decision that ran out in a hidden tab is refreshed silently on return; the page is where the owner left it', async () => {
  fixture(); mounts = 0;
  const previousTimer = window.setTimeout, previousNow = Date.now;
  const deadlines = []; let now = Date.now(), reads = 0, reply = () => access();
  Date.now = () => now;
  window.setTimeout = (fn, ms) => { if (ms > 15000 && ms !== ACCESS_HOLD_PROLONGED_MS) { deadlines.push({ fn, ms }); return 0; } return previousTimer(fn, ms); };
  globalThis.fetch = async () => { reads++; return reply(); };
  try {
    await act(async () => { tree = Renderer.create(React.createElement(LicenseOnboarding, null, React.createElement(Page))); });
    await useThePage();
    assert.equal(reads, 1);
    // The owner leaves; the tab is hidden for longer than the decision lasts.
    await show('hidden');
    now += 60000;
    await act(async () => deadlines.at(-1).fn());
    assert.equal(reads, 1, 'a hidden tab does not read');
    assert.equal(held().length, 1, 'and no management control is reachable without a current decision');
    assert.equal(layers().length, 0); assert.equal(tree.root.findAllByType('aside').length, 0, 'no gate replaces the page');
    assertPageAsLeft('hidden lapse');
    // The owner returns; the server answers at once.
    await show('visible');
    assert.equal(reads, 2, 'one read on return');
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    assert.equal(await fire(ACCESS_HOLD_QUIET_MS), 0, 'nothing was about to be shown');
    assertPageAsLeft('after returning');
    assert.equal(globalThis.holdTest.navigations.length, 0);

    // A throttled timer that never fired: on return the deadline is applied by the clock before the read answers.
    let answer; reply = () => new Promise(done => { answer = done; });
    now += 61000;
    await act(async () => window.dispatchEvent(new Event('focus')));
    assert.equal(held().length, 1, 'a decision past its deadline is not used while its replacement is in flight');
    assert.equal(layers().length, 0);
    await act(async () => answer(access()));
    assert.equal(held().length, 0);
    assertPageAsLeft('after a throttled timer');

    // Returning to a server that does not answer: the page is kept and the state explained.
    await show('hidden');
    now += 60000;
    await act(async () => deadlines.at(-1).fn());
    reply = () => { throw new TypeError('Failed to fetch'); };
    await show('visible');
    assert.equal(held().length, 1); assert.equal(layers().length, 1);
    // No answer arrived: the Panel did not answer; the license is not named (2026-10-10).
    assert.ok(text().includes('accessHold.availabilityTitle') && text().includes('accessHold.availabilityHelp') && text().includes('accessHold.resume'), text());
    assert.ok(!text().includes('accessHold.license'), text());
    assert.equal(tree.root.findAllByType('aside').length, 0);
    assert.equal(globalThis.holdTest.navigations.length, 0, 'unknown is not an activation requirement');
    assertPageAsLeft('explained hold');
    // It answers again: released, same page.
    reply = () => access();
    await act(async () => button('recovery.retry').props.onClick());
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    assertPageAsLeft('after the server answered');
  } finally { Date.now = previousNow; window.setTimeout = previousTimer; await clean(); }
});

// Seventh native record, cell 1 (2026-10-10): during the Panel's planned restart
// in an update the layer said "CelikPanel could not read the license result",
// although the Panel itself was not answering. The layer names the cause the read
// showed: no answer from the Panel (and, while this browser's update has not
// recorded its end, that update's restart), the Panel starting, and the license
// only when the Panel answered and its license result was what could not be read.
// Katman, okumanin gosterdigi nedeni soyler; lisans yalnizca Panel yanit verip
// lisans sonucu okunamadiginda anilir.
test('the hold layer names the cause the read showed: an update restart, an unanswered or starting Panel, the license only when the Panel answered', async () => {
  const id = 'e'.repeat(32);
  const record = (phase, extra = {}) => JSON.stringify({ state_version: 1, phase, marker: { marker_version: 1, request_id: id }, ...extra });
  const running = record('active'), finished = record('terminal', { outcome: 'succeeded' });
  const dropped = () => { throw new TypeError('Failed to fetch'); };
  const unavailable = () => Response.json({ can_use_panel: false, valid_until: 0, state: 'status_unavailable', observation: 'unavailable' });
  const previousStorage = globalThis.localStorage, previousTimer = window.setTimeout, previousNow = Date.now;
  for (const [saved, reply, title, help] of [
    // An update started here has not recorded its end and the Panel does not answer: its restart.
    [running, dropped, 'accessHold.updateTitle', 'accessHold.updateHelp'],
    [running, () => new Response('Bad gateway', { status: 502 }), 'accessHold.updateTitle', 'accessHold.updateHelp'],
    // No update in progress here, or one already finished: the Panel did not answer.
    [null, dropped, 'accessHold.availabilityTitle', 'accessHold.availabilityHelp'],
    [finished, dropped, 'accessHold.availabilityTitle', 'accessHold.availabilityHelp'],
    // The Panel answered that it is starting.
    [running, () => Response.json({ code: 'PANEL_STARTING' }, { status: 503 }), 'recovery.startingTitle', 'recovery.startingHelp'],
    // The Panel answered and could not read its license result: the license, also during an update.
    [null, unavailable, 'accessHold.licenseTitle', 'accessHold.licenseHelp'],
    [running, unavailable, 'accessHold.licenseTitle', 'accessHold.licenseHelp'],
  ]) {
    fixture(); mounts = 0;
    const deadlines = []; let now = Date.now(), current = () => access();
    Date.now = () => now;
    window.setTimeout = (fn, ms) => { if (ms > 15000 && ms !== ACCESS_HOLD_PROLONGED_MS) { deadlines.push({ fn, ms }); return 0; } return previousTimer(fn, ms); };
    globalThis.localStorage = { getItem: key => (key === 'celikpanel.system-update-operation.v1' ? saved : null) };
    globalThis.fetch = async () => current();
    try {
      await act(async () => { tree = Renderer.create(React.createElement(LicenseOnboarding, null, React.createElement(Page))); });
      await useThePage();
      // The decision runs out while the Panel is restarting; the read made at the deadline does not confirm access.
      current = reply; now += 60000;
      await act(async () => deadlines.at(-1).fn());
      await act(async () => {});
      assert.equal(held().length, 1, title); assert.equal(layers().length, 1, title);
      const shown = text();
      assert.ok(shown.includes(title) && shown.includes(help) && shown.includes('accessHold.resume'), `${title}: ${shown}`);
      for (const other of ['accessHold.updateTitle', 'accessHold.availabilityTitle', 'accessHold.licenseTitle', 'recovery.startingTitle'].filter(key => key !== title)) {
        assert.ok(!shown.includes(other), `${title} and ${other}: ${shown}`);
      }
      assert.equal(globalThis.holdTest.navigations.length, 0, 'an unknown state is not an activation requirement');
      assertPageAsLeft(title);
      // Access confirmed again: the same page continues.
      current = () => access();
      await act(async () => button('recovery.retry').props.onClick());
      assert.equal(held().length, 0); assert.equal(layers().length, 0);
      assertPageAsLeft(`${title}, after the Panel answered`);
    } finally {
      Date.now = previousNow; window.setTimeout = previousTimer;
      if (previousStorage === undefined) delete globalThis.localStorage; else globalThis.localStorage = previousStorage;
      await clean();
    }
  }
});

test('a known negative decision removes management and requires activation exactly as before, also from a hold', async () => {
  for (const state of ['missing', 'expired', 'invalid']) {
    fixture(); mounts = 0;
    let reply = () => access();
    globalThis.fetch = async () => reply();
    try {
      await act(async () => { tree = Renderer.create(React.createElement(LicenseOnboarding, null, React.createElement(Page))); });
      await useThePage();
      // Unknown first: held, not removed.
      reply = () => Response.json({}, { status: 503 });
      await act(async () => window.dispatchEvent(new Event('celikpanel:license-locked')));
      assert.equal(held().length, 1); assert.equal(tree.root.findAllByType('main').length, 1);
      assert.equal(globalThis.holdTest.navigations.length, 0);
      // Then the server's typed verdict: the gate, as before.
      reply = () => Response.json({ can_use_panel: false, valid_until: 0, state, observation: 'known' });
      await act(async () => button('recovery.retry').props.onClick());
      assert.equal(tree.root.findAllByType('main').length, 0, `${state}: management is removed`);
      assert.equal(held().length, 0); assert.equal(layers().length, 0);
      assert.equal(tree.root.findByType('aside').props.cause, 'lock');
      assert.equal(tree.root.findByType('aside').props.checking, false);
      assert.deepEqual(globalThis.holdTest.navigations.at(-1), ['/activate', { replace: true }]);
      // A later positive decision mounts management afresh; nothing of the removed page is resurrected.
      reply = () => access();
      await act(async () => tree.root.findByType('aside').props.onCheck());
      assert.equal(tree.root.findAllByType('main').length, 1);
      assert.equal(tree.root.findByType('input').props.value, '');
    } finally { await clean(); }
  }
});

test('before the first positive decision nothing is mounted: a first read in flight draws nothing before the quiet time, then a checking state, and a failed one the full page with the cause it read', async () => {
  fixture(); mounts = 0; let answer;
  globalThis.fetch = () => new Promise(done => { answer = done; });
  const quietSurface = () => tree.root.findAll(node => typeof node.type === 'string' && node.props['data-access-quiet'] !== undefined);
  try {
    await act(async () => { tree = Renderer.create(React.createElement(LicenseOnboarding, null, React.createElement(Page))); });
    await act(async () => {});
    assert.equal(tree.root.findAllByType('main').length, 0);
    // Seventh native record, cell 5: before the quiet time only the page background, no text and no control.
    assert.equal(quietSurface().length, 1); assert.equal(tree.root.findAllByType('aside').length, 0);
    assert.equal(tree.root.findAllByType('button').length, 0);
    assert.equal(await fire(ACCESS_HOLD_QUIET_MS), 1, 'the same quiet time as the hold layer');
    const waiting = tree.root.findByType('aside');
    assert.equal(waiting.props.cause, 'lock'); assert.equal(waiting.props.checking, true); assert.equal(waiting.props.failed, false);
    // A status that is not the Panel's own access answer: the Panel is named, not the license.
    await act(async () => answer(Response.json({}, { status: 503 })));
    assert.equal(tree.root.findByType('aside').props.cause, 'availability', 'the application cannot start: the full recovery page');
    assert.equal(tree.root.findAllByType('main').length, 0); assert.equal(held().length, 0);
    assert.equal(mounts, 0);
  } finally { await clean(); }
  // An answer before the quiet time replaces the background at once, with the cause that answer showed.
  for (const [reply, cause] of [
    [() => Response.json({ can_use_panel: false, valid_until: 0, state: 'status_unavailable', observation: 'unavailable' }), 'license'],
    [() => Response.json({ code: 'PANEL_STARTING' }, { status: 503 }), 'starting'],
    [() => { throw new TypeError('Failed to fetch'); }, 'availability'],
  ]) {
    fixture(); mounts = 0;
    globalThis.fetch = async () => reply();
    try {
      await act(async () => { tree = Renderer.create(React.createElement(LicenseOnboarding, null, React.createElement(Page))); });
      assert.equal(quietSurface().length, 0, cause);
      assert.equal(tree.root.findByType('aside').props.cause, cause);
    } finally { await clean(); }
  }
});

// The whole gate: a session, readiness, management requests through the real
// fetch interception, and the sign-in form.
function Routes() { return React.createElement(Page); }
async function gate(replies) {
  fixture({ Routes, me: () => replies.me(), reads: 0 });
  globalThis.fetch = (url, options) => { globalThis.holdTest.requests = [...(globalThis.holdTest.requests ?? []), [String(url), options?.method ?? 'GET']]; return String(url).includes('/panel/availability') ? replies.availability() : replies.management(String(url)); };
  await act(async () => { tree = Renderer.create(React.createElement(AuthGate)); });
  await act(async () => {});
}
const problem = (status, code) => Response.json({ code }, { status });

test('the first session and readiness reads are a checking state; could not be checked needs a failed read', async () => {
  let identity, ready;
  const replies = { me: () => new Promise(done => { identity = done; }), availability: () => new Promise(done => { ready = done; }), management: async () => Response.json({}) };
  mounts = 0;
  try {
    await gate(replies);
    assert.equal(tree.root.findByType('aside').props.cause, 'checking', 'session read in flight');
    await act(async () => identity(admin));
    const page = tree.root.findByType('aside');
    assert.equal(page.props.cause, 'checking', 'readiness read in flight is not "could not be checked"');
    assert.equal(page.props.checking, true);
    assert.equal(tree.root.findAllByType('main').length, 0);
    await act(async () => ready(available('ready')));
    assert.equal(tree.root.findAllByType('aside').length, 0);
    assert.equal(tree.root.findAllByType('main').length, 1);
    await clean();
    // The same reads failing: reported as failed, on the full page, because nothing is mounted yet.
    await gate({ me: async () => admin, availability: async () => Response.json({}, { status: 503 }), management: async () => Response.json({}) });
    assert.equal(tree.root.findByType('aside').props.cause, 'availability');
    assert.equal(tree.root.findAllByType('main').length, 0); assert.equal(held().length, 0);
    await clean();
    // Signing in: the readiness read after it is a checking state too.
    let afterLogin;
    await gate({ me: async () => null, availability: () => new Promise(done => { afterLogin = done; }), management: async () => Response.json({}) });
    assert.equal(tree.root.findAllByType(Login).length, 1);
    assert.equal(tree.root.findByType(Login).props.sessionEnded, false);
    await act(async () => tree.root.findByType(Login).props.onSuccess(admin));
    assert.equal(tree.root.findByType('aside').props.cause, 'checking');
    await act(async () => afterLogin(available('ready')));
    assert.equal(tree.root.findAllByType('main').length, 1);
  } finally { await clean(); }
});

test('PANEL_STARTING or AUTH_STATUS_UNAVAILABLE from a background request holds the mounted page instead of replacing it', async () => {
  let state = 'ready', me = async () => admin;
  const replies = { me: () => me(), availability: async () => available(state), management: async () => Response.json({}) };
  mounts = 0;
  try {
    await gate(replies);
    await useThePage();
    assert.equal(globalThis.holdTest.published.at(-1), true);

    // The Panel restarts under the page.
    state = 'starting';
    replies.management = async () => problem(503, 'PANEL_STARTING');
    await act(async () => { await fetch('/api/v1/domains'); });
    await act(async () => {});
    assert.equal(tree.root.findAllByType('aside').length, 0, 'no full page replaces a mounted one');
    assert.equal(held().length, 1);
    assert.ok(text().includes('recovery.startingTitle'), text());
    assert.equal(tree.root.findByType('article').props.suspended, true, 'one layer explains; the license hold stays silent');
    assert.equal(globalThis.holdTest.published.at(-1), false, 'the update tracker is paused exactly as on the recovery page');
    assertPageAsLeft('panel starting');
    // The page keeps polling and keeps being refused: no further session read, no flapping.
    const reads = globalThis.holdTest.requests.filter(([url]) => url.includes('/panel/availability')).length;
    await act(async () => { await fetch('/api/v1/domains'); await fetch('/api/v1/system/stats'); });
    await act(async () => {});
    assert.equal(globalThis.holdTest.requests.filter(([url]) => url.includes('/panel/availability')).length, reads);
    // Ready again: released, same page, nothing typed lost.
    state = 'ready';
    replies.management = async () => Response.json({});
    await act(async () => button('recovery.retry').props.onClick());
    assert.equal(held().length, 0); assert.equal(layers().length, 0);
    assert.equal(tree.root.findByType('article').props.suspended, false);
    assert.equal(globalThis.holdTest.published.at(-1), true);
    assertPageAsLeft('after the restart');

    // The session store cannot be read: unknown, not signed out, and no operation is shown.
    me = async () => { throw new Error('503'); };
    replies.management = async () => problem(503, 'AUTH_STATUS_UNAVAILABLE');
    await act(async () => { await fetch('/api/v1/domains'); });
    await act(async () => {});
    assert.equal(tree.root.findAllByType(Login).length, 0, 'an unknown session is not a sign-out');
    assert.equal(held().length, 1);
    assert.ok(text().includes('accessHold.authTitle'), text());
    assert.equal(tree.root.findAllByProps({ 'data-operation': 'admin' }).length, 0);
    assertPageAsLeft('session unknown');
    me = async () => admin;
    replies.management = async () => Response.json({});
    await act(async () => button('recovery.retry').props.onClick());
    assert.equal(held().length, 0);
    assertPageAsLeft('after the session was read again');
    assert.ok(globalThis.holdTest.requests.every(([, method]) => method === 'GET'), 'reads only');
    assert.deepEqual(history, [], 'the address never changed');
  } finally { await clean(); }
});

test('a session that really ended shows sign-in with the reason and returns to the same address; a sign-out gives no reason', async () => {
  const replies = { me: async () => admin, availability: async () => available('ready'), management: async () => Response.json({}) };
  mounts = 0;
  try {
    await gate(replies);
    await useThePage();
    // The server confirms that the session is over.
    replies.management = async () => problem(401, 'unauthorized');
    replies.me = async () => null;
    await act(async () => { await fetch('/api/v1/domains'); });
    await act(async () => {});
    assert.equal(tree.root.findAllByType('main').length, 0, 'a known negative removes the page');
    assert.equal(tree.root.findByType(Login).props.sessionEnded, true);
    assert.ok(text().includes('accessHold.sessionEnded'), text());
    assert.equal(tree.root.findByProps({ role: 'status' }).props.children, 'accessHold.sessionEnded');
    assert.deepEqual(history, [], 'the address is kept while the sign-in form is shown');
    // Signing in again opens the routes at that same address, as a new page.
    replies.management = async () => Response.json({});
    replies.me = async () => admin;
    await act(async () => tree.root.findByType(Login).props.onSuccess(admin));
    await act(async () => {});
    assert.equal(tree.root.findAllByType('main').length, 1);
    assert.deepEqual(history, []);
    assert.equal(tree.root.findByType('input').props.value, '', 'what was typed before the session ended is not kept');
    assert.equal(mounts, 2);
    // Signing out is the owner's own act: no reason is given, also not a stale one.
    await act(async () => globalThis.holdTest.logout());
    assert.equal(tree.root.findByType(Login).props.sessionEnded, false);
    assert.ok(!text().includes('accessHold.sessionEnded'));
  } finally { await clean(); }
  // The gate never navigates; the router's address is what brings the owner back.
  const gateSource = app.slice(app.indexOf('function AuthGate()'), app.indexOf('function StandaloneRecovery('));
  assert.doesNotMatch(gateSource, /navigate\(|useNavigate|location\.(assign|replace|href)/);
  assert.doesNotMatch(source('../src/components/Login.tsx'), /useNavigate|navigate\(|location\.(assign|replace|href)/);
  assert.match(gateSource, /<Login onSuccess=\{transitionAuthentication\} sessionEnded=\{sessionEnded\.current\} \/>/);
});

test('a part that fails to load after an update says so before the page reloads; with nothing drawn it reloads at once', async () => {
  fixture();
  try {
    await act(async () => { tree = Renderer.create(React.createElement(UpdateReloadNotice)); });
    assert.equal(tree.toJSON(), null);
    let claimed;
    await act(async () => { claimed = !window.dispatchEvent(new Event(UPDATE_RELOAD_EVENT, { cancelable: true })); });
    assert.equal(claimed, true, 'the mounted interface takes over the reload');
    assert.equal(tree.root.findByProps({ role: 'status' }).props.children, 'accessHold.updateReload');
    assert.ok(text().includes('accessHold.updateReloadTitle'));
    assert.equal(reloads, 0, 'the reason is shown first');
    // The owner may reload at once instead of waiting for it.
    await act(async () => button('app.reload').props.onClick());
    assert.equal(reloads, 1); reloads = 0;
    await act(async () => { window.dispatchEvent(new Event(UPDATE_RELOAD_EVENT, { cancelable: true })); });
    assert.equal(timers.filter(timer => timer.live && timer.ms === UPDATE_RELOAD_DELAY_MS).length, 1, 'one reload, however many parts fail');
    await fire(UPDATE_RELOAD_DELAY_MS);
    assert.equal(reloads, 1);
    await clean();
    // Wording not there (nothing has been drawn yet): the event is left alone and the entry point reloads at once.
    for (const [Component, extra] of [[BareUpdateReloadNotice, {}], [UpdateReloadNotice, { screensReady: false }]]) {
      fixture(extra);
      await act(async () => { tree = Renderer.create(React.createElement(Component)); });
      assert.equal(window.dispatchEvent(new Event(UPDATE_RELOAD_EVENT, { cancelable: true })), true);
      assert.equal(tree.toJSON(), null); assert.equal(timers.length, 0);
      await clean();
    }
  } finally { await clean(); }
  const entry = source('../src/main.tsx');
  assert.match(entry, /event\.preventDefault\(\)[\s\S]{0,400}if \(window\.dispatchEvent\(new Event\(UPDATE_RELOAD_EVENT, \{ cancelable: true \}\)\)\) window\.location\.reload\(\)/);
  assert.match(entry, /<ToastContainer \/>\s*<UpdateReloadNotice \/>\s*<App \/>/);
  // Still once per window: a reload that did not help is not repeated.
  assert.match(entry, /now - lastReload > CHUNK_RELOAD_WINDOW_MS/);
});

test('the wording part is fetched ahead of need and never at the moment a hold begins', () => {
  const loader = source('../src/lib/accessGuidance.ts');
  assert.match(loader, /loading \?\?= import\('\.\.\/components\/AccessGuidance'\)/);
  // No eager module imports the wording statically, or its copy would have to boot with the shell.
  for (const file of ['components/AccessHold.tsx', 'components/Login.tsx', 'components/UpdateReloadNotice.tsx', 'components/LicenseOnboarding.tsx', 'App.tsx', 'main.tsx']) {
    assert.doesNotMatch(source(`../src/${file}`), /from ['"][^'"]*\/AccessGuidance['"]/, file);
    assert.doesNotMatch(source(`../src/${file}`), /['"`]accessHold\./, file);
  }
  const hold = source('../src/components/AccessHold.tsx');
  assert.match(hold, /const guidance = useAccessGuidance\(\);/);
  assert.match(hold, /const copy = guidance && screensReady \? guidance\.accessHoldCopy\(t, updating \? 'update' : cause, waiting\) : null;/);
  // The layer is the shared dialogue, with no silent way out, above the page and outside its inert subtree.
  assert.match(hold, /createPortal\(\s*<div ref=\{layer\} className="relative z-\[120\]" data-top-layer="hold"/);
  assert.match(hold, /dismissible=\{false\}/);
  assert.doesNotMatch(hold, /onDismiss/);
});

// Real-browser inspection of commit 8a65d4ca (2026-10-08). Each of these was seen
// on screen; the tests here hold the cause, the browser run holds the look.
test('browser findings on the hold layer: no control-like ring, no empty band, one scrim, on top of every overlay', () => {
  const hold = source('../src/components/AccessHold.tsx');
  // The title takes programmatic focus for a screen reader and draws no focus ring: it is not a control.
  assert.match(hold, /<span ref=\{heading\} tabIndex=\{-1\} className="outline-none focus-visible:outline-none">\{title\}<\/span>/);
  // The body exists only when it has something to say; the description is the standing live region.
  assert.match(hold, /\{\(resume \|\| address \|\| later\) && <div [^>]*role="status"/);
  assert.match(hold, /description=\{<span aria-live="polite">\{help\}<\/span>\}/);
  // The address wraps as an address, never inside a word or the scheme.
  assert.doesNotMatch(hold, /break-all/);
  assert.doesNotMatch(source('../src/components/RecoveryAccess.tsx').slice(source('../src/components/RecoveryAccess.tsx').indexOf('export function RecoveryAccess(')), /break-all/);
  // Above the component-operation and update overlays, below the reload dialogue.
  const layerOf = file => [...source(`../src/components/${file}`).matchAll(/\bz-\[(\d+)\]/g)].map(match => Number(match[1]));
  for (const file of ['OperationOverlay.tsx', 'ComponentOperation.tsx', 'SystemUpdateOperation.tsx', 'ui.tsx', 'Toast.tsx']) {
    for (const z of layerOf(file)) assert.ok(z < 120, `${file} draws at ${z}, above the hold`);
  }
  assert.deepEqual(layerOf('AccessHold.tsx'), [120]);
  assert.deepEqual(layerOf('AccessGuidance.tsx'), [130]);
  // Focus stays in the layer while it is drawn, also against an overlay outside the held pages.
  assert.match(hold, /if \(heading\.current \? layer\.current\?\.contains\(event\.target\) : !node\.contains\(event\.target\)\) return;/);
  // One scrim at most: while one of the two top layers is drawn, every scrim under it is cleared.
  const css = source('../src/index.css');
  assert.match(css, /body:has\(\[data-top-layer\]\) \[class\*="bg-scrim"\]:not\(\[data-top-layer\] \*, \[data-top-layer\]\),\s*body:has\(\[data-top-layer="reload"\]\) \[data-top-layer="hold"\] \[class\*="bg-scrim"\] \{\s*background-color: transparent;/);
  // Every scrim the rule has to find is spelled with the class it looks for.
  for (const file of ['ui.tsx', 'OperationOverlay.tsx', 'ComponentOperation.tsx', 'SystemUpdateOperation.tsx', 'HelpDrawer.tsx', 'Layout.tsx']) {
    const text = source(`../src/components/${file}`);
    assert.equal((text.match(/fixed inset-0/g) ?? []).length <= (text.match(/bg-scrim\//g) ?? []).length, true, `${file} dims the page with something the rule cannot clear`);
  }
  // The reload notice is the shared dialogue on top, with its cost and an action.
  const guidance = source('../src/components/AccessGuidance.tsx');
  assert.match(guidance, /<div data-top-layer="reload" className="relative z-\[130\]">\s*<Dialog id="update-reload" dismissible=\{false\}/);
  assert.match(guidance, /actions=\{<Button variant="primary" onClick=\{\(\) => window\.location\.reload\(\)\}>\{t\('app\.reload'\)\}<\/Button>\}/);
});

test('a refusal cannot make the access gate read without pause', () => {
  // The access route's own coded refusal is the gate's answer; it does not ask for another read.
  const gate = app.slice(app.indexOf('function AuthGate()'), app.indexOf('function StandaloneRecovery('));
  assert.match(gate, /\.includes\(problem\.code\)\s*&& !url\.includes\('\/api\/v1\/license\/access'\)\) \{\s*window\.dispatchEvent\(new Event\('celikpanel:license-locked'\)\);/);
  // While access stays unknown, refusals read no more often than the regular re-check.
  const onboarding = source('../src/components/LicenseOnboarding.tsx');
  assert.match(onboarding, /if \(known \|\| Date\.now\(\) - lastRead\.current >= UNKNOWN_RECHECK_MS\) void check\(\);/);
  assert.match(onboarding, /controller\.current = request;\s*lastRead\.current = Date\.now\(\);/);
});
