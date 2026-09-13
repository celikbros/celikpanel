import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const dataModule = text => 'data:text/javascript;base64,' + Buffer.from(text).toString('base64');
const reactURL = pathToFileURL(require.resolve('react')).href;
const stub = dataModule(`
  import React from '${reactURL}';
  export const useAuth = () => globalThis.licenseTest.auth;
  export const useNavigate = () => globalThis.licenseTest.navigate;
  export const useLocation = () => ({ pathname: globalThis.licenseTest.pathname || '/domains' });
  export const useSearchParams = () => [new URLSearchParams(globalThis.licenseTest.search)];
  export const useI18n = () => ({ t: key => key, locale: 'en', screensReady: true, screensFailed: false });
 export const PanelAddressHint = () => null;
 export const BrandMark = () => null, LanguageSwitcher = () => null, ThemeSwitcher = () => null, ChangePasswordModal = () => null, ToastContainer = () => null;
 export const LicensePanel = () => React.createElement('section', null, 'activation');
 export const PanelUpdateCard = props => React.createElement('article', props, 'signed-update');
 export const Spinner = () => React.createElement('span', null, 'loading');
 export const LicenseLockScreen = props => React.createElement('aside', props, 'locked');
  export const Button = props => React.createElement('button', props);
  export const inputClass = '';
  export const BadgeCheck = () => null, Eye = () => null, EyeOff = () => null;
  export const readApiError = async response => { const data = await response.json(); return { message: data.error, code: data.code }; };
  export const apiErrorText = error => error.message;
`);
async function component(name) {
  const source = readFileSync(new URL(`../src/components/${name}.tsx`, import.meta.url), 'utf8');
  const compiled = ts.transpileModule(source, { compilerOptions: {
    jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020,
  }}).outputText.replace(/import\('\.\/LicenseLockScreen'\)/g, `import('${stub}')`).replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => `from '${specifier === 'react' ? reactURL : stub}'`);
  return (await import(dataModule(`import React from '${reactURL}';\n${compiled}`)))[name];
}
const LicenseOnboarding = await component('LicenseOnboarding');
const LicensePanel = await component('LicensePanel');
const LicenseLockScreen = await component('LicenseLockScreen');
const originalFetch = globalThis.fetch;
const events = new EventTarget();
globalThis.window = Object.assign(events, { setTimeout, clearTimeout, setInterval, clearInterval });
globalThis.document = { visibilityState: 'visible' };
let calls, navigations, tree;
function fixture(role = 'admin', state = 'missing') {
  calls = []; navigations = []; tree = undefined;
  globalThis.licenseTest = {
    auth: { role, user: { username: role } }, search: 'setup=1',
    navigate: (...args) => { navigations.push(args); return true; },
  };
  globalThis.fetch = async (url, options) => {
    calls.push({ url, options });
    return Response.json(url.includes('/license/access') ? { can_use_panel: state === 'active', valid_until: state === 'active' ? Math.floor(Date.now()/1000)+3600 : 0 } : { state, can_provision: state === 'active' });
  };
}
async function mount(Component) { await act(async () => { tree = Renderer.create(React.createElement(Component)); }); }
async function cleanup() {
  if (tree) await act(async () => tree.unmount());
  globalThis.fetch = originalFetch;
  delete globalThis.licenseTest;
}
const button = label => tree.root.findAllByType('button').find(node => node.props.children === label);
const input = () => tree.root.findByType('input');
async function enter(value) { await act(async () => input().props.onChange({ target: { value } })); }
async function submit() { await act(async () => tree.root.findByType('form').props.onSubmit({ preventDefault() {} })); }

