import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const dashboard = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');
const mailTruth = readFileSync(new URL('../src/lib/dashboardMailTruth.ts', import.meta.url), 'utf8');
const operation = readFileSync(new URL('../src/components/ComponentOperation.tsx', import.meta.url), 'utf8');
const services = readFileSync(new URL('../src/components/ServiceList.tsx', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('dashboard mail card is read-only and uses the closed profile decoder', () => {
  assert.match(dashboard, /decodeManagedMailProfiles\(payload\.profiles, serviceIDs\)/);
  assert.match(dashboard, /navigate\('\/services#mail-stacks'\)/);
  assert.doesNotMatch(dashboard, /service\/profile\/install|startInstall/);
  assert.match(dashboard, /summarizeDashboardMailTruth\(profiles, scanFresh\)/);
  assert.match(mailTruth, /profile\.latest_attempt_status === 'failed'/);
  assert.match(mailTruth, /const problem = attemptedProblem \?\? \(complete \? undefined : observedProblem\)/);
  assert.match(operation, /typeof profile\.verified !== 'boolean'/);
  assert.match(operation, /latestAttemptStatus !== 'none'/);
  assert.match(dashboard, /problem\?\.latest_attempt_error \|\| problem\?\.blocked_reason \|\| problem\?\.warning/);
  assert.match(dashboard, /scanFresh/);
});

test('mail stack destination scrolls and focuses the detailed section', () => {
  assert.match(services, /location\.hash !== '#mail-stacks'/);
  assert.match(services, /getElementById\('mail-stacks'\)/);
  assert.match(services, /scrollIntoView\(\{ block: 'start' \}\)/);
  assert.match(services, /focus\(\{ preventScroll: true \}\)/);
  assert.match(services, /id='mail-stacks' tabIndex=\{-1\}/);
});

test('dashboard mail copy stays in EN/TR parity', () => {
  for (const key of [
    'dashboard.mailStacks.title',
    'dashboard.mailStacks.status.ready',
    'dashboard.mailStacks.status.available',
    'dashboard.mailStacks.status.partial',
    'dashboard.mailStacks.status.attention',
    'dashboard.mailStacks.status.stale',
    'dashboard.mailStacks.availableHint',
    'dashboard.mailStacks.completeHint',
    'dashboard.mailStacks.partialHint',
    'dashboard.mailStacks.attentionHint',
    'dashboard.mailStacks.scanStale',
    'dashboard.mailStacks.reason',
    'dashboard.mailStacks.reconciliationFailed',
    'dashboard.mailStacks.reconciliationInProgress',
    'dashboard.mailStacks.reconciliationUnverified',
    'dashboard.mailStacks.open',
    'dashboard.mailStacks.rescan',
  ]) {
    assert.ok(en.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(tr.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
