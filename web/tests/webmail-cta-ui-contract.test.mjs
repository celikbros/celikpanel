import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const mailSource = readFileSync(new URL('../src/components/DomainMailManager.tsx', import.meta.url), 'utf8');
const accessSource = readFileSync(new URL('../src/components/WebmailAccess.tsx', import.meta.url), 'utf8');
const enSource = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const trSource = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('webmail availability comes only from the tenant-scoped setup endpoint', () => {
  assert.ok(mailSource.includes(`lazy(() => import('./WebmailAccess'))`));
  assert.ok(mailSource.includes('<WebmailAccess domainId={domainId} />'));
  assert.ok(accessSource.includes('fetch(`/api/v1/domains/${domainId}/mail/setup`'));
  assert.ok(accessSource.includes('}, [domainId]);'));
  assert.ok(!accessSource.includes('/api/v1/managed-services'));
  assert.ok(accessSource.includes('response.ok ? response.json() : null'));
  assert.ok(accessSource.includes('setPath(null)'));
});

test('webmail payload parsing fails closed and allowlists only the public webmail route', () => {
  assert.ok(accessSource.includes(`setup?.webmail_available === true && setup.webmail_url === '/webmail/'`));
  assert.ok(accessSource.includes('? setup.webmail_url : null'));
  assert.ok(!accessSource.includes('window.open'));
});

test('webmail CTA is safe, read-only accessible, and absent when unavailable', () => {
  assert.ok(accessSource.includes('path && ('));
  assert.ok(accessSource.includes('href={path}'));
  assert.ok(accessSource.includes(`target='_blank'`));
  assert.ok(accessSource.includes(`rel='noopener noreferrer'`));
  assert.ok(accessSource.includes(`t('mail.webmail.unavailable')`));
  assert.ok(!accessSource.includes('readOnly'));
});

test('webmail availability copy stays in EN/TR parity', () => {
  const keys = [
    'mail.webmail.title',
    'mail.webmail.checking',
    'mail.webmail.available',
    'mail.webmail.unavailable',
    'mail.webmail.open',
  ];
  for (const key of keys) {
    assert.ok(enSource.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(trSource.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