test('all roles are locked for missing, expired, invalid and unverifiable licenses; active access mounts children', async () => {
 for (const role of ['admin','reseller','customer','additional_user']) {
  for (const state of ['missing','expired','invalid','verification_unavailable','active']) {
   fixture(role,state);
   try {
    await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement('main',null,'management')))});
    assert.equal(calls.length,1);
    assert.equal(tree.root.findAllByType('main').length,state==='active'?1:0);
    assert.equal(tree.root.findAllByType('aside').length,state==='active'?0:1);
    if(state!=='active') assert.equal(navigations.at(-1)[0],'/activate','deep links return to activation');
   } finally {await cleanup()}
  }
 }
});
test('network errors, malformed status, and a license rejection close management; retry restores valid access',async()=>{
 for(const reply of [()=>{throw new Error('offline')},()=>Response.json({can_use_panel:true}),()=>Response.json({}, {status:503})]) {
  fixture();
  globalThis.fetch=async()=>reply();
  try {await mount(LicenseOnboarding);assert.equal(tree.root.findByType('aside').props.failed,true);assert.equal(navigations.length,0,'unavailable access is not activation')} finally {await cleanup()}
 }
 fixture('admin','active');
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement('main',null,'management')))});
  await act(async()=>window.dispatchEvent(new Event('celikpanel:license-locked')));
  assert.equal(tree.root.findAllByType('main').length,0);
  await act(async()=>tree.root.findByType('aside').props.onCheck());
  assert.equal(tree.root.findAllByType('main').length,1);
 } finally {await cleanup()}
});
test('late access after logout cannot mount management',async()=>{
 fixture();let resolve;
 globalThis.fetch=()=>new Promise(done=>{resolve=done});
 try {await mount(LicenseOnboarding);await act(async()=>tree.unmount());await act(async()=>resolve(Response.json({can_use_panel:true,valid_until:2000000000})));assert.equal(navigations.length,0)} finally {await cleanup()}
});
test('format errors are actionable; pasted whitespace is trimmed and successful activation clears the secret', async () => {
  fixture();
  try {
    await mount(LicensePanel);
    assert.equal(input().props.type, 'password');
    await enter('wrong'); await submit();
    assert.equal(calls.length, 1, 'invalid format must not contact activation');
    assert.equal(input().props['aria-invalid'], true);
    assert.equal(tree.root.findByProps({ id: 'license-key-error' }).props.children, 'license.keyInvalid');
    await act(async () => tree.root.findByProps({ 'aria-label': 'license.showKey' }).props.onClick());
    assert.equal(input().props.type, 'text');
    const key = 'CPK-' + 'a'.repeat(64);
    await enter('  ' + key + '\n');
    globalThis.fetch = async (url, options) => {
      calls.push({ url, options });
      assert.deepEqual(JSON.parse(options.body), { action: 'activate', key });
      return Response.json({ state: 'active', can_provision: true, expires_at: 2000000000 });
    };
    await submit();
    assert.equal(tree.root.findAllByType('input').length, 0);
    assert.ok(!JSON.stringify(tree.toJSON()).includes(key));
    assert.equal(button('license.continueSetup'), undefined);
    assert.equal(button('license.refresh'), undefined);
    assert.deepEqual(navigations, [['/', { replace: true }]]);
  } finally { await cleanup(); }
});
test('activation rejection preserves the key and offers no exploration bypass', async () => {
  fixture();
  try {
    await mount(LicensePanel);
    const key = 'CPK-' + 'b'.repeat(64);
    await enter(key);
    globalThis.fetch = async () => Response.json({ code: 'license_action_failed', error: 'license: license_in_use' }, { status: 409 });
    await submit();
    assert.equal(input().props.value, key);
    assert.equal(tree.root.findByProps({ role: 'alert' }).props.children, 'license.inUse');
    assert.equal(button('license.activate').props.disabled, false);
    assert.equal(button('license.explore'), undefined);
    assert.equal(navigations.length, 0);
  } finally { await cleanup(); }
});

test('an open session locks at the signed deadline and browser history cannot escape',async()=>{
 fixture('admin','active');
 const timers=[];const previous=window.setTimeout;
 window.setTimeout=(fn,ms)=>{if(ms>15000){timers.push(fn);return 0}return previous(fn,ms)};
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement('main',null,'management')))});
  globalThis.fetch=async()=>Response.json({can_use_panel:false,valid_until:0});
  await act(async()=>timers.at(-1)());
  assert.equal(tree.root.findAllByType('main').length,0);
  assert.equal(navigations.at(-1)[0],'/activate');
  globalThis.licenseTest.pathname='/services';
  await act(async()=>tree.update(React.createElement(LicenseOnboarding,null,React.createElement('main',null,'management'))));
  assert.equal(navigations.at(-1)[0],'/activate');
  assert.equal(tree.root.findAllByType('main').length,0);
 }finally{window.setTimeout=previous;await cleanup()}
});

