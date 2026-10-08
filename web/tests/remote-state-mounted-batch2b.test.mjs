import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { apiErrorURL, compileSource, dataModule, reactURL, remoteURL, sharedLayer } from './fixtures/shared-layer.mjs';
import { readAllowList, scanTree } from './remote-state-ratchet.mjs';
import { fileURLToPath } from 'node:url';

// NO NEGATIVE UI UNLESS KNOWN, second batch (9 Oct 2026, D-022, D-024): the
// database configuration editors, a domain's mail screens and the mail queue.
// This file follows remote-state-mounted.test.mjs and stands beside it so the
// two batches can be merged without touching each other's table.
//
// Every screen is mounted with the real shared layer and one of its reads is
// made to hang, to drop, to be refused or to answer with something that is not
// the contract. In each case the screen must not say that something is
// missing, empty or off, must not show an editor, and must not offer an
// enabled control that saves or removes. The same read is then answered with a
// real negative, to show the absence above is not an accident of the fixture.
//
// What these screens did before: the three database editors opened on nothing
// after a failed read with Save enabled (and Save posted text/plain to a
// handler that reads JSON, so no save ever worked); the catch-all field could
// be typed into before the current address was read; the mailbox list was
// empty without a word; the webmail card said "not available on this server";
// the mail queue said "empty".

