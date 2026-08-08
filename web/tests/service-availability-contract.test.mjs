import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const serviceList = readFileSync(new URL('../src/components/ServiceList.tsx', import.meta.url), 'utf8');
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('installed conflicts take precedence over install availability badges', () => {
  const branch = serviceList.slice(
    serviceList.indexOf('{!s.is_installed ? ('),
    serviceList.indexOf(') : s.requires_missing', serviceList.indexOf('{!s.is_installed ? (')),
  );

  assert.ok(branch.indexOf('s.conflict_with ? (') >= 0);
  assert.ok(branch.indexOf('s.conflict_with ? (') < branch.indexOf('s.not_offered ? ('));
});

test('integration blocks are distinct from missing distro packages', () => {
  assert.match(serviceList, /not_offered_kind === 'integration'/);
  assert.match(serviceList, /services\.integrationPending/);
  assert.match(serviceList, /services\.notOffered/);
});

test('vsftpd points operators to the built-in encrypted SFTP path', () => {
  assert.match(serviceList, /s\.id === 'vsftpd'/);
  assert.match(serviceList, /services\.useBuiltInSFTP/);
  assert.match(serviceList, /onClick=\{\(\) => navigate\('\/domains'\)\}/);
  assert.match(serviceList, /type="button"/);
  assert.match(english, /'services\.useBuiltInSFTP': 'Use built-in SFTP'/);
  assert.match(turkish, /'services\.useBuiltInSFTP': 'Yerleşik SFTP’yi kullanın'/);
});
