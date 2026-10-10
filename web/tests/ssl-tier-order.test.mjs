import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

// The certificate tier (D-031 step 1b, second round, 2026-10-10): a verified
// failure of the certificate in use (expired, invalid, untrusted) is shown as
// such, never as "waiting for a choice on the site's configuration file"; the
// waiting state names which of its two causes it is.
const source = readFileSync(new URL('../src/lib/sslTier.ts', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2022, target: ts.ScriptTarget.ES2020 } }).outputText;
const { sslTier, sslTierLabelFor } = await import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64'));

const base = {
  activated: true, usable: true, trust_status: 'trusted', activation_pending: false,
  dependents_pending: false, waiting_for_owner: true, days_until_expiry: 40,
};

test('an expired, invalid or untrusted certificate in use outranks waiting for the owner', () => {
  assert.equal(sslTier({ ...base, days_until_expiry: -3 }), 'expired');
  assert.equal(sslTier({ ...base, trust_status: 'invalid' }), 'invalid');
  assert.equal(sslTier({ ...base, trust_status: 'untrusted' }), 'untrusted');
  // A trust that could not be checked is not a verified failure.
  assert.equal(sslTier({ ...base, trust_status: 'unknown' }), 'waitingForOwner');
  assert.equal(sslTier(base), 'waitingForOwner');
  assert.equal(sslTier({ ...base, days_until_expiry: 5 }), 'waitingForOwner');
  // An activation in progress stays first, as before.
  assert.equal(sslTier({ ...base, activation_pending: true, days_until_expiry: -3 }), 'pending');
  // Without the wait, the order is unchanged.
  assert.equal(sslTier({ ...base, waiting_for_owner: false, trust_status: 'unknown' }), 'trustUnknown');
  assert.equal(sslTier({ ...base, waiting_for_owner: false, days_until_expiry: 5 }), 'expiring');
});

test('the waiting label names its cause when the answer says it', () => {
  assert.equal(sslTierLabelFor('waitingForOwner', { ...base, waiting_for_owner_reason: 'certificate' }), 'ssl.status.waitingForOwner.certificate');
  assert.equal(sslTierLabelFor('waitingForOwner', { ...base, waiting_for_owner_reason: 'certificate_validation' }), 'ssl.status.waitingForOwner.certificate_validation');
  assert.equal(sslTierLabelFor('waitingForOwner', base), 'ssl.status.waitingForOwner');
  assert.equal(sslTierLabelFor('expired', { ...base, waiting_for_owner_reason: 'certificate' }), 'ssl.status.expired');
  for (const path of ['../src/i18n/screens/en.ts', '../src/i18n/screens/tr.ts']) {
    const catalogue = readFileSync(new URL(path, import.meta.url), 'utf8');
    for (const key of ['ssl.status.waitingForOwner', 'ssl.status.waitingForOwner.certificate', 'ssl.status.waitingForOwner.certificate_validation',
      'ssl.waitingForOwner', 'ssl.waitingForOwner.validation', 'ssl.issuedWaitingForOwner', 'dashboard.certWaitingOwnerExpired']) {
      assert.ok(catalogue.includes(`'${key}':`), `${path} ${key}`);
    }
  }
});

test('customer-reachable sentences name the server administrator as the actor', () => {
  const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
  const sentence = (catalogue, key) => {
    const match = catalogue.match(new RegExp(`'${key.replace(/\./g, '\\.')}': '([^']*)'`));
    assert.ok(match, key);
    return match[1];
  };
  const en = read('../src/i18n/screens/en.ts');
  const tr = read('../src/i18n/screens/tr.ts');
  for (const key of ['ssl.waitingForOwner', 'ssl.waitingForOwner.validation', 'ssl.issuedWaitingForOwner']) {
    assert.match(sentence(en, key), /server administrator/, key);
    assert.match(sentence(tr, key), /sunucu yöneticisi/, key);
  }
  const shellEN = read('../src/i18n/en.ts');
  const shellTR = read('../src/i18n/tr.ts');
  for (const key of ['err.SITE_CONFIG_OWNER_EDITED.certificate_validation', 'err.CERTIFICATE_VALIDATION_UNKNOWN']) {
    assert.match(sentence(shellEN, key), /server administrator/, key);
    assert.match(sentence(shellTR, key), /(S|s)unucu yöneticisi/, key);
  }
});