const source = (path) => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const screenFiles = [
  'PostgreSQLManagement', 'MariaDBManagement', 'PostgreSQLSettings', 'MariaDBSettings', 'PostgreSQLAccessRules',
  'ConfigEditor', 'ConfigSettingsEditor', 'ConfigFileNotices', 'MailSettingsPanel', 'DomainMailManager', 'WebmailAccess',
  'PostfixManagement',
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
  const i18n = { t: (key, vars) => (vars ? key + JSON.stringify(vars) : key), locale: 'en' };
  export const useI18n = () => i18n;
  export const useNavigate = () => () => true;
  export const showToast = (...args) => globalThis.currentTest.toasts.push(args);
  export const ServiceShell = (props) => React.createElement('main', null, props.children);
  export const ComponentPanels = () => React.createElement('aside', null, 'component-panels');
  export const MailAuthPanel = () => null;
  // What components/ComponentOperation.tsx imports and never calls here: only
  // its decoder is used, and that is the real one.
  export const createPortal = () => null;
  export const scanRefusedBySetup = () => false;
  export const useNavigationBlocker = () => {};
`);
const apiURL = compileSource('lib/api.ts', () => stub);
const shared = sharedLayer(stub);
const lib = (path, more = {}) => shared.compile(path, more);
// A module of src/lib imports its neighbours by their bare name.
const beside = { '/api': apiURL, '/apiError': apiErrorURL, '/remote': remoteURL };
const configFileURL = lib('lib/configFile.ts', beside);
const textURL = compileSource('lib/dbConfigText.ts', () => stub);
// The one decoder of the cached component scan is the fail-closed one the
// operation tracker exports, so it is compiled from source here: a stand-in
// could accept an answer the shipped pages refuse.
const operationURL = lib('components/ComponentOperation.tsx');
const managedURL = lib('lib/managedServices.ts', { ...beside, '/ComponentOperation': operationURL });
const mailSetupURL = compileSource('lib/mailSetup.ts', () => stub);
const currentURL = lib('components/CurrentSettings.tsx');
const own = {
  '/lib/configFile': configFileURL, '/lib/dbConfigText': textURL, '/lib/managedServices': managedURL,
  '/lib/mailSetup': mailSetupURL, '/CurrentSettings': currentURL,
};
const noticesURL = lib('components/ConfigFileNotices.tsx', own);
const settingsEditorURL = lib('components/ConfigSettingsEditor.tsx', { ...own, '/ConfigFileNotices': noticesURL });
const editors = { ...own, '/ConfigFileNotices': noticesURL, '/ConfigSettingsEditor': settingsEditorURL };
const pgSettingsURL = lib('components/PostgreSQLSettings.tsx', editors);
const mySettingsURL = lib('components/MariaDBSettings.tsx', editors);
const hbaURL = lib('components/PostgreSQLAccessRules.tsx', editors);
const rawURL = lib('components/ConfigEditor.tsx', editors);
const pages = { ...editors, '/PostgreSQLSettings': pgSettingsURL, '/MariaDBSettings': mySettingsURL, '/PostgreSQLAccessRules': hbaURL, '/ConfigEditor': rawURL };
const pick = async (url, name) => (await import(url))[name];
const PostgreSQLManagement = await pick(lib('components/PostgreSQLManagement.tsx', pages), 'PostgreSQLManagement');
const MariaDBManagement = await pick(lib('components/MariaDBManagement.tsx', pages), 'MariaDBManagement');
const PostgreSQLSettings = await pick(pgSettingsURL, 'PostgreSQLSettings');
const MariaDBSettings = await pick(mySettingsURL, 'MariaDBSettings');
const PostgreSQLAccessRules = await pick(hbaURL, 'PostgreSQLAccessRules');
const ConfigEditor = await pick(rawURL, 'ConfigEditor');
const mailSettingsURL = lib('components/MailSettingsPanel.tsx', own);
const MailSettingsPanel = await pick(mailSettingsURL, 'MailSettingsPanel');
const webmailURL = lib('components/WebmailAccess.tsx', own);
const WebmailAccess = (await import(webmailURL)).default;
const PostfixManagement = await pick(lib('components/PostfixManagement.tsx', own), 'PostfixManagement');

// DomainMailManager loads the webmail card with a dynamic import, which a
// module compiled into a data: URL cannot resolve by itself.
const mailManagerText = source('components/DomainMailManager.tsx').replace("import('./WebmailAccess')", `import('${webmailURL}')`);
assert.ok(mailManagerText.includes(webmailURL), 'DomainMailManager no longer loads the webmail card lazily');
const mailManagerCompiled = ts.transpileModule(mailManagerText, {
  compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 },
}).outputText.replace(/from ['"]([^'"]+)['"]/g, (_, specifier) => {
  if (specifier === 'react') return `from '${reactURL}'`;
  if (specifier.endsWith('/MailSettingsPanel')) return `from '${mailSettingsURL}'`;
  return `from '${shared.resolve(specifier)}'`;
});
const DomainMailManagerInner = (await import(dataModule(`import React from '${reactURL}';\n${mailManagerCompiled}`))).DomainMailManager;
const DomainMailManager = (props) => React.createElement(React.Suspense, { fallback: null }, React.createElement(DomainMailManagerInner, props));

// --- What the server answers when nothing is wrong ----------------------------
const pgConf = "# CONNECTIONS AND AUTHENTICATION\n\n#listen_addresses = 'localhost'\t\t# what IP address(es) to listen on;\nmax_connections = 100\t\t\t# (change requires restart)\n   shared_buffers=128MB # tuned by hand\ninclude_dir = 'conf.d'\n# the owner's note\n";
const hbaConf = "# DO NOT DISABLE!\nlocal   all             postgres                                peer\n\n# TYPE  DATABASE        USER            ADDRESS                 METHOD\nlocal   all             all                                     peer\nhost    all             all             127.0.0.1/32            scram-sha-256\nhost    all             app             10.0.0.0/8              ldap ldapserver=x\ninclude_dir hba.d\n";
const myConf = "# tuned 2024\n[server]\n\n[mysqld]\nbind-address            = 127.0.0.1\n#key_buffer_size        = 128M\nmax_connections=150   # raised for the shop\n!includedir /etc/mysql/extra.d/\n";
const fileFor = (url) => {
  const path = decodeURIComponent(url.split('path=')[1] ?? '');
  const content = path.endsWith('pg_hba.conf') ? hbaConf : path.endsWith('.cnf') ? myConf : pgConf;
  return { Content: content, Parsed: '', Version: 'cf1-' + path.split('/').pop() };
};
// The whole contract of the cached scan, as the Panel writes it: the shared
// decoder refuses an answer with a part missing, and a refused answer is "could
// not be read", never "this component has no files".
const component = (id, config_files) => ({
  id, name: id, description: `${id} on this server`, icon: '', category: 'database', status: 'active (running)', is_installed: true,
  versions: [], config_files,
});
const mailProfile = (id) => ({
  id, name: id, description: `${id} for this server`, status: 'available', available: true, verified: false,
  latest_attempt_status: 'none', services: ['postfix'],
});
const scanOf = (...services) => ({
  scanned_at: '2026-10-09T00:00:00Z',
  services: [...services, component('postfix', [])],
  profiles: ['core-mail', 'webmail', 'protected-mail'].map(mailProfile),
  dns_identity_ready: true,
  mail_hostname: { current: 'server1', current_usable: false, hostname: '', source: '', will_set_hostname: false },
});
const scan = scanOf(
  component('postgresql', [{ path: '/etc/postgresql/17/main/postgresql.conf' }, { path: '/etc/postgresql/17/main/pg_hba.conf' }]),
  component('mariadb', [{ path: '/etc/mysql/mariadb.conf.d/50-server.cnf' }]),
);
const protocol = (port, security) => ({ host: 'mail.example.com', port, security });
const setup = (overrides = {}) => ({
  mail_host: 'mail.example.com', imap: protocol(993, 'SSL/TLS'), pop3: protocol(995, 'SSL/TLS'), smtp: protocol(587, 'STARTTLS'),
  username_is_full_email: true, webmail_available: true, webmail_url: '/webmail/', ...overrides,
});
const json = (body, status = 200) => () => Response.json(body, { status });
const healthy = {
  '/api/v1/managed-services': json(scan),
  '/api/v1/config': ({ url }) => Response.json(fileFor(url)),
  '/api/v1/domains/1/mail/setup': json(setup()),
  '/api/v1/domains/1/mail/catch-all': json({ enabled: true, destination: 'owner@example.net', version: 'ca1-a' }),
  '/api/v1/domains/1/mail/accounts': json({ accounts: [{ id: 5, address: 'info@example.com', quota_mb: 1024 }] }),
  '/api/v1/domains/1/mail/forwardings': json({ forwardings: [{ id: 7, source: 'sales@example.com', destination: 'team@example.net' }] }),
  '/api/v1/domains/1/mail/quota': json({ plugin_enabled: true, usages: [{ email: 'info@example.com', used_kb: 2048, limit_kb: 1048576, available: true }] }),
  '/api/v1/domains/1/mail/health': json({ overall: 'ok', server_ip: '192.0.2.4', expected_ptr: 'mail.example.com', checks: [{ id: 'ptr', status: 'ok' }] }),
  '/api/v1/postfix/queue': json([{ id: '4F2C1A0B', size: '2.0 KB', sender: 'shop@example.com', arrival: '2026-10-09 10:00', status: 'deferred' }]),
  '/api/v1/mail/policy': json({ message_size_mb: 9, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-a' }),
};

// --- The ways a read can fail to produce a known answer -----------------------
const failures = {
  'still on its way': () => new Promise(() => {}),
  'dropped connection': () => Promise.reject(new TypeError('fetch failed')),
  'refused by the server': json({ error: 'server sentence', code: 'CURRENT_SETTINGS_UNREADABLE' }, 502),
  'an answer that is not the contract': json('not the contract'),
};

const originalFetch = globalThis.fetch;
let tree, calls;
function serve(overrides = {}, writes = {}) {
  calls = [];
  globalThis.currentTest = { toasts: [] };
  globalThis.confirm = () => true;
  globalThis.document = { addEventListener() {}, removeEventListener() {} };
  const table = { ...healthy, ...overrides };
  globalThis.fetch = (url, options = {}) => {
    const method = options.method || 'GET';
    const body = options.body ? JSON.parse(options.body) : undefined;
    calls.push({ method, url, body });
    const path = url.split('?')[0];
    if (method !== 'GET') {
      const answer = writes[`${method} ${path}`];
      return Promise.resolve(answer ? answer({ url, body }) : Response.json({ success: true, version: 'next' }));
    }
    const answer = table[path];
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
const has = (key) => text().includes(key);
const hostButtons = () => tree.root.findAll((node) => node.type === 'button');
const flat = (children) => [].concat(children ?? []).flat(Infinity);
const labelOf = (node) => [node.props['aria-label'], node.props.title, ...flat(node.props.children)]
  .filter((part) => typeof part === 'string').join(' ');
const buttons = (label) => hostButtons().filter((node) => labelOf(node).includes(label));
const press = async (node) => {
  await act(async () => {
    await node.props.onClick({});
    await settled();
    await settled();
    await settled();
  });
};
const type = async (node, value) => {
  await act(async () => {
    node.props.onChange({ target: { value, checked: value } });
    await settled();
  });
};
// Every string drawn inside a node.
const says = (node) => node.findAll(() => true).flatMap((inner) => flat(inner.props.children)).filter((part) => typeof part === 'string').join(' ');
const reads = (path) => calls.filter((call) => call.method === 'GET' && call.url.split('?')[0] === path).length;
const writesMade = () => calls.filter((call) => call.method !== 'GET');
const fields = () => tree.root.findAll((node) => ['input', 'textarea', 'select'].includes(node.type));

const destructiveWords = /delete|remove|\bsil\b|kaldır|disable|flush/i;
const dangerous = (node) => node.props.type === 'submit' || destructiveWords.test(labelOf(node))
  || /text-danger|hover:text-danger/.test(node.props.className ?? '') || /\.(save|update|enable)\b/.test(labelOf(node));
const enabledDangerousControls = () => hostButtons().filter((node) => !node.props.disabled && dangerous(node)).map(labelOf);

// --- The table -----------------------------------------------------------------
// screen / Component / props: what is mounted
// read:     the address whose answer is withheld
// negative: text keys that claim something about the server; none may be drawn
// checking / unknown: what the screen says instead
// known:    an answer that makes one of the negative keys true, and that key
// noEditor: nothing that edits or saves may exist while the read is withheld
// after:    a step taken after mounting, before looking (opening a tab)
const domain = { domainId: 1, domainName: 'example.com' };
const pgPath = { configPath: '/etc/postgresql/17/main/postgresql.conf' };
const hbaPath = { configPath: '/etc/postgresql/17/main/pg_hba.conf' };
const myPath = { configPath: '/etc/mysql/mariadb.conf.d/50-server.cnf' };
const openTab = (label) => async () => press(buttons(label)[0]);
const table = [
  {
    screen: 'PostgreSQL page (which files it has)', Component: PostgreSQLManagement, props: { onBack() {} },
    read: '/api/v1/managed-services', negative: ['db.fileNotFound'],
    checking: 'dbconf.files.checking', unknown: 'dbconf.files.unknown',
    known: [json(scanOf()), 'db.fileNotFound'], noEditor: true,
  },
  {
    screen: 'PostgreSQL page (access rules tab, which files it has)', Component: PostgreSQLManagement, props: { onBack() {} },
    after: openTab('db.tab.access'),
    read: '/api/v1/managed-services', negative: ['db.fileNotFound', 'dbconf.hba.none'],
    checking: 'dbconf.files.checking', unknown: 'dbconf.files.unknown',
    known: [json(scanOf(component('postgresql', null))), 'db.fileNotFound'], noEditor: true,
  },
  {
    screen: 'postgresql.conf settings', Component: PostgreSQLSettings, props: pgPath,
    read: '/api/v1/config', negative: ['dbconf.noSettings', 'dbconf.noMatch', 'dbconf.noChanges'],
    checking: 'dbconf.checking', unknown: 'dbconf.unknown',
    known: [json({ Content: '# the owner removed every setting\n', Parsed: '', Version: 'cf1-x' }), 'dbconf.noSettings'], noEditor: true,
  },
  {
    screen: 'pg_hba.conf access rules', Component: PostgreSQLAccessRules, props: hbaPath,
    read: '/api/v1/config', negative: ['dbconf.hba.none', 'No access rules', 'dbconf.noChanges'],
    checking: 'dbconf.checking', unknown: 'dbconf.unknown',
    known: [json({ Content: '# comments only\n', Parsed: '', Version: 'cf1-x' }), 'dbconf.hba.none'], noEditor: true,
  },
  {
    screen: 'MariaDB page (which files it has)', Component: MariaDBManagement, props: { onBack() {} },
    read: '/api/v1/managed-services', negative: ['db.fileNotFound'],
    checking: 'dbconf.files.checking', unknown: 'dbconf.files.unknown',
    known: [json(scanOf(component('mariadb', []))), 'db.fileNotFound'], noEditor: true,
  },
  {
    screen: 'MariaDB option file', Component: MariaDBSettings, props: myPath,
    read: '/api/v1/config', negative: ['dbconf.noSettings', 'dbconf.noChanges'],
    checking: 'dbconf.checking', unknown: 'dbconf.unknown',
    known: [json({ Content: '# nothing here\n', Parsed: '', Version: 'cf1-x' }), 'dbconf.noSettings'], noEditor: true,
  },
  {
    screen: 'raw configuration file', Component: ConfigEditor, props: { path: '/etc/postgresql/17/main/pg_hba.conf', onBack() {} },
    read: '/api/v1/config', negative: ['dbconf.noChanges'],
    checking: 'dbconf.checking', unknown: 'dbconf.unknown', known: null, noEditor: true,
  },
  {
    screen: 'mail client settings', Component: MailSettingsPanel, props: domain,
    read: '/api/v1/domains/1/mail/setup', negative: [],
    checking: 'mail.setup.checking', unknown: 'mail.setup.unknown', known: null,
  },
  {
    screen: 'catch-all address', Component: MailSettingsPanel, props: domain,
    read: '/api/v1/domains/1/mail/catch-all', negative: ['mail.catchAll.none', 'mail.catchAll.active'],
    checking: 'mail.catchAll.checking', unknown: 'mail.catchAll.unknown',
    known: [json({ enabled: false, destination: '', version: 'ca1-none' }), 'mail.catchAll.none'],
    off: ['mail.catchAll.enable', 'mail.catchAll.update', 'mail.catchAll.disable'], fieldsOff: (node) => node.props.type === 'email',
  },
  {
    screen: 'webmail card', Component: WebmailAccess, props: { domainId: 1 },
    read: '/api/v1/domains/1/mail/setup', negative: ['mail.webmail.unavailable', 'mail.webmail.available'],
    checking: 'mail.webmail.checking', unknown: 'mail.webmail.unknown',
    known: [json(setup({ webmail_available: false, webmail_url: '' })), 'mail.webmail.unavailable'],
  },
  {
    screen: 'mailboxes of a domain', Component: DomainMailManager, props: domain,
    read: '/api/v1/domains/1/mail/accounts', negative: ['mail.emptyAccounts', 'common.itemsTotal'],
    checking: 'mail.accounts.checking', unknown: 'mail.accounts.unknown',
    known: [json({ accounts: [] }), 'mail.emptyAccounts'], off: ['mail.addAccount'],
  },
  {
    screen: 'forwarders of a domain', Component: DomainMailManager, props: domain,
    after: openTab('mail.tab.forwarding'),
    read: '/api/v1/domains/1/mail/forwardings', negative: ['mail.emptyForwarders', 'common.itemsTotal'],
    checking: 'mail.forwarders.checking', unknown: 'mail.forwarders.unknown',
    known: [json({ forwardings: [] }), 'mail.emptyForwarders'], off: ['mail.addForwarder'],
  },
  {
    screen: 'deliverability checks', Component: DomainMailManager, props: domain,
    after: openTab('mailauth.tab'),
    read: '/api/v1/domains/1/mail/health', negative: ['mail.health.ptrFix'],
    checking: 'mail.healthChecking', unknown: 'mail.healthUnknown', known: null,
  },
  {
    screen: 'mail queue', Component: PostfixManagement, props: { onBack() {} },
    read: '/api/v1/postfix/queue', negative: ['postfix.empty', 'postfix.active', 'postfix.deferred'],
    checking: 'postfix.queue.checking', unknown: 'postfix.queue.unreadable',
    known: [json([]), 'postfix.empty'], off: ['postfix.flush', 'postfix.deleteAll'],
  },
];

const escape = (key) => key.replaceAll('.', '\\.');
for (const row of table) {
  for (const [failure, answer] of Object.entries(failures)) {
    test(`${row.screen}: ${failure} → nothing negative, no editor, nothing that depends on it enabled`, async () => {
      serve({ [row.read]: answer });
      try {
        await mount(row.Component, row.props);
        if (row.after) await row.after();
        for (const key of row.negative) assert.ok(!has(key), `${key} is drawn although ${row.read} is not known`);
        for (const label of row.off ?? []) {
          assert.ok(buttons(label).every((node) => node.props.disabled), `${label} is enabled although ${row.read} is not known`);
        }
        if (row.fieldsOff) {
          assert.ok(fields().filter(row.fieldsOff).every((node) => node.props.disabled), 'a field can be typed into before its value is known');
        }
        if (row.noEditor) {
          assert.deepEqual(fields().map((node) => node.type), [], 'an editor is shown for a file that is not known');
          assert.equal(buttons('dbconf.save').length, 0, 'Save is offered for a file that is not known');
          assert.equal(buttons('dbconf.hba.add').length, 0);
        }
        const pending = failure === 'still on its way';
        assert.match(text(), new RegExp(escape(pending ? row.checking : row.unknown)), `the screen does not say "${pending ? row.checking : row.unknown}"`);
        if (pending) {
          assert.doesNotMatch(text(), new RegExp(escape(row.unknown)), 'a read that has not failed is shown as failed');
          assert.equal(buttons('common.retry').length, 0, 'Retry is offered for a read that has not failed');
        } else {
          assert.equal(buttons('common.retry').length, 1, 'a failed read offers no Retry, or more than one');
          assert.doesNotMatch(text(), new RegExp(escape(row.checking) + '(?!Tab)'), 'a failed read is still shown as checking');
        }
        assert.deepEqual(writesMade(), [], 'the screen changed something');
      } finally {
        await cleanup();
      }
    });
  }

  if (row.known) {
    test(`${row.screen}: the negative text does appear once the server has said so`, async () => {
      serve({ [row.read]: row.known[0] });
      try {
        await mount(row.Component, row.props);
        if (row.after) await row.after();
        assert.ok(has(row.known[1]), `${row.known[1]} is not drawn for a known negative answer`);
        assert.equal(buttons('common.retry').length, 0);
      } finally {
        await cleanup();
      }
    });
  }

  test(`${row.screen}: Retry reads again and the answer replaces the notice`, async () => {
    let attempt = 0;
    serve({ [row.read]: (request) => (attempt++ === 0 ? failures['refused by the server']() : healthy[row.read](request)) });
    try {
      await mount(row.Component, row.props);
      if (row.after) await row.after();
      const before = reads(row.read);
      assert.equal(buttons('common.retry').length, 1);
      await press(buttons('common.retry')[0]);
      assert.equal(reads(row.read), before + 1, 'Retry did not read exactly once');
      assert.equal(buttons('common.retry').length, 0, 'the notice stayed after a good answer');
      assert.deepEqual(writesMade(), [], 'Retry changed something');
    } finally {
      await cleanup();
    }
  });
}

// --- One decoder and one request for the cached scan -----------------------------
// The scan has one decoder for every screen that reads it (lib/remote.ts keeps
// the first reader's decoder for the address). These are answers a looser
// reader used to take for "the scan names no file": a server nobody has
// scanned, a scan without the parts the component pages need, and a file list
// that is not a list.
const refusedScans = {
  'a server that was never scanned': { ...scanOf(), scanned_at: null },
  'only a list of services': { scanned_at: '2026-10-09T00:00:00Z', services: [] },
  'records without the catalogue fields': { ...scan, services: [{ id: 'postgresql', config_files: [] }, { id: 'mariadb', config_files: [] }] },
  'a file list that is not a list': scanOf(component('postgresql', 'none'), component('mariadb', {})),
  'a file without a path': scanOf(component('postgresql', [{}]), component('mariadb', [{ path: 7 }])),
};
for (const [Component, service] of [[PostgreSQLManagement, 'PostgreSQL'], [MariaDBManagement, 'MariaDB']]) {
  for (const [what, answer] of Object.entries(refusedScans)) {
    test(`${service} page: ${what} is "could not be read", not "not found"`, async () => {
      serve({ '/api/v1/managed-services': json(answer) });
      try {
        await mount(Component, { onBack() {} });
        assert.ok(!has('db.fileNotFound'), 'a refused scan is shown as a scan that names no file');
        assert.ok(has('dbconf.files.unknown'));
        assert.equal(buttons('common.retry').length, 1);
        assert.deepEqual(fields().map((node) => node.type), [], 'an editor is shown for a scan that was refused');
        assert.equal(reads('/api/v1/config'), 0, 'a file was read although the scan names none');
      } finally {
        await cleanup();
      }
    });
  }
}

// The overview and the log under the editor are drawn from the same read as
// the file list. One read that is on its way or has failed is said once, by
// the file card; the sections come with the answer.
for (const [Component, service] of [[PostgreSQLManagement, 'PostgreSQL'], [MariaDBManagement, 'MariaDB']]) {
  test(`${service} page: one read is announced once, and the sections under the editor come with its answer`, async () => {
    for (const [failure, answer] of Object.entries(failures)) {
      serve({ '/api/v1/managed-services': answer });
      try {
        await mount(Component, { onBack() {} });
        assert.ok(!has('component-panels'), `${failure}: the sections are drawn beside the file card's own notice`);
        assert.equal(buttons('common.retry').length, failure === 'still on its way' ? 0 : 1, failure);
      } finally {
        await cleanup();
      }
    }
    serve();
    try {
      await mount(Component, { onBack() {} });
      assert.ok(has('component-panels'), 'the sections are missing although the records are known');
    } finally {
      await cleanup();
    }
  });
}

