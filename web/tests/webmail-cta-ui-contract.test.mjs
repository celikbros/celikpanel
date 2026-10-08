import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { englishCatalogue, turkishCatalogue } from './locale-catalogue.mjs';

const mailSource = readFileSync(new URL('../src/components/DomainMailManager.tsx', import.meta.url), 'utf8');
const accessSource = readFileSync(new URL('../src/components/WebmailAccess.tsx', import.meta.url), 'utf8');
const setupSource = readFileSync(new URL('../src/lib/mailSetup.ts', import.meta.url), 'utf8');
const enSource = englishCatalogue;
const trSource = turkishCatalogue;

// Since 9 Oct 2026 the card reads through the shared remote-state layer: the
// address and the decoder live in lib/mailSetup.ts, and a read that failed is
// "could not be checked", not "not available on this server".
test('webmail availability comes only from the tenant-scoped setup endpoint', () => {
  assert.ok(mailSource.includes(`lazy(() => import('./WebmailAccess'))`));
  assert.ok(mailSource.includes('<WebmailAccess domainId={domainId} />'));
  assert.ok(setupSource.includes('`/api/v1/domains/${domainId}/mail/setup`'));
  assert.ok(accessSource.includes('useRemote(mailSetupURL(domainId), decodeMailSetup)'));
  assert.ok(!accessSource.includes('/api/v1/managed-services'));
  assert.ok(!accessSource.includes('fetch('), 'the card reads through lib/remote.ts, not its own fetch');
});

test('a failed read of the setup is unknown, never "webmail is not available"', () => {
  // The old card turned a refused or dropped read into null and drew the
  // "unavailable" sentence from it.
  assert.ok(!accessSource.includes('response.ok ? response.json() : null'));
  assert.ok(!accessSource.includes('setPath(null)'));
  assert.ok(accessSource.includes("remote.state === 'loading'"));
  assert.ok(accessSource.includes("remote.state === 'unknown'"));
  assert.ok(accessSource.includes("t('mail.webmail.unknown')"));
  assert.ok(accessSource.includes('onRetry={() => void retry()}'));
  // "Unavailable" is drawn only in the branch left for a known answer.
  const unknownAt = accessSource.indexOf("remote.state === 'unknown'");
  assert.ok(accessSource.indexOf("t('mail.webmail.unavailable')") > unknownAt);
});

test('webmail payload parsing fails closed and allowlists only the public webmail route', () => {
  assert.ok(setupSource.includes(`setup.webmail_available === true && setup.webmail_url === '/webmail/' ? setup.webmail_url : null`));
  assert.ok(setupSource.includes(`typeof setup.webmail_available !== 'boolean') throw`), 'an answer without the flag is not the contract');
  assert.ok(accessSource.includes("remote.state === 'known' ? remote.value.webmail : null"));
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
    'mail.webmail.unknown',
    'mail.webmail.open',
  ];
  for (const key of keys) {
    assert.ok(enSource.includes(`'${key}':`), 'missing EN key ' + key);
    assert.ok(trSource.includes(`'${key}':`), 'missing TR key ' + key);
  }
});
