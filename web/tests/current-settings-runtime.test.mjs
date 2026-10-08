import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import test from 'node:test';
import React from 'react';
import Renderer, { act } from 'react-test-renderer';
import ts from 'typescript';
import { englishCatalogue, turkishCatalogue } from './locale-catalogue.mjs';
import { sharedLayer } from './fixtures/shared-layer.mjs';

// The rule these three screens follow (8 Oct 2026): nothing is shown as a
// setting, an empty list or an editable form until the server's current state
// is known, and no save is built from defaults. Before it, one failed read on
// a healthy server left the mail policy, a domain's backup schedule and a
// domain's scheduled tasks one click away from being replaced.

const require = createRequire(import.meta.url);
const dataModule = (text) => 'data:text/javascript;base64,' + Buffer.from(text).toString('base64');
const reactURL = pathToFileURL(require.resolve('react')).href;
const compilerOptions = { jsx: ts.JsxEmit.React, module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 };
const source = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

const icons = ['Mail', 'Activity', 'Trash2', 'RefreshCw', 'RotateCw', 'Archive', 'Download', 'RotateCcw', 'HardDrive',
  'Database', 'Clock', 'Info', 'Plus', 'Edit2', 'Play', 'Pause', 'Save', 'X', 'AlertTriangle'];
const stub = dataModule(`
  import React from '${reactURL}';
  ${icons.map((name) => `export const ${name} = () => null;`).join('\n')}
  export const useI18n = () => ({ t: (key) => key });
  export const useNavigate = () => () => true;
  export const showToast = (...args) => globalThis.currentTest.toasts.push(args);
  export const ServiceShell = (props) => React.createElement('main', null, props.children);
`);
// Since 9 Oct 2026 these three screens stand on the shared remote-state layer,
// so they are mounted with the real one: lib/remote.ts for the read and the
// real Checking and CouldNotCheck of components/ui.tsx for what is drawn.
const shared = sharedLayer(stub);
const currentURL = shared.compile('components/CurrentSettings.tsx');
const load = async (name) => (await import(shared.compile(`components/${name}.tsx`, { '/CurrentSettings': currentURL })))[name];
const PostfixManagement = await load('PostfixManagement');
const DomainBackupManager = await load('DomainBackupManager');
const DomainCronManager = await load('DomainCronManager');

const originalFetch = globalThis.fetch;
let tree, calls;
// routes: [method, url fragment, answer]; the first match answers. An answer
// is a function (called per request), 'pending' or 'offline'.
function fixture(routes) {
  calls = [];
  globalThis.currentTest = { toasts: [] };
  globalThis.confirm = () => true;
  globalThis.fetch = (url, options = {}) => {
    const method = options.method || 'GET';
    const body = options.body ? JSON.parse(options.body) : undefined;
    calls.push({ method, url, body });
    const route = routes.find(([m, fragment]) => m === method && url.includes(fragment));
    if (!route) return Promise.resolve(Response.json([]));
    if (route[2] === 'pending') return new Promise(() => {});
    if (route[2] === 'offline') return Promise.reject(new TypeError('fetch failed'));
    return Promise.resolve(route[2]({ method, url, body }));
  };
}
const refusal = (status, code, error = 'server sentence') => () => Response.json({ error, code }, { status });
// The screens start their reads and reloads without awaiting them, so every
// step also waits one turn of the event loop for those answers to land.
const settled = () => new Promise((resolve) => setTimeout(resolve, 0));
async function mount(Component, props = {}) {
  await act(async () => {
    tree = Renderer.create(React.createElement(Component, props));
    await settled();
  });
}
async function cleanup() {
  if (tree) await act(async () => tree.unmount());
  tree = undefined;
  globalThis.fetch = originalFetch;
  delete globalThis.currentTest;
}
const text = () => JSON.stringify(tree.toJSON());
const buttons = (label) => tree.root.findAllByType('button').filter((node) => [].concat(node.props.children).includes(label));
const button = (label) => buttons(label)[0];
const press = async (node) => {
  await act(async () => {
    await node.props.onClick({});
    await settled();
  });
};
const click = (label) => press(button(label));
const writes = () => calls.filter((call) => call.method !== 'GET');
const scenario = (name, routes, Component, props, body) => test(name, async () => {
  fixture(routes);
  try {
    await mount(Component, props);
    await body();
  } finally {
    await cleanup();
  }
});