test('the file list and the component records of one page are one request and one answer', async () => {
  const { useComponentConfigFiles, useManagedServices } = await import(managedURL);
  const Both = () => {
    const records = useManagedServices();
    const { files } = useComponentConfigFiles('postgresql');
    const other = useComponentConfigFiles('mariadb');
    return React.createElement('p', null,
      records.remote.state, ' ', files.state === 'known' ? files.value.join(',') : files.state, ' ',
      other.files.state === 'known' ? other.files.value.join(',') : other.files.state);
  };
  serve();
  try {
    await mount(Both);
    assert.equal(reads('/api/v1/managed-services'), 1, 'two readers of the cached scan made more than one request');
    assert.ok(has('/etc/postgresql/17/main/pg_hba.conf') && has('/etc/mysql/mariadb.conf.d/50-server.cnf'));
  } finally {
    await cleanup();
  }
});

// --- Every read of a screen withheld at once ------------------------------------
const mounts = new Map();
for (const row of table) {
  if (!mounts.has(row.Component)) mounts.set(row.Component, { ...row, negative: [] });
  mounts.get(row.Component).negative.push(...row.negative);
}
for (const row of mounts.values()) {
  for (const failure of ['still on its way', 'dropped connection']) {
    test(`${row.screen.split(' (')[0]}: every read ${failure} → no negative text, no enabled save or remove`, async () => {
      serve(Object.fromEntries(Object.keys(healthy).map((path) => [path, failures[failure]])));
      try {
        await mount(row.Component, row.props);
        for (const key of new Set(row.negative)) assert.ok(!has(key), `${key} is drawn although nothing is known`);
        assert.deepEqual(enabledDangerousControls(), [], 'a saving or removing control is enabled');
        assert.deepEqual(writesMade(), []);
      } finally {
        await cleanup();
      }
    });
  }
}

