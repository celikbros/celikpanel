import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import { dataModule, reactURL, remoteURL, sharedLayer } from './fixtures/shared-layer.mjs';
import { readAllowList, scanTree } from './remote-state-ratchet.mjs';
import { fileURLToPath } from 'node:url';

// NO NEGATIVE UI UNLESS KNOWN, third batch (9 Oct 2026, D-024): the Fail2ban,
// Nginx, PHP and Dovecot pages, the PowerDNS page's file list, and the DNS
// settings before their first read. It follows the two earlier mounted tests
// and stands beside them.
//
// Every screen is mounted with the real shared layer and one of its reads is
// made to hang, to drop, to be refused or to answer with something that is not
// the contract. In each case the screen must not say that something is
// missing, empty or off, and must not offer an enabled control that changes or
// removes. The same read is then answered with a real negative, to show the
// absence above is not an accident of the fixture.
//
// What these screens did before: "No jails active" and "No banned IPs" on a
// server that had both, with 0 beside the tabs; every Nginx value "—" and "No
// rate-limit zones defined"; "No extensions found"; Dovecot's figures "—"
// without a word; a red banner with no Retry in place of the DNS settings.

const source = (path) => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const screenFiles = ['Fail2banManagement', 'NginxManagement', 'PHPManagement', 'PHPExtendedConfig', 'DovecotManagement', 'PowerDNSManagement', 'DNSServerSettings'];
const icons = new Set(['AlertTriangle']);
// The dashboard is mounted too, for its attention list and the firewall's
// state. It stays on the ratchet's list (its system figures, audit trail and
// component read are not moved), so it is not one of `screenFiles`.
for (const name of [...screenFiles, 'Dashboard', 'DNSEngineCard']) {
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
  export const Link = (props) => React.createElement('a', null, props.children);
  export const showToast = (...args) => globalThis.currentTest.toasts.push(args);
  export const ServiceShell = (props) => React.createElement('main', null, props.children);
  export const ComponentPanels = () => React.createElement('aside', null, 'component-panels');
  export const HelpButton = () => null;
  export const DNSEngineCard = () => React.createElement('aside', null, 'dns-engine-card');
  export const ServerSetupDNSAccess = () => null, ServerSetupDNSManagement = () => null;
  export const dnsEngineText = (locale, key) => key;
  export const dnsEngineIdentityStagesATakeover = () => false, dnsEngineMutationsHeld = () => false, dnsEngineIdentityReviewLocked = () => false;
  export const Dialog = () => null;
  // The settings form is mounted on the route that stages the names before an
  // engine is chosen; its first read is what this test withholds.
  export const dnsEngineSettingsFlow = () => 'identityStaging', exactStagedIdentityIsCurrent = () => true;
  export const api = {
    getConfig: async () => ({ Content: '' }),
    getSystemStats: async () => ({ hostname: 'server1', uptime_seconds: 3600, cpu_percent: 5, cpu_cores: 2, load_avg: [0.1, 0.1, 0.1], mem_used_bytes: 1, mem_total_bytes: 4, disk_used_bytes: 1, disk_total_bytes: 4 }),
  };
  // What the dashboard imports beside the shared layer.
  export const useAuth = () => ({ role: 'admin', user: { username: 'admin', role: 'admin' } });
  export const ServerSetupDashboardNotice = () => null, LicenseNotice = () => null, PageHeader = () => null;
  export const FirewallNoSSHAcknowledgement = () => null, readFirewallSSHReason = () => null;
  export const accountStart = () => null, hasMailActivity = () => false;
  // What components/ComponentOperation.tsx imports and never calls here: only
  // its decoder is used, and that is the real one.
  export const createPortal = () => null;
  export const scanRefusedBySetup = () => false;
  export const useNavigationBlocker = () => {};
`);
const shared = sharedLayer(stub);
const lib = (path, more = {}) => shared.compile(path, more);
const operationURL = lib('components/ComponentOperation.tsx');
const managedURL = lib('lib/managedServices.ts', { '/remote': remoteURL, '/ComponentOperation': operationURL });
const own = { '/lib/managedServices': managedURL };
const pick = async (url, name) => (await import(url))[name];
const iniURL = lib('components/PHPExtendedConfig.tsx', own);
const Fail2banManagement = await pick(lib('components/Fail2banManagement.tsx', own), 'Fail2banManagement');
const NginxManagement = await pick(lib('components/NginxManagement.tsx', own), 'NginxManagement');
const PHPManagement = await pick(lib('components/PHPManagement.tsx', { ...own, '/PHPExtendedConfig': iniURL }), 'PHPManagement');
const PHPExtendedConfig = await pick(iniURL, 'PHPExtendedConfig');
const DovecotManagement = await pick(lib('components/DovecotManagement.tsx', own), 'DovecotManagement');
const PowerDNSManagement = await pick(lib('components/PowerDNSManagement.tsx', own), 'PowerDNSManagement');
const DNSServerSettings = await pick(lib('components/DNSServerSettings.tsx', own), 'DNSServerSettings');
const { refreshRemote, useRemote } = await import(remoteURL);
// The dashboard, with the real reader of the firewall, the real decoder of the
// component records and the real mail summary.
const firewallURL = lib('lib/firewall.ts', { '/remote': remoteURL });
const { decodeFirewallStatus, useFirewallStatus } = await import(firewallURL);
const Dashboard = await pick(lib('components/Dashboard.tsx', {
  '/lib/firewall': firewallURL,
  '/lib/accounts': lib('lib/accounts.ts', { '/remote': remoteURL }),
  '/lib/dashboardMailTruth': lib('lib/dashboardMailTruth.ts'),
  '/lib/componentCensus': lib('lib/componentCensus.ts'),
  '/ComponentOperation': operationURL,
}), 'Dashboard');
globalThis.window ??= globalThis;
// The real DNS engine card, mounted with the real decoder of its state. What
// stands in for the tracker is the lock it would draw: every view the card
// hands it is recorded, so the test can read the lock and press its action.
const lockURL = dataModule(`
  const lock = globalThis.dnsLock = { view: null, held: false };
  // One stable function, as the real provider gives: the card's own reads depend on its identity.
  const tracker = { acquireInteractionBlock: (view) => {
    lock.view = view; lock.held = true;
    return { update: (next) => { lock.view = next; }, release: () => { lock.held = false; lock.view = null; } };
  } };
  export const useComponentOperation = () => tracker;
`);
const RealDNSEngineCard = await pick(lib('components/DNSEngineCard.tsx', {
  '/lib/dnsEngineContract': lib('lib/dnsEngineContract.ts'),
  '/ComponentOperation': lockURL,
}), 'DNSEngineCard');

// --- What the server answers when nothing is wrong ----------------------------
const component = (id, config_files) => ({ id, name: id, description: `${id} on this server`, icon: '', category: 'dns', status: 'active (running)', is_installed: true, versions: [], config_files });
const profile = (id) => ({ id, name: id, description: `${id} for this server`, status: 'available', available: true, verified: false, latest_attempt_status: 'none', services: ['postfix'] });
const scan = (services) => ({ scanned_at: '2026-10-09T09:00:00Z', dns_identity_ready: true, mail_hostname: { current: 'server1', hostname: '', source: '', current_usable: true, will_set_hostname: false }, profiles: [profile('core-mail'), profile('webmail'), profile('protected-mail')], services });
const good = {
  '/api/v1/fail2ban/jails': [{ name: 'sshd', enabled: true, active: true, banned: 1 }],
  '/api/v1/fail2ban/banned': [{ ip: '198.51.100.23', jail: 'sshd', time: '', country: '' }],
  '/api/v1/fail2ban/config': { ban_time: '10m', find_time: '10m', max_retry: 5, ignore_ip: ['127.0.0.1/8'] },
  '/api/v1/nginx/global': { worker_processes: 'auto', worker_connections: '768', keepalive_timeout: '65', client_max_body_size: '64m', server_tokens: 'off', gzip: 'on' },
  '/api/v1/nginx/ssl': { ssl_protocols: 'TLSv1.3', ssl_ciphers: 'ECDHE', ssl_prefer_server_ciphers: 'off' },
  '/api/v1/nginx/ratelimits': [{ name: 'login', zone: '$binary_remote_addr', size: '10m', rate: '5r/m' }],
  '/api/v1/php/extensions': [{ name: 'gd', enabled: true }, { name: 'intl', enabled: false }],
  '/api/v1/php/extended-config': { memory_limit: '256M', max_execution_time: '30' },
  '/api/v1/dovecot/stats': { uptime: '3 days', connections: 4, logins: 0, auth_success: 0, auth_fail: 0 },
  '/api/v1/managed-services': scan([component('pdns', [{ path: '/etc/powerdns/pdns.conf' }]), component('postfix', [])]),
  '/api/v1/settings/nameservers': { ns1: 'ns1.example.com', ns2: 'ns2.example.com', derived: false, server_ip: '203.0.113.10', facts: [], usable: true },
  '/api/v1/settings/dns-cluster': { configured: true, role: 'standalone', peer_ip: '', peer_ns: '', peer_reachable: false, server_ip: '203.0.113.10', ns1: 'ns1.example.com', ns2: 'ns2.example.com', facts: [], steps: [] },
  // The dashboard's other reads: a server on which nothing needs attention.
  '/api/v1/firewall': { enabled: true, engine_available: true, tcp_ports: [22, 80, 443], udp_ports: [], ssh_ports: [22], persistence_state: 'ready' },
  '/api/v1/dashboard': { databases: 2, mail_accounts: 3, expiring_certs: [] },
  '/api/v1/domains': [],
  '/api/v1/users': { users: [] },
  '/api/v1/audit-logs': { entries: [] },
  '/api/v1/host-mutation-readiness': { ready: true },
};
// What the same read answers when the server really has nothing.
const none = {
  '/api/v1/fail2ban/jails': [],
  '/api/v1/fail2ban/banned': null,
  '/api/v1/nginx/ratelimits': [],
  '/api/v1/php/extensions': [],
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
    if (typeof reply === 'function') return reply(init);
    if (reply === undefined) return answer({ error: 'no route' }, 404);
    return answer(reply);
  };
}
const HANG = () => new Promise((resolve) => { withheld.push(resolve); });
const DROP = () => { throw new TypeError('network'); };
const REFUSED = () => answer({ error: 'agent unavailable', code: 'INTERNAL' }, 502);
const NOT_THE_CONTRACT = () => answer('<html>', 200);

async function mount(element) {
  globalThis.currentTest = { toasts: [] };
  withheld = [];
  await act(async () => { tree = Renderer.create(element); });
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 15)); });
}
async function settle() { await act(async () => { await new Promise((resolve) => setTimeout(resolve, 15)); }); }
async function unmount() {
  for (const resolve of withheld) resolve(answer({}, 500));
  if (tree) await act(async () => tree.unmount());
  tree = undefined;
  globalThis.fetch = originalFetch;
  delete globalThis.currentTest;
}
const text = () => {
  const walk = (node) => (typeof node === 'string' ? node : !node ? '' : Array.isArray(node) ? node.map(walk).join(' ') : walk(node.children));
  return walk(tree.toJSON());
};
const buttons = () => tree.root.findAll((node) => node.type === 'button');
const label = (node) => { const walk = (n) => (typeof n === 'string' ? n : (n.children || []).map(walk).join('')); return walk(node); };
const enabled = (names) => buttons().filter((node) => !node.props.disabled && names.some((name) => label(node).includes(name))).map(label);
const fields = () => tree.root.findAll((node) => ['input', 'textarea', 'select'].includes(node.type));
const press = async (name) => {
  const node = buttons().find((item) => label(item).includes(name));
  assert.ok(node, `no button ${name}`);
  await act(async () => { await node.props.onClick(); });
  await settle();
};
globalThis.confirm = () => true;

// --- The table ---------------------------------------------------------------------
// screen, the read that is withheld, the tab to open first, the sentences that
// claim something, the controls that change something, and the notice key.
const screens = [
  { name: 'Fail2ban, the jails', element: () => React.createElement(Fail2banManagement, { onBack() {} }), read: '/api/v1/fail2ban/jails', negative: ['f2b.emptyJails'], changes: [], checking: 'f2b.jails.checking', unknown: 'f2b.jails.unknown', empty: 'f2b.emptyJails' },
  { name: 'Fail2ban, the banned addresses', element: () => React.createElement(Fail2banManagement, { onBack() {} }), tab: 'f2b.tab.banned', read: '/api/v1/fail2ban/banned', negative: ['f2b.emptyBanned'], changes: ['f2b.unban'], checking: 'f2b.banned.checking', unknown: 'f2b.banned.unknown', empty: 'f2b.emptyBanned' },
  { name: 'Fail2ban, the settings', element: () => React.createElement(Fail2banManagement, { onBack() {} }), tab: 'f2b.tab.config', read: '/api/v1/fail2ban/config', negative: ['f2b.configReadonly'], changes: [], checking: 'f2b.config.checking', unknown: 'f2b.config.unknown' },
  { name: 'Nginx, the global settings', element: () => React.createElement(NginxManagement, { onBack() {} }), read: '/api/v1/nginx/global', negative: ['—', 'nginx.readonly'], changes: [], checking: 'nginx.global.checking', unknown: 'nginx.global.unknown' },
  { name: 'Nginx, the TLS settings', element: () => React.createElement(NginxManagement, { onBack() {} }), tab: 'nginx.tab.ssl', read: '/api/v1/nginx/ssl', negative: ['—', 'nginx.readonly'], changes: [], checking: 'nginx.ssl.checking', unknown: 'nginx.ssl.unknown' },
  { name: 'Nginx, the rate limits', element: () => React.createElement(NginxManagement, { onBack() {} }), tab: 'nginx.tab.rateLimits', read: '/api/v1/nginx/ratelimits', negative: ['nginx.emptyRateLimits'], changes: [], checking: 'nginx.rate.checking', unknown: 'nginx.rate.unknown', empty: 'nginx.emptyRateLimits' },
  { name: 'PHP, the extensions', element: () => React.createElement(PHPManagement, { versions: ['8.3'], onBack() {} }), read: '/api/v1/php/extensions', negative: ['php.emptyExtensions'], changes: [], switches: true, checking: 'php.extensions.checking', unknown: 'php.extensions.unknown', empty: 'php.emptyExtensions' },
  { name: 'PHP, php.ini', element: () => React.createElement(PHPExtendedConfig, { version: '8.3' }), read: '/api/v1/php/extended-config', negative: [], changes: ['Save', 'Apply'], formFields: true, checking: 'php.ini.checking', unknown: 'php.ini.unknown' },
  { name: 'Dovecot, the figures', element: () => React.createElement(DovecotManagement, { onBack() {} }), read: '/api/v1/dovecot/stats', negative: ['—'], changes: [], checking: null, unknown: 'dovecot.statsUnknown', pending: '…' },
  { name: 'PowerDNS, which files it has', element: () => React.createElement(PowerDNSManagement, { onBack() {} }), read: '/api/v1/managed-services', negative: [], changes: [], checking: 'dbconf.files.checking', unknown: 'dbconf.files.unknown' },
  { name: 'DNS settings, the saved names', element: () => React.createElement(DNSServerSettings, {}), read: '/api/v1/settings/nameservers', negative: ['dnssrv.blocker'], changes: ['dnssrv.saveAndPublish'], formFields: true, checking: 'dnssrv.checking', unknown: 'dnssrv.unknown' },
  { name: 'DNS settings, the saved role', element: () => React.createElement(DNSServerSettings, {}), read: '/api/v1/settings/dns-cluster', negative: ['dnssrv.blocker'], changes: ['dnssrv.saveAndPublish'], formFields: true, checking: 'dnssrv.checking', unknown: 'dnssrv.unknown' },
];
const openTab = async (screen) => { if (screen.tab) await press(screen.tab); };

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
        await openTab(screen);
        const shown = text();
        for (const phrase of screen.negative) assert.ok(!shown.includes(phrase), `"${phrase}" is on screen while the read is ${how}: ${shown.slice(0, 300)}`);
        assert.deepEqual(enabled(screen.changes), [], 'a control that changes something is enabled');
        if (screen.switches) assert.equal(fields().filter((node) => node.props.type === 'checkbox').length, 0, 'a switch exists for an extension the server did not list');
        if (screen.formFields) assert.equal(fields().length, 0, 'a form was built from nothing');
        if (state === 'checking') {
          if (screen.checking) assert.ok(shown.includes(screen.checking), `the checking line is missing: ${shown.slice(0, 300)}`);
          if (screen.pending) assert.ok(shown.includes(screen.pending), 'a figure that is being read is "…"');
          assert.ok(!shown.includes(screen.unknown), 'a read that has not answered is not reported as failed');
        } else {
          assert.ok(shown.includes(screen.unknown), `the could-not-check sentence is missing: ${shown.slice(0, 300)}`);
          assert.ok(enabled(['common.retry']).length >= 1, 'Retry is not offered');
          // Retry reads, and only reads.
          const before = requests.length;
          serve();
          requests = [];
          await press('common.retry');
          assert.ok(requests.length > 0 && requests.every((line) => line.startsWith('GET ')), `Retry sent something other than a read: ${requests.join(', ')} (${before})`);
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
        await openTab(screen);
        assert.ok(text().includes(screen.empty), text().slice(0, 300));
      } finally { await unmount(); }
    });
  }
}

test('the counts beside the Fail2ban tabs: "…" while read, "–" when not read, the number when known', async () => {
  serve({ '/api/v1/fail2ban/jails': HANG, '/api/v1/fail2ban/banned': REFUSED });
  await mount(React.createElement(Fail2banManagement, { onBack() {} }));
  try {
    const tabs = () => buttons().map(label);
    assert.ok(tabs().some((item) => item === 'f2b.tab.jails…'), tabs().join(' | '));
    assert.ok(tabs().some((item) => item === 'f2b.tab.banned–'), tabs().join(' | '));
    assert.ok(!tabs().some((item) => /f2b\.tab\.(jails|banned)0$/.test(item)), 'a count of 0 from no answer');
  } finally { await unmount(); }
  serve();
  await mount(React.createElement(Fail2banManagement, { onBack() {} }));
  try {
    assert.ok(buttons().map(label).includes('f2b.tab.jails1'));
  } finally { await unmount(); }
});

test('a ban lifted, then the list could not be read again: the earlier list stays, marked, with Unban off', async () => {
  let reads = 0;
  serve({ '/api/v1/fail2ban/banned': (init) => {
    if (init.method === 'POST') return answer({ success: true });
    reads += 1;
    return reads === 1 ? answer(good['/api/v1/fail2ban/banned']) : REFUSED();
  } });
  await mount(React.createElement(Fail2banManagement, { onBack() {} }));
  try {
    await press('f2b.tab.banned');
    assert.deepEqual(enabled(['f2b.unban']), ['f2b.unban']);
    await press('f2b.unban');
    assert.equal(requests.filter((line) => line === 'POST /api/v1/fail2ban/banned').length, 1, 'the change is sent once');
    const shown = text();
    assert.ok(shown.includes('common.staleNotice'), shown.slice(0, 300));
    assert.ok(shown.includes('198.51.100.23'), 'the earlier answer stays on screen');
    assert.ok(!shown.includes('f2b.emptyBanned'));
    assert.deepEqual(enabled(['f2b.unban']), [], 'Unban is offered on a list that is not known');
  } finally { await unmount(); }
});

test('a PHP extension switch is sent once and then shows what the server says, accepted or refused', async () => {
  let posted = 0;
  let list = [{ name: 'gd', enabled: true }, { name: 'intl', enabled: false }];
  serve({ '/api/v1/php/extensions': async (init) => {
    if (init.method === 'POST') { posted += 1; return answer({ error: 'refused' }, 502); }
    return answer(list);
  } });
  await mount(React.createElement(PHPManagement, { versions: ['8.3'], onBack() {} }));
  try {
    const switches = () => fields().filter((node) => node.props.type === 'checkbox');
    assert.deepEqual(switches().map((node) => node.props.checked), [true, false]);
    await act(async () => { await switches()[1].props.onChange(); });
    await settle();
    assert.equal(posted, 1);
    assert.deepEqual(globalThis.currentTest.toasts, [['error', 'php.toggleFailed']]);
    assert.deepEqual(switches().map((node) => node.props.checked), [true, false], 'a refused switch must not stay on');
    assert.ok(switches().every((node) => !node.props.disabled));
  } finally { await unmount(); }
});

test('refreshRemote: whoever shows an address reads it again once; with no reader nothing is requested', async () => {
  const decode = (raw) => raw;
  let served = 0;
  globalThis.fetch = async () => { served += 1; return answer({ n: served }); };
  const seen = [];
  const Reader = () => { const { remote } = useRemote('/api/v1/refresh-probe', decode); seen.push(remote.state === 'known' ? remote.value.n : remote.state); return null; };
  // Nobody shows the address: nothing to refresh.
  refreshRemote('/api/v1/refresh-probe');
  assert.equal(served, 0);
  await act(async () => { tree = Renderer.create(React.createElement(React.Fragment, null, React.createElement(Reader), React.createElement(Reader))); });
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 15)); });
  assert.equal(served, 1, 'two readers of one address share one request');
  refreshRemote('/api/v1/refresh-probe');
  // The value on screen stays until the new answer arrives.
  assert.equal(seen[seen.length - 1], 1);
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 15)); });
  assert.equal(served, 2, 'one request for the refresh, whatever the number of readers');
  assert.equal(seen[seen.length - 1], 2);
  await act(async () => tree.unmount());
  tree = undefined;
  refreshRemote('/api/v1/refresh-probe');
  assert.equal(served, 2, 'the last reader left: nothing is requested');
  globalThis.fetch = originalFetch;
});

test('the screens of the third batch read through the shared layer and are off the list', () => {
  const webDir = fileURLToPath(new URL('../', import.meta.url));
  const actual = scanTree(webDir);
  const allowList = readAllowList();
  for (const name of screenFiles) {
    const path = `src/components/${name}.tsx`;
    assert.equal(actual[path], undefined, `${path} reads the old way again: ${JSON.stringify(actual[path])}`);
    assert.equal(allowList.files[path], undefined, `${path} is back on the allow-list`);
  }
});

// --- The dashboard's attention list and the firewall's state ----------------------
// The list is built from four reads. With any one of them withheld it must not
// say the firewall is off, must not offer to turn it on, and must not say that
// nothing needs action. Before this batch any answer of GET /api/v1/firewall
// was stored as the firewall's state, so an Agent error sent with a 200 drew
// "Firewall is off" with its button.
const freshScan = () => ({ ...good['/api/v1/managed-services'], scanned_at: new Date().toISOString() });
const dashboard = () => React.createElement(Dashboard);
const statusLabels = () => tree.root.findAll((node) => node.props.role === 'status' && typeof node.type === 'string').map((node) => node.props['aria-label']);
const CLAIMS = ['dashboard.fwOffItem', 'dashboard.fwPersistenceItem', 'dashboard.attentionNone', 'dashboard.attentionNoneUnchecked', 'dashboard.allGood'];
const AGENT_ERROR_AS_200 = () => answer({ error: 'agent unavailable' });
const attentionReads = ['/api/v1/firewall', '/api/v1/dashboard', '/api/v1/domains', '/api/v1/managed-services'];

for (const read of attentionReads) {
  for (const [how, reply, state] of [...withholdings, ...(read === '/api/v1/firewall' ? [['an Agent error sent with a 200', AGENT_ERROR_AS_200, 'unknown']] : [])]) {
    test(`Dashboard, the attention list with ${read} ${how} → no claim about the server, nothing to turn on`, async () => {
      serve({ '/api/v1/managed-services': freshScan(), [read]: reply });
      await mount(dashboard());
      try {
        const shown = text();
        for (const phrase of CLAIMS) assert.ok(!shown.includes(phrase), `"${phrase}" is on screen while ${read} is ${how}: ${shown.slice(0, 400)}`);
        assert.deepEqual(enabled(['firewall.turnOn', 'dashboard.saveFirewall']), [], 'a firewall change is offered without the firewall being known');
        assert.equal(requests.filter((line) => !line.startsWith('GET ')).length, 0, `the page sent a change by itself: ${requests.join(', ')}`);
        if (state === 'checking') {
          assert.ok(shown.includes('dashboard.attentionChecking'), `the checking line is missing: ${shown.slice(0, 400)}`);
          assert.ok(!shown.includes('dashboard.attentionUnread'), 'a read that has not answered is not reported as failed');
        } else {
          assert.ok(shown.includes('dashboard.attentionUnread'), `the could-not-check sentence is missing: ${shown.slice(0, 400)}`);
          serve({ '/api/v1/managed-services': freshScan() });
          requests = [];
          await press('common.retry');
          // Retry reads, and reads only what could not be read.
          assert.deepEqual([...new Set(requests)], [`GET ${read}`], `Retry sent ${requests.join(', ')}`);
          assert.ok(!text().includes('dashboard.attentionUnread'), 'the notice stays after a successful read');
          assert.ok(text().includes('dashboard.attentionNone'), text().slice(0, 400));
        }
      } finally { await unmount(); }
    });
  }
}

test('Dashboard: "Firewall is off" and "Turn on" exist for an answer with enabled: false, and only then', async () => {
  serve({ '/api/v1/managed-services': freshScan(), '/api/v1/firewall': { ...good['/api/v1/firewall'], enabled: false, persistence_state: 'disabled' } });
  await mount(dashboard());
  try {
    assert.ok(text().includes('dashboard.fwOffItem'), text().slice(0, 400));
    assert.deepEqual(enabled(['firewall.turnOn']), ['firewall.turnOn']);
    assert.ok(!text().includes('dashboard.attentionNone'));
    assert.equal(requests.filter((line) => !line.startsWith('GET ')).length, 0, 'drawing the item sent nothing');
  } finally { await unmount(); }
});

test('Dashboard: the calm line names what was read, and does not speak for components nobody checked lately', async () => {
  serve({ '/api/v1/managed-services': freshScan() });
  await mount(dashboard());
  try {
    assert.ok(text().includes('dashboard.attentionNone') && !text().includes('dashboard.attentionNoneUnchecked'), text().slice(0, 400));
    assert.ok(!statusLabels().includes('dashboard.attentionChecking'), 'the checking line stays after every read answered');
  } finally { await unmount(); }
  // The stored scan of the fixture is from 9 Oct 2026, 09:00 UTC: not current.
  serve();
  await mount(dashboard());
  try {
    assert.ok(text().includes('dashboard.attentionNoneUnchecked'), text().slice(0, 400));
  } finally { await unmount(); }
});

test('Dashboard: an item is already listed while another read is on its way → no row that comes and goes', async () => {
  serve({ '/api/v1/managed-services': freshScan(), '/api/v1/firewall': { ...good['/api/v1/firewall'], enabled: false, persistence_state: 'disabled' }, '/api/v1/dashboard': HANG });
  await mount(dashboard());
  try {
    const shown = text();
    assert.ok(shown.includes('dashboard.fwOffItem'), 'a known item waits for an unrelated read');
    // Said beside the count (the spinner's label), not as a line in the list.
    assert.ok(statusLabels().includes('dashboard.attentionChecking'), 'nothing says another read is on its way');
    assert.ok(!shown.includes('dashboard.attentionChecking'), 'the checking line is drawn as a row above known items; it leaves when the read answers and everything under it moves');
    assert.ok(!shown.includes('dashboard.attentionNone'));
  } finally { await unmount(); }
});

test('the firewall decoder: "off" is a boolean the Agent sent, never the lack of one', async () => {
  for (const raw of [null, [], '<html>', {}, { enabled: 'false' }, { enabled: 0 }, { error: 'agent unavailable' }, { enabled: false, error: 'iptables: permission denied' }]) {
    assert.throws(() => decodeFirewallStatus(raw), `${JSON.stringify(raw)} was read as a firewall state`);
  }
  assert.equal(decodeFirewallStatus({ enabled: false }).enabled, false);
  assert.equal(decodeFirewallStatus({ enabled: true, error: '' }).enabled, true);
  // A signed-in user who may not read the firewall: nothing is requested.
  serve();
  const Reader = () => { useFirewallStatus({ enabled: false }); return null; };
  await mount(React.createElement(Reader));
  try {
    assert.deepEqual(requests, []);
  } finally { await unmount(); }
});

// --- The DNS engine card: polling only reads --------------------------------------
// A change the server accepted has recorded nothing for minutes. Until 9 Oct 2026
// the card's own timer then sent POST /api/v1/dns/engine/reconcile (its first
// tick did, half a second after the state was read). That request takes the
// server's change lock and is audited under the signed-in administrator, so a
// timer sends it zero times now; the person at the screen may send it.
const REQUEST = 'b'.repeat(32);
const OPERATION = 'a'.repeat(32);
const ago = (minutes) => new Date(Date.now() - minutes * 60_000).toISOString();
const dnsEngines = (active) => ['pdns', 'bind'].map((id) => ({ id, installed: true, running: id === active, managed: true, status: id === active ? 'active' : 'installed_standby' }));
const dnsChange = (started, updated) => ({
  revision: 5, topology: 'standalone', dnssec_zone_count: 0, zone_count: 2, pending_zone_count: 0, engine_epoch: 1, active_engine: 'bind', state: 'switching', engines: dnsEngines('bind'), operation_id: OPERATION,
  operation: { id: OPERATION, request_id: REQUEST, target_engine: 'pdns', phase: 'activating', status: 'running', started_at: ago(started), updated_at: ago(updated) },
});
const dnsDone = () => ({
  revision: 6, topology: 'standalone', dnssec_zone_count: 0, zone_count: 2, pending_zone_count: 0, engine_epoch: 2, active_engine: 'pdns', state: 'ready', engines: dnsEngines('pdns'),
  operation: { id: OPERATION, request_id: REQUEST, target_engine: 'pdns', phase: 'committed', status: 'succeeded', started_at: ago(5), updated_at: ago(0) },
});
const RECONCILE = 'POST /api/v1/dns/engine/reconcile';
const sent = (line) => requests.filter((item) => item === line).length;
const changes = () => requests.filter((line) => !line.startsWith('GET '));
// The card's first poll runs half a second after it adopts the change.
const afterFirstPoll = async () => { await act(async () => { await new Promise((resolve) => setTimeout(resolve, 800)); }); };
// What the server does with a reconcile request: nothing new, a refusal, or it closes the change.
function serveDNS(snapshot, reconcile) {
  const state = { snapshot };
  serve({
    '/api/v1/dns/engine': () => answer(state.snapshot),
    '/api/v1/dns/engine/reconcile': () => {
      if (reconcile === 'refused') return answer({ error: 'DNS engine state could not be verified.', code: 'DNS_ENGINE_STATE_UNVERIFIED' }, 409);
      if (reconcile === 'finishes') state.snapshot = dnsDone();
      return answer({ reconciled: reconcile === 'finishes' });
    },
  });
  return state;
}

test('DNS engine card, a change stalled for four minutes: the poll sends nothing; "Check now" in the lock sends one request', async () => {
  const server = serveDNS(dnsChange(5, 4), 'unchanged');
  await mount(React.createElement(RealDNSEngineCard));
  try {
    await afterFirstPoll();
    const lock = globalThis.dnsLock;
    assert.ok(lock.held, 'the change is not locked');
    assert.deepEqual(changes(), [], `the page sent a change by itself: ${requests.join(', ')}`);
    assert.ok(sent('GET /api/v1/dns/engine') >= 2, 'the poll did not read');
    assert.ok(lock.view.message.startsWith('dnsEngine.guard.reconcileDue') && lock.view.message.includes('dnsEngine.guard.reconcileOffer'), lock.view.message);
    assert.ok(text().includes('dnsEngine.operation.autoTracking'), 'a change that is still polled says so on the card');
    assert.equal(lock.view.action?.label, 'dnsEngine.guard.checkNow');
    assert.equal(lock.view.busy, false, 'a stalled change still shows a working lock');

    // Pressed twice before the answer: one request.
    await act(async () => { lock.view.action.onAct(); lock.view.action.onAct(); });
    await settle();
    assert.equal(sent(RECONCILE), 1, 'the check is not single-flight');
    assert.deepEqual(changes(), [RECONCILE], 'the check sent something other than the reconcile request');
    assert.ok(lock.held, 'a check that changed nothing released the lock');
    assert.ok(lock.view.message.startsWith('dnsEngine.guard.reconcileChecked'), lock.view.message);
    assert.equal(lock.view.action?.busy, false);

    // The server closes the change on the next check: the lock is released.
    serveDNS(dnsChange(5, 4), 'finishes');
    await act(async () => { lock.view.action.onAct(); });
    await settle();
    assert.equal(sent(RECONCILE), 1);
    assert.equal(lock.held, false, 'the finished change is still locked');
    assert.deepEqual(globalThis.currentTest.toasts, [['success', 'dnsEngine.switchCompleted']]);
    void server;
  } finally { await unmount(); }
});

test('DNS engine card, a check the server refuses: its refusal is said in the lock and the offer stays', async () => {
  serveDNS(dnsChange(5, 4), 'refused');
  await mount(React.createElement(RealDNSEngineCard));
  try {
    await afterFirstPoll();
    const lock = globalThis.dnsLock;
    await act(async () => { lock.view.action.onAct(); });
    await settle();
    assert.equal(sent(RECONCILE), 1);
    assert.ok(lock.held);
    assert.ok(lock.view.message.includes('dnsEngine.guard.reconcileOffer') && !lock.view.message.startsWith('dnsEngine.guard.reconcileChecked'), lock.view.message);
    assert.equal(lock.view.action?.label, 'dnsEngine.guard.checkNow');
  } finally { await unmount(); }
});

test('DNS engine card, an operation the server has not published: nothing is offered', async () => {
  // Another request's operation is in progress; this page holds none of its own.
  serveDNS({ ...dnsChange(5, 4), state: 'ready', operation: undefined, operation_id: undefined }, 'unchanged');
  await mount(React.createElement(RealDNSEngineCard));
  try {
    await afterFirstPoll();
    assert.equal(globalThis.dnsLock.held, false);
    assert.deepEqual(changes(), []);
    assert.equal(tree.root.findAll((node) => node.props['data-testid'] === 'dns-engine-deadline-check').length, 0);
  } finally { await unmount(); }
});

test('DNS engine card, past the safety limit: the lock is released, nothing is sent, and the card still offers the one check', async () => {
  serveDNS(dnsChange(40, 39), 'unchanged');
  await mount(React.createElement(RealDNSEngineCard));
  const notice = () => tree.root.findAll((node) => node.props['data-testid'] === 'dns-engine-deadline-check');
  const noticeText = () => label(notice()[0]);
  try {
    await afterFirstPoll();
    assert.equal(globalThis.dnsLock.held, false, 'past the safety limit the lock must not trap the page');
    assert.deepEqual(changes(), [], `the page sent a change by itself: ${requests.join(', ')}`);
    assert.equal(notice().length, 1, 'nothing on the card offers the check: the saved record could never be closed from the interface');
    assert.ok(noticeText().includes('dnsEngine.guard.deadlineDue') && noticeText().includes('dnsEngine.guard.deadlineOffer'), noticeText());
    assert.ok(!text().includes('dnsEngine.operation.trackingDelayed'), '"tracking continues" is said beside "stopped checking"');
    assert.ok(!text().includes('dnsEngine.operation.autoTracking'), '"updating automatically" is said beside "stopped checking"');
    // The loop has stopped: no further read arrives by itself.
    const reads = sent('GET /api/v1/dns/engine');
    await afterFirstPoll();
    assert.equal(sent('GET /api/v1/dns/engine'), reads, 'the loop went on reading past its limit');

    await press('dnsEngine.guard.checkNow');
    assert.equal(sent(RECONCILE), 1);
    assert.deepEqual(changes(), [RECONCILE]);
    assert.ok(noticeText().startsWith('dnsEngine.guard.reconcileChecked') && noticeText().includes('dnsEngine.guard.deadlineOffer'), noticeText());

    serveDNS(dnsChange(40, 39), 'finishes');
    await press('dnsEngine.guard.checkNow');
    assert.equal(sent(RECONCILE), 1);
    assert.equal(notice().length, 0, 'the finished change still shows the notice');
    assert.deepEqual(globalThis.currentTest.toasts, [['success', 'dnsEngine.switchCompleted']]);
  } finally { await unmount(); }
});
