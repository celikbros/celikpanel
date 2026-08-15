import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

const source = readFileSync(new URL('../src/lib/dashboardMailTruth.ts', import.meta.url), 'utf8');

async function loadPolicy() {
  const javascript = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const url = `data:text/javascript;base64,${Buffer.from(javascript).toString('base64')}`;
  return import(url);
}

function profile(id, status, latestAttemptStatus, verified = false, extra = {}) {
  return {
    id,
    status,
    latest_attempt_status: latestAttemptStatus,
    verified,
    ...extra,
  };
}

test('verified Core Mail ignores only unattempted shared-component partial alternatives', async () => {
  const { summarizeDashboardMailTruth } = await loadPolicy();
  const summary = summarizeDashboardMailTruth([
    profile('core-mail', 'complete', 'succeeded', true),
    profile('webmail', 'partial', 'none'),
    profile('protected-mail', 'partial', 'none'),
  ], true);

  assert.equal(summary.complete, true);
  assert.equal(summary.problem, undefined);
  assert.equal(summary.partial, false);
  assert.equal(summary.needsAttention, false);
});

test('verified Core Mail does not hide a newer failed partial Webmail attempt', async () => {
  const { summarizeDashboardMailTruth } = await loadPolicy();
  const failedWebmail = profile('webmail', 'partial', 'failed', false, {
    latest_attempt_error: 'Roundcube readiness verification failed.',
  });
  const summary = summarizeDashboardMailTruth([
    profile('core-mail', 'complete', 'succeeded', true),
    failedWebmail,
    profile('protected-mail', 'partial', 'none'),
  ], true);

  assert.equal(summary.problem, failedWebmail);
  assert.equal(summary.partial, false);
  assert.equal(summary.needsAttention, true);
});

test('a genuinely partial manual mail setup remains partial without an attempt receipt', async () => {
  const { summarizeDashboardMailTruth } = await loadPolicy();
  const summary = summarizeDashboardMailTruth([
    profile('core-mail', 'partial', 'none'),
    profile('webmail', 'partial', 'none'),
    profile('protected-mail', 'partial', 'none'),
  ], true);

  assert.equal(summary.problem?.status, 'partial');
  assert.equal(summary.partial, true);
  assert.equal(summary.needsAttention, false);
});
