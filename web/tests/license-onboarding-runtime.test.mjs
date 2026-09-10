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
  try {await mount(LicenseOnboarding);assert.equal(tree.root.findByType('aside').props.failed,true)} finally {await cleanup()}
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
    await act(async () => button('license.continueSetup').props.onClick());
    assert.equal(navigations.at(-1)[0], '/');
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