// --- The editors, once the file is known ------------------------------------------

const saveButton = () => buttons('dbconf.save')[0];
const valueOf = (name) => tree.root.findAll((node) => node.type === 'input' && node.props['aria-label'] === `dbconf.valueOf{"name":"${name}"}`)[0];

test('postgresql.conf: Save sends the file that was read with only the changed line replaced, and its version', async () => {
  serve({}, { 'POST /api/v1/config': json({ success: true, version: 'cf1-next', applied: 'reloaded', daemon_check: 'accepted', restart_required: ['shared_buffers'], backup: '/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-20261009T120000Z' }) });
  try {
    await mount(PostgreSQLSettings, pgPath);
    assert.equal(saveButton().props.disabled, true, 'Save is on although nothing changed');
    await type(valueOf('shared_buffers'), '256MB');
    assert.equal(saveButton().props.disabled, false);
    await press(saveButton());
    const [write] = writesMade();
    assert.equal(write.url, '/api/v1/config');
    assert.deepEqual(Object.keys(write.body).sort(), ['content', 'path', 'version']);
    assert.equal(write.body.path, pgPath.configPath);
    assert.equal(write.body.version, 'cf1-postgresql.conf');
    assert.equal(write.body.content, pgConf.replace('   shared_buffers=128MB # tuned by hand', '   shared_buffers=256MB # tuned by hand'));
    // What the server did is said, with what still waits for a restart.
    assert.ok(has('dbconf.saved.reloaded') && has('dbconf.saved.waitsForRestart') && has('shared_buffers') && has('celikpanel-backup-20261009T120000Z'));
    assert.equal(reads('/api/v1/config'), 2, 'the file was not read again after the save');
  } finally {
    await cleanup();
  }
});

