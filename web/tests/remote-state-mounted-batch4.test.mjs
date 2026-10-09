import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test, { mock } from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import { dataModule, reactURL, remoteURL, sharedLayer } from './fixtures/shared-layer.mjs';
import { readAllowList, scanTree } from './remote-state-ratchet.mjs';
import { fileURLToPath } from 'node:url';

// NO NEGATIVE UI UNLESS KNOWN, fourth batch (9 Oct 2026, D-024): the panels of
// one domain. DNS records and signing, hosting type and its live application,
// PHP, the general settings, the applications, mail authentication, the logs,
// the backups and the scheduled tasks.
//
// As in the three earlier mounted tests, every screen is mounted with the real
// shared layer and one of its reads is made to hang, to drop, to be refused or
// to answer with something that is not the contract. The screen must not say
// that something is missing, empty or off, must not build a form, and must not
// offer an enabled control that changes or removes. The same read is then
// answered with a real negative, so the absence above is not an accident.
//
// This batch adds the second half of the rule: a change whose answer did not
// arrive. The panel's changes carry no identity the server keeps, so the test
// drops the connection during one and requires that nothing is sent a second
// time, that what the change acts on is read again (and only read), that the
// changing controls stay off until that read answers, and that the notice
// stays until the person closes it.
//
// What these panels did before: "DNS zone status could not be checked" as an
// empty state; "No records yet" for a failed read; the DNSSEC card missing; a
// hosting form that never appeared; "Could not load PHP settings" in red with
// no way to read again; "No applications available", "No backups yet" and "No
// linked databases" for a failed read; a spinner without end for mail
// authentication; and, with auto-refresh on, an error toast every five seconds.

const source = (path) => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const screenFiles = ['DomainDNSManager', 'HostingTypePanel', 'DomainPHPSettings', 'DomainGeneralSettings', 'DomainAppsPanel',
  'MailAuthPanel', 'DomainLogsViewer', 'DomainBackupManager', 'DomainCronManager'];
const icons = new Set(['AlertTriangle']);
for (const name of screenFiles) {
  for (const match of source(`components/${name}.tsx`).matchAll(/import\s*\{([^}]+)\}\s*from 'lucide-react'/g)) {
    for (const item of match[1].split(',')) {
      const icon = item.replace(/\btype\b/, '').trim().split(/\s+as\s+/)[0];
      if (icon && icon !== 'LucideIcon') icons.add(icon);
    }
  }
}

const stub = dataModule(`
  import React from '${reactURL}';
  ${[...icons].map((name) => `export const ${name} = () => null;`).join('\n')}
  const i18n = { t: (key, vars) => (vars ? key + JSON.stringify(vars) : key), locale: 'en' };
  export const useI18n = () => i18n;
  export const useNavigate = () => () => true;
  export const showToast = (...args) => globalThis.currentTest.toasts.push(args);
`);
const shared = sharedLayer(stub);
const own = {
  '/CurrentSettings': shared.compile('components/CurrentSettings.tsx'),
  '/lib/domainLogs': shared.compile('lib/domainLogs.ts'),
};
const pick = async (name) => (await import(shared.compile(`components/${name}.tsx`, own)))[name];
const DomainDNSManager = await pick('DomainDNSManager');
const HostingTypePanel = await pick('HostingTypePanel');
const DomainPHPSettings = await pick('DomainPHPSettings');
const DomainGeneralSettings = await pick('DomainGeneralSettings');
const DomainAppsPanel = await pick('DomainAppsPanel');
const MailAuthPanel = await pick('MailAuthPanel');
const DomainLogsViewer = await pick('DomainLogsViewer');
const DomainBackupManager = await pick('DomainBackupManager');
const DomainCronManager = await pick('DomainCronManager');
const { useRefreshEvery, useRemote } = await import(remoteURL);
globalThis.window ??= globalThis;

// --- What the server answers when nothing is wrong --------------------------------
const D = '/api/v1/domains/1';
const authRecord = (status) => ({ name: 'example.com', recommended: 'v=spf1 mx -all', zone_value: '', dns_value: '', resolved: true, status });
const good = {
  '/api/v1/hosting/capabilities': { web_server: 'nginx', php_versions: ['8.3'], dns_server: 'bind', dns_identity_ready: true, dns_management_mode: 'local', dns_management_ready: true, mail_server: true, database_servers: ['mariadb'], db_tools: [] },
  [`${D}/dns/zone`]: { type: 'NATIVE', management: 'local' },
  [`${D}/dns/records`]: { records: [{ id: 7, name: 'www.example.com', type: 'A', content: '192.0.2.4', ttl: 3600, disabled: false }] },
  [`${D}/dnssec`]: { secured: true, ds: ['12345 13 2 ABCDEF'] },
  [`${D}/hosting`]: { project_type: 'node', start_command: 'node server.js', runtime_version: '22.3.0', app_port: 3001 },
  '/api/v1/runtimes/node': { installed: ['22.3.0', '20.11.1'] },
  [`${D}/app/status`]: { exists: true, active: 'active', pid: 4242, memory_mb: 64 },
  [`${D}/app/logs`]: { lines: ['listening on 3001'] },
  [`${D}/php`]: { domain_id: 1, domain_name: 'example.com', php_version: '8.3', available_versions: ['8.3'], pool_name: 'example_com', pool_config: { pm: 'ondemand', pm_max_children: 9, pm_start_servers: 3, pm_min_spare_servers: 2, pm_max_spare_servers: 4, user: 'site1', group: 'site1' } },
  [`${D}/general`]: { domain_id: 1, domain_name: 'example.com', document_root: '/home/site1/public_html', web_server: 'nginx', redirect_www: true, redirect_www_available: true, redirect_https: true, aliases: ['alias.example.net'] },
  '/api/v1/apps': { apps: [{ id: 'wordpress', name: 'WordPress', description: 'A blog', icon: '', requires_db: true, requires_php: true }] },
  [`${D}/mail/auth`]: { domain: 'example.com', zone_exists: true, dns_management_mode: 'local', spf: authRecord('pending'), dkim: authRecord('pending'), dmarc: authRecord('pending'), dkim_selector: 'default', signing_installed: true },
  [`${D}/logs/access`]: { success: true, lines: ['GET / 200'], total: 1, log_path: '/var/log/a.log' },
  [`${D}/backups`]: { backups: [{ name: 'files-1.tar.gz', size: 2048, type: 'files', origin: 'manual', legacy: false, restorable: true, created_at: '2026-10-08T10:00:00Z' }] },
  [`${D}/databases`]: { databases: [{ id: 3, name: 'shop', type: 'mariadb', user: 'shop', created_at: '' }], available_types: ['mysql'] },
  [`${D}/backups/schedule`]: { enabled: false, version: 'v1' },
  [`${D}/cron`]: { jobs: [{ id: 'a1', schedule: '0 * * * *', command: 'php artisan schedule:run', enabled: true, comment: '' }], version: 'c1' },
};
// What the same read answers when the server really has nothing.
const none = {
  [`${D}/dns/records`]: { records: [] },
  [`${D}/app/logs`]: { lines: [] },
  '/api/v1/apps': { apps: null },
  [`${D}/logs/access`]: { success: true, lines: [], total: 0, log_path: '/var/log/a.log' },
  [`${D}/backups`]: { backups: [] },
  [`${D}/databases`]: { databases: [], available_types: [] },
};

