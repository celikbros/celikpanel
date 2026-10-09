import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const detailSource = readFileSync(new URL('../src/components/DomainDetail.tsx', import.meta.url), 'utf8');
const domainsSource = readFileSync(new URL('../src/components/Domains.tsx', import.meta.url), 'utf8');
const filesSource = readFileSync(new URL('../src/components/DomainFileManager.tsx', import.meta.url), 'utf8');
const databasesSource = readFileSync(new URL('../src/components/DomainDatabaseManager.tsx', import.meta.url), 'utf8');
const phpSource = readFileSync(new URL('../src/components/DomainPHPSettings.tsx', import.meta.url), 'utf8');
const dnsSource = readFileSync(new URL('../src/components/DomainDNSManager.tsx', import.meta.url), 'utf8');
const capabilitiesSource = readFileSync(new URL('../src/lib/hostingCapabilities.ts', import.meta.url), 'utf8');
const remoteSource = readFileSync(new URL('../src/lib/remote.ts', import.meta.url), 'utf8');
// The decoder of a domain's databases moved to lib/domainDatabases.ts in the
// fourth batch (9 Oct 2026), so the Databases and Backups tabs share one
// decoder for one address. What was pinned in the component is pinned there.
const domainDatabasesSource = readFileSync(new URL('../src/lib/domainDatabases.ts', import.meta.url), 'utf8');