test('postgresql.conf: a file that changed on the server keeps what was typed, turns Save off and offers the reload', async () => {
  serve({}, { 'POST /api/v1/config': json({ error: 'x', code: 'SETTINGS_CHANGED', reason: 'config_file' }, 409) });
  try {
    await mount(PostgreSQLSettings, pgPath);
    await type(valueOf('max_connections'), '300');
    await press(saveButton());
    assert.ok(has('dbconf.stale'));
    assert.equal(valueOf('max_connections').props.value, '300', 'what was typed was lost');
    assert.equal(saveButton().props.disabled, true);
    assert.equal(valueOf('max_connections').props.disabled, true, 'a stale form can still be edited into a second refused save');
    const before = reads('/api/v1/config');
    await press(buttons('dbconf.reload')[0]);
    assert.equal(reads('/api/v1/config'), before + 1);
    assert.ok(!has('dbconf.stale'));
    assert.equal(writesMade().length, 1, 'the reload saved again');
  } finally {
    await cleanup();
  }
});

test('postgresql.conf: what PostgreSQL said is shown next to the setting it named, and what was typed stays', async () => {
  const refusal = { error: 'x', code: 'CONFIG_INVALID', reason: 'daemon', vars: { detail: 'invalid value for parameter "max_connections": "lots"', name: 'max_connections' } };
  serve({}, { 'POST /api/v1/config': json(refusal, 422) });
  try {
    await mount(PostgreSQLSettings, pgPath);
    await type(valueOf('max_connections'), 'lots');
    await press(saveButton());
    const field = valueOf('max_connections');
    assert.equal(field.props.value, 'lots');
    assert.equal(field.props['aria-invalid'], true);
    const message = tree.root.findAll((node) => typeof node.type === 'string' && node.props.id === field.props['aria-describedby'])[0];
    assert.ok(message, 'the message is not tied to the field');
    assert.match(says(message), /invalid value for parameter/);
    assert.ok(has('dbconf.refused.daemon'));
    assert.equal(saveButton().props.disabled, false, 'the correction cannot be saved');
    // Correcting the field takes the refusal away.
    await type(valueOf('max_connections'), '200');
    assert.ok(!has('dbconf.refused.daemon'));
  } finally {
    await cleanup();
  }
});

