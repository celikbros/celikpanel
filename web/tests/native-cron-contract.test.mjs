import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { englishCatalogue, turkishCatalogue } from './locale-catalogue.mjs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const hasKey = (catalogue, key) => catalogue.includes(`'${key}':`) || catalogue.includes(`"${key}":`);
const value = (catalogue, key) => {
  const match = catalogue.match(new RegExp(`['"]${key.replace(/\./g, '\\.')}['"]:\\s*(['"])(.*?)\\1,\\n`));
  return match ? match[2] : '';
};

// upd1 (1 Oct 2026): a missing cron was answered 500 INTERNAL. The typed
// answer, the setup component label and the Components category must exist in
// both languages, and the guidance must name the owner's next action.
test('native cron copy stays in EN/TR parity', () => {
  const keys = [
    'err.CRON_NOT_INSTALLED',
    'err.CRON_NOT_INSTALLED.read',
    'err.NATIVE_CRON_REMOVAL_REFUSED',
    'setup.component.cron',
    'services.cat.system',
  ];
  for (const key of keys) {
    assert.ok(hasKey(englishCatalogue, key), 'missing EN key ' + key);
    assert.ok(hasKey(turkishCatalogue, key), 'missing TR key ' + key);
  }
  for (const catalogue of [englishCatalogue, turkishCatalogue]) {
    for (const key of ['err.CRON_NOT_INSTALLED', 'err.CRON_NOT_INSTALLED.read']) {
      const text = value(catalogue, key);
      for (const part of ['sudo apt-get install cron', 'sudo pacman -S cronie', 'sudo systemctl enable --now cronie', 'Scheduled tasks (cron)']) {
        assert.ok(text.includes(part), `${key} lacks ${part}: ${text}`);
      }
    }
  }
});

test('scheduled tasks screen keeps the typed cron answer on screen', () => {
  const source = read('../src/components/DomainCronManager.tsx');
  assert.match(source, /readApiError/);
  assert.match(source, /apiErrorText/);
  assert.match(source, /'CRON_NOT_INSTALLED'/);
  // A condition found on load is announced politely, like the domain list's
  // pending-deletion guidance; the refused change itself is the error toast.
  assert.match(source, /<div role="status">\s*\{blocked && \(/);
  // While cron is missing no new task can be saved, so Add is not offered.
  assert.match(source, /!readOnly && !showForm && !blocked &&/);
  // A refused read must not fall back to the "no tasks" empty state.
  assert.match(source, /blocked && jobs\.length === 0 \? null/);
});

test('setup and components name native cron', () => {
  assert.match(read('../src/components/ServerSetup.tsx'), /target === 'cron'\) return t\('setup\.component\.cron'\)/);
  assert.match(read('../src/components/ServiceList.tsx'), /id: 'system', labelKey: 'services\.cat\.system'/);
});