const originalFetch = globalThis.fetch;
let tree;
let withheld;
let requests;
const answer = (body, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
function serve(overrides = {}) {
  requests = [];
  globalThis.fetch = async (input, init = {}) => {
    const url = String(input);
    const path = url.split('?')[0];
    requests.push(`${init.method || 'GET'} ${path}`);
    const reply = path in overrides ? overrides[path] : good[path];
    if (typeof reply === 'function') return reply(init, url);
    if (reply === undefined) return answer({ error: 'no route' }, 404);
    return answer(reply);
  };
}
const HANG = () => new Promise((resolve) => { withheld.push(resolve); });
const DROP = () => { throw new TypeError('network'); };
const REFUSED = () => answer({ error: 'agent unavailable', code: 'INTERNAL' }, 502);
const NOT_THE_CONTRACT = () => answer('<html>', 200);
// A gateway in front of the Panel that lost the Panel's answer.
const GATEWAY = () => new Response('<html>502 Bad Gateway</html>', { status: 502, headers: { 'Content-Type': 'text/html' } });

async function mount(element) {
  globalThis.currentTest = { toasts: [] };
  withheld = [];
  await act(async () => { tree = Renderer.create(element); });
  await settle();
}
async function settle() { await act(async () => { await new Promise((resolve) => setTimeout(resolve, 15)); }); }
async function unmount() {
  for (const resolve of withheld) resolve(answer({}, 500));
  if (tree) await act(async () => tree.unmount());
  tree = undefined;
  globalThis.fetch = originalFetch;
  delete globalThis.currentTest;
}
// What is on screen: text inside an `aria-hidden` box that is also `invisible`
// is neither drawn nor read out. The one such box (the room the signing card
// keeps for its usual answer) is pinned by its own test below.
const offScreen = (node) => node.props?.['aria-hidden'] === 'true' && String(node.props.className || '').split(' ').includes('invisible');
const text = () => {
  const walk = (node) => (typeof node === 'string' ? node : !node || offScreen(node) ? '' : Array.isArray(node) ? node.map(walk).join(' ') : walk(node.children));
  return walk(tree.toJSON());
};
const buttons = () => tree.root.findAll((node) => node.type === 'button');
// What a button is called: its text, and the name an icon-only button carries.
const label = (node) => {
  const walk = (n) => (typeof n === 'string' ? n : (n.children || []).map(walk).join(''));
  return `${walk(node)} ${node.props['aria-label'] ?? ''}`.trim();
};
const enabled = (names) => buttons().filter((node) => !node.props.disabled && names.some((name) => label(node).includes(name))).map(label);
const fields = () => tree.root.findAll((node) => ['input', 'textarea', 'select'].includes(node.type));
const press = async (name) => {
  const node = buttons().find((item) => label(item).includes(name));
  assert.ok(node, `no button ${name}: ${buttons().map(label).join(' | ')}`);
  assert.ok(!node.props.disabled, `${name} is disabled`);
  await act(async () => { await node.props.onClick({ preventDefault() {} }); });
  await settle();
};
globalThis.confirm = () => true;

// --- The table ---------------------------------------------------------------------
const domain = { domainId: 1, domainName: 'example.com' };
const el = (Component, props = {}) => () => React.createElement(Component, { ...domain, ...props });
const php = el(DomainPHPSettings, { currentVersion: '8.3', onVersionChange() {} });
const screens = [
  { name: 'DNS, the zone', element: el(DomainDNSManager), read: `${D}/dns/zone`, negative: ['dns.zoneMissing', 'dns.noRecords'], changes: ['dns.enableZone', 'dns.addRecord', 'dns.republish', 'dns.confirmDelete'], checking: 'dns.checking', unknown: 'dns.zoneUnknown' },
  { name: 'DNS, the records', element: el(DomainDNSManager), read: `${D}/dns/records`, negative: ['dns.noRecords', 'common.itemsTotal{"n":"0"}'], changes: ['dns.addRecord', 'dns.confirmDelete', 'dns.save'], checking: 'dns.records.checking', unknown: 'dns.records.unknown', empty: 'dns.noRecords' },
  { name: 'DNS, whether the zone is signed', element: el(DomainDNSManager), read: `${D}/dnssec`, negative: ['dnssec.offHint'], changes: ['dnssec.sign'], checking: 'dnssec.checking', unknown: 'dnssec.unknown', negativeAnswer: [{ secured: false, ds: null }, 'dnssec.offHint'] },
  { name: 'Hosting type, the saved settings', element: el(HostingTypePanel), read: `${D}/hosting`, negative: ['hosting.app.state.inactive'], changes: ['hosting.save', 'services.start', 'services.stop', 'services.restart'], formFields: true, checking: 'hosting.checking', unknown: 'hosting.unknown' },
  { name: 'Hosting type, the installed Node.js versions', element: el(HostingTypePanel), read: '/api/v1/runtimes/node', negative: [], changes: [], options: ['22.3.0'], checking: 'hosting.nodeChecking', unknown: 'hosting.nodeUnknown' },
  { name: 'Hosting type, the application’s state', element: el(HostingTypePanel), read: `${D}/app/status`, negative: ['hosting.app.state.inactive', 'hosting.app.state.failed'], changes: ['services.start', 'services.stop', 'services.restart'], checking: 'hosting.app.checking', unknown: 'hosting.app.unknown', negativeAnswer: [{ exists: true, active: 'inactive', pid: 0, memory_mb: 0 }, 'hosting.app.state.inactive'] },
  { name: 'Hosting type, the application’s logs', element: el(HostingTypePanel), read: `${D}/app/logs`, negative: ['hosting.app.empty'], changes: [], checking: 'hosting.app.logsChecking', unknown: 'hosting.app.unknown', empty: 'hosting.app.empty' },
  { name: 'PHP settings', element: php, read: `${D}/php`, negative: ['php.poolUnavailable', 'php.applyFirst'], changes: ['php.apply', 'php.savePool'], formFields: true, checking: 'php.checking', unknown: 'php.unknown', negativeAnswer: [{ php_version: '8.3', pool_name: 'example_com', available_versions: [] }, 'php.poolUnavailable'] },
  { name: 'General settings', element: el(DomainGeneralSettings), read: `${D}/general`, negative: ['general.noAliases', 'general.redirectWwwUnavailable'], changes: ['general.save', 'general.add', 'common.remove'], formFields: true, checking: 'general.checking', unknown: 'general.unknown', negativeAnswer: [{ ...good[`${D}/general`], aliases: null }, 'general.noAliases'] },
  { name: 'Applications', element: el(DomainAppsPanel), read: '/api/v1/apps', negative: ['apps.empty'], changes: ['apps.install'], checking: 'apps.checking', unknown: 'apps.unknown', empty: 'apps.empty' },
  { name: 'Mail authentication', element: el(MailAuthPanel), read: `${D}/mail/auth`, negative: ['mailauth.status.missing', 'mailauth.status.no_key', 'mailauth.signingMissing'], changes: ['mailauth.apply', 'mailauth.generateKey'], checking: 'mailauth.checking', unknown: 'mailauth.unknown', negativeAnswer: [{ ...good[`${D}/mail/auth`], spf: authRecord('missing') }, 'mailauth.status.missing'] },
  { name: 'Logs', element: el(DomainLogsViewer), read: `${D}/logs/access`, negative: ['logs.empty', 'logs.linesN{"n":"0"}'], changes: ['logs.clear', 'logs.download'], checking: 'logs.checking', unknown: 'logs.unknown', empty: 'logs.empty' },
  { name: 'Backups, the list', element: el(DomainBackupManager), read: `${D}/backups`, negative: ['backup.empty'], changes: ['backup.restore', 'backup.delete', 'backup.files', 'backup.create', 'backup.full'], checking: 'backup.checking', unknown: 'backup.unknown', empty: 'backup.empty' },
  { name: 'Backups, the linked databases', element: el(DomainBackupManager), read: `${D}/databases`, negative: ['backup.noDatabases', 'backup.fullDesc{"count":0}'], changes: ['backup.create', 'backup.full'], checking: 'backup.loadingDatabases', unknown: 'backup.databasesUnknown', empty: 'backup.noDatabases' },
];

const withholdings = [
  ['still on its way', HANG, 'checking'],
  ['dropped', DROP, 'unknown'],
  ['refused', REFUSED, 'unknown'],
  ['not the contract', NOT_THE_CONTRACT, 'unknown'],
];

for (const screen of screens) {
  for (const [how, reply, state] of withholdings) {
    test(`${screen.name}: ${how} → nothing negative, nothing that changes it enabled`, async () => {
      serve({ [screen.read]: reply });
      await mount(screen.element());
      try {
        const shown = text();
        for (const phrase of screen.negative) assert.ok(!shown.includes(phrase), `"${phrase}" is on screen while the read is ${how}: ${shown.slice(0, 400)}`);
        assert.deepEqual(enabled(screen.changes), [], 'a control that changes something is enabled');
        if (screen.formFields) assert.equal(fields().length, 0, 'a form was built from nothing');
        if (screen.options) {
          const listed = tree.root.findAll((node) => node.type === 'option').map((node) => node.props.value);
          assert.deepEqual(listed.filter((value) => value !== ''), screen.options, 'a version the server did not name is listed');
        }
        if (state === 'checking') {
          assert.ok(shown.includes(screen.checking), `the checking line is missing: ${shown.slice(0, 400)}`);
          assert.ok(!shown.includes(screen.unknown), 'a read that has not answered is not reported as failed');
          assert.equal(enabled(['common.retry']).length, 0, 'Retry is offered for a read that has not failed');
        } else {
          assert.ok(shown.includes(screen.unknown), `the could-not-check sentence is missing: ${shown.slice(0, 400)}`);
          assert.ok(enabled(['common.retry']).length >= 1, 'Retry is not offered');
          assert.deepEqual(globalThis.currentTest.toasts, [], 'a failed read raised a toast');
          // Retry reads, and only reads.
          serve();
          requests = [];
          await press('common.retry');
          assert.ok(requests.length > 0 && requests.every((line) => line.startsWith('GET ')), `Retry sent something other than a read: ${requests.join(', ')}`);
          assert.ok(!text().includes(screen.unknown), 'the notice stays after a successful read');
        }
      } finally { await unmount(); }
    });
  }
  if (screen.empty) {
    test(`${screen.name}: a known empty answer → the empty state, and only then`, async () => {
      serve({ [screen.read]: none[screen.read] });
      await mount(screen.element());
      try {
        assert.ok(text().includes(screen.empty), text().slice(0, 400));
      } finally { await unmount(); }
    });
  }
  if (screen.negativeAnswer) {
    test(`${screen.name}: the negative is said once the server has said it`, async () => {
      serve({ [screen.read]: screen.negativeAnswer[0] });
      await mount(screen.element());
      try {
        assert.ok(text().includes(screen.negativeAnswer[1]), text().slice(0, 400));
        assert.equal(enabled(['common.retry']).length, 0);
      } finally { await unmount(); }
    });
  }
}

// --- The zone: 404 is the server's answer, nothing else is ---------------------------
test('DNS: only the server’s 404 for this domain’s zone says there is no zone', async () => {
  serve({ [`${D}/dns/zone`]: () => answer({ error: 'zone not found' }, 404) });
  await mount(el(DomainDNSManager)());
  try {
    assert.ok(text().includes('dns.zoneMissing'));
    assert.deepEqual(enabled(['dns.enableZone']), ['dns.enableZone']);
    assert.ok(!requests.includes(`GET ${D}/dns/records`), 'the records of a zone that does not exist were asked for');
  } finally { await unmount(); }
  for (const status of [401, 403, 500, 503]) {
    serve({ [`${D}/dns/zone`]: () => answer({ error: 'no' }, status) });
    await mount(el(DomainDNSManager)());
    try {
      assert.ok(!text().includes('dns.zoneMissing'), `${status} was read as "no zone"`);
      assert.ok(text().includes('dns.zoneUnknown'));
    } finally { await unmount(); }
  }
});

test('DNS: a read-only or externally managed zone is offered nothing that changes it', async () => {
  serve();
  await mount(el(DomainDNSManager, { readOnly: true })());
  try {
    assert.deepEqual(enabled(['dns.addRecord', 'dns.republish', 'dns.confirmDelete', 'dnssec.sign']), []);
    assert.deepEqual(enabled(['dns.refresh']), ['dns.refresh']);
  } finally { await unmount(); }
  serve({ [`${D}/dns/zone`]: { type: 'EXTERNAL', management: 'external' } });
  await mount(el(DomainDNSManager)());
  try {
    assert.ok(text().includes('dns.externalTitle'));
    assert.deepEqual(enabled(['dns.addRecord', 'dns.republish', 'dns.confirmDelete', 'dnssec.sign']), []);
  } finally { await unmount(); }
});

// --- A change whose answer did not arrive ---------------------------------------------
// screen, the change, how it is started, the read that follows it, and the
// controls that must stay off until that read has answered.
const openAddRecord = async () => {
  await press('dns.addRecord');
  const value = fields().find((node) => node.props.placeholder === '192.168.1.1');
  await act(async () => { value.props.onChange({ target: { value: '192.0.2.9' } }); });
};
const lost = [
  { name: 'DNS, adding a record', element: el(DomainDNSManager), change: `POST ${D}/dns/records`, path: `${D}/dns/records`, start: async () => { await openAddRecord(); await press('dns.save'); }, reread: `${D}/dns/records`, held: ['dns.save', 'dns.confirmDelete', 'dns.republish'], after: 'not-made' },
  { name: 'DNS, deleting a record', element: el(DomainDNSManager), change: `DELETE ${D}/dns/records`, path: `${D}/dns/records`, start: () => press('dns.confirmDelete'), reread: `${D}/dns/records`, held: ['dns.addRecord', 'dns.confirmDelete', 'dns.republish'] },
  { name: 'DNS, publishing the zone', element: el(DomainDNSManager), change: `POST ${D}/dns/zone`, path: `${D}/dns/zone`, start: () => press('dns.republish'), reread: `${D}/dns/zone`, held: ['dns.addRecord', 'dns.confirmDelete', 'dns.republish'] },
  { name: 'DNS, signing the zone', element: el(DomainDNSManager), with: { [`${D}/dnssec`]: { secured: false, ds: null } }, change: `POST ${D}/dnssec`, path: `${D}/dnssec`, start: () => press('dnssec.sign'), reread: `${D}/dnssec`, held: ['dnssec.sign'] },
  { name: 'Hosting type, applying', element: el(HostingTypePanel), change: `PUT ${D}/hosting`, path: `${D}/hosting`, start: () => press('hosting.save'), reread: `${D}/hosting`, held: ['hosting.save'], after: 'made' },
  { name: 'Hosting type, stopping the application', element: el(HostingTypePanel), change: `POST ${D}/app/stop`, path: `${D}/app/stop`, start: () => press('services.stop'), reread: `${D}/app/status`, held: ['services.start', 'services.stop', 'services.restart'] },
  { name: 'PHP, saving the pool', element: php, change: `POST ${D}/php/pool`, path: `${D}/php/pool`, start: async () => {
    const form = tree.root.findByType('form');
    await act(async () => { await form.props.onSubmit({ preventDefault() {}, currentTarget: {} }); });
    await settle();
  }, reread: `${D}/php`, held: ['php.savePool', 'php.apply'], after: 'not-made' },
  { name: 'General settings, adding an alias', element: el(DomainGeneralSettings), change: `POST ${D}/aliases`, path: `${D}/aliases`, start: async () => {
    const input = fields().find((node) => node.props.type === 'text');
    await act(async () => { input.props.onChange({ target: { value: 'more.example.net' } }); });
    await press('general.add');
  }, reread: `${D}/general`, held: ['general.save', 'general.add', 'common.remove'], after: 'not-made' },
  { name: 'General settings, removing an alias', element: el(DomainGeneralSettings), change: `DELETE ${D}/aliases/alias.example.net`, path: `${D}/aliases/alias.example.net`, start: () => press('common.remove'), reread: `${D}/general`, held: ['general.save', 'general.add', 'common.remove'] },
  { name: 'Applications, installing', element: el(DomainAppsPanel), change: `POST ${D}/apps/install`, path: `${D}/apps/install`, start: () => press('apps.install'), reread: '/api/v1/apps', held: ['apps.install'], where: 'apps.resultUnknownWhere' },
  { name: 'Mail authentication, publishing a record', element: el(MailAuthPanel), change: `POST ${D}/mail/auth/apply`, path: `${D}/mail/auth/apply`, start: () => press('mailauth.apply'), reread: `${D}/mail/auth`, held: ['mailauth.apply'] },
  { name: 'Logs, clearing', element: el(DomainLogsViewer), change: `DELETE ${D}/logs/access`, path: `${D}/logs/access`, start: () => press('logs.clear'), reread: `${D}/logs/access`, held: ['logs.clear'] },
  { name: 'Backups, creating', element: el(DomainBackupManager), change: `POST ${D}/backups`, path: `${D}/backups`, start: () => press('backup.files'), reread: `${D}/backups`, held: ['backup.files', 'backup.create', 'backup.full', 'backup.restore', 'backup.delete'], asked: true },
  { name: 'Backups, restoring', element: el(DomainBackupManager), change: `POST ${D}/backups/restore`, path: `${D}/backups/restore`, start: () => press('backup.restore'), reread: `${D}/backups`, held: ['backup.files', 'backup.create', 'backup.full', 'backup.restore', 'backup.delete'], asked: true },
  { name: 'Backups, deleting', element: el(DomainBackupManager), change: `DELETE ${D}/backups`, path: `${D}/backups`, start: () => press('backup.delete'), reread: `${D}/backups`, held: ['backup.files', 'backup.create', 'backup.full', 'backup.restore', 'backup.delete'] },
];
globalThis.FormData ??= class {};
const realFormData = globalThis.FormData;

for (const row of lost) {
  for (const [how, reply] of [['the connection drops', DROP], ['a gateway answers in the Panel’s place', GATEWAY]]) {
    test(`${row.name}: ${how} → result unknown, nothing sent twice, a read-only re-read, controls held`, async () => {
      // The pool form is read through FormData; the test supplies the fields.
      globalThis.FormData = class { get(name) { return { pm: 'ondemand', user: 'site1', group: 'site1' }[name] ?? '5'; } };
      let rereadHeld = null;
      let changing = false;
      const isChange = (init) => init.method && init.method !== 'GET';
      const overrides = { ...row.with };
      const base = (path) => (path in (row.with ?? {}) ? row.with[path] : good[path]);
      // The change itself is lost; the read that follows it is withheld, so
      // the state "result unknown, not read again yet" can be looked at.
      overrides[row.path] = (init) => {
        if (isChange(init)) { changing = true; return reply(); }
        if (changing && row.reread === row.path) return new Promise((resolve) => { rereadHeld = () => resolve(answer(base(row.path))); });
        return answer(base(row.path));
      };
      if (row.reread !== row.path) {
        overrides[row.reread] = () => (changing
          ? new Promise((resolve) => { rereadHeld = () => resolve(answer(base(row.reread))); })
          : answer(base(row.reread)));
      }
      serve(overrides);
      await mount(row.element());
      try {
        assert.ok(!text().includes('common.resultUnknown'));
        await row.start();
        const sent = () => requests.filter((line) => line === row.change).length;
        assert.equal(sent(), 1, `the change was sent ${sent()} times: ${requests.join(', ')}`);
        // A route that carries an identity (D-029) was asked once more by
        // the fetch interceptor before the screen heard of it; its notice says
        // so and never says "nothing is sent a second time".
        if (row.asked) {
          assert.ok(text().includes('common.lostAsked') && text().includes('common.lostStateReading'), `no "asked again" notice: ${text().slice(0, 400)}`);
          assert.ok(!text().includes('common.resultUnknown'), 'a change that was asked for again is told as "nothing is sent a second time"');
        } else {
          assert.ok(text().includes('common.resultUnknown'), `no result-unknown notice: ${text().slice(0, 400)}`);
          assert.ok(!text().includes('common.lost'), 'a change without an identity is told as asked for again');
        }
        assert.equal(tree.root.findAll((node) => node.props['data-lost-cause'] === (row.asked ? 'asked' : 'dropped')).length, 1, 'the notice does not carry its cause');
        if (row.where) assert.ok(text().includes(row.where), 'the notice does not say where to look');
        assert.ok(tree.root.findAll((node) => node.props['data-result-unknown'] === 'holding').length === 1, 'the notice is not in its holding state');
        assert.deepEqual(enabled(row.held), [], 'a changing control is enabled while the result is unknown and unread');
        assert.ok(!globalThis.currentTest.toasts.some(([tone]) => tone === 'success'), 'an unknown result was reported as done');
        assert.ok(rereadHeld, `the state was not read again after the lost answer: ${requests.join(', ')}`);

        // The re-read answers: the notice stays and says when; the person may act again.
        const before = requests.length;
        await act(async () => { rereadHeld(); });
        await settle();
        assert.equal(sent(), 1, 'the re-read sent the change again');
        // A form that asks the re-read state a question says its answer; in
        // these rows the server sends back the state from before the change
        // (Apply sent what was already saved, so there the state shows it).
        const after = row.after ?? 'read';
        const sentence = (row.asked
          ? { read: 'common.lostStateRead', made: 'common.lostStateMade', 'not-made': 'common.lostStateNotMade' }
          : { read: 'common.resultUnknownRead', made: 'common.resultUnknownMade', 'not-made': 'common.resultUnknownNotMade' })[after];
        assert.ok(text().includes(sentence), `the notice left by itself or does not say ${sentence}: ${text().slice(0, 400)}`);
        assert.equal(tree.root.findAll((node) => node.props['data-result-unknown'] === after).length, 1, `the notice is not in its "${after}" state`);
        assert.ok(requests.slice(before).every((line) => line.startsWith('GET ')), 'something other than a read followed');

        // "Check again" only reads; "Close" removes the notice. A change the
        // state shows as made has nothing left to check: only "Close".
        changing = false;
        requests = [];
        if (after === 'made') {
          assert.equal(enabled(['common.checkAgain']).length, 0, '"Check again" is offered for a change that is shown as saved');
        } else {
          await press('common.checkAgain');
          assert.ok(requests.length > 0 && requests.every((line) => line.startsWith('GET ')), `Check again sent: ${requests.join(', ')}`);
        }
        await press('common.close');
        assert.ok(!text().includes('common.resultUnknown') && !text().includes('common.lost'), 'the notice stayed after Close');
      } finally {
        globalThis.FormData = realFormData;
        await unmount();
      }
    });
  }
}

test('a refusal the Panel itself sent is the server’s reason, not an unknown result', async () => {
  serve({ [`${D}/backups`]: (init) => (init.method === 'POST' ? answer({ error: 'quota reached', code: 'QUOTA' }, 502) : answer(good[`${D}/backups`])) });
  await mount(el(DomainBackupManager)());
  try {
    await press('backup.files');
    assert.ok(!text().includes('common.resultUnknown'), 'a JSON refusal with a gateway status was treated as a lost answer');
    assert.deepEqual(globalThis.currentTest.toasts.map(([tone]) => tone), ['error']);
    assert.equal(enabled(['backup.files']).length, 1, 'a refused change left the control off');
  } finally { await unmount(); }
});

test('the read after a lost answer fails too: the controls stay off and the notice says so', async () => {
  let dropped = false;
  serve({ [`${D}/backups`]: (init) => {
    if (init.method === 'POST') { dropped = true; return DROP(); }
    return dropped ? REFUSED() : answer(good[`${D}/backups`]);
  } });
  await mount(el(DomainBackupManager)());
  try {
    await press('backup.files');
    await settle();
    assert.ok(text().includes('common.lostAsked') && text().includes('common.lostStateUnread'), text().slice(0, 400));
    assert.deepEqual(enabled(['backup.files', 'backup.create', 'backup.full', 'backup.restore', 'backup.delete']), []);
    assert.equal(enabled(['common.close']).length, 0, 'the notice can be closed before anything was read');
    assert.equal(requests.filter((line) => line === `POST ${D}/backups`).length, 1);
  } finally { await unmount(); }
});

test('a later change that is answered settles the earlier question', async () => {
  let posts = 0;
  serve({ [`${D}/backups`]: (init) => {
    if (init.method === 'POST') { posts += 1; if (posts === 1) return DROP(); return answer({ success: true }); }
    return answer(good[`${D}/backups`]);
  } });
  await mount(el(DomainBackupManager)());
  try {
    await press('backup.files');
    await settle();
    assert.ok(text().includes('common.lostStateRead'));
    await press('backup.files');
    assert.equal(posts, 2, 'the person’s own second request was not sent');
    assert.ok(!text().includes('common.resultUnknown') && !text().includes('common.lost'), 'the notice stays after a change that was answered');
  } finally { await unmount(); }
});

// --- Polling: one calm notice, never a toast, never a change ---------------------------
test('logs with auto-refresh on: refused polls raise no toast, keep the lines under one notice, and send nothing but reads', async () => {
  let fail = false;
  serve({ [`${D}/logs/access`]: () => (fail ? REFUSED() : answer(good[`${D}/logs/access`])) });
  mock.timers.enable({ apis: ['setInterval'] });
  await mount(el(DomainLogsViewer)());
  try {
    const box = tree.root.findAll((node) => node.type === 'input' && node.props.type === 'checkbox')[0];
    await act(async () => { box.props.onChange({ target: { checked: true } }); });
    fail = true;
    for (let tick = 0; tick < 4; tick += 1) {
      await act(async () => { mock.timers.tick(5000); });
      await settle();
    }
    assert.ok(requests.filter((line) => line === `GET ${D}/logs/access`).length >= 4, `the polls did not run: ${requests.join(', ')}`);
    assert.deepEqual(globalThis.currentTest.toasts, [], 'a refused poll raised a toast');
    const shown = text();
    assert.equal(shown.split('common.staleNotice').length - 1, 1, 'not exactly one stale notice');
    assert.ok(shown.includes('GET / 200'), 'the lines already read left the screen');
    assert.ok(!shown.includes('logs.empty'));
    assert.deepEqual(enabled(['logs.clear']), [], 'Clear is offered on a log that could not be read again');
    assert.ok(requests.every((line) => line.startsWith('GET ')), 'a poll sent a change');
    // The next good poll removes the notice by itself.
    fail = false;
    await act(async () => { mock.timers.tick(5000); });
    await settle();
    assert.ok(!text().includes('common.staleNotice'));
  } finally {
    mock.timers.reset();
    await unmount();
  }
});

test('the application panel: a failed poll keeps the earlier state, marked once, with start and stop off; polling sends only reads', async () => {
  let fail = false;
  serve({
    [`${D}/app/status`]: () => (fail ? DROP() : answer(good[`${D}/app/status`])),
    [`${D}/app/logs`]: () => (fail ? DROP() : answer(good[`${D}/app/logs`])),
  });
  mock.timers.enable({ apis: ['setInterval'] });
  await mount(el(HostingTypePanel)());
  try {
    assert.deepEqual(enabled(['services.stop']), ['services.stop']);
    fail = true;
    for (let tick = 0; tick < 3; tick += 1) {
      await act(async () => { mock.timers.tick(5000); });
      await settle();
    }
    const shown = text();
    assert.equal(shown.split('hosting.app.stale').length - 1, 1, `not exactly one notice: ${shown.slice(0, 500)}`);
    assert.ok(shown.includes('hosting.app.state.active'), 'the earlier state left the screen');
    assert.ok(shown.includes('listening on 3001'), 'the earlier log lines left the screen');
    assert.ok(!shown.includes('hosting.app.state.inactive') && !shown.includes('hosting.app.empty'));
    assert.deepEqual(enabled(['services.start', 'services.stop', 'services.restart']), []);
    assert.deepEqual(globalThis.currentTest.toasts, []);
    assert.ok(requests.every((line) => line.startsWith('GET ')), `a poll sent a change: ${requests.join(', ')}`);
  } finally {
    mock.timers.reset();
    await unmount();
  }
});

test('the live application panel follows the saved type, not the type picked in the form', async () => {
  serve({ [`${D}/hosting`]: { project_type: 'php', php_version: '8.3' } });
  await mount(el(HostingTypePanel)());
  try {
    await press('hosting.type.node');
    assert.ok(!requests.some((line) => line.includes('/app/')), `the application of a type that is not saved was asked about: ${requests.join(', ')}`);
    assert.ok(!text().includes('hosting.app.title'));
  } finally { await unmount(); }
});

test('useRefreshEvery does not ask again while a read is on its way, and stops with the screen', async () => {
  let asked = 0;
  let release;
  globalThis.fetch = async () => { asked += 1; return new Promise((resolve) => { release = () => resolve(answer({ ok: true })); }); };
  const decode = (raw) => raw;
  function Probe() {
    const handle = useRemote('/api/v1/probe-batch4', decode);
    useRefreshEvery(handle, 1000);
    return React.createElement('p', null, handle.remote.state);
  }
  mock.timers.enable({ apis: ['setInterval'] });
  withheld = [];
  globalThis.currentTest = { toasts: [] };
  await act(async () => { tree = Renderer.create(React.createElement(Probe)); });
  try {
    assert.equal(asked, 1);
    for (let tick = 0; tick < 5; tick += 1) await act(async () => { mock.timers.tick(1000); });
    assert.equal(asked, 1, 'a slow read was asked again before it answered');
    await act(async () => { release(); });
    await settle();
    await act(async () => { mock.timers.tick(1000); });
    assert.equal(asked, 2);
    await act(async () => { release(); });
    await settle();
    await act(async () => tree.unmount());
    tree = undefined;
    for (let tick = 0; tick < 3; tick += 1) mock.timers.tick(1000);
    assert.equal(asked, 2, 'the poll went on after the screen left');
  } finally {
    mock.timers.reset();
    await unmount();
  }
});

// --- Forms hold what the server sent ----------------------------------------------------
test('the PHP pool form holds the server’s values, not defaults', async () => {
  serve();
  await mount(php());
  try {
    const value = (name) => fields().find((node) => node.props.name === name).props.defaultValue;
    assert.equal(value('pm'), 'ondemand');
    assert.equal(value('pm_max_children'), 9);
    assert.equal(value('user'), 'site1');
  } finally { await unmount(); }
  assert.doesNotMatch(source('components/DomainPHPSettings.tsx'), /defaultValue=\{pc\.\w+ \|\|/, 'a pool field falls back to a default');
});

test('the hosting form shows the saved settings again after Apply, and the draft before it', async () => {
  let saved = { project_type: 'proxy', forward_to: 'http://127.0.0.1:9000' };
  serve({ [`${D}/hosting`]: (init) => {
    if (init.method === 'PUT') { saved = { ...JSON.parse(init.body), forward_to: 'http://127.0.0.1:9001' }; return answer({ success: true }); }
    return answer(saved);
  } });
  await mount(el(HostingTypePanel)());
  try {
    const target = () => fields().find((node) => node.props.placeholder === 'https://example.com');
    assert.equal(target().props.value, 'http://127.0.0.1:9000');
    await act(async () => { target().props.onChange({ target: { value: 'http://typed' } }); });
    assert.equal(target().props.value, 'http://typed', 'what was typed is not kept');
    await press('hosting.save');
    assert.equal(target().props.value, 'http://127.0.0.1:9001', 'the form goes on showing the draft as if it were saved');
  } finally { await unmount(); }
});

test('scheduled tasks: an answer that does not carry the list is not "no scheduled tasks"', async () => {
  serve({ [`${D}/cron`]: { version: 'c1' } });
  await mount(el(DomainCronManager)());
  try {
    assert.ok(!text().includes('cron.empty'), text().slice(0, 300));
    assert.ok(text().includes('cron.unknown'));
  } finally { await unmount(); }
  serve({ [`${D}/cron`]: { jobs: null, version: 'c1' } });
  await mount(el(DomainCronManager)());
  try {
    assert.ok(text().includes('cron.empty'));
  } finally { await unmount(); }
});

// --- Scheduled tasks: why the list could not be read (10 Oct 2026) ---------------------------
// The cron answer names the cause when the server verified one. The screen
// says that cause in its own words, on the neutral surface: it is the server
// owner's rule, not a failure. Any other answer keeps the sentence that names
// no cause, followed by the line crontab printed when it printed one.
const cronUnreadable = (extra = {}) => () => answer({ error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE', reason: 'scheduled_tasks', ...extra }, 502);
const crontabSaid = 'You (site1) are not allowed to use this program (crontab)';
for (const row of [
  { name: 'the user is not in /etc/cron.allow', reply: cronUnreadable({ detail: 'cron_allow', vars: { detail: crontabSaid } }), sentence: 'cron.unknown.cron_allow', cause: 'cron_allow' },
  { name: 'the user is in /etc/cron.deny', reply: cronUnreadable({ detail: 'cron_deny', vars: { detail: crontabSaid } }), sentence: 'cron.unknown.cron_deny', cause: 'cron_deny' },
  { name: 'no verified cause, crontab printed a line', reply: cronUnreadable({ vars: { detail: 'crontab: can\'t open /var/spool/cron: Permission denied' } }), sentence: 'cron.unknown', said: 'crontab: can\'t open /var/spool/cron: Permission denied' },
  { name: 'no verified cause, nothing printed', reply: cronUnreadable(), sentence: 'cron.unknown' },
  { name: 'a cause this screen has no words for', reply: cronUnreadable({ detail: 'spool_moved' }), sentence: 'cron.unknown' },
  { name: 'a refusal that is not this answer', reply: () => answer({ error: 'agent unavailable', code: 'INTERNAL', detail: 'cron_allow', vars: { detail: crontabSaid } }, 502), sentence: 'cron.unknown' },
]) {
  test(`scheduled tasks could not be read, ${row.name} → ${row.sentence}, Retry, nothing that changes`, async () => {
    serve({ [`${D}/cron`]: row.reply });
    await mount(el(DomainCronManager)());
    try {
      const shown = text();
      const sentences = ['cron.unknown.cron_allow', 'cron.unknown.cron_deny', 'cron.unknown.said'];
      for (const other of sentences.filter((key) => key !== row.sentence && !(row.said && key === 'cron.unknown.said'))) {
        assert.ok(!shown.includes(other), `"${other}" is on screen: ${shown.slice(0, 400)}`);
      }
      assert.ok(shown.includes(row.sentence), shown.slice(0, 400));
      assert.ok(!shown.includes('cron.empty'), 'an unreadable crontab is drawn as "no scheduled tasks"');
      assert.deepEqual(enabled(['cron.add', 'cron.save', 'cron.delete', 'cron.edit']), [], 'a task can be added or changed against a crontab that was not read');
      assert.deepEqual(globalThis.currentTest.toasts, [], 'a failed read raised a toast');
      const specific = tree.root.findAll((node) => node.props['data-cron-unreadable'] !== undefined);
      const alerts = tree.root.findAll((node) => node.props.role === 'alert');
      if (row.cause) {
        // The server owner's rule: said plainly, with no attention colour and
        // no alarm for a screen reader. The generic sentence is not said too.
        assert.equal(specific.length, 1);
        assert.equal(specific[0].props['data-cron-unreadable'], row.cause);
        assert.equal(specific[0].props.role, 'status');
        assert.doesNotMatch(specific[0].props.className, /warning|danger/, 'a server policy is drawn as a problem');
        assert.equal(alerts.length, 0, 'a server policy is announced as an alert');
        assert.ok(!shown.replace(row.sentence, '').includes('cron.unknown'), 'the neutral sentence is said beside the cause');
        assert.ok(!shown.includes(crontabSaid), 'the program line is repeated under a cause that already says it');
      } else {
        assert.equal(specific.length, 0, 'a cause the server did not verify is named');
        assert.equal(alerts.length, 1);
        const line = tree.root.findAll((node) => node.props['data-cron-said'] !== undefined);
        assert.equal(line.length, row.said ? 1 : 0);
        if (row.said) {
          assert.ok(shown.includes('cron.unknown.said') && shown.includes(row.said), `what crontab said is not shown: ${shown.slice(0, 400)}`);
          assert.ok(shown.indexOf('cron.unknown') < shown.indexOf(row.said), 'the program line comes before the sentence');
        }
      }
      // Retry stays in every case, and only reads.
      assert.equal(enabled(['common.retry']).length, 1, 'Retry is not offered');
      serve();
      requests = [];
      await press('common.retry');
      assert.ok(requests.length > 0 && requests.every((line) => line.startsWith('GET ')), `Retry sent something other than a read: ${requests.join(', ')}`);
      assert.ok(!text().includes('cron.unknown'), 'the notice stays after a successful read');
      assert.ok(text().includes('php artisan schedule:run'));
    } finally { await unmount(); }
  });
}

// --- After a lost answer: what the re-read state shows (10 Oct 2026) -------------------------
// A form whose answer was lost asks the state that was read again one
// question. It shows the change: the form is closed or cleared and the notice
// says it was saved, so the same record is not one press from being saved
// twice. It does not show it: what was typed stays and the notice says so. It
// could not be read: the controls stay off. Nothing is ever sent a second time
// by the page.
// `afterwards` answers every other request and is told whether the change has
// been lost yet, so a row can appear only after it.
const loseOnce = (path, method, afterwards) => {
  let lostOne = false;
  return (init) => {
    if ((init.method || 'GET') === method && !lostOne) { lostOne = true; return DROP(); }
    return afterwards(init, lostOne);
  };
};
const noticeState = () => tree.root.findAll((node) => node.props['data-result-unknown'] !== undefined).map((node) => node.props['data-result-unknown']);
const recordField = () => fields().find((node) => node.props.placeholder === '192.168.1.1');
const newRecord = { id: 8, name: 'example.com', type: 'A', content: '192.0.2.9', ttl: 3600, disabled: false };

test('DNS, a record whose answer was lost and that the records read again show: the form closes and the notice says it was saved', async () => {
  serve({ [`${D}/dns/records`]: loseOnce(`${D}/dns/records`, 'POST', (_, lostOne) => answer({ records: lostOne ? [...good[`${D}/dns/records`].records, newRecord] : good[`${D}/dns/records`].records })) });
  await mount(el(DomainDNSManager)());
  try {
    await openAddRecord();
    await press('dns.save');
    await settle();
    assert.deepEqual(noticeState(), ['made']);
    assert.ok(text().includes('common.resultUnknownMade') && !text().includes('common.resultUnknownNotMade'));
    assert.equal(recordField(), undefined, 'the form that sent the record is still open: one press would send it again');
    assert.equal(enabled(['dns.save']).length, 0);
    assert.ok(text().includes('192.0.2.9'), 'the record is not in the list');
    assert.equal(requests.filter((line) => line === `POST ${D}/dns/records`).length, 1);
    assert.equal(tree.root.findAll((node) => node.props.role === 'alert' && node.props['data-result-unknown']).length, 0, 'a saved change is announced as an alert');
    assert.ok(!globalThis.currentTest.toasts.some(([tone]) => tone === 'error'));
    // Nothing is left to check, and the form opened again starts empty, not
    // with the record that was saved.
    assert.equal(enabled(['common.checkAgain']).length, 0);
    await press('dns.addRecord');
    assert.equal(recordField().props.value, '');
    await press('common.close');
    assert.deepEqual(noticeState(), []);
  } finally { await unmount(); }
});

test('DNS, a record whose answer was lost and that the records read again do not show: the form keeps what was typed, and a later check that shows it closes the form', async () => {
  let landed = false;
  serve({ [`${D}/dns/records`]: loseOnce(`${D}/dns/records`, 'POST', () => answer({ records: landed ? [...good[`${D}/dns/records`].records, newRecord] : good[`${D}/dns/records`].records })) });
  await mount(el(DomainDNSManager)());
  try {
    await openAddRecord();
    await press('dns.save');
    await settle();
    assert.deepEqual(noticeState(), ['not-made']);
    assert.ok(text().includes('common.resultUnknownNotMade'));
    assert.equal(recordField().props.value, '192.0.2.9', 'what was typed is gone');
    assert.equal(enabled(['dns.save']).length, 1, 'the person cannot decide to send it again');
    assert.ok(!globalThis.currentTest.toasts.some(([tone]) => tone === 'success'));
    // The server was still working when the connection dropped: the record
    // appears, and the next check finds it.
    landed = true;
    await press('common.checkAgain');
    assert.deepEqual(noticeState(), ['made']);
    assert.equal(recordField(), undefined);
    assert.equal(requests.filter((line) => line === `POST ${D}/dns/records`).length, 1, 'the page sent the record a second time');
  } finally { await unmount(); }
});

test('DNS, a record whose answer was lost: a new row that is not clearly this record decides nothing, and an unread list keeps the controls off', async () => {
  const other = { id: 9, name: 'mail.example.com', type: 'A', content: '192.0.2.77', ttl: 3600, disabled: false };
  serve({ [`${D}/dns/records`]: loseOnce(`${D}/dns/records`, 'POST', (_, lostOne) => answer({ records: lostOne ? [...good[`${D}/dns/records`].records, other] : good[`${D}/dns/records`].records })) });
  await mount(el(DomainDNSManager)());
  try {
    await openAddRecord();
    await press('dns.save');
    await settle();
    assert.deepEqual(noticeState(), ['read']);
    assert.ok(text().includes('common.resultUnknownRead'));
    assert.equal(recordField().props.value, '192.0.2.9');
  } finally { await unmount(); }
  let dropped = false;
  serve({ [`${D}/dns/records`]: (init) => {
    if (init.method === 'POST') { dropped = true; return DROP(); }
    return dropped ? REFUSED() : answer(good[`${D}/dns/records`]);
  } });
  await mount(el(DomainDNSManager)());
  try {
    await openAddRecord();
    await press('dns.save');
    await settle();
    assert.deepEqual(noticeState(), ['holding']);
    assert.ok(text().includes('common.resultUnknownUnread'));
    assert.deepEqual(enabled(['dns.save', 'dns.confirmDelete', 'dns.republish']), [], 'a changing control is enabled although nothing was read again');
    assert.equal(recordField().props.value, '192.0.2.9');
  } finally { await unmount(); }
});

test('the server’s rewriting of a record is not "not there": full owner name, quotes, a trailing dot and case', async () => {
  const cases = [
    [{ name: 'www', type: 'CNAME', content: 'Target.Example.NET' }, { name: 'www.example.com', type: 'CNAME', content: 'target.example.net.' }],
    [{ name: '@', type: 'TXT', content: 'v=spf1 mx -all' }, { name: 'example.com', type: 'TXT', content: '"v=spf1 mx -all"' }],
    [{ name: 'example.com.', type: 'MX', content: 'mail.example.com' }, { name: 'example.com', type: 'MX', content: 'mail.example.com.' }],
  ];
  for (const [typed, stored] of cases) {
    serve({ [`${D}/dns/records`]: loseOnce(`${D}/dns/records`, 'POST', (_, lostOne) => answer({ records: lostOne ? [...good[`${D}/dns/records`].records, { id: 11, ttl: 3600, disabled: false, ...stored }] : good[`${D}/dns/records`].records })) });
    await mount(el(DomainDNSManager)());
    try {
      await press('dns.addRecord');
      const select = fields().find((node) => node.type === 'select');
      await act(async () => { select.props.onChange({ target: { value: typed.type } }); });
      const name = fields().find((node) => node.props.placeholder === 'dns.nameHint');
      await act(async () => { name.props.onChange({ target: { value: typed.name } }); });
      await act(async () => { recordField().props.onChange({ target: { value: typed.content } }); });
      await press('dns.save');
      await settle();
      assert.deepEqual(noticeState(), ['made'], `${typed.type} ${typed.name} ${typed.content} was not recognised as ${JSON.stringify(stored)}`);
    } finally { await unmount(); }
  }
});

test('General settings, an alias whose answer was lost: in the aliases read again the field is emptied; not in them, what was typed stays', async () => {
  const typeAlias = async () => {
    const input = fields().find((node) => node.props.type === 'text');
    await act(async () => { input.props.onChange({ target: { value: 'More.Example.net' } }); });
    await press('general.add');
    await settle();
  };
  const aliasField = () => fields().find((node) => node.props.type === 'text').props.value;
  let listed = true;
  const general = () => answer({ ...good[`${D}/general`], aliases: listed ? ['alias.example.net', 'more.example.net'] : ['alias.example.net'] });
  for (const shows of [true, false]) {
    listed = false;
    let lostOne = false;
    serve({
      [`${D}/aliases`]: () => { lostOne = true; listed = shows; return DROP(); },
      [`${D}/general`]: () => (lostOne ? general() : answer(good[`${D}/general`])),
    });
    await mount(el(DomainGeneralSettings)());
    try {
      await typeAlias();
      assert.deepEqual(noticeState(), [shows ? 'made' : 'not-made']);
      assert.equal(aliasField(), shows ? '' : 'More.Example.net');
      assert.equal(requests.filter((line) => line === `POST ${D}/aliases`).length, 1);
      assert.equal(text().includes('more.example.net'), shows);
    } finally { await unmount(); }
  }
});

test('General settings, the switch whose answer was lost: shown as saved when the settings read again hold it, kept when they do not', async () => {
  // The switch is read through FormData; an absent field is "off", and the
  // server has it on.
  globalThis.FormData = class { get() { return null; } };
  try {
    for (const taken of [true, false]) {
      let lostOne = false;
      serve({ [`${D}/general`]: (init) => {
        if (init.method === 'POST') { lostOne = true; return DROP(); }
        return answer({ ...good[`${D}/general`], redirect_www: lostOne && taken ? false : true });
      } });
      await mount(el(DomainGeneralSettings)());
      try {
        const toggle = () => fields().find((node) => node.props.name === 'redirect_www');
        assert.equal(toggle().props.defaultChecked, true);
        const form = tree.root.findByType('form');
        await act(async () => { await form.props.onSubmit({ preventDefault() {}, currentTarget: {} }); });
        await settle();
        assert.deepEqual(noticeState(), [taken ? 'made' : 'not-made']);
        // Saved: the switch is the server's "off". Not shown as saved: the
        // server says "on" and the switch still holds the "off" that was sent.
        assert.equal(toggle().props.defaultChecked, false, taken ? 'the switch does not show the saved value' : 'what was switched is gone');
        assert.equal(requests.filter((line) => line === `POST ${D}/general`).length, 1);
      } finally { await unmount(); }
    }
  } finally { globalThis.FormData = realFormData; }
});

test('Hosting type, Apply whose answer was lost: what was typed is put back when the settings read again are still the earlier ones', async () => {
  const command = () => fields().find((node) => node.props.placeholder === 'node server.js');
  for (const taken of [true, false]) {
    let lostOne = false;
    serve({ [`${D}/hosting`]: (init) => {
      if (init.method === 'PUT') { lostOne = true; return DROP(); }
      return answer({ ...good[`${D}/hosting`], start_command: lostOne && taken ? 'node other.js' : 'node server.js' });
    } });
    await mount(el(HostingTypePanel)());
    try {
      await act(async () => { command().props.onChange({ target: { value: 'node other.js' } }); });
      await press('hosting.save');
      await settle();
      assert.deepEqual(noticeState(), [taken ? 'made' : 'not-made']);
      assert.equal(command().props.value, 'node other.js', taken ? 'the form does not show the saved command' : 'what was typed was replaced by the server’s value unseen');
      assert.equal(requests.filter((line) => line === `PUT ${D}/hosting`).length, 1);
      assert.equal(enabled(['hosting.save']).length, 1);
    } finally { await unmount(); }
  }
  // Something else changed: neither the sent settings nor the earlier ones.
  let lostOne = false;
  serve({ [`${D}/hosting`]: (init) => {
    if (init.method === 'PUT') { lostOne = true; return DROP(); }
    return answer({ ...good[`${D}/hosting`], start_command: lostOne ? 'node third.js' : 'node server.js' });
  } });
  await mount(el(HostingTypePanel)());
  try {
    await act(async () => { command().props.onChange({ target: { value: 'node other.js' } }); });
    await press('hosting.save');
    await settle();
    assert.deepEqual(noticeState(), ['read']);
    assert.equal(command().props.value, 'node third.js');
  } finally { await unmount(); }
});

test('PHP, the pool whose answer was lost: the form keeps the sent values when the pool read again does not hold them', async () => {
  globalThis.FormData = class { get(name) { return { pm: 'ondemand', user: 'site1', group: 'site1' }[name] ?? '5'; } };
  const sentPool = { pm: 'ondemand', pm_max_children: 5, pm_start_servers: 5, pm_min_spare_servers: 5, pm_max_spare_servers: 5, user: 'site1', group: 'site1' };
  try {
    for (const taken of [true, false]) {
      let lostOne = false;
      serve({
        [`${D}/php/pool`]: () => { lostOne = true; return DROP(); },
        [`${D}/php`]: () => answer(lostOne && taken ? { ...good[`${D}/php`], pool_config: sentPool } : good[`${D}/php`]),
      });
      await mount(php());
      try {
        const value = (name) => fields().find((node) => node.props.name === name).props.defaultValue;
        assert.equal(value('pm_max_children'), 9);
        const form = tree.root.findByType('form');
        await act(async () => { await form.props.onSubmit({ preventDefault() {}, currentTarget: {} }); });
        await settle();
        assert.deepEqual(noticeState(), [taken ? 'made' : 'not-made']);
        assert.equal(value('pm_max_children'), 5, taken ? 'the form does not show the saved pool' : 'what was entered was replaced by the server’s value unseen');
        assert.equal(requests.filter((line) => line === `POST ${D}/php/pool`).length, 1);
      } finally { await unmount(); }
    }
  } finally { globalThis.FormData = realFormData; }
});

test('PHP, the version whose answer was lost: applied when the settings read again name it, still picked when they do not', async () => {
  for (const taken of [true, false]) {
    const told = [];
    let lostOne = false;
    serve({
      '/api/v1/hosting/capabilities': { ...good['/api/v1/hosting/capabilities'], php_versions: ['8.3', '8.4'] },
      [`${D}/php`]: (init) => {
        if (init.method === 'POST') { lostOne = true; return DROP(); }
        return answer({ ...good[`${D}/php`], php_version: lostOne && taken ? '8.4' : '8.3' });
      },
    });
    await mount(React.createElement(DomainPHPSettings, { ...domain, currentVersion: '8.3', onVersionChange: (version) => told.push(version) }));
    try {
      const picker = () => fields().find((node) => node.type === 'select' && !node.props.name);
      await act(async () => { picker().props.onChange({ target: { value: '8.4' } }); });
      await press('php.apply');
      await settle();
      assert.deepEqual(noticeState(), [taken ? 'made' : 'not-made']);
      assert.deepEqual(told, taken ? ['8.4'] : [], 'the page was told of a version the server did not confirm, or not told of one it did');
      if (!taken) assert.equal(picker().props.value, '8.4', 'the pick is gone');
      assert.equal(requests.filter((line) => line === `POST ${D}/php`).length, 1);
    } finally { await unmount(); }
  }
});

test('a change with no form to ask keeps the notice of the fourth batch: the person looks at what was read again', async () => {
  serve({ [`${D}/backups`]: loseOnce(`${D}/backups`, 'POST', () => answer(good[`${D}/backups`])) });
  await mount(el(DomainBackupManager)());
  try {
    await press('backup.files');
    await settle();
    assert.deepEqual(noticeState(), ['read']);
    assert.ok(text().includes('common.lostAsked') && text().includes('common.lostStateRead'));
  } finally { await unmount(); }
});

// D-029 with the fourth batch. On a route that carries an identity the Panel
// can say itself that the result is not known: it restarted while the change
// ran, or the first arrival is still running. Both are the unknown result, said
// in place with the state read again; neither is a red refusal.
for (const [code, cause, sentence] of [
  ['REQUEST_OUTCOME_UNKNOWN', 'interrupted', 'common.lostInterrupted'],
  ['REQUEST_IN_PROGRESS', 'running', 'common.lostRunning'],
]) {
  test(`Backups: the Panel answers ${code} → an unknown result in place, read again, not a failure`, async () => {
    let posts = 0;
    serve({ [`${D}/backups`]: (init) => {
      if (init.method === 'POST') { posts += 1; return answer({ error: 'server English', code, vars: { request_id: 'c'.repeat(32) } }, 409); }
      return answer(good[`${D}/backups`]);
    } });
    await mount(el(DomainBackupManager)());
    try {
      await press('backup.files');
      await settle();
      assert.equal(posts, 1);
      assert.equal(tree.root.findAll((node) => node.props['data-lost-cause'] === cause).length, 1, text().slice(0, 300));
      assert.ok(text().includes(sentence) && text().includes('common.lostStateRead'), text().slice(0, 400));
      assert.ok(!text().includes('server English') && !text().includes('common.resultUnknown'));
      assert.deepEqual(globalThis.currentTest.toasts, [], 'an unknown result was also shown as a refusal');
      assert.ok(requests.filter((line) => line === `GET ${D}/backups`).length >= 2, 'the list was not read again');
    } finally { await unmount(); }
  });
}

test('Backups: any other 409 on an identified route is the Panel’s own refusal, shown as one', async () => {
  serve({ [`${D}/backups/restore`]: () => answer({ error: 'server English', code: 'BACKUP_RESTORE_IN_PROGRESS' }, 409) });
  globalThis.confirm = () => true;
  await mount(el(DomainBackupManager)());
  try {
    await press('backup.restore');
    await settle();
    assert.equal(noticeState().length, 0, 'a refusal was drawn as an unknown result');
    assert.deepEqual(globalThis.currentTest.toasts.map(([tone]) => tone), ['error']);
  } finally { await unmount(); }
});

test('mail authentication: a record state this screen does not know is not "Missing"', async () => {
  serve({ [`${D}/mail/auth`]: { ...good[`${D}/mail/auth`], dkim: authRecord('quarantined') } });
  await mount(el(MailAuthPanel)());
  try {
    assert.ok(!text().includes('mailauth.status.missing'));
    assert.ok(text().includes('mailauth.unknown'));
  } finally { await unmount(); }
});

test('the room the signing card keeps while it checks is not drawn, not read out and cannot be used', async () => {
  serve({ [`${D}/dnssec`]: HANG });
  await mount(el(DomainDNSManager)());
  try {
    const hidden = tree.root.findAll((node) => node.type === 'div' && node.props['aria-hidden'] === 'true' && String(node.props.className).includes('invisible'));
    assert.equal(hidden.length, 1, 'the reserved room is gone, or there is more than one');
    assert.ok(hidden[0].findAll((node) => node.type === 'button').every((node) => node.props.disabled), 'a control in the reserved room can be used');
    assert.equal(tree.root.findAll((node) => node.props['aria-hidden'] === 'true' && node.type === 'div').length, 1, 'something else is hidden from the screen');
    assert.ok(text().includes('dnssec.checking'));
  } finally { await unmount(); }
  // Nowhere else in this batch is text kept in the page without being shown.
  for (const name of screenFiles.filter((item) => item !== 'DomainDNSManager')) {
    assert.doesNotMatch(source(`components/${name}.tsx`), /className="invisible/, `${name} keeps something invisible`);
  }
});

// --- Row actions on a phone ---------------------------------------------------------------
test('row actions stay at the table’s edge on a phone: one rule, and the tables that carry it', () => {
  const css = readFileSync(new URL('../src/index.css', import.meta.url), 'utf8');
  const rule = css.match(/@media \(max-width: 639px\) \{\s*\.row-actions \{([^}]*)\}/);
  assert.ok(rule, 'the row-actions rule is gone');
  assert.match(rule[1], /position: sticky;/);
  assert.match(rule[1], /inset-inline-end: 0;/);
  assert.match(rule[1], /white-space: nowrap;/, 'a named action may break into two lines in its fixed cell ("Yasağı kaldır" at 390 px)');
  assert.match(rule[1], /background-color: rgb\(var\(--row-ground, var\(--surface\)\)\);/, 'the cell is not opaque: the columns would show through it');
  for (const [file, cells] of [['DomainDNSManager', 2], ['Fail2banManagement', 2], ['DatabaseManagementV2', 3], ['Domains', 2]]) {
    const found = source(`components/${file}.tsx`).split('row-actions').length - 1;
    assert.ok(found >= cells, `${file}: ${found} row-actions cells, expected at least ${cells}`);
  }
});

// --- The ratchet -----------------------------------------------------------------------------
test('the panels of this batch are off the ratchet’s list; the log viewer keeps one read, and only that', () => {
  const tree = scanTree(fileURLToPath(new URL('../', import.meta.url)));
  const list = readAllowList().files;
  for (const name of screenFiles.filter((item) => item !== 'DomainLogsViewer').concat('DomainSSLOverviewCard')) {
    const path = `src/components/${name}.tsx`;
    assert.equal(tree[path], undefined, `${path} reads the old way again: ${JSON.stringify(tree[path])}`);
    assert.equal(list[path], undefined, `${path} is back on the allow-list`);
  }
  // cmd/panel/domain_logs_frontend_test.go pins `parseDomainLogsResponse(await
  // res.json())` in the viewer, so its one read stays in the file. It builds
  // the same three states; nothing else of the old way may come back.
  assert.deepEqual(tree['src/components/DomainLogsViewer.tsx'], { rawRead: 1 });
  assert.deepEqual(list['src/components/DomainLogsViewer.tsx'], { rawRead: 1 });
});