test('postgresql.conf: a reload that failed is a verified failure with the service’s line; an answer that never came is unknown', async () => {
  serve({}, { 'POST /api/v1/config': json({ error: 'x', code: 'CONFIG_RELOAD_FAILED', reason: 'restored', vars: { detail: 'Error: could not reload' } }, 502) });
  try {
    await mount(PostgreSQLSettings, pgPath);
    await type(valueOf('max_connections'), '300');
    await press(saveButton());
    assert.ok(has('dbconf.reloadFailed.restored') && has('Error: could not reload'));
    assert.equal(valueOf('max_connections').props.value, '300');
  } finally {
    await cleanup();
  }
  serve({}, { 'POST /api/v1/config': () => { throw new TypeError('fetch failed'); } });
  try {
    await mount(PostgreSQLSettings, pgPath);
    await type(valueOf('max_connections'), '300');
    await press(saveButton());
    assert.ok(has('dbconf.saveUnknown'), 'a lost answer is not said to be unknown');
    assert.ok(!has('dbconf.saved.'), 'a lost answer is shown as saved');
    assert.equal(buttons('dbconf.reload').length, 1);
  } finally {
    await cleanup();
  }
});

test('a MariaDB option file: only the changed option is rewritten; the save says the change waits for a restart', async () => {
  serve({}, { 'POST /api/v1/config': json({ success: true, version: 'cf1-next', applied: 'restart_required', daemon_check: 'accepted', restart_required: [] }) });
  try {
    await mount(MariaDBSettings, myPath);
    await type(valueOf('max_connections'), '300');
    await press(saveButton());
    assert.equal(writesMade()[0].body.content, myConf.replace('max_connections=150   # raised for the shop', 'max_connections=300   # raised for the shop'));
    assert.ok(has('dbconf.saved.restartRequired'));
  } finally {
    await cleanup();
  }
});

test('pg_hba.conf: a removed rule and a new rule are the only lines that change; a lockout refusal is said in words', async () => {
  serve({}, { 'POST /api/v1/config': json({ error: 'x', code: 'CONFIG_INVALID', reason: 'lockout', vars: { name: 'denied' } }, 422) });
  try {
    await mount(PostgreSQLAccessRules, hbaPath);
    // The rule with options and the include are shown as written, not as fields.
    assert.ok(has('ldap ldapserver=x') && has('include_dir hba.d') && has('dbconf.hba.asWritten'));
    await press(buttons('dbconf.hba.remove{"n":3}')[0]);
    await press(buttons('dbconf.hba.add')[0]);
    assert.equal(saveButton().props.disabled, true, 'a new rule with empty fields can be saved');
    assert.ok(has('dbconf.hba.incomplete'));
    const textInputs = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'text' && !node.props.disabled);
    const blank = () => textInputs().filter((node) => node.props.value === '');
    await type(blank()[0], 'shop');
    await type(blank()[0], 'shop');
    await type(blank()[0], '192.0.2.0/24');
    assert.equal(saveButton().props.disabled, false);
    await press(saveButton());
    const sent = writesMade()[0].body;
    assert.equal(sent.version, 'cf1-pg_hba.conf');
    assert.equal(sent.content, hbaConf
      .replace('host    all             all             127.0.0.1/32            scram-sha-256\n', '')
      .replace(/$/, 'host    shop            shop            192.0.2.0/24            scram-sha-256\n'));
    assert.ok(has('dbconf.refused.lockout'));
  } finally {
    await cleanup();
  }
});

test('pg_hba.conf: a refusal that names a line is shown next to the rule on that line', async () => {
  serve({}, { 'POST /api/v1/config': json({ error: 'x', code: 'CONFIG_INVALID', reason: 'daemon', vars: { detail: 'hostssl record cannot match because SSL is disabled', line: '9' } }, 422) });
  try {
    await mount(PostgreSQLAccessRules, hbaPath);
    await press(buttons('dbconf.hba.add')[0]);
    const blank = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'text' && node.props.value === '');
    await type(blank()[0], 'shop');
    await type(blank()[0], 'shop');
    await type(blank()[0], '192.0.2.0/24');
    await press(saveButton());
    const message = tree.root.findAll((node) => typeof node.type === 'string' && node.props.id === 'hba-new-0-refusal')[0];
    assert.ok(message, 'the server’s line is not next to the new rule');
    assert.match(says(message), /SSL is disabled/);
  } finally {
    await cleanup();
  }
});

// --- The catch-all address -----------------------------------------------------------

const catchAllField = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'email')[0];

