import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import { compileSource, dataModule, reactURL, remoteURL, sharedLayer } from './fixtures/shared-layer.mjs';
import { englishCatalogue, turkishCatalogue } from './locale-catalogue.mjs';

// NO NEGATIVE UI UNLESS KNOWN, on the mounted screens (9 Oct 2026, D-024).
//
// Every screen of the first batch is mounted with the real shared layer and one
// of its reads is made to hang, to drop, to be refused or to answer with
// something that is not the contract. In each of those cases the screen must
// not say that something is missing, empty or not ready, and it must not offer
// an enabled control that submits or removes anything. The same read is then
// answered with a real negative, to show that the absence above is not an
// accident of the fixture: the text does appear once the server has said so.
//
// The report this comes from (8 Oct 2026): opening "Add domain" on a server
// that has DNS showed "an active, panel-managed DNS server is required…
// [Choose a DNS engine]" with the form disabled for the seconds the read took.

const source = (path) => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const screenFiles = [
  'AddDomainModal', 'Domains', 'DatabaseManagementV2', 'DatabaseAccountStrip', 'DomainDatabaseManager',
  'DomainConnection', 'DomainDNSManager', 'DomainPHPSettings', 'HostingTypePanel',
];
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
  const i18n = { t: (key) => key, locale: 'en' };
  export const useI18n = () => i18n;
  export const useNavigate = () => (path) => { globalThis.currentTest.navigated.push(path); };
  export const useAuth = () => ({ role: globalThis.currentTest.role });
  export const showToast = (...args) => globalThis.currentTest.toasts.push(args);
  export const PageHeader = (props) => React.createElement('header', null, props.title, props.subtitle, props.actions);
  export const HelpButton = () => null;
  export const AddDatabaseModalV2 = () => React.createElement('aside', null, 'add-database-dialog');
  export const AddUserModalV2 = () => React.createElement('aside', null, 'add-user-dialog');
