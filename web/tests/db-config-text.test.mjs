import assert from 'node:assert/strict';
import test from 'node:test';
import { compileSource } from './fixtures/shared-layer.mjs';

// lib/dbConfigText.ts: a save is the file that was read with ONLY the lines the
// person changed replaced (9 Oct 2026; D-022). Everything else an owner wrote
// by hand (comments, includes, unknown directives, blank lines, spacing, line
// endings) must come back byte for byte and in place.
//
// Before this the editors rewrote every line they could parse, gave two lines
// with the same name the same value, and wrote pg_hba.conf from scratch.

const text = await import(compileSource('lib/dbConfigText.ts', (specifier) => {
  throw new Error(`lib/dbConfigText.ts imports ${specifier}; it is meant to stand alone`);
}));
const { parsePostgresConf, applyPostgresConf, parseOptionFile, applyOptionFile, parseHba, applyHba, formatHbaRule, hbaRuleComplete } = text;

const items = (sections) => sections.flatMap((section) => section.items);
const find = (sections, key, nth = 0) => items(sections).filter((item) => item.key === key)[nth];
const edit = (...pairs) => new Map(pairs);

// --- postgresql.conf -----------------------------------------------------------

const pg = [
  '# -----------------------------',
  '# PostgreSQL configuration file',
  '# -----------------------------',
  '#',
  '# This file consists of lines of the form:',
  '#',
  '#   name = value',
  '#',
  '# CONNECTIONS AND AUTHENTICATION',
  '',
  "#listen_addresses = 'localhost'\t\t# what IP address(es) to listen on;",
  '\t\t\t\t\t# comma-separated list of addresses;',
  'max_connections = 100\t\t\t# (change requires restart)',
  '   shared_buffers=128MB # tuned by hand, 2025-03',
  "log_line_prefix = '%m [%p] %q%u@%d '\t\t# special values: # is not a comment inside quotes",
  'pg_stat_statements.track = all',
  "include_dir = 'conf.d'\t\t\t# include files ending in '.conf' from",
  'max_connections = 150',
  "search_path = '\"$user\", public'",
  'port 5433',
  '# RESOURCE USAGE (except WAL)',
  '#work_mem = 4MB\t\t\t\t# min 64kB',
  "# the owner's last note",
].join('\n');

test('postgresql.conf: prose, headers and continued comments are not settings', () => {
  const sections = parsePostgresConf(pg);
  assert.deepEqual(sections.map((section) => section.title), ['CONNECTIONS AND AUTHENTICATION', 'RESOURCE USAGE (except WAL)']);
  const keys = items(sections).map((item) => item.key);
  assert.ok(!keys.includes('name'), '"#   name = value" in the header prose was taken for a setting');
  assert.deepEqual(keys, ['listen_addresses', 'max_connections', 'shared_buffers', 'log_line_prefix', 'pg_stat_statements.track', 'include_dir', 'max_connections', 'search_path', 'port', 'work_mem']);
  assert.deepEqual(find(sections, 'listen_addresses'), { line: 10, key: 'listen_addresses', value: 'localhost', enabled: false, description: 'what IP address(es) to listen on;' });
  assert.equal(find(sections, 'log_line_prefix').value, '%m [%p] %q%u@%d ');
  assert.equal(find(sections, 'log_line_prefix').description, 'special values: # is not a comment inside quotes');
  assert.equal(find(sections, 'port').value, '5433');
  assert.equal(find(sections, 'search_path').value, '"$user", public');
});

test('postgresql.conf: no edit, no change; one edit changes exactly that line', () => {
  const sections = parsePostgresConf(pg);
  assert.equal(applyPostgresConf(pg, new Map()), pg);

  const buffers = find(sections, 'shared_buffers');
  const saved = applyPostgresConf(pg, edit([buffers.line, { value: '256MB', enabled: true }]));
  const before = pg.split('\n');
  const after = saved.split('\n');
  assert.equal(after.length, before.length);
  after.forEach((line, index) => {
    if (index === buffers.line) assert.equal(line, '   shared_buffers=256MB # tuned by hand, 2025-03', 'the indent, the spacing and the comment of the line were not kept');
    else assert.equal(line, before[index], `line ${index + 1} was rewritten although nobody changed it`);
  });
});

test('postgresql.conf: two lines with the same name are two settings', () => {
  const sections = parsePostgresConf(pg);
  const second = find(sections, 'max_connections', 1);
  assert.equal(second.value, '150');
  const saved = applyPostgresConf(pg, edit([second.line, { value: '300', enabled: true }]));
  assert.ok(saved.includes('max_connections = 100\t\t\t# (change requires restart)\n'), 'the first line took the value of the second');
  assert.ok(saved.includes('\nmax_connections = 300\n'));
});