test('catch-all: the current address is shown before it can be changed; a change carries its version', async () => {
  let release;
  serve({ '/api/v1/domains/1/mail/catch-all': () => new Promise((resolve) => { release = () => resolve(Response.json({ enabled: true, destination: 'owner@example.net', version: 'ca1-a' })); }) },
    { 'PUT /api/v1/domains/1/mail/catch-all': json({ enabled: true, destination: 'new@example.org', version: 'ca1-b' }) });
  try {
    await mount(MailSettingsPanel, domain);
    assert.equal(catchAllField().props.disabled, true, 'the field can be typed into before the address is read');
    assert.equal(catchAllField().props.value, '');
    assert.ok(buttons('mail.catchAll.disable').every((node) => node.props.disabled));
    await act(async () => { release(); await settled(); await settled(); });
    assert.ok(has('mail.catchAll.active') && has('owner@example.net'), 'the current address is not shown');
    assert.equal(catchAllField().props.value, 'owner@example.net');
    // The one that was hidden after a failed read: turning it off.
    assert.equal(buttons('mail.catchAll.disable')[0].props.disabled, false);
    assert.equal(buttons('mail.catchAll.update')[0].props.disabled, true, 'saving the address that is already set is offered');
    await type(catchAllField(), 'new@example.org');
    await press(buttons('mail.catchAll.update')[0]);
    assert.deepEqual(writesMade()[0].body, { destination: 'new@example.org', version: 'ca1-a' });
  } finally {
    await cleanup();
  }
});

test('catch-all: an address that changed on the server keeps what was typed and offers the reload; turning off carries the version', async () => {
  serve({}, { 'PUT /api/v1/domains/1/mail/catch-all': json({ error: 'x', code: 'SETTINGS_CHANGED', reason: 'mail_catch_all' }, 409) });
  try {
    await mount(MailSettingsPanel, domain);
    await type(catchAllField(), 'typed@example.org');
    await press(buttons('mail.catchAll.update')[0]);
    assert.ok(has('mail.catchAll.stale'));
    assert.equal(catchAllField().props.value, 'typed@example.org');
    assert.ok(buttons('mail.catchAll.update')[0].props.disabled && buttons('mail.catchAll.disable')[0].props.disabled);
    await press(buttons('mail.catchAll.reload')[0]);
    assert.ok(!has('mail.catchAll.stale'));
    await press(buttons('mail.catchAll.disable')[0]);
    const off = writesMade().at(-1);
    assert.equal(off.method, 'DELETE');
    assert.equal(off.url, '/api/v1/domains/1/mail/catch-all?version=ca1-a');
  } finally {
    await cleanup();
  }
});

// --- Mailboxes and the queue ------------------------------------------------------------

test('mailboxes: the number beside a tab is a count only when the list is known', async () => {
  serve({ '/api/v1/domains/1/mail/forwardings': failures['dropped connection'] });
  try {
    await mount(DomainMailManager, domain);
    const counts = tree.root.findAll((node) => node.type === 'span' && /rounded-full bg-surface-2/.test(node.props.className ?? '')).map((node) => node.props.children);
    assert.deepEqual(counts, [1, '–'], 'a list that could not be read is counted as 0');
  } finally {
    await cleanup();
  }
});

test('mailboxes: usage that could not be read is said in the column, with the mailboxes still listed', async () => {
  serve({ '/api/v1/domains/1/mail/quota': failures['refused by the server'] });
  try {
    await mount(DomainMailManager, domain);
    assert.ok(has('info@example.com') && has('mail.quota.unknown') && has('mail.usageUnknown'));
    assert.equal(buttons('common.retry').length, 1);
  } finally {
    await cleanup();
  }
});

test('mail queue: an action the server did not confirm is not announced as done, and the queue is read again', async () => {
  serve({}, { 'POST /api/v1/postfix/queue': json({ error: 'mail queue action was not confirmed by the agent' }, 502) });
  try {
    await mount(PostfixManagement, { onBack() {} });
    const before = reads('/api/v1/postfix/queue');
    await press(buttons('postfix.flush')[0]);
    assert.deepEqual(globalThis.currentTest.toasts.map(([kind]) => kind), ['error']);
    assert.equal(reads('/api/v1/postfix/queue'), before + 1);
  } finally {
    await cleanup();
  }
});

test('mail policy: written but not reloaded stays on screen as a failure, above the values that were saved', async () => {
  let version = 'mp1-a';
  serve({ '/api/v1/mail/policy': () => Response.json({ message_size_mb: version === 'mp1-a' ? 9 : 50, dnsbl_zones: [], outbound_rate_limit: 0, version }) },
    { 'PUT /api/v1/mail/policy': () => { version = 'mp1-b'; return Response.json({ error: 'x', code: 'MAIL_POLICY_NOT_RELOADED', mutation_applied: true, partial_success: true, vars: { detail: 'Job for postfix.service failed' } }, { status: 502 }); } });
  try {
    await mount(PostfixManagement, { onBack() {} });
    const size = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'number')[0];
    await type(size(), '50');
    await press(buttons('mailpolicy.save')[0]);
    assert.ok(has('err.MAIL_POLICY_NOT_RELOADED') && has('Job for postfix.service failed'));
    assert.equal(size().props.value, 50, 'the form does not show what main.cf holds now');
    assert.deepEqual(globalThis.currentTest.toasts.filter(([kind]) => kind === 'success'), [], 'a save that was not loaded is announced as applied');
  } finally {
    await cleanup();
  }
});

// --- The ratchet -----------------------------------------------------------------------

test('the screens of this batch are off the ratchet allow-list for good', () => {
  const actual = scanTree(fileURLToPath(new URL('../', import.meta.url)));
  const allowList = readAllowList();
  for (const name of [...screenFiles, 'CurrentSettings']) {
    const path = `src/components/${name}.tsx`;
    assert.equal(actual[path], undefined, `${path} reads the old way again: ${JSON.stringify(actual[path])}`);
    assert.equal(allowList.files[path], undefined, `${path} is back on the allow-list`);
  }
  for (const name of ['configFile', 'dbConfigText', 'managedServices', 'mailSetup']) {
    assert.equal(actual[`src/lib/${name}.ts`], undefined);
  }
});

// --- Corrections from the first native measurement of settings writes (10 Oct 2026) ---

