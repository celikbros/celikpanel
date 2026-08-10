import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const detailSource = readFileSync(new URL('../src/components/DomainDetail.tsx', import.meta.url), 'utf8');
const domainsSource = readFileSync(new URL('../src/components/Domains.tsx', import.meta.url), 'utf8');
const filesSource = readFileSync(new URL('../src/components/DomainFileManager.tsx', import.meta.url), 'utf8');
const databasesSource = readFileSync(new URL('../src/components/DomainDatabaseManager.tsx', import.meta.url), 'utf8');
const phpSource = readFileSync(new URL('../src/components/DomainPHPSettings.tsx', import.meta.url), 'utf8');
const dnsSource = readFileSync(new URL('../src/components/DomainDNSManager.tsx', import.meta.url), 'utf8');

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

test('team-member DB, PHP and DNS panels avoid server-global capability calls', () => {
  const dbEngineEffect = databasesSource.slice(
    databasesSource.indexOf('const [engines'),
    databasesSource.indexOf('// Form state'),
  );
  assert.match(dbEngineEffect, /if \(isAdditionalUser\)[\s\S]*return;[\s\S]*fetch\('\/api\/v1\/hosting\/capabilities'\)/);
  assert.match(databasesSource, /!isAdditionalUser && <DBToolsCard \/>/);
  assert.match(databasesSource, /fetch\(`\/api\/v1\/domains\/\$\{domainId\}\/databases`\)/);
  assert.match(databasesSource, /function parseAvailableDatabaseTypes\(value: unknown\)/);
  assert.match(databasesSource, /if \(!Array\.isArray\(value\)\) return \[\];/);
  assert.match(databasesSource, /item !== 'mysql' && item !== 'postgresql'\) return \[\];/);
  assert.match(databasesSource, /parseAvailableDatabaseTypes\(payload\.available_types\)/);
  assert.match(databasesSource, /const loadDatabases[\s\S]*if \(isAdditionalUser\) \{[\s\S]*setEngines\(\[\]\);[\s\S]*setDatabases\(\[\]\);/);
  assert.doesNotMatch(databasesSource, /database\.type\.toLowerCase\(\)/);

  const phpLoadEffect = phpSource.slice(phpSource.indexOf('useEffect(() =>'), phpSource.indexOf('const loadVersions'));
  assert.match(phpLoadEffect, /if \(!isAdditionalUser\) \{[\s\S]*loadVersions\(\)/);
  assert.match(phpSource, /function parseAvailablePHPVersions\(value: unknown\)/);
  assert.match(phpSource, /typeof item !== 'string' \|\| !phpVersionPattern\.test\(item\)\) return \[\];/);
  assert.match(phpSource, /parseAvailablePHPVersions\(nextSettings\.available_versions\)/);
  assert.match(phpSource, /const loadSettings[\s\S]*if \(isAdditionalUser\) \{[\s\S]*setVersions\(\[\]\);[\s\S]*setSettings\(null\);/);
  assert.match(phpSource, /if \(isAdditionalUser && !versions\.includes\(selectedVersion\)\) return;/);
  assert.doesNotMatch(phpSource, /readOnly \|\| isAdditionalUser \|\| selectedVersion/);
  assert.match(phpSource, /disabled=\{readOnly \|\| \(isAdditionalUser && versions\.length === 0\)\}/);
  assert.match(phpSource, /fetch\(`\/api\/v1\/domains\/\$\{domainId\}\/php`/);

  const dnsLoadEffect = dnsSource.slice(dnsSource.indexOf('useEffect(() =>'), dnsSource.indexOf('const checkZone'));
  assert.match(dnsLoadEffect, /if \(isAdditionalUser\)[\s\S]*return;[\s\S]*fetch\('\/api\/v1\/hosting\/capabilities'\)/);
  assert.match(dnsSource, /isAdditionalUser \|\| \(dnsServer !== null/);
  assert.match(dnsSource, /DNSSECSection[^>]+readOnly=\{readOnly\}/s);
  assert.match(dnsSource, /readOnly \? \([\s\S]*loadRecords\(\)[\s\S]*\) : \(/);
  assert.match(dnsSource, /!readOnly && showAddForm/);
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