test('additional-user domain surfaces stay fail-closed without disabling the accessible view tree', () => {
  assert.doesNotMatch(detailSource, /<fieldset/);
  assert.doesNotMatch(detailSource, /setAttribute\('inert'/);
  assert.match(detailSource, /currentSub\.render\(readOnly\)/);
  assert.match(detailSource, /current\.render!\(readOnly\)/);

  assert.match(detailSource, /!isTeamMember && canView\('files'\)/);
  assert.match(detailSource, /!isTeamMember && projectType === 'php'.*id: 'apps'/s);
  assert.match(detailSource, /DomainFileManager[^>]+readOnly=\{readOnly\}/s);

  assert.match(filesSource, /if \(readOnly\) \{[\s\S]*files\/download/);
  assert.match(filesSource, /!readOnly && <ToolButton icon=\{Edit\}/);
  assert.match(filesSource, /ToolButton icon=\{Download\}/);
  assert.match(filesSource, /!readOnly && <ToolButton icon=\{Trash2\}/);
  assert.match(filesSource, /aria-label=\{title\}/);
});

// The server-wide capability inventory is read in one place
// (lib/hostingCapabilities.ts). A team member is never given that read:
// `enabled: false` makes the address null, and a null address requests nothing.
test('team-member DB, PHP and DNS panels avoid server-global capability calls', () => {
  assert.match(capabilitiesSource, /options\.enabled === false \? null : HOSTING_CAPABILITIES_URL/);
  assert.match(remoteSource, /if \(url === null\) return;/);
  for (const [name, source] of [['databases', databasesSource], ['PHP', phpSource], ['DNS', dnsSource], ['detail', detailSource]]) {
    assert.doesNotMatch(source, /hosting\/capabilities/, `${name} reads the capability address itself`);
  }

  assert.match(databasesSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isAdditionalUser \}\);/);
  // A team member's engines are the tenant-safe available_types of this
  // domain's own answer; an administrator's are the server's capabilities.
  assert.match(
    databasesSource,
    /const engineSource: Remote<DatabaseEngine\[\]> = isAdditionalUser\s*\? mapRemote\(list\.remote, \(value\) => value\.availableTypes\)\s*: mapRemote\(capabilities\.remote,/,
  );
  assert.match(databasesSource, /!isAdditionalUser && <DBToolsCard capabilities=\{capabilities\.remote\} \/>/);
  assert.match(databasesSource, /useRemote\(`\/api\/v1\/domains\/\$\{domainId\}\/databases`, decodeDomainDatabases\)/);
  assert.match(databasesSource, /import \{ decodeDomainDatabases, [^}]*\} from '\.\.\/lib\/domainDatabases';/);
  assert.doesNotMatch(databasesSource, /function (decodeDomainDatabases|parseAvailableDatabaseTypes)/, 'a second decoder of the same address');
  assert.match(domainDatabasesSource, /export function parseAvailableDatabaseTypes\(value: unknown\)/);
  assert.match(domainDatabasesSource, /if \(!Array\.isArray\(value\)\) return \[\];/);
  assert.match(domainDatabasesSource, /item !== 'mysql' && item !== 'postgresql'\) return \[\];/);
  assert.match(domainDatabasesSource, /availableTypes: parseAvailableDatabaseTypes\(payload\.available_types\)/);
  assert.match(domainDatabasesSource, /databases: decodeList<DatabaseInfo>\(payload\.databases\)/);
  // Nothing is created on an engine the server did not name, and nothing of
  // another domain's answer is carried over: engines exist only for a known
  // answer of this domain's own address.
  assert.match(databasesSource, /const engines = engineSource\.state === 'known' \? engineSource\.value : \[\];/);
  assert.match(databasesSource, /const enginesReady = !readOnly && engineSource\.state === 'known' && engines\.length > 0;/);
  assert.match(databasesSource, /if \(!canCreate \|\| dbType === null\) return;/);
  assert.match(databasesSource, /\{showCreateForm && enginesReady && \(/);
  assert.doesNotMatch(databasesSource, /useState<DatabaseType>\('mysql'\)/, 'a default engine is back');
  assert.doesNotMatch(databasesSource, /database\.type\.toLowerCase\(\)/);

  assert.match(phpSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isAdditionalUser \}\);/);
  assert.match(
    phpSource,
    /const versions = isAdditionalUser\s*\? teamVersions\s*: capabilities\.remote\.state === 'known' \? capabilities\.remote\.value\.php_versions : \[\];/,
  );
  assert.match(phpSource, /function parseAvailablePHPVersions\(value: unknown\)/);
  assert.match(phpSource, /typeof item !== 'string' \|\| !phpVersionPattern\.test\(item\)\) return \[\];/);
  // Since the fourth batch (9 Oct 2026) the PHP settings are read through
  // lib/remote.ts. The same properties, pinned on the new shape: a team
  // member's versions are the tenant-safe available_versions of the answer of
  // this domain's own address, they exist only while that answer is known (so
  // nothing of another domain's answer is carried over, which the cleared
  // state used to guarantee), and no version outside them is sent.
  assert.match(phpSource, /availableVersions: parseAvailablePHPVersions\(body\.available_versions\)/);
  assert.match(phpSource, /const settings = useRemote\(`\/api\/v1\/domains\/\$\{domainId\}\/php`, decodePHPSettings\);/);
  assert.match(phpSource, /const teamVersions = settings\.remote\.state === 'known' \? settings\.remote\.value\.availableVersions : \[\];/);
  assert.doesNotMatch(phpSource, /useState<string\[\]>/, 'a version list kept beside the answer it came from');
  assert.match(phpSource, /if \(isAdditionalUser && !versions\.includes\(selectedVersion\)\) return;/);
  assert.doesNotMatch(phpSource, /readOnly \|\| isAdditionalUser \|\| selectedVersion/);
  assert.match(phpSource, /disabled=\{readOnly \|\| \(isAdditionalUser && versions\.length === 0\)\}/);
  assert.match(phpSource, /answer\.send\(`\/api\/v1\/domains\/\$\{domainId\}\/php`, \{/);
  assert.doesNotMatch(phpSource, /\bfetch\(/, 'the PHP panel reads or writes outside the shared layer');

  assert.match(dnsSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isAdditionalUser \}\);/);
  assert.match(
    dnsSource,
    /const dnsServer = !isAdditionalUser && capabilities\.remote\.state === 'known' \? capabilities\.remote\.value\.dns_server : null;/,
  );
  assert.match(dnsSource, /isAdditionalUser \|\| \(dnsServer !== null/);
  assert.match(dnsSource, /DNSSECSection[^>]+readOnly=\{readOnly\}/s);
  // A read-only or externally managed zone gets the read of its records and
  // nothing that changes them (the read is `records.retry()` since the fourth
  // batch, 9 Oct 2026; it was `loadRecords()`).
  assert.match(dnsSource, /readOnly \|\| externalDNS \? \(\s*<Button variant="secondary" icon=\{RefreshCw\} disabled=\{records\.reading\} onClick=\{\(\) => void records\.retry\(\)\}>[\s\S]*?\) : \(/);
  assert.match(dnsSource, /const canChange = !readOnly && !externalDNS && zoneRemote\.state === 'known' && !answer\.holding;/);
  for (const change of ['publishZone', 'addRecord', 'deleteRecord']) {
    assert.match(dnsSource, new RegExp(`const ${change} = async \\([^)]*\\) => \\{\\s*if \\(!canChange`), `${change} is not held by canChange`);
  }
  assert.match(dnsSource, /!readOnly && !externalDNS && showAddForm/);

  assert.match(detailSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isTeamMember \}\);/);
  assert.match(detailSource, /const caps = !isTeamMember && capabilities\.remote\.state === 'known' \? capabilities\.remote\.value : null;/);
});

test('redacted domain metadata stays optional and fails closed in the UI', () => {
  for (const source of [domainsSource, detailSource]) {
    assert.match(source, /php_version\?: string;/);
    assert.match(source, /ssl_enabled\?: boolean;/);
  }

  assert.match(detailSource, /currentVersion=\{domain\.php_version \?\? ''\}/);
  assert.match(domainsSource, /canView\(d, 'ssl'\) && d\.ssl_enabled/);
  assert.match(domainsSource, /canView\(d, 'php'\)[\s\S]*d\.php_version/);
});
