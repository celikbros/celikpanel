import assert from 'node:assert/strict';
import test from 'node:test';

import { apiErrorText, readApiError } from '../src/lib/apiError.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

test('readApiError keeps only string detail lines from JSON errors', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    code: 'service_install_failed',
    partial_success: true,
    mutation_applied: true,
    details: ['postgresql@18-main is inactive', null, 18, {}, '+2'],
  }), {
    status: 500,
    headers: { 'content-type': 'application/json' },
  });

  const error = await readApiError(response);

  assert.equal(error.message, 'installation failed');
  assert.equal(error.code, 'service_install_failed');
  assert.equal(error.partialSuccess, true);
  assert.equal(error.mutationApplied, true);
  assert.deepEqual(error.details, ['postgresql@18-main is inactive', '+2']);
});

test('readApiError ignores a non-array details value', async () => {
  const response = new Response(JSON.stringify({
    error: 'installation failed',
    partial_success: 'true',
    mutation_applied: false,
    details: 'not an array',
  }), { status: 500 });

  const error = await readApiError(response);

  assert.equal(error.details, undefined);
  assert.equal(error.partialSuccess, undefined);
  assert.equal(error.mutationApplied, undefined);
});

test('DNS engine outcome codes replace remote text in EN and TR', () => {
  const translate = (catalog) => (key) => catalog[key] ?? key;
  for (const code of [
    'DNS_ENGINE_PLAN_REJECTED',
    'DNS_ENGINE_CHANGE_NOT_COMMITTED',
    'DNS_ENGINE_STATE_UNVERIFIED',
    'DNS_ENGINE_CHANGE_APPLIED_REFRESH_REQUIRED',
  ]) {
    const remoteText = '/etc/bind/private token=do-not-leak';
    const english = apiErrorText({ code, message: remoteText }, translate(en));
    const turkish = apiErrorText({ code, message: remoteText }, translate(tr));
    assert.equal(english, en['err.' + code]);
    assert.equal(turkish, tr['err.' + code]);
    assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
  }
});

test('DNS engine not-committed copy does not overclaim the target serving state', () => {
  assert.equal(
    en['err.DNS_ENGINE_CHANGE_NOT_COMMITTED'],
    'The DNS engine change was not committed. The pre-operation serving state was verified; packages or setup files may still have changed. Refresh state before creating a new review.',
  );
  assert.equal(
    tr['err.DNS_ENGINE_CHANGE_NOT_COMMITTED'],
    'DNS motoru değişikliği kesinleştirilmedi. İşlem öncesindeki hizmet durumu doğrulandı; paketler veya kurulum dosyaları yine de değişmiş olabilir. Yeni bir inceleme oluşturmadan önce DNS durumunu yenileyin.',
  );
  assert.doesNotMatch(en['err.DNS_ENGINE_CHANGE_NOT_COMMITTED'], /not serving|reverted/i);
  assert.doesNotMatch(tr['err.DNS_ENGINE_CHANGE_NOT_COMMITTED'], /hizmet vermiyor|geri alınd/i);
});

test('DNS engine plan-rejected copy is mode-neutral', () => {
  assert.equal(
    en['err.DNS_ENGINE_PLAN_REJECTED'],
    'The server agent rejected the reviewed DNS plan. The DNS engine change was not committed. Verify current DNS state before creating a new review; do not retry the old confirmation.',
  );
  assert.equal(
    tr['err.DNS_ENGINE_PLAN_REJECTED'],
    'Sunucu agentı incelenen DNS planını reddetti. DNS motoru değişikliği kesinleştirilmedi. Yeni bir inceleme oluşturmadan önce güncel DNS durumunu doğrulayın; eski onayı yeniden denemeyin.',
  );
  assert.doesNotMatch(en['err.DNS_ENGINE_PLAN_REJECTED'], /activation|not serving|stopped/i);
  assert.doesNotMatch(tr['err.DNS_ENGINE_PLAN_REJECTED'], /etkinleştirme|hizmet vermiyor|durdur/i);
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
  assert.match(english, /fully qualified domain name/);
  assert.match(turkish, /tam nitelikli bir alan adı/);
  assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
});

// R-036. The refusal that used to be a dead end now names a field to fill, and
// it too replaces whatever the remote text was.
test('mail profile missing-hostname refusal names the field in EN and TR', () => {
  const translate = (catalog) => (key) => catalog[key] ?? key;
  const code = 'mail_profile_server_hostname_required';
  const remoteText = '/etc/private: hostname read failed with secret command output';

  const english = apiErrorText({ code, message: remoteText }, translate(en));
  const turkish = apiErrorText({ code, message: remoteText }, translate(tr));

  assert.equal(english, en['err.' + code]);
  assert.equal(turkish, tr['err.' + code]);
  assert.match(english, /mail\.example\.com/);
  assert.match(turkish, /mail\.example\.com/);
  assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
});

// R-035. Each of the three SSH outcomes is its own translated sentence, so the
// browser never shows the agent's own English back to the operator.
test('firewall SSH refusals are three separate translated sentences', () => {
  const translate = (catalog) => (key) => catalog[key] ?? key;
  const remoteText = 'ss permission denied: /usr/sbin/ss exited 1';
  const codes = [
    'FIREWALL_NO_SSH_SERVICE',
    'FIREWALL_SSH_NOT_LISTENING',
    'FIREWALL_SSH_DISCOVERY_FAILED',
  ];
  const seen = new Set();
  for (const code of codes) {
    const english = apiErrorText({ code, message: remoteText }, translate(en));
    const turkish = apiErrorText({ code, message: remoteText }, translate(tr));
    assert.equal(english, en['err.' + code]);
    assert.equal(turkish, tr['err.' + code]);
    assert.ok(english !== 'err.' + code && turkish !== 'err.' + code);
    assert.ok(!english.includes(remoteText) && !turkish.includes(remoteText));
    assert.ok(!seen.has(english), `${code} reuses another refusal's sentence`);
    seen.add(english);
  }
});