test('postgresql.conf: enabling and disabling only adds or removes the comment mark', () => {
  const sections = parsePostgresConf(pg);
  const listen = find(sections, 'listen_addresses');
  const work = find(sections, 'work_mem');
  const saved = applyPostgresConf(pg, edit(
    [listen.line, { value: listen.value, enabled: true }],
    [find(sections, 'port').line, { value: '5433', enabled: false }],
    [work.line, { value: '16MB', enabled: true }],
  ));
  assert.ok(saved.includes("\nlisten_addresses = 'localhost'\t\t# what IP address(es) to listen on;\n"));
  assert.ok(saved.includes('\n#port 5433\n'));
  assert.ok(saved.includes('\nwork_mem = 16MB\t\t\t\t# min 64kB\n'));
});

test('postgresql.conf: a changed value is quoted when it needs to be, and a quote inside it is doubled', () => {
  const sections = parsePostgresConf(pg);
  const saved = applyPostgresConf(pg, edit(
    [find(sections, 'log_line_prefix').line, { value: "%m it's ", enabled: true }],
    [find(sections, 'pg_stat_statements.track').line, { value: 'top level', enabled: true }],
    [find(sections, 'shared_buffers').line, { value: '0.5GB', enabled: true }],
  ));
  assert.ok(saved.includes("log_line_prefix = '%m it''s '\t\t# special values"));
  assert.ok(saved.includes("\npg_stat_statements.track = 'top level'\n"));
  assert.ok(saved.includes('shared_buffers=0.5GB # tuned'));
  // What was written parses back to what was typed.
  assert.equal(find(parsePostgresConf(saved), 'log_line_prefix').value, "%m it's ");
});

test('postgresql.conf: CRLF line endings and a missing final line break are kept as they are', () => {
  const crlf = "max_connections = 100\r\n#work_mem = 4MB\t# min\r\nshared_buffers = 128MB";
  const sections = parsePostgresConf(crlf);
  assert.deepEqual(items(sections).map((item) => [item.key, item.value]), [['max_connections', '100'], ['work_mem', '4MB'], ['shared_buffers', '128MB']]);
  const saved = applyPostgresConf(crlf, edit([find(sections, 'work_mem').line, { value: '8MB', enabled: true }]));
  assert.equal(saved, "max_connections = 100\r\nwork_mem = 8MB\t# min\r\nshared_buffers = 128MB");
});

test('postgresql.conf: a line this reader cannot take apart with certainty is not offered and not rewritten', () => {
  const odd = "shared_preload_libraries = 'a, b\nlog_destination = stderr extra words\nmax_connections = 5\n";
  const sections = parsePostgresConf(odd);
  assert.deepEqual(items(sections).map((item) => item.key), ['max_connections']);
  assert.equal(applyPostgresConf(odd, edit([0, { value: 'x', enabled: true }], [1, { value: 'y', enabled: false }])), odd);
});

// --- a MariaDB option file -------------------------------------------------------

const my = [
  '# The MariaDB server, tuned 2024',
  '# this is read by the standalone daemon and embedded servers',
  '[server]',
  '',
  '[mysqld]',
  'pid-file                = /run/mysqld/mysqld.pid',
  'bind-address            = 127.0.0.1',
  '#key_buffer_size        = 128M',
  ';max_allowed_packet     = 1G',
  'max_connections=150   # raised for the shop',
  'skip-name-resolve',
  '#skip-external-locking',
  "init_connect = 'SET NAMES utf8mb4'  # per connection",
  '!includedir /etc/mysql/extra.d/',
  '#Note for whoever edits this next',
  '[mariadb-11.8]',
  'plugin-load-add = auth_socket',
  '',
].join('\n');

test('option file: groups, options, commented options and flags; prose and includes are left alone', () => {
  const sections = parseOptionFile(my);
  assert.deepEqual(sections.map((section) => section.title), ['mysqld', 'mariadb-11.8']);
  assert.deepEqual(sections[0].items.map((item) => [item.key, item.value, item.enabled]), [
    ['pid-file', '/run/mysqld/mysqld.pid', true],
    ['bind-address', '127.0.0.1', true],
    ['key_buffer_size', '128M', false],
    ['max_allowed_packet', '1G', false],
    ['max_connections', '150', true],
    ['skip-name-resolve', '', true],
    ['skip-external-locking', '', false],
    ['init_connect', 'SET NAMES utf8mb4', true],
  ]);
  assert.equal(find(sections, 'max_connections').description, 'raised for the shop');
});

test('option file: one edit changes exactly that line; marks, spacing and comments stay', () => {
  const sections = parseOptionFile(my);
  assert.equal(applyOptionFile(my, new Map()), my);
  const saved = applyOptionFile(my, edit(
    [find(sections, 'max_connections').line, { value: '300', enabled: true }],
    [find(sections, 'key_buffer_size').line, { value: '128M', enabled: true }],
    [find(sections, 'max_allowed_packet').line, { value: '1G', enabled: false }],
    [find(sections, 'skip-name-resolve').line, { value: '', enabled: false }],
    [find(sections, 'init_connect').line, { value: 'SET NAMES latin1', enabled: true }],
  ));
  const before = my.split('\n');
  const after = saved.split('\n');
  const expected = {
    7: 'key_buffer_size        = 128M',
    9: 'max_connections=300   # raised for the shop',
    10: '#skip-name-resolve',
    12: "init_connect = 'SET NAMES latin1'  # per connection",
  };
  after.forEach((line, index) => assert.equal(line, expected[index] ?? before[index], `line ${index + 1}`));
});

