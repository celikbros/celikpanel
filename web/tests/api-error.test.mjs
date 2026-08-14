import assert from 'node:assert/strict';
import test from 'node:test';

import { apiErrorText, readApiError } from '../src/lib/apiError.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

test('readApiError keeps only string detail lines from JSON errors', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    code: 'service_install_failed',
    details: ['postgresql@18-main is inactive', null, 18, {}, '+2'],
  }), {
    status: 500,
    headers: { 'content-type': 'application/json' },
  });

  const error = await readApiError(response);

  assert.equal(error.message, 'installation failed');
  assert.equal(error.code, 'service_install_failed');
  assert.deepEqual(error.details, ['postgresql@18-main is inactive', '+2']);
});

test('readApiError ignores a non-array details value', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    details: 'not an array',
  }), { status: 500 });

  const error = await readApiError(response);

  assert.equal(error.details, undefined);
});

test('platform refusal codes have localized operator-safe text', () => {
  const translate = (catalog) => (key) => catalog[key] ?? key;
  for (const code of [
    'PLATFORM_CAPABILITY_UNAVAILABLE',
    'PLATFORM_IDENTITY_UNAVAILABLE',
  ]) {
    const remoteText = 'remote secret and command output';
    const english = apiErrorText({ code, message: remoteText }, translate(en));
    const turkish = apiErrorText({ code, message: remoteText }, translate(tr));
    assert.equal(english, en['err.' + code]);
    assert.equal(turkish, tr['err.' + code]);
    assert.ok(english.length > 0 && turkish.length > 0);
    assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
    assert.ok(!english.includes('No host changes were made'));
    assert.ok(!turkish.includes('hiçbir değişiklik yapılmadı'));
  }
});

test('mail profile hostname refusal replaces untrusted transport text in EN and TR', () => {
  const translate = (catalog) => (key) => catalog[key] ?? key;
  const code = 'mail_profile_server_hostname_invalid';
  const remoteText = '/etc/private: hostnamectl failed with secret command output';

  const english = apiErrorText({ code, message: remoteText }, translate(en));
  const turkish = apiErrorText({ code, message: remoteText }, translate(tr));

  assert.equal(english, en['err.' + code]);
  assert.equal(turkish, tr['err.' + code]);
  assert.match(english, /fully qualified domain name \(FQDN\)/);
  assert.match(turkish, /tam nitelikli bir alan adı \(FQDN\)/);
  assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
});
