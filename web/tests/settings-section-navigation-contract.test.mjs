import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const settings = readFileSync(
  new URL('../src/components/Settings.tsx', import.meta.url),
  'utf8',
);
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

function sourceBetween(startMarker, endMarker) {
  const start = settings.indexOf(startMarker);
  const end = settings.indexOf(endMarker, start + startMarker.length);
  assert.ok(start >= 0, `missing source marker: ${startMarker}`);
  assert.ok(end > start, `missing source marker after ${startMarker}: ${endMarker}`);
  return settings.slice(start, end);
}

function tabPanelSource(sectionID) {
  const marker = `id="settings-${sectionID}-panel"`;
  const start = settings.indexOf(marker);
  assert.ok(start >= 0, `missing ${sectionID} tab panel`);

  const panelEnd = settings.indexOf('</div>', start + marker.length);
  assert.ok(panelEnd > start, 'missing closing element for ' + sectionID + ' tab panel');
  return settings.slice(start, panelEnd + '</div>'.length);
}

test('software updates are a dedicated localized admin Settings section', () => {
  assert.match(settings, /type SettingsSectionID\s*=\s*[^;]*'updates'/s);

  const adminSections = sourceBetween("...(role === 'admin'", '];');
  assert.match(adminSections, /id:\s*'updates'\s+as const/);
  assert.match(adminSections, /t\('settings\.section\.updates'\)/);
  assert.match(adminSections, /t\('settings\.section\.updates\.desc'\)/);

  for (const catalog of [english, turkish]) {
    assert.match(catalog, /'settings\.section\.updates':/);
    assert.match(catalog, /'settings\.section\.updates\.desc':/);
  }
});

test('panel access owns only the panel certificate while updates own PanelUpdateCard', () => {
  const panel = tabPanelSource('panel');
  const updates = tabPanelSource('updates');

  assert.match(panel, /<PanelCertificatePanel\s*\/>/);
  assert.doesNotMatch(panel, /<PanelUpdateCard\s*\/>/);
  assert.match(updates, /<PanelUpdateCard\s*\/>/);
  assert.doesNotMatch(updates, /<PanelCertificatePanel\s*\/>/);
});

test('operational Settings components mount only while their URL section is active', () => {
  const updates = tabPanelSource('updates');
  const dns = tabPanelSource('dns');

  assert.match(
    updates,
    /hidden=\{activeID !== 'updates'\}[\s\S]*\{activeID === 'updates' && <Suspense[^>]*><PanelUpdateCard \/><\/Suspense>\}/,
  );
  assert.match(
    dns,
    /hidden=\{activeID !== 'dns'\}[\s\S]*\{activeID === 'dns' && <DNSServerSettings \/>\}/,
  );
});

test('user section changes add history while invalid section canonicalization replaces it', () => {
  const canonicalization = sourceBetween('const requestedSection', 'const selectSection');
  assert.match(canonicalization, /if \(requestedSection === activeSection\.id\) return/);
  assert.match(
    canonicalization,
    /setSearchParams\(\s*next\s*,\s*\{\s*replace:\s*true\s*\}\s*\)/s,
  );

  const selection = sourceBetween('const selectSection', 'const moveSection');
  assert.ok(selection.includes('if (section === activeSection.id) return;'));
  assert.match(
    selection,
    /setSearchParams\(\s*next(?:\s*,\s*\{\s*replace:\s*false\s*\})?\s*\)/s,
  );
  assert.doesNotMatch(selection, /replace:\s*true/);
});

test('the horizontally scrolling mobile tab list keeps the active section visible', () => {
  const workspace = sourceBetween('function SettingsWorkspace(', '// The certificate');

  assert.match(workspace, /overflow-x-auto/);
  assert.match(workspace, /tabRefs\.current\[section\.id\]\s*=\s*element/);
  assert.match(workspace, /const activeTab\s*=\s*tabRefs\.current\[activeID\]/);
  assert.match(workspace, /matchMedia\('\(max-width:\s*1023px\)'\)\.matches/);
  assert.match(workspace, /activeTab\.scrollIntoView\(\s*\{/);
  assert.match(workspace, /block:\s*'nearest'/);
  assert.match(workspace, /inline:\s*'nearest'/);
  assert.match(workspace, /\[activeID\]/);
});