test('mail queue: an unreadable queue shows what Postfix said and names no cause of its own', async () => {
  serve({ '/api/v1/postfix/queue': () => Response.json({ error: 'x', code: 'MAIL_QUEUE_UNREADABLE', reason: 'postfix_config', vars: { detail: 'bad numerical configuration: default_process_limit = 200 # raised' } }, { status: 502 }) });
  try {
    await mount(PostfixManagement, { onBack() {} });
    assert.ok(has('postfix.queue.unreadable.postfix_config'), 'the sentence for the cause the server verified is missing');
    assert.ok(has('postfix.queue.said') && has('bad numerical configuration: default_process_limit'), 'the line Postfix printed is not shown');
    assert.ok(!has('postfix.queue.unknown'), 'the sentence that tells the owner to check that Postfix is running is back');
    assert.ok(!has('postfix.empty'));
  } finally {
    await cleanup();
  }
});

test('mail policy: the not-reloaded answer carries the saved values, so they are shown without a second read', async () => {
  for (const [code, reason, label] of [
    ['MAIL_POLICY_NOT_RELOADED', 'check', 'mailpolicy.postfixSaid'],
    ['MAIL_POLICY_RELOAD_UNKNOWN', undefined, 'mailpolicy.observed'],
  ]) {
    serve({ '/api/v1/mail/policy': () => Response.json({ message_size_mb: 9, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-a' }) },
      { 'PUT /api/v1/mail/policy': () => Response.json({
        error: 'x', code, reason, mutation_applied: true, partial_success: true, vars: { detail: 'postfix: fatal: bad numerical configuration' },
        policy: { message_size_mb: 50, dnsbl_zones: [], outbound_rate_limit: 46, version: 'mp1-b' },
      }, { status: 502 }) });
    try {
      await mount(PostfixManagement, { onBack() {} });
      const size = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'number')[0];
      const before = reads('/api/v1/mail/policy');
      await type(size(), '50');
      await press(buttons('mailpolicy.save')[0]);
      assert.ok(has('err.' + code) && has(label) && has('postfix: fatal: bad numerical configuration'), code + ' is not shown with its line');
      // A verified failure is drawn as one; an outcome that could not be
      // established is not (its sentence says "this is not a verified
      // failure"): it stands on the attention surface, never the failure one.
      const box = tree.root.findAll((node) => node.props['data-policy-outcome'] !== undefined);
      assert.equal(box.length, 1);
      const surfaces = box[0].findAll((node) => typeof node.type === 'string' && String(node.props.className ?? '').split(' ').includes('border')).map((node) => node.props.className);
      assert.equal(surfaces.length, 1, 'the sentence stands on ' + surfaces.length + ' surfaces');
      if (code === 'MAIL_POLICY_RELOAD_UNKNOWN') {
        assert.equal(box[0].props['data-policy-outcome'], 'unknown');
        assert.match(surfaces[0], /warning-mark/, 'an unknown outcome is not on the attention surface');
        assert.doesNotMatch(surfaces[0], /danger/, 'an unknown outcome is drawn as a failure');
      } else {
        assert.equal(box[0].props['data-policy-outcome'], 'not-reloaded');
        assert.match(surfaces[0], /danger/, 'a verified failure is not drawn as one');
      }
      assert.equal(size().props.value, 50, 'the form does not show what main.cf holds now');
      assert.equal(reads('/api/v1/mail/policy'), before, 'the saved values were read a second time although the answer carried them');
      assert.deepEqual(globalThis.currentTest.toasts.filter(([kind]) => kind === 'success'), [], 'a save Postfix did not take is announced as applied');
    } finally {
      await cleanup();
    }
  }
});

test('mail policy: "applied" is said only for a verified reload; a stopped Postfix and an unchanged policy say so', async () => {
  for (const [applied, key] of [['reloaded', 'mailpolicy.saved'], ['not_running', 'mailpolicy.saved.notRunning'], ['unchanged', 'mailpolicy.saved.unchanged'], ['unchanged_reloaded', 'mailpolicy.saved.unchangedReloaded'], [undefined, 'mailpolicy.saved']]) {
    serve({ '/api/v1/mail/policy': () => Response.json({ message_size_mb: 9, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-a' }) },
      { 'PUT /api/v1/mail/policy': () => Response.json({ success: true, applied, policy: { message_size_mb: 50, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-b' } }) });
    try {
      await mount(PostfixManagement, { onBack() {} });
      const size = () => tree.root.findAll((node) => node.type === 'input' && node.props.type === 'number')[0];
      await type(size(), '50');
      await press(buttons('mailpolicy.save')[0]);
      assert.deepEqual(globalThis.currentTest.toasts, [['success', key]], 'applied=' + applied);
    } finally {
      await cleanup();
    }
  }
});

test('configuration editor: a failed reload says what the server verified, and names a copy only when the file is not back', async () => {
  for (const [reason, shownName] of [['restored_unit_reload_failed', false], ['restored_running_unknown', false], ['not_restored', true]]) {
    serve({}, { 'POST /api/v1/config': json({ error: 'x', code: 'CONFIG_RELOAD_FAILED', reason, vars: { detail: 'Failed to reload pooler.service', unit: 'postgresql@17-main', name: '/etc/postgresql/17/main/postgresql.conf.celikpanel-backup-1' } }, 502) });
    try {
      await mount(PostgreSQLSettings, pgPath);
      await type(valueOf('max_connections'), '300');
      await press(saveButton());
      const key = reason === 'not_restored' ? 'dbconf.reloadFailed.notRestored' : 'dbconf.reloadFailed.' + reason;
      assert.ok(has(key) && has('Failed to reload pooler.service') && has('postgresql@17-main'), reason + ' is not shown with its own sentence');
      assert.equal(reason !== 'not_restored' && has('dbconf.reloadFailed.notRestored'), false, reason + ' is shown as a file that could not be put back');
      assert.equal(text().split('celikpanel-backup-1').length > 2, shownName, reason + ': the line that names a kept copy');
      if (reason !== 'not_restored') assert.equal(valueOf('max_connections').props.value, '300');
    } finally {
      await cleanup();
    }
  }
});
