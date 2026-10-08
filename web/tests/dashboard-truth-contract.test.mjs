import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { englishCatalogue, turkishCatalogue } from './locale-catalogue.mjs';

const dashboard = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');
const operation = readFileSync(new URL('../src/components/ComponentOperation.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const census = readFileSync(new URL('../src/lib/componentCensus.ts', import.meta.url), 'utf8');
const en = englishCatalogue;
const tr = turkishCatalogue;

test('mail journey requires fresh runtime state and completed profile reconciliation', () => {
  assert.match(operation, /typeof profile\.verified !== 'boolean'/);
  assert.match(operation, /profile\.verified && status !== 'complete'/);
  assert.match(dashboard, /const serviceScanFresh = freshScanTimestamp\(serviceScannedAt, freshnessNow\)/);
  assert.match(dashboard, /profile\.status === 'complete'[\s\S]*profile\.verified[\s\S]*!profile\.warning/);
  assert.match(dashboard, /const mailProfileVerified = verifiedMailProfiles\.length > 0/);
  assert.match(dashboard, /mailProfiles && hasMailActivity\(mailProfiles\)/);
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
  assert.match(dashboard, /const hostNeverChecked = services\.length > 0 && uncheckedServices\.length === services\.length/);
  assert.match(dashboard, /!serviceScanFresh[\s\S]*dashboard\.statusUnknown/);
  assert.match(dashboard, /serviceScanFresh && hostsContent && !hasClamAV/);
  assert.match(dashboard, /attention\.length > 0 && \(/);
  assert.doesNotMatch(dashboard, /t\('dashboard\.allGood'\)/);
  // 9 Oct 2026: the attention section keeps its place instead of appearing
  // late and pushing the page down, so it has a line for "no items". That line
  // is not "all good". It is drawn only when every read the list is built from
  // has answered and none could not be read, it names what was read, and it
  // does not speak for components whose check is not current.
  assert.match(dashboard,
    /\{!attentionChecking && !attentionUnread && attention\.length === 0 && \(\s*<p[^>]*>\s*\{t\(serviceScanFresh \? 'dashboard\.attentionNone' : 'dashboard\.attentionNoneUnchecked'\)\}/);
  assert.match(dashboard, /const attentionChecking = attentionReads\.includes\('loading'\);/);
  assert.match(dashboard, /const attentionUnread = attentionReads\.includes\('unknown'\);/);
  assert.match(dashboard,
    /const attentionReads = \[\s*extrasRead\.remote\.state,\s*domainList\.remote\.state,\s*firewall\.remote\.state,\s*scanRead === 'reading' \? 'loading' : scanRead === 'known' \? 'known' : 'unknown',\s*\];/,
    'the calm line waits for the certificates, the domain list, the firewall and the component records');
  // The firewall is "off" only for an answer that says so: an answer without a
  // boolean `enabled`, or one carrying the Agent's own error, is unknown.
  assert.match(dashboard, /const firewall = useFirewallStatus\(\);/);
  assert.doesNotMatch(dashboard, /setFw\(|\.then\(setFw\)/, 'no answer is stored as the firewall state without being decoded');
  assert.match(census, /observed\.filter\(\(row\) => row\.is_installed === true\)\.length/);
});

test('dashboard preserves independent service and firewall evidence while setup has its own persisted state', () => {
  assert.match(dashboard, /typeof payload\.dns_identity_ready !== 'boolean'/);
  assert.match(dashboard, /<ServerSetupDashboardNotice/);
  assert.doesNotMatch(dashboard, /<StartGuide/);

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
