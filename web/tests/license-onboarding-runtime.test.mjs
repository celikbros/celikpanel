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
  export const useSearchParams = () => [new URLSearchParams(globalThis.licenseTest.search)];
  export const useI18n = () => ({ t: key => key, locale: 'en' });
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
  }}).outputText.replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => `from '${specifier === 'react' ? reactURL : stub}'`);
  return (await import(dataModule(`import React from '${reactURL}';\n${compiled}`)))[name];
}
const LicenseOnboarding = await component('LicenseOnboarding');
const LicensePanel = await component('LicensePanel');
const originalFetch = globalThis.fetch;
let calls, navigations, tree;
function fixture(role = 'admin', state = 'missing') {
  calls = []; navigations = []; tree = undefined;
  globalThis.licenseTest = {
    auth: { role, user: { username: role } }, search: 'setup=1',
    navigate: (...args) => { navigations.push(args); return true; },
  };
  globalThis.fetch = async (url, options) => {
    calls.push({ url, options });
    return Response.json({ state, can_provision: state === 'active' });
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

test('only an authenticated administrator with no license is redirected', async () => {
  for (const role of ['admin', 'reseller', 'user', 'customer', 'member']) {
    fixture(role);
    try {
      await mount(LicenseOnboarding);
      assert.equal(calls.length, role === 'admin' ? 1 : 0);
      assert.equal(navigations.length, role === 'admin' ? 1 : 0);
      if (role === 'admin') assert.equal(navigations[0][0], '/settings?section=license&setup=1');
    } finally { await cleanup(); }
  }
});
test('an existing, expired, invalid, or temporarily unverifiable license does not start onboarding', async () => {
  for (const state of ['active', 'expired', 'invalid', 'verification_unavailable']) {
    fixture('admin', state);
    try { await mount(LicenseOnboarding); assert.equal(navigations.length, 0); }
    finally { await cleanup(); }
  }
});
test('late status cannot redirect after logout and network failure leaves navigation usable', async () => {
  fixture();
  try {
    let resolve;
    globalThis.fetch = () => new Promise(done => { resolve = done; });
    await mount(LicenseOnboarding);
    await act(async () => tree.unmount());
    await act(async () => resolve(Response.json({ state: 'missing', can_provision: false })));
    assert.equal(navigations.length, 0);
  } finally { await cleanup(); }
  fixture();
  try {
    globalThis.fetch = async () => { throw new Error('offline'); };
    await mount(LicenseOnboarding);
    assert.equal(navigations.length, 0);
  } finally { await cleanup(); }
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
test('activation rejection preserves the key for correction and exploration does not activate anything', async () => {
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
    await act(async () => button('license.explore').props.onClick());
    assert.equal(navigations.at(-1)[0], '/');
  } finally { await cleanup(); }
});
