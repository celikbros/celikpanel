import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const dashboard = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');
const operation = readFileSync(new URL('../src/components/ComponentOperation.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('mail journey requires fresh runtime state and completed profile reconciliation', () => {
  assert.match(operation, /typeof profile\.verified !== 'boolean'/);
  assert.match(operation, /profile\.verified && status !== 'complete'/);
  assert.match(dashboard, /const serviceScanFresh = freshScanTimestamp\(serviceScannedAt, freshnessNow\)/);
  assert.match(dashboard, /profile\.status === 'complete'[\s\S]*profile\.verified[\s\S]*!profile\.warning/);
  assert.match(dashboard, /const mailProfileVerified = verifiedMailProfiles\.length > 0/);
  assert.match(dashboard, /key: 'dashboard\.step\.mail', done: mailProfileVerified/);
});

test('Boston rspamd tuple never produces a false SpamAssassin install alert', () => {
  assert.match(dashboard, /const hasSpam = serviceRunning\('spamassassin'\) \|\| serviceRunning\('rspamd'\)/);
  assert.match(dashboard, /if \(mailProfileVerified && !hasSpam\)/);
  assert.doesNotMatch(dashboard, /install SpamAssassin/);
});

test('system service truth never promotes tools into running daemons', () => {
  assert.match(dashboard, /if \(!serviceScanFresh\) return false/);
  assert.match(dashboard, /const systemServices = serviceScanFresh[\s\S]*installed\.filter\(\(s\) => s\.kind === 'service'\)/);
  assert.match(dashboard, /normalized === 'running' \|\| normalized\.startsWith\('active'\)/);
  assert.match(dashboard, /key: 'dashboard\.step\.serviceScan'[\s\S]*done: serviceScanFresh/);
  assert.match(dashboard, /!serviceScanFresh[\s\S]*dashboard\.statusUnknown/);
  assert.match(dashboard, /serviceScanFresh && hostsContent && !hasClamAV/);
  assert.match(dashboard, /attention\.length > 0 && \(/);
  assert.doesNotMatch(dashboard, /t\('dashboard\.allGood'\)/);
  assert.match(layout, /service\.is_installed\)\.length/);
});

test('DNS and firewall journey steps use their independent backend truth axes', () => {
  assert.match(dashboard, /typeof payload\.dns_identity_ready !== 'boolean'/);
  assert.match(dashboard, /done: dnsIdentityReady/);
  assert.match(dashboard, /done: fw\?\.enabled === true && fw\.persistence_state === 'ready'/);
  assert.match(dashboard, /fw\?\.enabled && fw\.persistence_state !== 'ready'/);
});

test('audit, alert and gauge fixes remain localized in EN and TR', () => {
  assert.match(dashboard, /audit-logs\?limit=28/);
  assert.match(dashboard, /groupAuditEntries\(audit\)\.slice\(0, 7\)/);
  assert.match(dashboard, /attention\.length === 1/);
  assert.match(dashboard, /dashboard\.percentValue/);
  assert.equal(dashboard.match(/t\('dashboard\.percentValue'/g)?.length, 6);
  for (const key of [
    'dashboard.warnCountOne',
    'dashboard.percentValue',
    'dashboard.loadValue',
    'dashboard.statusUnknown',
    'dashboard.step.serviceScan',
    'dashboard.fwPersistenceItem',
    'dashboard.audit.mailProfileFailed',
    'dashboard.step.dnsIdentity',
    'dashboard.saveFirewall',
  ]) {
    assert.ok(en.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(tr.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