test('only administrators can open signed updates while activation remains mounted', async () => {
 for (const role of ['admin','reseller','customer','additional_user']) {
  fixture(role);
  try {
   await act(async()=>{tree=Renderer.create(React.createElement(LicenseLockScreen,{checking:false,failed:false,onCheck(){}}))});
   const disclosure=tree.root.findAllByType('details');
   assert.equal(disclosure.length,role==='admin'?1:0);
   assert.equal(tree.root.findAllByType('article').length,0);
   if(role==='admin') {
    await act(async()=>disclosure[0].props.onToggle({currentTarget:{open:true}}));
    assert.equal(tree.root.findByType('article').props.activation,true);
    assert.equal(tree.root.findAllByType('section').length,1,'activation stays available');
    await act(async()=>disclosure[0].props.onToggle({currentTarget:{open:false}}));
    assert.equal(tree.root.findAllByType('article').length,0);
   }
  } finally {await cleanup()}
 }
});

test('short verification renews before expiry without remounting management; hidden pages do not poll',async()=>{
 fixture('admin','active');
 const previousTimer=window.setTimeout,previousNow=Date.now;
 const timers=[];let now=Date.now(),mounts=0;
 Date.now=()=>now;
 window.setTimeout=(fn,ms)=>{if(ms>15000){timers.push({fn,ms});return 0}return previousTimer(fn,ms)};
 globalThis.fetch=async()=>{calls.push('access');return Response.json({can_use_panel:true,valid_until:Math.floor(now/1000)+60})};
 function Management(){React.useEffect(()=>{mounts++},[]);return React.createElement('main',null,'management')}
 try{
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement(Management)))});
  const early=timers.find(timer=>timer.ms>40000&&timer.ms<=45000);
  assert.ok(early,'renew before the hard deadline');
  now+=45000;
  await act(async()=>early.fn());
  assert.equal(calls.length,2);
  assert.equal(mounts,1,'successful renewal preserves page state');
  assert.equal(navigations.length,0);
  document.visibilityState='hidden';
  const lastEarly=timers.filter(timer=>timer.ms>40000&&timer.ms<=45000).at(-1);
  await act(async()=>lastEarly.fn());
  assert.equal(calls.length,2,'hidden page does not renew');
  const deadline=timers.at(-1);
  await act(async()=>deadline.fn());
  assert.equal(calls.length,2,'expiry itself does not poll while hidden');
  assert.equal(tree.root.findAllByType('main').length,0,'hidden expiry still locks management');
 }finally{Date.now=previousNow;window.setTimeout=previousTimer;document.visibilityState='visible';await cleanup()}
});


test('locked activation automatically rechecks access; active settings stay on their page', async () => {
  for (const locked of [true, false]) {
    fixture();
    globalThis.licenseTest.search = '';
    let accessChecks = 0;
    try {
      await act(async () => { tree = Renderer.create(React.createElement(LicensePanel, {
        locked, onContinue: () => { accessChecks++; },
      })); });
      await enter('CPK-' + 'c'.repeat(64));
      globalThis.fetch = async () => Response.json({ state: 'active', can_provision: true });
      await submit();
      assert.equal(accessChecks, locked ? 1 : 0);
      assert.equal(navigations.length, 0, 'locked activation delegates to the verified access gate');
      assert.equal(button('license.continueSetup'), undefined);
      assert.equal(!!button('license.refresh'), !locked);
    } finally { await cleanup(); }
  }
});

test('an already active activation page automatically rechecks access', async () => {
  fixture('admin', 'active');
  let accessChecks = 0;
  try {
    await act(async () => { tree = Renderer.create(React.createElement(LicensePanel, {
      locked: true, onContinue: () => { accessChecks++; },
    })); });
    assert.equal(accessChecks, 1);
    assert.equal(button('license.continueSetup'), undefined);
    assert.equal(button('license.refresh'), undefined);
  } finally { await cleanup(); }
});