// --- Server mail policy ---

const stockPolicy = { message_size_mb: 9, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-a' };
const policyInputs = () => tree.root.findAllByType('input');

for (const [name, answer] of [
  ['still loading', 'pending'],
  ['refused by the server', refusal(502, 'CURRENT_SETTINGS_UNREADABLE')],
  ['unreachable', 'offline'],
  ['an old Agent answer without a body', () => new Response('', { status: 500 })],
]) {
  scenario(`mail policy ${name}: no form, no defaults, no Save`, [['GET', '/mail/policy', answer]], PostfixManagement, { onBack() {} }, async () => {
    assert.equal(policyInputs().length, 0, 'no policy field may be shown before the policy is known');
    assert.equal(buttons('mailpolicy.save').length, 0);
    assert.doesNotMatch(text(), /mailpolicy\.(dnsbl|rate|maxSize)"/);
    if (answer === 'pending') {
      assert.match(text(), /current\.checking/);
      assert.doesNotMatch(text(), /mailpolicy\.unknown/);
    } else {
      assert.match(text(), /mailpolicy\.unknown/);
      assert.equal(buttons('common.retry').length, 1);
    }
    assert.equal(writes().length, 0);
  });
}

test('mail policy: Retry reads again and only then shows the server values', async () => {
  let attempt = 0;
  fixture([['GET', '/mail/policy', () => (attempt++ === 0
    ? Response.json({ error: 'x', code: 'CURRENT_SETTINGS_UNREADABLE' }, { status: 502 })
    : Response.json({ ...stockPolicy, message_size_mb: 40, dnsbl_zones: ['zen.spamhaus.org'], outbound_rate_limit: 12 }))]]);
  try {
    await mount(PostfixManagement, { onBack() {} });
    await click('common.retry');
    assert.doesNotMatch(text(), /mailpolicy\.unknown/);
    const values = policyInputs().map((node) => node.props.value ?? node.props.checked);
    assert.deepEqual(values, [40, 'zen.spamhaus.org', true, true, 12]);
  } finally {
    await cleanup();
  }
});

scenario('mail policy: a save carries the version and the loaded values it did not change',
  [['GET', '/mail/policy', () => Response.json(stockPolicy)],
    ['PUT', '/mail/policy', ({ body }) => Response.json({ success: true, policy: { ...stockPolicy, ...body, version: 'mp1-b' } })]],
  PostfixManagement, { onBack() {} }, async () => {
    await click('mailpolicy.save');
    assert.deepEqual(writes()[0].body, { message_size_mb: 9, dnsbl_zones: [], outbound_rate_limit: 0, version: 'mp1-a' });
    await click('mailpolicy.save');
    assert.equal(writes()[1].body.version, 'mp1-b', 'the next save uses the version the server answered');
  });

for (const code of ['SETTINGS_CHANGED', 'SETTINGS_VERSION_REQUIRED']) {
  test(`mail policy: ${code} shows the reload guidance and keeps what was typed`, async () => {
    let served = stockPolicy;
    fixture([['GET', '/mail/policy', () => Response.json(served)], ['PUT', '/mail/policy', refusal(409, code)]]);
    try {
      await mount(PostfixManagement, { onBack() {} });
      await act(async () => policyInputs()[0].props.onChange({ target: { value: '50' } }));
      await click('mailpolicy.save');
      assert.match(text(), /mailpolicy\.stale/);
      assert.equal(policyInputs()[0].props.value, 50, 'the typed size stays on screen');
      assert.equal(button('mailpolicy.save').props.disabled, true, 'a stale form cannot be saved again');
      assert.deepEqual(globalThis.currentTest.toasts, [], 'the guidance is on screen, not a passing toast');
      served = { ...stockPolicy, message_size_mb: 20, version: 'mp1-c' };
      await click('current.reload');
      assert.doesNotMatch(text(), /mailpolicy\.stale/);
      assert.equal(policyInputs()[0].props.value, 20);
      assert.equal(button('mailpolicy.save').props.disabled, false);
    } finally {
      await cleanup();
    }
  });
}

scenario('mail policy: restrictions CelikPanel will not rewrite withdraw the DNSBL controls and are never sent changed',
  [['GET', '/mail/policy', () => Response.json({ ...stockPolicy, dnsbl_zones: ['bl.example.org'], dnsbl_locked: 'no_baseline' })],
    ['PUT', '/mail/policy', ({ body }) => Response.json({ success: true, policy: { ...body, dnsbl_locked: 'no_baseline', version: 'mp1-b' } })]],
  PostfixManagement, { onBack() {} }, async () => {
    assert.match(text(), /mailpolicy\.dnsblLocked\.other/, 'the reason is on screen (the stub has no words for the reason)');
    assert.match(text(), /mailpolicy\.dnsblLockedAction/);
    assert.doesNotMatch(text(), /mailpolicy\.zones|mailpolicy\.dnsbl"/);
    await click('mailpolicy.save');
    assert.deepEqual(writes()[0].body.dnsbl_zones, ['bl.example.org']);
  });

scenario('mail policy: another refusal is the error toast and the form stays',
  [['GET', '/mail/policy', () => Response.json(stockPolicy)], ['PUT', '/mail/policy', refusal(400, 'MAIL_POLICY_INVALID', 'zone sentence')]],
  PostfixManagement, { onBack() {} }, async () => {
    await click('mailpolicy.save');
    assert.deepEqual(globalThis.currentTest.toasts, [['error', 'zone sentence']]);
    assert.doesNotMatch(text(), /mailpolicy\.stale/);
    assert.equal(button('mailpolicy.save').props.disabled, false);
  });

// --- Automatic backup schedule ---

const domain = { domainId: 7, domainName: 'example.com' };
const weeklyFull = { enabled: true, frequency: 'weekly', backup_type: 'full', retention: 30, last_status: 'success', last_run: '2026-10-07T03:00:00Z', version: 'bs1-a' };
const scheduleFields = () => [...tree.root.findAllByType('select'), ...tree.root.findAllByType('input')]
  .filter((node) => node.props.id !== 'backup-database');
const emptyLists = [['GET', '/backups/schedule', null], ['GET', '/backups', () => Response.json({ backups: [] })], ['GET', '/databases', () => Response.json({ databases: [] })]];
const withSchedule = (answer, ...more) => [...more, ['GET', '/backups/schedule', answer], ...emptyLists.slice(1)];

for (const [name, answer] of [
  ['still loading', 'pending'],
  ['refused by the server', refusal(500, 'INTERNAL')],
  ['unreachable', 'offline'],
]) {
  scenario(`backup schedule ${name}: no "off / daily / files / 7" form and no Turn on`, withSchedule(answer), DomainBackupManager, domain, async () => {
    assert.equal(scheduleFields().length, 0, 'no schedule field may be shown before the schedule is known');
    assert.equal(buttons('backup.auto.enable').length + buttons('backup.auto.update').length + buttons('backup.auto.turnOff').length, 0);
    assert.doesNotMatch(text(), /backup\.auto\.on"/);
    if (answer === 'pending') assert.match(text(), /current\.checking/);
    else {
      assert.match(text(), /backup\.auto\.unknown/);
      assert.equal(buttons('common.retry').length, 1);
    }
    assert.equal(writes().length, 0);
  });
}

scenario('backup schedule: a known schedule fills the form and both writes carry its version',
  withSchedule(() => Response.json(weeklyFull),
    ['PUT', '/backups/schedule', () => Response.json({ success: true, version: 'bs1-b' })],
    ['DELETE', '/backups/schedule', () => Response.json({ success: true, version: 'bs1-none' })]),
  DomainBackupManager, domain, async () => {
    assert.deepEqual(scheduleFields().map((node) => node.props.value), ['weekly', 'full', 30]);
    await click('backup.auto.update');
    assert.deepEqual(writes()[0].body, { frequency: 'weekly', backup_type: 'full', retention: 30, version: 'bs1-a' });
    await click('backup.auto.turnOff');
    assert.match(writes()[1].url, /\/backups\/schedule\?version=bs1-b$/, 'turn off carries the version the save answered');
    assert.equal(buttons('backup.auto.enable').length, 1);
    assert.equal(buttons('backup.auto.turnOff').length, 0);
  });

scenario('backup schedule: no schedule yet is a known state that offers Turn on with its version',
  withSchedule(() => Response.json({ enabled: false, version: 'bs1-none' }),
    ['PUT', '/backups/schedule', () => Response.json({ success: true, version: 'bs1-b' })]),
  DomainBackupManager, domain, async () => {
    await click('backup.auto.enable');
    assert.deepEqual(writes()[0].body, { frequency: 'daily', backup_type: 'files', retention: 7, version: 'bs1-none' });
  });

test('backup schedule: a stale save shows the reload guidance and keeps the chosen values', async () => {
  let served = weeklyFull;
  fixture(withSchedule(() => Response.json(served), ['PUT', '/backups/schedule', refusal(409, 'SETTINGS_CHANGED')]));
  try {
    await mount(DomainBackupManager, domain);
    await act(async () => scheduleFields()[2].props.onChange({ target: { value: '14' } }));
    await click('backup.auto.update');
    assert.match(text(), /backup\.auto\.stale/);
    assert.deepEqual(scheduleFields().map((node) => node.props.value), ['weekly', 'full', 14]);
    assert.equal(button('backup.auto.update').props.disabled, true);
    assert.equal(button('backup.auto.turnOff').props.disabled, true);
    assert.deepEqual(globalThis.currentTest.toasts, []);
    served = { ...weeklyFull, frequency: 'daily', retention: 5, version: 'bs1-c' };
    await click('current.reload');
    assert.doesNotMatch(text(), /backup\.auto\.stale/);
    assert.deepEqual(scheduleFields().map((node) => node.props.value), ['daily', 'full', 5]);
  } finally {
    await cleanup();
  }
});

// --- Scheduled tasks ---

const job = { id: '0000abcd', schedule: '0 3 * * *', command: '/usr/bin/true', enabled: true, comment: '' };
const cronInputs = () => tree.root.findAllByType('input');

for (const [name, answer] of [
  ['still loading', 'pending'],
  ['unreadable on the server', refusal(502, 'CURRENT_SETTINGS_UNREADABLE')],
  ['an unclassified failure', refusal(500, 'INTERNAL')],
  ['unreachable', 'offline'],
]) {
  scenario(`scheduled tasks ${name}: no "No scheduled tasks" and no Add`, [['GET', '/cron', answer]], DomainCronManager, domain, async () => {
    assert.doesNotMatch(text(), /cron\.empty/);
    assert.equal(buttons('cron.add').length, 0);
    assert.equal(cronInputs().length, 0);
    if (answer === 'pending') assert.match(text(), /current\.checking/);
    else {
      assert.match(text(), /cron\.unknown/);
      assert.equal(buttons('common.retry').length, 1);
    }
    assert.equal(writes().length, 0);
  });
}

scenario('scheduled tasks: cron missing keeps its own guidance, not the could-not-load notice',
  [['GET', '/cron', refusal(409, 'CRON_NOT_INSTALLED', 'install cron sentence')]], DomainCronManager, domain, async () => {
    assert.match(text(), /install cron sentence/);
    assert.doesNotMatch(text(), /cron\.unknown|cron\.empty/);
    assert.equal(buttons('cron.add').length, 0);
  });

scenario('scheduled tasks: a read list with no tasks is the empty state and offers Add',
  [['GET', '/cron', () => Response.json({ jobs: [], version: 'ct1-a' })]], DomainCronManager, domain, async () => {
    assert.match(text(), /cron\.empty/);
    assert.equal(buttons('cron.add').length, 1);
  });

test('scheduled tasks: every change carries the list version; a stale one keeps the typed task', async () => {
  let version = 'ct1-a';
  let refuse = true;
  fixture([
    ['GET', '/cron', () => Response.json({ jobs: [job], version })],
    ['POST', '/cron', (request) => (refuse ? refusal(409, 'SETTINGS_CHANGED')() : Response.json({ success: true }))],
    ['PUT', '/cron', () => Response.json({ success: true })],
    ['DELETE', '/cron', () => Response.json({ success: true })],
  ]);
  try {
    await mount(DomainCronManager, domain);
    await click('cron.add');
    await act(async () => cronInputs()[1].props.onChange({ target: { value: '/usr/local/bin/report' } }));
    await click('cron.save');
    assert.deepEqual(writes()[0].body, { schedule: '0 * * * *', command: '/usr/local/bin/report', comment: '', version: 'ct1-a' });
    assert.match(text(), /cron\.stale/);
    assert.equal(cronInputs()[1].props.value, '/usr/local/bin/report', 'the typed command stays in the form');
    assert.equal(button('cron.save').props.disabled, true);
    assert.deepEqual(globalThis.currentTest.toasts, []);

    version = 'ct1-b';
    refuse = false;
    await click('cron.reload');
    assert.doesNotMatch(text(), /cron\.stale/);
    assert.equal(cronInputs()[1].props.value, '/usr/local/bin/report', 'reloading the list keeps the form');
    await click('cron.save');
    assert.equal(writes()[1].body.version, 'ct1-b');

    const iconButton = (title) => tree.root.findAllByType('button').find((node) => node.props.title === title);
    await press(iconButton('cron.disable'));
    assert.deepEqual(writes()[2].body, { ...job, enabled: false, version: 'ct1-b' });
    await press(iconButton('cron.delete'));
    assert.match(writes()[3].url, /\/cron\?id=0000abcd&version=ct1-b$/);
  } finally {
    await cleanup();
  }
});

scenario('scheduled tasks: a duplicate is refused with the server sentence and the form stays',
  [['GET', '/cron', () => Response.json({ jobs: [job], version: 'ct1-a' })],
    ['POST', '/cron', refusal(409, 'CRON_JOB_DUPLICATE', 'already exists sentence')]], DomainCronManager, domain, async () => {
    await click('cron.add');
    await act(async () => cronInputs()[1].props.onChange({ target: { value: '/usr/bin/true' } }));
    await click('cron.save');
    assert.deepEqual(globalThis.currentTest.toasts, [['error', 'already exists sentence']]);
    assert.doesNotMatch(text(), /cron\.stale/);
    assert.equal(cronInputs()[1].props.value, '/usr/bin/true');
  });

// --- Copy ---

const newKeys = [
  'current.checking', 'current.reload',
  'mailpolicy.unknown', 'mailpolicy.stale', 'mailpolicy.dnsblLockedAction',
  'mailpolicy.dnsblLocked.variable', 'mailpolicy.dnsblLocked.malformed', 'mailpolicy.dnsblLocked.no_baseline',
  'mailpolicy.dnsblLocked.terminal', 'mailpolicy.dnsblLocked.other',
  'backup.auto.unknown', 'backup.auto.stale',
  'cron.unknown', 'cron.stale', 'cron.reload',
  'err.CURRENT_SETTINGS_UNREADABLE', 'err.CRON_JOB_DUPLICATE', 'err.MAIL_POLICY_INVALID', 'err.MAIL_POLICY_INVALID.dnsbl_zone',
];
const value = (catalogue, key) => {
  const match = catalogue.match(new RegExp(`'${key.replace(/\./g, '\\.')}':\\s*'((?:[^'\\\\]|\\\\.)*)',\\n`));
  return match ? match[1] : '';
};

test('the three screens have their reading, could-not-load and stale texts in both languages', () => {
  for (const key of newKeys) {
    assert.ok(value(englishCatalogue, key), 'missing EN text ' + key);
    assert.ok(value(turkishCatalogue, key), 'missing TR text ' + key);
    assert.notEqual(value(englishCatalogue, key), value(turkishCatalogue, key), key + ' is not translated');
  }
  // A could-not-load text says nothing was changed and how to go on; a stale
  // text says nothing was saved and that what was typed is kept.
  for (const key of ['mailpolicy.unknown', 'backup.auto.unknown', 'cron.unknown']) {
    assert.match(value(englishCatalogue, key), /could not be (read|loaded).*Nothing was changed.*Try again/);
    assert.match(value(turkishCatalogue, key), /(okunamadı|yüklenemedi).*Hiçbir şey değiştirilmedi.*Tekrar deneyin/);
  }
  for (const key of ['mailpolicy.stale', 'backup.auto.stale', 'cron.stale']) {
    assert.match(value(englishCatalogue, key), /changed on the server after this (page|list) loaded, so nothing was (saved|changed)/);
    assert.match(value(turkishCatalogue, key), /yüklendikten sonra sunucuda değişti; bu yüzden hiçbir şey (kaydedilmedi|değiştirilmedi)/);
  }
  // The owner's own commands and setting names are the same in both languages.
  for (const literal of ['/etc/postfix/main.cf', 'smtpd_recipient_restrictions', 'reject_rbl_client', 'sudo systemctl reload postfix']) {
    assert.ok(value(englishCatalogue, 'mailpolicy.dnsblLockedAction').includes(literal), 'EN lacks ' + literal);
    assert.ok(value(turkishCatalogue, 'mailpolicy.dnsblLockedAction').includes(literal), 'TR lacks ' + literal);
  }
});

test('the three screens read through the shared current-state helper and never seed a form from defaults', () => {
  const postfix = source('../src/components/PostfixManagement.tsx');
  const backup = source('../src/components/DomainBackupManager.tsx');
  const cron = source('../src/components/DomainCronManager.tsx');
  for (const [name, text] of [['mail policy', postfix], ['backup schedule', backup], ['scheduled tasks', cron]]) {
    assert.match(text, /readCurrent</, name + ' must read its state through readCurrent');
    assert.match(text, /isStaleWrite\(error\)/, name + ' must tell a stale write from other refusals');
    assert.match(text, /<CurrentGate /, name + ' must gate its form or list on the known state');
  }
  // The old loaders: a failed read fell through to the defaults.
  assert.doesNotMatch(postfix, /fetch\('\/api\/v1\/mail\/policy'\)\s*\.then/);
  assert.doesNotMatch(postfix, /if \(!loaded\) return null/);
  assert.doesNotMatch(postfix, /useState\(25\)/);
  // Every write sends the version it was read at.
  assert.match(postfix, /version: policy\.version/);
  assert.match(backup, /version: schedule\.version/);
  assert.match(backup, /\?version=\$\{encodeURIComponent\(schedule\.version\)\}/);
  assert.match(cron, /enabled: editingJob\.enabled, comment, version \}/);
  assert.match(cron, /\{ schedule, command, comment, version \}/);
  assert.match(cron, /enabled: !job\.enabled, version \}/);
  assert.match(cron, /&version=\$\{encodeURIComponent\(version\)\}/);
});