test('option file: an option before any group is not one the server reads, so it is not offered', () => {
  assert.deepEqual(parseOptionFile('max_connections = 5\n# nothing else\n'), []);
});

// --- pg_hba.conf -------------------------------------------------------------------

const hba = [
  '# PostgreSQL Client Authentication Configuration File',
  '# DO NOT DISABLE!',
  'local   all             postgres                                peer',
  '',
  '# TYPE  DATABASE        USER            ADDRESS                 METHOD',
  'local   all             all                                     peer',
  'host    all             all             127.0.0.1/32            scram-sha-256',
  'host all  app   10.0.0.0/8   md5',
  'host    all             ldapusers       10.0.0.0/8              ldap ldapserver=ldap.example.net ldapprefix="cn="',
  'host    all             all             192.168.1.0 255.255.255.0 md5',
  'host    "my db"         all             10.1.0.0/16             md5',
  'host    all             all             10.2.0.0/16             md5 # the office',
  'include_dir hba.d',
  'host    replication     all             ::1/128                 \\',
  '        scram-sha-256',
  '',
].join('\n');

test('pg_hba.conf: simple rules are fields; everything the grid cannot hold is shown as written', () => {
  const rules = parseHba(hba);
  assert.deepEqual(rules.map((rule) => [rule.line, rule.editable]), [
    [2, true], [5, true], [6, true], [7, true], [8, false], [9, false], [10, false], [11, false], [12, false], [13, false],
  ]);
  assert.deepEqual({ ...rules[3], text: undefined, line: undefined, editable: undefined },
    { type: 'host', database: 'all', user: 'app', address: '10.0.0.0/8', method: 'md5', text: undefined, line: undefined, editable: undefined });
  assert.equal(rules.at(-1).text, 'host    replication     all             ::1/128                 \\\n        scram-sha-256');
  assert.equal(rules[0].address, '');
});

test('pg_hba.conf: no change, no change; an edit, a removal and a new rule touch only their own lines', () => {
  const none = { edited: new Map(), removed: new Set(), added: [] };
  assert.equal(applyHba(hba, none).content, hba);

  // Opening a rule and leaving its fields as they are does not reformat it.
  const untouched = applyHba(hba, { ...none, edited: new Map([[7, { type: 'host', database: 'all', user: 'app', address: '10.0.0.0/8', method: 'md5' }]]) });
  assert.equal(untouched.content, hba, 'a rule with unusual spacing was reformatted without a change');

  const result = applyHba(hba, {
    edited: new Map([[6, { type: 'host', database: 'all', user: 'all', address: '127.0.0.1/32', method: 'md5' }]]),
    removed: new Set([7, 8, 12]), // 8 and 12 are not editable rules: they must stay
    added: [{ type: 'hostssl', database: 'shop', user: 'shop', address: '192.0.2.0/24', method: 'scram-sha-256' }, { type: 'local', database: 'shop', user: 'shop', address: '', method: 'peer' }],
  });
  const before = hba.split('\n');
  const expected = [
    ...before.slice(0, 6),
    'host    all             all             127.0.0.1/32            md5',
    ...before.slice(8, 15),
    'hostssl shop            shop            192.0.2.0/24            scram-sha-256',
    'local   shop            shop                                    peer',
    '',
  ];
  assert.equal(result.content, expected.join('\n'));
  assert.deepEqual(result.addedLines, [14, 15], 'the lines the new rules stand on are not reported');
});

test('pg_hba.conf: a file without a final line break gets its new rule on a line of its own', () => {
  const result = applyHba('local all all peer', { edited: new Map(), removed: new Set(), added: [{ type: 'host', database: 'a', user: 'b', address: '::1/128', method: 'md5' }] });
  assert.equal(result.content, 'local all all peer\nhost    a               b               ::1/128                 md5');
  assert.deepEqual(result.addedLines, [1]);
});

test('pg_hba.conf: a rule is complete only with every field a word of its own', () => {
  const rule = { type: 'host', database: 'all', user: 'all', address: '10.0.0.0/8', method: 'md5' };
  assert.equal(hbaRuleComplete(rule), true);
  assert.equal(hbaRuleComplete({ ...rule, type: 'local', address: '' }), true);
  for (const broken of [{ database: '' }, { user: 'a b' }, { address: '' }, { method: '' }, { database: 'x # y' }, { user: 'a,b' }, { type: 'bogus' }]) {
    assert.equal(hbaRuleComplete({ ...rule, ...broken }), false, JSON.stringify(broken));
  }
  assert.equal(formatHbaRule({ ...rule, type: 'local', address: '', method: 'peer' }), 'local   all             all                                     peer');
});
