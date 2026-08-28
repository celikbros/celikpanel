import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/router-history.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText;
const routerHistory = await import(
  'data:text/javascript;base64,' + Buffer.from(compiled).toString('base64')
);

test('trusted metadata cleanup preserves the managed entry and cannot resurrect its query', () => {
  const managedState = { __celikpanel_router_v1: { index: 7, value: { source: 'user' } } };
  const calls = [];
  const result = routerHistory.replaceRouterHistoryURL({
    href: 'https://panel.example/settings?section=updates&_cp_update=A#x',
    origin: 'https://panel.example',
    state: managedState,
    replaceState: (...args) => calls.push(args),
  }, '/settings?section=updates#x');

  assert.deepEqual(result, {
    state: managedState,
    url: '/settings?section=updates#x',
    pathname: '/settings',
    search: '?section=updates',
    hash: '#x',
  });
  assert.deepEqual(calls, [[managedState, '', '/settings?section=updates#x']]);
  assert.equal(result.state.__celikpanel_router_v1.index, 7);
  assert.equal(result.url.includes('_cp_update'), false);
  const next = new URLSearchParams(result.search);
  next.set('section', 'dns');
  assert.equal(next.has('_cp_update'), false);
  assert.equal(result.pathname + '?' + next + result.hash, '/settings?section=dns#x');
});

test('cross-origin or invalid cleanup never mutates history', () => {
  let mutations = 0;
  const port = {
    href: 'https://panel.example/settings',
    origin: 'https://panel.example',
    state: null,
    replaceState: () => { mutations += 1; },
  };
  assert.equal(routerHistory.replaceRouterHistoryURL(port, 'https://evil.example/'), null);
  assert.equal(routerHistory.replaceRouterHistoryURL(port, 'http://[invalid'), null);
  assert.equal(mutations, 0);
});
