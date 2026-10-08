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
  assert.match(databasesSource, /function parseAvailableDatabaseTypes\(value: unknown\)/);
  assert.match(databasesSource, /if \(!Array\.isArray\(value\)\) return \[\];/);
  assert.match(databasesSource, /item !== 'mysql' && item !== 'postgresql'\) return \[\];/);
  assert.match(databasesSource, /availableTypes: parseAvailableDatabaseTypes\(payload\.available_types\)/);
  // Nothing is created on an engine the server did not name, and nothing of
  // another domain's answer is carried over: engines exist only for a known
  // answer of this domain's own address.
  assert.match(databasesSource, /const engines = engineSource\.state === 'known' \? engineSource\.value : \[\];/);
  assert.match(databasesSource, /const canCreate = !readOnly && engineSource\.state === 'known' && engines\.length > 0;/);
  assert.match(databasesSource, /if \(!canCreate \|\| dbType === null\) return;/);
  assert.match(databasesSource, /\{showCreateForm && canCreate && \(/);
  assert.doesNotMatch(databasesSource, /useState<DatabaseType>\('mysql'\)/, 'a default engine is back');
  assert.doesNotMatch(databasesSource, /database\.type\.toLowerCase\(\)/);

  assert.match(phpSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isAdditionalUser \}\);/);
  assert.match(
    phpSource,
    /const versions = isAdditionalUser\s*\? teamVersions\s*: capabilities\.remote\.state === 'known' \? capabilities\.remote\.value\.php_versions : \[\];/,
  );
  assert.match(phpSource, /function parseAvailablePHPVersions\(value: unknown\)/);
  assert.match(phpSource, /typeof item !== 'string' \|\| !phpVersionPattern\.test\(item\)\) return \[\];/);
  assert.match(phpSource, /parseAvailablePHPVersions\(nextSettings\.available_versions\)/);
  assert.match(phpSource, /const loadSettings[\s\S]*if \(isAdditionalUser\) \{[\s\S]*setVersions\(\[\]\);[\s\S]*setSettings\(null\);/);
  assert.match(phpSource, /if \(isAdditionalUser && !versions\.includes\(selectedVersion\)\) return;/);
  assert.doesNotMatch(phpSource, /readOnly \|\| isAdditionalUser \|\| selectedVersion/);
  assert.match(phpSource, /disabled=\{readOnly \|\| \(isAdditionalUser && versions\.length === 0\)\}/);
  assert.match(phpSource, /fetch\(`\/api\/v1\/domains\/\$\{domainId\}\/php`/);

  assert.match(dnsSource, /const capabilities = useHostingCapabilities\(\{ enabled: !isAdditionalUser \}\);/);
  assert.match(
    dnsSource,
    /const dnsServer = !isAdditionalUser && capabilities\.remote\.state === 'known' \? capabilities\.remote\.value\.dns_server : null;/,
  );
  assert.match(dnsSource, /isAdditionalUser \|\| \(dnsServer !== null/);
  assert.match(dnsSource, /DNSSECSection[^>]+readOnly=\{readOnly\}/s);
  assert.match(dnsSource, /readOnly \|\| externalDNS \? \([\s\S]*loadRecords\(\)[\s\S]*\) : \(/);
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