`);
const shared = sharedLayer(stub);
const domainAccessURL = compileSource('auth/domainAccess.ts', () => stub);
const deletionURL = compileSource('lib/domainDeletionPending.ts', () => stub);
const own = { '/auth/domainAccess': domainAccessURL, '/lib/domainDeletionPending': deletionURL };
const modalURL = shared.compile('components/AddDomainModal.tsx', own);
const stripURL = shared.compile('components/DatabaseAccountStrip.tsx', own);
const load = async (name, more = {}) => (await import(shared.compile(`components/${name}.tsx`, { ...own, ...more })))[name];
const AddDomainModal = (await import(modalURL)).AddDomainModal;
const Domains = await load('Domains', { '/AddDomainModal': modalURL });
const DatabaseManagementV2 = await load('DatabaseManagementV2', { '/DatabaseAccountStrip': stripURL });
const DomainDatabaseManager = await load('DomainDatabaseManager');
const DomainConnection = await load('DomainConnection');
const DomainDNSManager = await load('DomainDNSManager');
const DomainPHPSettings = await load('DomainPHPSettings');
const HostingTypePanel = await load('HostingTypePanel');

// --- What the server answers when nothing is wrong ----------------------------
const caps = (overrides = {}) => ({
  web_server: 'nginx', php_versions: ['8.3'], dns_server: 'bind', dns_identity_ready: true,
  dns_management_mode: 'local', dns_management_ready: true, mail_server: true,
  database_servers: ['mariadb'], db_tools: [], ...overrides,
});
const domainRow = { id: 1, domain_name: 'example.com', status: 'active', created_at: '2026-01-01T00:00:00Z', project_type: 'php', php_version: '8.3' };
const engine = {
  id: 1, type_id: 1, type_name: 'mariadb', type_icon: 'M', name: 'MariaDB', version: '11.4', host: 'localhost', port: 3306,
  is_default: true, status: 'active', created_at: '', admin_username: 'celikpanel', is_local: true,
};
const connection = (overrides = {}) => ({
  domain: 'example.com', server_ip: '192.0.2.4', nameservers: ['ns1.example.net', 'ns2.example.net'],
  live_nameservers: ['ns1.example.net', 'ns2.example.net'], live_ips: ['192.0.2.4'], status: 'delegated', ssl_ready: true,
  glue_needed: false, nameservers_usable: true, checked_at: '2026-10-09T00:00:00Z', dns_management_mode: 'local', ...overrides,
});
const json = (body, status = 200) => () => Response.json(body, { status });
const healthy = {
  '/api/v1/hosting/capabilities': json(caps()),
  '/api/v1/domains': json([domainRow]),
  '/api/v1/subscriptions': json({ subscriptions: [] }),
  '/api/v1/database-servers': json([engine]),
  '/api/v1/database-servers/1/databases': json([{ id: 5, name: 'shop', users: ['shop'], created_at: '' }]),
  '/api/v1/database-servers/1/users': json([{ id: 7, username: 'shop', databases: ['shop'], created_at: '' }]),
  '/api/v1/domains/1/databases': json({ databases: [{ id: 3, name: 'example_com_app', type: 'mysql', user: 'app', created_at: '2026-01-01T00:00:00Z' }], available_types: ['mysql'] }),
  '/api/v1/domains/1/connection': json(connection()),
  '/api/v1/domains/1/dns/zone': json({ type: 'NATIVE', management: 'local' }),
  '/api/v1/domains/1/dns/records': json({ records: [{ id: 1, name: 'example.com', type: 'A', content: '192.0.2.4', ttl: 3600, disabled: false }] }),
  '/api/v1/domains/1/dnssec': json({ enabled: false }),
  '/api/v1/domains/1/php': json({ domain_id: 1, domain_name: 'example.com', php_version: '8.3', pool_name: 'example', pool_config: { pm: 'dynamic' } }),
  '/api/v1/domains/1/hosting': json({ project_type: 'static' }),
  '/api/v1/runtimes/node': json({ installed: [] }),
};

// --- The ways a read can fail to produce a known answer -----------------------
const failures = {
  'still on its way': () => new Promise(() => {}),
  'dropped connection': () => Promise.reject(new TypeError('fetch failed')),
  'refused by the server': json({ error: 'server sentence', code: 'INTERNAL' }, 502),
  'an answer that is not the contract': json('not the contract'),
};

const originalFetch = globalThis.fetch;
let tree, calls;
function serve(overrides = {}, role = 'admin') {
  calls = [];
  globalThis.currentTest = { toasts: [], navigated: [], role };
  globalThis.confirm = () => true;
  globalThis.document = { addEventListener() {}, removeEventListener() {} };
  const table = { ...healthy, ...overrides };
  globalThis.fetch = (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ method, url });
    if (method !== 'GET') return Promise.resolve(Response.json({ success: true }));
    const answer = table[url.split('?')[0]];
    if (!answer) return Promise.resolve(Response.json({ error: 'no such route in this test' }, { status: 404 }));
    return Promise.resolve().then(() => answer({ url }));
  };
}
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));
async function mount(Component, props = {}) {
  await act(async () => {
    tree = Renderer.create(React.createElement(Component, props));
    await settled();
    await settled();
  });
}
async function cleanup() {
  if (tree) await act(async () => tree.unmount());
  tree = undefined;
  globalThis.fetch = originalFetch;
  delete globalThis.currentTest;
  delete globalThis.document;
}
const text = () => JSON.stringify(tree.toJSON());
const has = (key) => text().includes(`"${key}"`) || text().includes(key);
const hostButtons = () => tree.root.findAll((node) => node.type === 'button');
const labelOf = (node) => [node.props['aria-label'], node.props.title, ...[].concat(node.props.children ?? [])]
  .filter((part) => typeof part === 'string').join(' ');
const buttons = (label) => hostButtons().filter((node) => labelOf(node).includes(label));
const press = async (node) => {
  await act(async () => {
    await node.props.onClick({});
    await settled();
    await settled();
  });
};
const reads = (path) => calls.filter((call) => call.method === 'GET' && call.url.split('?')[0] === path).length;

// A control that submits a form or removes something. None of these may be
// enabled while what it acts on is not known: none at all on a screen none of
// whose reads has answered, and none that depends on the one read withheld.
const destructiveWords = /delete|remove|\bsil\b|kaldır|rotate/i;
const submits = (node) => node.props.type === 'submit';
const removes = (node) => destructiveWords.test(labelOf(node)) || /text-danger|hover:text-danger/.test(node.props.className ?? '');
const enabled = (which) => hostButtons().filter((node) => !node.props.disabled && which(node)).map(labelOf);
const enabledDangerousControls = () => enabled((node) => submits(node) || removes(node));

// --- The table -----------------------------------------------------------------
// screen:   what is mounted, with which props and role
// read:     the address whose answer is withheld
// negative: text keys that claim something about the server; none may be drawn
// checking / unknown: what the screen says instead ('' = it says nothing itself)
// known:    an answer that makes one of the negative keys true, and that key
const domain = { domainId: 1, domainName: 'example.com' };
const table = [
  {
    screen: 'Add domain dialogue', Component: AddDomainModal, props: { onClose() {}, onSuccess() {} },
    read: '/api/v1/hosting/capabilities',
    negative: ['domains.add.needsDns', 'err.DNS_SETTINGS_REQUIRED', 'err.DNS_SERVER_REQUIRED.action', 'err.DNS_SETTINGS_REQUIRED.action', 'domains.add.needsWebServer'],
    checking: 'dns.checkingServer', unknown: 'domains.add.dnsUnknown',
    known: [json(caps({ dns_server: '', dns_identity_ready: false, dns_management_ready: false })), 'domains.add.needsDns'],
    alsoDisabled: 'domains.add.create',
  },
  {
    screen: 'Domains page (DNS readiness, with domains)', Component: Domains,
    read: '/api/v1/hosting/capabilities',
    negative: ['domains.add.needsDns', 'err.DNS_SETTINGS_REQUIRED', 'err.DNS_SERVER_REQUIRED.action', 'err.DNS_SETTINGS_REQUIRED.action'],
    checking: '', unknown: '',
    known: [json(caps({ dns_server: 'bind', dns_identity_ready: false, dns_management_ready: false })), 'err.DNS_SETTINGS_REQUIRED'],
  },
  {
    screen: 'Domains page (DNS readiness, no domains yet)', Component: Domains,
    with: { '/api/v1/domains': json([]) },
    read: '/api/v1/hosting/capabilities',
    negative: ['domains.add.needsDns', 'err.DNS_SETTINGS_REQUIRED', 'err.DNS_SERVER_REQUIRED.action', 'err.DNS_SETTINGS_REQUIRED.action'],
    checking: '', unknown: '',
    known: [json(caps({ dns_server: '', dns_identity_ready: false, dns_management_ready: false })), 'err.DNS_SERVER_REQUIRED.action'],
  },
  {
    screen: 'Domains page (the list)', Component: Domains,
    read: '/api/v1/domains',
    negative: ['domains.empty', 'domains.emptyHint'],
    checking: 'domains.checking', unknown: 'domains.unknown',
    known: [json([]), 'domains.empty'],
  },
  {
    screen: 'Domains page (subscription usage)', Component: Domains,
    read: '/api/v1/subscriptions',
    negative: ['quota.unlimited'],
    checking: '', unknown: 'quota.unknown',
    known: [json({ subscriptions: [{ id: 1, name: 'Main', owner: 'admin', usage: { disk_used_bytes: 1, disk_limit_bytes: 0, domains: 1, domains_limit: 1, databases: 0, databases_limit: 1, mail_accounts: 0, mail_limit: 1 } }] }), 'quota.unlimited'],
  },
  {
    screen: 'Databases page (engines)', Component: DatabaseManagementV2,
    read: '/api/v1/database-servers',
    negative: ['databases.noServers', 'databases.noServersHint', 'domains.goServices', 'databases.account.missing'],
    checking: 'databases.checkingServers', unknown: 'databases.serversUnknown',
    known: [json([]), 'databases.noServers'],
  },
  {
    screen: 'Databases page (databases of an engine)', Component: DatabaseManagementV2,
    read: '/api/v1/database-servers/1/databases',
    negative: ['databases.empty.databases', 'databases.empty.databasesHint'],
    checking: 'databases.checkingDatabases', unknown: 'databases.databasesUnknown',
    known: [json([]), 'databases.empty.databases'],
  },
  {
    screen: 'Databases page (users of an engine)', Component: DatabaseManagementV2,
    read: '/api/v1/database-servers/1/users',
    // The users tab is not open, so its own lines are not on screen; what must
    // hold is that "Add database", whose dialogue offers the users, is off.
    negative: ['databases.empty.users'],
    checking: '', unknown: '',
    known: null,
    alsoDisabled: 'databases.addDatabase',
  },
  {
    screen: 'Domain databases (the list)', Component: DomainDatabaseManager, props: domain,
    read: '/api/v1/domains/1/databases',
    negative: ['databases.empty.databases', 'No databases created yet', 'db.teamEngineUnavailable'],
    checking: 'db.checking', unknown: 'db.unknown',
    known: [json({ databases: [], available_types: ['mysql'] }), 'databases.empty.databases'],
  },
  {
    screen: 'Domain databases (engines, administrator)', Component: DomainDatabaseManager, props: domain,
    read: '/api/v1/hosting/capabilities',
    negative: ['databases.noServersHint', 'db.teamEngineUnavailable', 'dbtools.install'],
    checking: 'db.checkingEngines', unknown: 'db.enginesUnknown',
    known: [json(caps({ database_servers: [] })), 'databases.noServersHint'],
    alsoDisabled: 'Create Database',
  },
  {
    screen: 'Domain databases (team member)', Component: DomainDatabaseManager, props: { ...domain, isAdditionalUser: true }, role: 'additional_user',
    read: '/api/v1/domains/1/databases',
    negative: ['db.teamEngineUnavailable', 'databases.empty.databases'],
    checking: 'db.checking', unknown: 'db.unknown',
    known: [json({ databases: [], available_types: [] }), 'db.teamEngineUnavailable'],
    alsoDisabled: 'Create Database',
  },
  {
    screen: 'Domain connection card', Component: DomainConnection, props: domain,
    read: '/api/v1/domains/1/connection',
    negative: ['conn.intro', 'conn.sslBlocked', 'conn.none', 'conn.status.unresolved', 'conn.status.elsewhere', 'conn.nsBroken.title'],
    checking: 'conn.checking', unknown: 'conn.readFailed',
    known: [json(connection({ status: 'unresolved', live_nameservers: [], live_ips: [], ssl_ready: false })), 'conn.intro'],
  },
  {
    screen: 'Domain DNS records (is the zone served)', Component: DomainDNSManager, props: domain,
    read: '/api/v1/hosting/capabilities',
    negative: ['dns.notServed'],
    checking: 'dns.checkingServer', unknown: 'server sentence|dns.statusUnavailable',
    known: [json(caps({ dns_server: '', dns_identity_ready: false })), 'dns.notServed'],
  },
  {
    screen: 'Domain PHP settings (installed versions)', Component: DomainPHPSettings, props: { ...domain, currentVersion: '8.3', onVersionChange() {} },
    read: '/api/v1/hosting/capabilities',
    negative: [],
    checking: 'php.checkingVersions', unknown: 'php.versionsUnknown',
    known: null,
  },
  {
    screen: 'Hosting type (is PHP installed)', Component: HostingTypePanel, props: domain,
    read: '/api/v1/hosting/capabilities',
    negative: ['hosting.phpMissing'],
    checking: '', unknown: 'hosting.phpUnknown',
    known: [json(caps({ php_versions: [] })), 'hosting.phpMissing'],
  },
];

for (const row of table) {
  for (const [failure, answer] of Object.entries(failures)) {
    test(`${row.screen}: ${failure} → nothing negative, nothing that depends on it enabled`, async () => {
      serve({ ...row.with, [row.read]: answer }, row.role);
      try {
        await mount(row.Component, row.props);
        for (const key of row.negative) assert.ok(!has(key), `${key} is drawn although ${row.read} is not known`);
        if (row.alsoDisabled) {
          const controls = buttons(row.alsoDisabled);
          assert.ok(controls.length > 0, `${row.alsoDisabled} is not drawn`);
          assert.ok(controls.every((node) => node.props.disabled), `${row.alsoDisabled} is enabled`);
        }
        const pending = failure === 'still on its way';
        const expected = pending ? row.checking : row.unknown;
        if (expected) assert.match(text(), new RegExp(expected.replaceAll('.', '\\.')), `the screen does not say "${expected}"`);
        if (pending) {
          if (row.unknown) assert.doesNotMatch(text(), new RegExp(row.unknown.replaceAll('.', '\\.')), 'a read that has not failed is shown as failed');
          assert.equal(buttons('common.retry').length, 0, 'Retry is offered for a read that has not failed');
        } else if (row.unknown) {
          assert.ok(buttons('common.retry').length >= 1, 'a failed read offers no Retry');
          if (row.checking) assert.doesNotMatch(text(), new RegExp(row.checking.replaceAll('.', '\\.')), 'a failed read is still shown as checking');
        }
        assert.equal(calls.filter((call) => call.method !== 'GET').length, 0, 'the screen changed something');
      } finally {
        await cleanup();
      }
    });
  }

  if (row.known) {
    test(`${row.screen}: the negative text does appear once the server has said so`, async () => {
      serve({ ...row.with, [row.read]: row.known[0] }, row.role);
      try {
        await mount(row.Component, row.props);
        assert.ok(has(row.known[1]), `${row.known[1]} is not drawn for a known negative answer`);
        assert.equal(buttons('common.retry').length, 0);
      } finally {
        await cleanup();
      }
    });
  }

  if (row.unknown) test(`${row.screen}: Retry reads again and the answer replaces the notice`, async () => {
    let attempt = 0;
    serve({ ...row.with, [row.read]: (request) => (attempt++ === 0 ? failures['refused by the server']() : healthy[row.read](request)) }, row.role);
    try {
      await mount(row.Component, row.props);
      const before = reads(row.read);
      assert.equal(buttons('common.retry').length, 1);
      await press(buttons('common.retry')[0]);
      assert.equal(reads(row.read), before + 1, 'Retry did not read exactly once');
      assert.equal(buttons('common.retry').length, 0, 'the notice stayed after a good answer');
      assert.equal(calls.filter((call) => call.method !== 'GET').length, 0, 'Retry changed something');
    } finally {
      await cleanup();
    }
  });
}

// --- Every read of a screen withheld at once ------------------------------------
// The whole screen, with nothing answered or with nothing reachable: no claim
// about the server anywhere on it, and not one enabled control that submits or
// removes.
const mounts = new Map();
for (const row of table) {
  const key = `${row.Component.name}/${row.role ?? 'admin'}`;
  if (!mounts.has(key)) mounts.set(key, { ...row, negative: [] });
  mounts.get(key).negative.push(...row.negative);
}
for (const row of mounts.values()) {
  for (const failure of ['still on its way', 'dropped connection']) {
    test(`${row.Component.name} (${row.role ?? 'administrator'}): every read ${failure} → no negative text, no enabled submit or remove`, async () => {
      serve(Object.fromEntries(Object.keys(healthy).map((path) => [path, failures[failure]])), row.role);
      try {
        await mount(row.Component, row.props);
        for (const key of new Set(row.negative)) assert.ok(!has(key), `${key} is drawn although nothing is known`);
        assert.deepEqual(enabledDangerousControls(), [], 'a submitting or removing control is enabled');
        assert.equal(calls.filter((call) => call.method !== 'GET').length, 0);
      } finally {
        await cleanup();
      }
    });
  }
}

// --- The owner's incident, step by step ----------------------------------------

test('Add domain: checking shows the form and one quiet line; the blocker never appears on the way to a good answer', async () => {
  let release;
  serve({ '/api/v1/hosting/capabilities': () => new Promise((resolve) => { release = () => resolve(Response.json(caps())); }) });
  try {
    await mount(AddDomainModal, { onClose() {}, onSuccess() {} });
    const submit = () => tree.root.findAll((node) => node.type === 'button' && node.props.type === 'submit')[0];
    assert.ok(has('dns.checkingServer'));
    assert.ok(has('domains.add.domainName'), 'the form is not shown while checking');
    assert.equal(tree.root.findAllByProps({ type: 'text' })[0].props.disabled, undefined, 'the name cannot be typed while checking');
    assert.deepEqual(tree.root.findAllByProps({ name: 'purpose' }).map((radio) => radio.props.disabled), [false, false]);
    assert.equal(submit().props.disabled, true);
    assert.ok(!has('domains.add.needsDns') && !has('err.DNS_SERVER_REQUIRED.action'));

    await act(async () => { release(); await settled(); await settled(); });
    assert.ok(!has('dns.checkingServer'), 'the checking line stayed');
    assert.ok(has('domains.add.dnsServed'));
    assert.ok(!has('domains.add.needsDns') && !has('err.DNS_SERVER_REQUIRED.action'));
    assert.equal(submit().props.disabled, false);
  } finally {
    await cleanup();
  }
});

test('Add domain: a purpose chosen while checking is kept; the default follows a known answer only until then', async () => {
  let release;
  serve({ '/api/v1/hosting/capabilities': () => new Promise((resolve) => { release = (body) => resolve(Response.json(body)); }) });
  try {
    await mount(AddDomainModal, { onClose() {}, onSuccess() {} });
    const radios = () => tree.root.findAllByProps({ name: 'purpose' });
    assert.deepEqual(radios().map((radio) => radio.props.checked), [true, false]);
    await act(async () => { radios()[1].props.onChange(); await settled(); });
    await act(async () => { release(caps()); await settled(); await settled(); });
    assert.deepEqual(radios().map((radio) => radio.props.checked), [false, true], 'the choice made while checking was replaced');
  } finally {
    await cleanup();
  }

  serve({ '/api/v1/hosting/capabilities': json(caps({ web_server: '' })) });
  try {
    await mount(AddDomainModal, { onClose() {}, onSuccess() {} });
    const radios = tree.root.findAllByProps({ name: 'purpose' });
    assert.deepEqual(radios.map((radio) => radio.props.checked), [false, true], 'no web server is known: only the DNS zone is possible');
    assert.equal(radios[0].props.disabled, true);
    assert.ok(has('domains.add.needsWebServer'));
  } finally {
    await cleanup();
  }
});

test('Add domain: nothing is submitted while DNS is not known, even through the form itself', async () => {
  for (const answer of [failures['still on its way'], failures['dropped connection']]) {
    serve({ '/api/v1/hosting/capabilities': answer });
    try {
      await mount(AddDomainModal, { onClose() {}, onSuccess() {} });
      const form = tree.root.findAll((node) => node.type === 'form')[0];
      await act(async () => { await form.props.onSubmit({ preventDefault() {} }); await settled(); });
      assert.equal(calls.filter((call) => call.method !== 'GET').length, 0);
    } finally {
      await cleanup();
    }
  }
});

test('the dialogue uses the answer the page already has: one request for both', async () => {
  serve();
  try {
    await mount(Domains);
    assert.equal(reads('/api/v1/hosting/capabilities'), 1);
    await press(buttons('domains.add')[0]);
    assert.ok(has('domains.add.title'), 'the dialogue did not open');
    assert.equal(reads('/api/v1/hosting/capabilities'), 1, 'the dialogue asked again for what the page has');
    assert.ok(!has('dns.checkingServer'), 'the dialogue showed a checking line for an answer the page has');
    assert.ok(has('domains.add.dnsServed'));
    assert.deepEqual(tree.root.findAll((node) => node.type === 'button' && node.props.type === 'submit').map((node) => node.props.disabled), [false]);
  } finally {
    await cleanup();
  }
});

test('two panels mounted together cause one request; a later visit reads again', async () => {
  serve();
  try {
    await act(async () => {
      tree = Renderer.create(React.createElement(React.Fragment, null,
        React.createElement(DomainDatabaseManager, domain),
        React.createElement(HostingTypePanel, domain),
        React.createElement(DomainPHPSettings, { ...domain, currentVersion: '8.3', onVersionChange() {} })));
      await settled();
      await settled();
    });
    assert.equal(reads('/api/v1/hosting/capabilities'), 1);
    await act(async () => tree.unmount());
    tree = undefined;
    await mount(HostingTypePanel, domain);
    assert.equal(reads('/api/v1/hosting/capabilities'), 2, 'an answer nobody was showing any more was reused');
  } finally {
    await cleanup();
  }
});

test('a team member is never asked for the server-wide inventory', async () => {
  for (const [Component, props] of [
    [Domains, {}],
    [DomainDatabaseManager, { ...domain, isAdditionalUser: true }],
    [DomainDNSManager, { ...domain, isAdditionalUser: true }],
    [DomainPHPSettings, { ...domain, currentVersion: '8.3', onVersionChange() {}, isAdditionalUser: true }],
  ]) {
    serve({ '/api/v1/domains': json([{ ...domainRow, access: { dns: 'manage' } }]) }, 'additional_user');
    try {
      await mount(Component, props);
      assert.equal(reads('/api/v1/hosting/capabilities'), 0, `${Component.name} read the capabilities for a team member`);
      for (const key of ['dns.checkingServer', 'php.checkingVersions', 'db.checkingEngines']) assert.ok(!has(key), `${Component.name} waits for a read it never makes`);
    } finally {
      await cleanup();
    }
  }
});

// --- A list that was known, then could not be read again -----------------------

test('Domains: after a failed refresh the earlier list stays, marked, and nothing that removes a domain is enabled', async () => {
  let fail = false;
  serve({ '/api/v1/domains': (request) => (fail ? failures['refused by the server']() : healthy['/api/v1/domains'](request)) });
  try {
    await mount(Domains);
    const remove = () => buttons('domains.action.delete');
    assert.equal(remove().length, 1);
    assert.equal(Boolean(remove()[0].props.disabled), false);
    fail = true;
    globalThis.confirm = () => true;
    await press(remove()[0]);
    assert.ok(has('example.com'), 'the earlier list was dropped');
    assert.ok(has('common.staleNotice'), 'the earlier list is not marked as the earlier list');
    assert.ok(!has('domains.empty'));
    assert.equal(remove()[0].props.disabled, true, 'a domain can be removed from a list that is not current');
    assert.deepEqual(enabledDangerousControls(), []);
    assert.equal(buttons('common.retry').length, 1);
    fail = false;
    await press(buttons('common.retry')[0]);
    assert.ok(!has('common.staleNotice'));
    assert.equal(Boolean(remove()[0].props.disabled), false);
  } finally {
    await cleanup();
  }
});

test('Domains: a pending row whose saved deletion could not be read says exactly that', async () => {
  const pendingRow = { ...domainRow, id: 9, domain_name: 'leaving.example', status: 'pending' };
  serve({ '/api/v1/domains': json([pendingRow]), '/api/v1/domains/9/deletion-status': failures['refused by the server'] });
  try {
    await mount(Domains);
    assert.ok(has('domains.pendingUnknown'));
    assert.ok(!has('domains.deletionWaiting'), 'an unread marker is announced as a waiting deletion');
    assert.equal(calls.filter((call) => call.method !== 'GET').length, 0);
  } finally {
    await cleanup();
  }
  serve({ '/api/v1/domains': json([pendingRow]), '/api/v1/domains/9/deletion-status': () => new Response(null, { status: 204 }) });
  try {
    await mount(Domains);
    assert.ok(!has('domains.pendingUnknown') && !has('domains.deletionWaiting'), 'the server said there is no marker');
  } finally {
    await cleanup();
  }
  serve({ '/api/v1/domains': json([pendingRow]), '/api/v1/domains/9/deletion-status': json({ status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_proof_timeout' }) });
  try {
    await mount(Domains);
    // The stand-in catalogue has no sentence for the reason, so the screen
    // falls back to its generic waiting text, as it does for an unreviewed one.
    assert.ok(has('domains.deletionWaiting') && has('domains.deletionPending') && !has('domains.pendingUnknown'));
  } finally {
    await cleanup();
  }
});

// --- Databases: the account strip follows the latest answer ---------------------

test('Databases: after the panel account is removed the strip shows the new answer, not the row it was mounted with', async () => {
  let account = 'celikpanel';
  serve({ '/api/v1/database-servers': () => Response.json([{ ...engine, admin_username: account }]) });
  try {
    await mount(DatabaseManagementV2);
    assert.ok(has('databases.account.present'));
    assert.equal(buttons('databases.account.rotate').length, 1);
    account = '';
    await press(buttons('databases.account.remove')[0]);
    assert.equal(calls.filter((call) => call.method === 'DELETE' && call.url.endsWith('/admin-account')).length, 1);
    assert.ok(!has('databases.account.present'), 'the strip still shows the account that was just removed');
    assert.equal(buttons('databases.account.rotate').length, 0, '"New password" is still offered for a removed account');
    assert.ok(has('databases.account.missing'));
  } finally {
    await cleanup();
  }
});

test('Databases: while the engine list is being read again, or could not be, the account controls are off', async () => {
  let mode = 'ok';
  let release;
  serve({ '/api/v1/database-servers': () => {
    if (mode === 'hang') return new Promise((resolve) => { release = () => resolve(Response.json([engine])); });
    if (mode === 'fail') return failures['refused by the server']();
    return Response.json([engine]);
  } });
  try {
    await mount(DatabaseManagementV2);
    const account = () => ['databases.account.show', 'databases.account.rotate', 'databases.account.remove'].flatMap(buttons);
    assert.deepEqual(account().map((node) => Boolean(node.props.disabled)), [false, false, false]);
    mode = 'hang';
    await press(buttons('databases.account.rotate')[0]);
    assert.equal(calls.filter((call) => call.method === 'POST').length, 1);
    assert.deepEqual(account().map((node) => Boolean(node.props.disabled)), [true, true, true], 'the row is being read again');
    await act(async () => { release(); await settled(); await settled(); });
    assert.deepEqual(account().map((node) => Boolean(node.props.disabled)), [false, false, false], 'the row is current again');
    mode = 'fail';
    await press(buttons('databases.account.rotate')[0]);
    assert.equal(calls.filter((call) => call.method === 'POST').length, 2);
    assert.ok(has('common.staleNotice'));
    assert.deepEqual(account().map((node) => Boolean(node.props.disabled)), [true, true, true], 'the row could not be read again');
    assert.deepEqual(enabledDangerousControls().filter((label) => label.includes('account')), []);
  } finally {
    await cleanup();
  }
});

test('Databases: a list that could not be read again keeps its rows and withdraws delete; counts are never invented', async () => {
  let fail = false;
  serve({ '/api/v1/database-servers/1/databases': (request) => (fail ? failures['dropped connection']() : healthy['/api/v1/database-servers/1/databases'](request)) });
  try {
    await mount(DatabaseManagementV2);
    assert.ok(has('shop'));
    const remove = () => buttons('databases.deleteDatabase');
    assert.equal(Boolean(remove()[0].props.disabled), false);
    fail = true;
    await press(remove()[0]);
    assert.ok(has('shop') && has('common.staleNotice'));
    assert.equal(remove()[0].props.disabled, true);
    assert.ok(buttons('databases.addDatabase').every((node) => node.props.disabled));
  } finally {
    await cleanup();
  }
  serve({ '/api/v1/database-servers/1/databases': failures['still on its way'], '/api/v1/database-servers/1/users': failures['dropped connection'] });
  try {
    await mount(DatabaseManagementV2);
    const badges = tree.root.findAll((node) => node.type === 'span' && /min-w-\[1\.5rem\]/.test(node.props.className ?? '')).map((node) => node.props.children);
    assert.deepEqual(badges, ['…', '–'], 'a count is shown for a list that is not known');
    assert.ok(!has('common.itemsTotal'));
  } finally {
    await cleanup();
  }
});

// --- Domain databases: no default engine ----------------------------------------

test('Domain databases: nothing is created on an engine the server did not name', async () => {
  for (const answer of [failures['still on its way'], failures['refused by the server'], json(caps({ database_servers: [] }))]) {
    serve({ '/api/v1/hosting/capabilities': answer });
    try {
      await mount(DomainDatabaseManager, domain);
      assert.equal(tree.root.findAll((node) => node.type === 'form').length, 0, 'the create form is shown without a known engine');
      for (const node of buttons('Create Database')) assert.equal(node.props.disabled, true);
      assert.equal(calls.filter((call) => call.method !== 'GET').length, 0);
    } finally {
      await cleanup();
    }
  }
  serve();
  try {
    await mount(DomainDatabaseManager, domain);
    await press(buttons('Create Database')[0]);
    assert.deepEqual(tree.root.findAll((node) => node.type === 'option').map((node) => node.props.value), ['mysql']);
  } finally {
    await cleanup();
  }
});

// --- Domain connection: "unknown" is not "does not point here" -------------------

test('Domain connection: an answer of status unknown with null lists is drawn, and reads as not checked', async () => {
  serve({ '/api/v1/domains/1/connection': json(connection({
    status: 'unknown', ssl_ready: false, nameservers_usable: false,
    nameservers: null, live_nameservers: null, live_ips: null, resolver_observations: null, nameserver_facts: null,
  })) });
  try {
    await mount(DomainConnection, domain);
    assert.ok(has('conn.status.unknown') && has('conn.unknownHelp') && has('conn.notChecked') && has('conn.sslUnknown'));
    for (const key of ['conn.intro', 'conn.sslBlocked', 'conn.none', 'conn.nsBroken.title', 'conn.routeA.title', 'conn.status.unresolved']) {
      assert.ok(!has(key), `${key} is drawn for a domain whose public DNS could not be checked`);
    }
    assert.ok(has('conn.routeAUnknown') && has('conn.routeB.title'));
    assert.doesNotMatch(text(), /text-warning"|text-danger/);
  } finally {
    await cleanup();
  }
});

test('Domain connection: null lists never crash the card, whatever the status', async () => {
  for (const status of ['delegated', 'delegated_mismatch', 'a_record', 'elsewhere', 'unresolved', 'unknown']) {
    serve({ '/api/v1/domains/1/connection': json(connection({
      status, nameservers: null, live_nameservers: null, live_ips: null, glue_needed: true, propagation_pending: true,
      resolver_observations: [{ resolver: 'one', nameservers: null, ips: null, status, ssl_ready: false }],
      nameserver_facts: [{ host: 'ns1', ips: null, points_here: false }],
    })) });
    try {
      await mount(DomainConnection, domain);
      assert.ok(has('conn.title'), status);
    } finally {
      await cleanup();
    }
  }
  serve({ '/api/v1/domains/1/connection': json(connection({ status: 'somewhere_new' })) });
  try {
    await mount(DomainConnection, domain);
    assert.ok(has('conn.readFailed'), 'a status this card has no words for is guessed at');
  } finally {
    await cleanup();
  }
});

test('Domain connection: "Check again" keeps the card and reads once; a failed re-check keeps the earlier answer, marked', async () => {
  let fail = false;
  serve({ '/api/v1/domains/1/connection': (request) => (fail ? failures['dropped connection']() : healthy['/api/v1/domains/1/connection'](request)) });
  try {
    await mount(DomainConnection, domain);
    assert.ok(has('conn.okDelegated'));
    fail = true;
    await press(buttons('conn.recheck')[0]);
    assert.equal(reads('/api/v1/domains/1/connection'), 2);
    assert.ok(has('conn.okDelegated') && has('common.staleNotice'));
  } finally {
    await cleanup();
  }
});

// --- The layer itself ------------------------------------------------------------

test('readRemote never throws and never invents a value', async () => {
  const { readRemote, decodeList, lastKnown, mapRemote, LOADING } = await import(remoteURL);
  const run = async (answer, decode = (raw) => raw, earlier) => {
    globalThis.fetch = answer;
    try {
      return await readRemote('/x', decode, earlier);
    } finally {
      globalThis.fetch = originalFetch;
    }
  };
  assert.equal((await run(() => Promise.reject(new TypeError('offline')))).state, 'unknown');
  assert.equal((await run(async () => new Response('', { status: 500 }))).state, 'unknown');
  assert.equal((await run(async () => new Response('not json'))).state, 'unknown');
  assert.equal((await run(async () => Response.json({ rows: 1 }), decodeList)).state, 'unknown');
  assert.deepEqual((await run(async () => Response.json(null), decodeList)).value, []);
  const refused = await run(async () => Response.json({ error: 'e', code: 'C' }, { status: 409 }));
  assert.deepEqual([refused.state, refused.reason.code, 'previous' in refused], ['unknown', 'C', false]);
  const known = await run(async () => Response.json([1]), decodeList);
  assert.equal(known.state, 'known');
  assert.ok(Math.abs(known.observedAt - Date.now()) < 5000);
  const stale = await run(() => Promise.reject(new TypeError('offline')), decodeList, known);
  assert.deepEqual(stale.previous, { value: [1], observedAt: known.observedAt });
  const twice = await run(() => Promise.reject(new TypeError('offline')), decodeList, stale);
  assert.deepEqual(twice.previous, stale.previous, 'the earlier answer is lost by a second failure');
  assert.equal(lastKnown(LOADING), undefined);
  assert.equal(mapRemote(LOADING, () => 1), LOADING);
  assert.deepEqual(mapRemote(stale, (value) => value.length).previous.value, 1);
});

test('every text of the three states exists in both languages, and the could-not-check ones say nothing was changed', () => {
  const failed = new Set(['common.staleNotice', 'domains.pendingUnknown', 'databases.usersUnknown']);
  const other = new Set(['common.retry', 'conn.notChecked', 'conn.unknownHelp', 'conn.routeAUnknown', 'conn.sslUnknown',
    'databases.checkingUsers', 'databases.deleteDatabase', 'databases.deleteUser']);
  for (const row of table) {
    if (row.checking) other.add(row.checking);
    for (const key of row.unknown.split('|')) if (key.includes('.') && !key.includes(' ')) failed.add(key);
  }
  const line = (catalogue, key) => catalogue.split('\n').find((candidate) => candidate.trimStart().startsWith(`'${key}'`));
  for (const key of [...failed, ...other]) {
    const en = line(englishCatalogue, key);
    const tr = line(turkishCatalogue, key);
    assert.ok(en, `the English catalogue has no ${key}`);
    assert.ok(tr, `the Turkish catalogue has no ${key}`);
    assert.notEqual(en.split(':').slice(1).join(':'), tr.split(':').slice(1).join(':'), `${key} is not translated`);
  }
  // A could-not-check sentence says that nothing was changed, in both
  // languages. Two are about a look-up that could change nothing: the public
  // DNS of a domain, and the older DNS status sentence.
  for (const key of failed) {
    if (key === 'conn.readFailed' || key === 'dns.statusUnavailable') continue;
    assert.match(line(englishCatalogue, key), /Nothing was changed/, `${key} does not say that nothing was changed`);
    assert.match(line(turkishCatalogue, key), /Hiçbir şey değiştirilmedi/, `${key}: Turkish does not say that nothing was changed`);
  }
  // A checking line is one short sentence that ends in an ellipsis.
  for (const row of table) {
    if (!row.checking) continue;
    for (const catalogue of [englishCatalogue, turkishCatalogue]) assert.match(line(catalogue, row.checking), /…',$/, row.checking);
  }
});
