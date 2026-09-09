import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import React from 'react';
import renderer from 'react-test-renderer';
import ts from 'typescript';

function load(relative, dependencies = {}) {
  const source = readFileSync(new URL(relative, import.meta.url), 'utf8');
  const javascript = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.React,
  }}).outputText;
  const module = { exports: {} };
  new Function('require', 'exports', 'module', 'React', javascript)(
    (id) => { assert.ok(id in dependencies, `unexpected runtime dependency ${id}`); return dependencies[id]; },
    module.exports, module, React,
  );
  return module.exports;
}
const policy = load('../src/lib/startGuidance.ts');

test('account guidance keeps host administration and delegated access out of tenant actions', () => {
  assert.equal(policy.accountStart('reseller').to, '/users');
  assert.equal(policy.accountStart('customer').to, '/domains');
  assert.equal(policy.accountStart('additional_user'), null);
  assert.equal(policy.accountStart('admin'), null);
  assert.equal(policy.accountStart('unrecognized'), null);
});

test('DNS completion requires fresh evidence, configured identity and a running engine together', () => {
  for (const fresh of [false, true]) for (const identity of [false, true]) for (const running of [false, true]) {
    assert.equal(policy.dnsStartReady(fresh, identity, running), fresh && identity && running);
  }
});

test('unused unavailable mail is optional; attempted and partial mail remains visible', () => {
  const profile = {status: 'blocked', latest_attempt_status: 'none', verified: false};
  assert.equal(policy.hasMailActivity([profile]), false);
  assert.equal(policy.hasMailActivity([{...profile, status: 'available'}]), false);
  assert.equal(policy.hasMailActivity([{...profile, status: 'partial'}]), true);
  for (const status of ['failed', 'in_progress', 'succeeded']) {
    assert.equal(policy.hasMailActivity([{...profile, latest_attempt_status: status}]), true);
  }
});

test('panel HTTPS cannot be completed by site SSL, an expired certificate or an unknown response', () => {
  const now = Date.parse('2026-09-09T00:00:00Z');
  const cert = { https_enabled: true, self_signed: false, expires_at: '2026-10-01T00:00:00Z' };
  assert.equal(policy.panelCertificateReady(cert, now), true);
  assert.equal(policy.panelCertificateReady({...cert, self_signed: true}, now), false);
  assert.equal(policy.panelCertificateReady({...cert, https_enabled: false}, now), false);
  assert.equal(policy.panelCertificateReady({...cert, expires_at: '2026-08-01T00:00:00Z'}, now), false);
  for (const value of [null, {}, {ssl_enabled: true}, {...cert, expires_at: 'bad'}]) {
    assert.equal(policy.panelCertificateReady(value, now), null);
  }
});

const { StartGuide } = load('../src/components/StartGuide.tsx', {
  'lucide-react': { Check: () => null, ArrowRight: () => null },
  '../i18n': { useI18n: () => ({t: (key) => key}) },
  '../router': { Link: ({to, children, ...props}) => React.createElement('a', {...props, href: to}, children) },
  './ui': { Button: ({children, ...props}) => React.createElement('button', props, children) },
});

test('DNS is first; security remains accessible while DNS is incomplete; actions use existing handlers', () => {
  let confirmations = 0;
  let scans = 0;
  const tree = renderer.create(React.createElement(StartGuide, {
    dnsReady: false, scanFresh: false, scanning: false, onScan: () => scans++,
    panelSecured: null, firewallReady: false, onFirewall: () => confirmations++, firewallBusy: false,
  }));
  const links = tree.root.findAllByType('a').map((a) => a.props.href);
  assert.equal(links[0], '/settings?section=dns');
  for (const section of ['panel', 'account', 'security']) assert.ok(links.includes(`/settings?section=${section}`));
  assert.ok(!links.includes('/services#mail-stacks'));
  assert.equal(confirmations, 0);
  tree.root.findAllByType('button').find((b) => b.children.includes('firewall.turnOn')).props.onClick();
  tree.root.findAllByType('button').find((b) => b.children.includes('start.scan.action')).props.onClick();
  assert.equal(confirmations, 1);
  assert.equal(scans, 1);
  assert.ok(JSON.stringify(tree.toJSON()).includes('dashboard.statusUnknown'));
  tree.unmount();
});

test('ready DNS exposes optional hosting choices without counting mail as mandatory completion', () => {
  const tree = renderer.create(React.createElement(StartGuide, {
    dnsReady: true, scanFresh: true, scanning: false, onScan: () => {},
    panelSecured: true, firewallReady: true, firewallBusy: false,
  }));
  const links = tree.root.findAllByType('a').map((a) => a.props.href);
  assert.ok(links.includes('/domains'));
  assert.ok(links.includes('/services#mail-stacks'));
  assert.equal(tree.root.findAllByType('button').length, 0);
  tree.unmount();
});