test('transient access failures preserve the current route and mounted form until the signed deadline', async () => {
 fixture('admin','active');
 const originalTimer=window.setTimeout;
 const timers=[];let mounts=0;
 window.setTimeout=(fn,ms)=>{if(ms>15000){timers.push(fn);return 0}return originalTimer(fn,ms)};
 function Management(){React.useEffect(()=>{mounts++},[]);return React.createElement('main',null,'setup')}
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement(Management)))});
  for(const reply of [()=>{throw new TypeError('Failed to fetch')},()=>Response.json({}, {status:503}),()=>Response.json({can_use_panel:true})]) {
   globalThis.fetch=async()=>reply();
   await act(async()=>window.dispatchEvent(new Event('focus')));
   assert.equal(tree.root.findAllByType('main').length,1);
   assert.equal(mounts,1,'connection banner must not remount the form');
   assert.equal(navigations.length,0);
   assert.ok(button('common.reloadPage'),'a full page reload is available for TLS recovery');
  }
  await act(async()=>timers.at(-1)());
  assert.equal(tree.root.findAllByType('main').length,0,'cached access cannot outlive the server deadline');
  assert.equal(tree.root.findByType('aside').props.failed,true);
  assert.equal(navigations.length,0,'unknown state retains the setup URL');
 } finally {window.setTimeout=originalTimer;await cleanup()}
});

test('focus checks share an in-flight access request and explicit rejection still locks immediately',async()=>{
 fixture('admin','active');let resolve;let requests=0;
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseOnboarding,null,React.createElement('main')))});
  globalThis.fetch=()=>{requests++;return new Promise(done=>{resolve=done})};
  await act(async()=>{window.dispatchEvent(new Event('focus'));window.dispatchEvent(new Event('focus'))});
  assert.equal(requests,1);
  await act(async()=>window.dispatchEvent(new Event('celikpanel:license-locked')));
  assert.equal(tree.root.findAllByType('main').length,0);
  await act(async()=>resolve(Response.json({can_use_panel:true,valid_until:Math.floor(Date.now()/1000)+3600})));
  assert.equal(tree.root.findAllByType('main').length,0,'superseded response cannot undo rejection');
  assert.equal(navigations.at(-1)[0],'/activate');
 }finally{await cleanup()}
});

test('connection recovery has retry and reload without an activation form or update requests',async()=>{
 fixture();let reloads=0;window.location={reload(){reloads++}};
 try {
  await act(async()=>{tree=Renderer.create(React.createElement(LicenseLockScreen,{checking:false,failed:true,onCheck(){}}))});
  assert.equal(tree.root.findAllByType('section').length,1);
  assert.ok(!JSON.stringify(tree.toJSON()).includes('activation'));
  assert.equal(tree.root.findAllByType('details').length,0);
  assert.ok(button('license.refresh'));
  await act(async()=>button('common.reloadPage').props.onClick());assert.equal(reloads,1);
  assert.equal(calls.length,0);
 }finally{await cleanup()}
});

test('license status fetch failure does not ask for a replacement key',async()=>{
 fixture();globalThis.fetch=async()=>{throw new TypeError('Failed to fetch')};
 try {
  await mount(LicensePanel);
  assert.equal(tree.root.findAllByType('form').length,0);
  assert.equal(tree.root.findByProps({role:'alert'}).props.children,'license.lockError');
  assert.ok(button('license.refresh'));
 }finally{await cleanup()}
});


test('panel address hint only links to safe server metadata and preserves the current port',async()=>{
 const Hint=await component('PanelAddressHint');
 for(const hostname of ['panel.example.test','evil.example/path','user@evil.example','*.example.test','',null,'127.0.0.1']) {
  fixture(); window.location={origin:'https://127.0.0.1:2083',hostname:'127.0.0.1'};
  globalThis.fetch=async()=>Response.json({hostname});
  try {
   await mount(Hint);
   const links=tree.root.findAllByType('a');
   assert.equal(links.length,hostname==='panel.example.test'?1:0);
   if(links.length)assert.equal(links[0].props.href,'https://panel.example.test:2083/');
  }finally{await cleanup()}
 }
});
