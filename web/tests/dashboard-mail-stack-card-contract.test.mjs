import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const dashboard = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');
const services = readFileSync(new URL('../src/components/ServiceList.tsx', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('dashboard mail card is read-only and uses the closed profile decoder', () => {
  assert.match(dashboard, /decodeManagedMailProfiles\(payload\.profiles, serviceIDs\)/);
  assert.match(dashboard, /navigate\('\/services#mail-stacks'\)/);
  assert.doesNotMatch(dashboard, /service\/profile\/install|startInstall/);
  assert.match(dashboard, /profile\.status === 'partial'/);
  assert.match(dashboard, /profile\.status === 'blocked' \|\| profile\.status === 'unknown'/);
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
    'dashboard.mailStacks.availableHint',
    'dashboard.mailStacks.completeHint',
    'dashboard.mailStacks.partialHint',
    'dashboard.mailStacks.attentionHint',
    'dashboard.mailStacks.open',
  ]) {
    assert.ok(en.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(tr.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
