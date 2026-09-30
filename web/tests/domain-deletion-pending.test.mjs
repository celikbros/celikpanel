import assert from 'node:assert/strict';
import test from 'node:test';
import { domainDeletionDetailKey, readDomainDeletionPending, readDomainDeletionOutcome, readSavedDomainDeletionStatus, readSavedDomainDeletionState } from '../src/lib/domainDeletionPending.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

function response(status, body) {
  return new Response(JSON.stringify(body), {
    status, headers: { 'content-type': 'application/json' },
  });
}

test('202 exact DNS deletion pending returns only a reviewed reason', async () => {
  const reason = await readDomainDeletionPending(response(202, {
    status: 'deletion_pending',
    stage: 'dns_cleanup',
    reason: 'dns_peer_native_unknown',
    message: 'untrusted peer detail',
  }));
  assert.equal(reason, 'dns_peer_native_unknown');
});

test('generic, malformed and unrelated deletion results have no reason', async () => {
  for (const [status, body] of [
    [202, { status: 'deletion_pending', stage: 'dns_cleanup' }],
    [202, { status: 'deletion_pending', stage: 'dns_cleanup', reason: 'private peer output' }],
    [202, { status: 'deletion_pending', stage: 'site_cleanup', reason: 'dns_peer_native_unknown' }],
    [200, { status: 'deleted', reason: 'dns_peer_native_unknown' }],
    [202, null],
  ]) {
    assert.equal(await readDomainDeletionPending(response(status, body)), '');
  }
});

test('deletion UI classifies 202 as pending, only 200/204 as success', async () => {
  for (const status of [200, 204]) {
    const response = status === 204
      ? new Response(null, { status })
      : responseWithStatus(status);
    assert.deepEqual(await readDomainDeletionOutcome(response), { state: 'succeeded', reason: '' });
  }
  assert.deepEqual(await readDomainDeletionOutcome(response(202, {
    status: 'deletion_pending', stage: 'dns_cleanup',
    reason: 'dns_peer_native_unknown',
  })), { state: 'pending', reason: 'dns_peer_native_unknown' });
  for (const status of [201, 206, 409, 500]) {
    assert.deepEqual(await readDomainDeletionOutcome(response(status, {})), { state: 'error', reason: '' });
  }
});

function responseWithStatus(status) {
  return response(status, {});
}

test('every reviewed pending reason has readable EN/TR copy', () => {
  for (const reason of [
    'dns_peer_enrollment_required',
    'dns_peer_enrollment_changed',
    'dns_peer_inspection_unknown',
    'dns_peer_native_unknown',
    'dns_peer_journal_unknown',
    'dns_peer_owner_edit_unknown',
    'dns_peer_catalog_transfer_refused',
    'dns_peer_proof_internal',
    'bind_rndc_unavailable',
  ]) {
    const key = `err.DNS_PUBLICATION_FAILED.${reason}`;
    assert.match(en[key], /same publication/);
    assert.match(tr[key], /ayn\u0131 yay\u0131n\u0131/);
    assert.doesNotMatch(tr[key], /\?/);
  }
  for (const key of ['domains.deletionPending', 'domains.deletionWaiting', 'domains.retryDeletion', 'domains.checkDeletionStatus']) {
    assert.ok(enScreens[key]);
    assert.ok(trScreens[key]);
    assert.doesNotMatch(trScreens[key], /\?/);
  }
});

test('reload restores only a verified saved deletion marker and reviewed reason', async () => {
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'deletion_pending', stage: 'dns_cleanup',
    reason: 'dns_peer_journal_unknown',
  })), 'dns_peer_journal_unknown');
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'deletion_pending', stage: 'unknown', reason: 'dns_peer_journal_unknown',
  })), '');
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'deletion_pending', stage: 'dns_cleanup', reason: 'private peer output',
  })), '');
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'unknown', stage: 'unknown', reason: 'dns_peer_journal_unknown',
  })), '');
  assert.equal(await readSavedDomainDeletionStatus(new Response(null, { status: 204 })), null);
  assert.equal(await readSavedDomainDeletionStatus(response(404, {})), null);
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'completed', reason: 'dns_peer_journal_unknown',
  })), null);
});

test('rndc guidance names the reason, the owner, the commands and the retry action', async () => {
  assert.equal(await readDomainDeletionPending(response(202, {
    status: 'deletion_pending', stage: 'dns_cleanup', reason: 'bind_rndc_unavailable',
  })), 'bind_rndc_unavailable');
  const en_ = en['err.DNS_PUBLICATION_FAILED.bind_rndc_unavailable'];
  const tr_ = tr['err.DNS_PUBLICATION_FAILED.bind_rndc_unavailable'];
  for (const text of [en_, tr_]) {
    assert.match(text, /rndc key|rndc anahtar/);
    assert.match(text, /sudo rndc-confgen -a/);
    assert.match(text, /sudo systemctl restart named/);
    assert.match(text, /\/etc\/bind\/rndc\.key/);
    assert.doesNotMatch(text, /secret/i);
  }
  assert.ok(en_.includes(`“${enScreens['domains.retryDeletion']}”`));
  assert.ok(tr_.includes(`“${trScreens['domains.retryDeletion']}”`));
  assert.match(en_, /server owner/);
  assert.match(tr_, /sunucunun sahibi/);
});

test('refused local catalog transfer names the secondary owner, the statement, both cases and the retry', async () => {
  assert.equal(await readDomainDeletionPending(response(202, {
    status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_catalog_transfer_refused',
  })), 'dns_peer_catalog_transfer_refused');
  const en_ = en['err.DNS_PUBLICATION_FAILED.dns_peer_catalog_transfer_refused'];
  const tr_ = tr['err.DNS_PUBLICATION_FAILED.dns_peer_catalog_transfer_refused'];
  for (const text of [en_, tr_]) {
    assert.match(text, /allow-transfer/);
    assert.match(text, /127\.0\.0\.1/);
    assert.match(text, /::1/);
    assert.match(text, /CelikPanel/);
  }
  assert.match(en_, /secondary's owner/);
  assert.match(tr_, /ikincil sunucunun sahibi/);
  assert.ok(en_.includes(`“${enScreens['domains.retryDeletion']}”`));
  assert.ok(tr_.includes(`“${trScreens['domains.retryDeletion']}”`));
});

test('only a reviewed inspector detail of an incomplete inspection is kept, with EN/TR copy', async () => {
  const details = ['inspector_policy', 'named_unavailable', 'listeners_unverified', 'catalog_unverified',
    'catalog_transfer_failed', 'catalog_malformed', 'observation_expired', 'config_unreviewed'];
  for (const detail of details) {
    assert.deepEqual(await readDomainDeletionOutcome(response(202, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_inspection_unknown', detail,
    })), { state: 'pending', reason: 'dns_peer_inspection_unknown', detail });
    assert.deepEqual(await readSavedDomainDeletionState(response(200, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_inspection_unknown', detail,
    })), { reason: 'dns_peer_inspection_unknown', detail });
    const key = `domains.peerInspectorDetail.${detail}`;
    assert.match(enScreens[key], /secondary's inspector reported/);
    assert.match(trScreens[key], /denetleyicisi/);
    assert.doesNotMatch(trScreens[key], /\?/);
  }
  for (const [reason, detail] of [
    ['dns_peer_inspection_unknown', 'raw peer stderr'],
    ['dns_peer_inspection_unknown', 'catalog_transfer_refused'],
    ['dns_peer_native_unknown', 'named_unavailable'],
  ]) {
    assert.deepEqual(await readDomainDeletionOutcome(response(202, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason, detail,
    })), { state: 'pending', reason });
    assert.equal((await readSavedDomainDeletionState(response(200, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason, detail,
    }))).detail, '');
  }
});

test('the PowerDNS unreviewed-configuration detail names both recognised configurations', () => {
  const key = 'domains.peerInspectorDetail.config_unreviewed';
  assert.equal(enScreens[key],
    "The secondary's inspector reported that PowerDNS is not running with a configuration it recognises (the panel's own or the documented panel-free one).");
  assert.match(trScreens[key], /PowerDNS/);
  assert.match(trScreens[key], /panelin kendi yapılandırması/);
  assert.match(trScreens[key], /panelsiz yapılandırma/);
});

test('an internal proof failure names no owner change, the owner, the log command and the retry', async () => {
  assert.deepEqual(await readDomainDeletionOutcome(response(202, {
    status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_proof_internal',
  })), { state: 'pending', reason: 'dns_peer_proof_internal' });
  assert.equal(await readSavedDomainDeletionStatus(response(200, {
    status: 'deletion_pending', stage: 'dns_cleanup', reason: 'dns_peer_proof_internal',
  })), 'dns_peer_proof_internal');
  const en_ = en['err.DNS_PUBLICATION_FAILED.dns_peer_proof_internal'];
  const tr_ = tr['err.DNS_PUBLICATION_FAILED.dns_peer_proof_internal'];
  for (const text of [en_, tr_]) {
    assert.ok(text.includes('sudo journalctl -u celikpanel-agent | grep peer'));
    assert.match(text, /CelikPanel Agent/);
    assert.doesNotMatch(text, /secret|password|parola/i);
  }
  assert.match(en_, /could not run its own check of the secondary/);
  assert.match(en_, /No change by either server's owner was found/);
  assert.match(en_, /server owner/);
  assert.match(en_, /Retrying does not help until/);
  assert.match(en_, /DNS answers are unaffected/);
  assert.doesNotMatch(en_, /evidence changed|reconcile/);
  assert.match(tr_, /kendi denetimiyle kontrol edemedi/);
  assert.match(tr_, /değişiklik bulunmadı/);
  assert.match(tr_, /sunucunun sahibi/);
  assert.match(tr_, /DNS yanıtları etkilenmez/);
  assert.doesNotMatch(tr_, /kanıt değişti|uzlaştır/);
  assert.ok(en_.includes(`“${enScreens['domains.retryDeletion']}”`));
  assert.ok(tr_.includes(`“${trScreens['domains.retryDeletion']}”`));
});

test('owner-edit guidance keeps its meaning and names the serial comparison', () => {
  const en_ = en['err.DNS_PUBLICATION_FAILED.dns_peer_owner_edit_unknown'];
  const tr_ = tr['err.DNS_PUBLICATION_FAILED.dns_peer_owner_edit_unknown'];
  assert.match(en_, /evidence changed during verification/);
  assert.match(en_, /compare the catalog zone and zone serials on both servers/);
  assert.match(tr_, /seri numaralarını karşılaştırıp/);
  assert.ok(en_.includes(`“${enScreens['domains.retryDeletion']}”`));
  assert.ok(tr_.includes(`“${trScreens['domains.retryDeletion']}”`));
});

test('owner-edit pending names the check that differed through its own sentence', async () => {
  const checks = ['operation_attempt', 'engine_state', 'active_engine', 'native_binding', 'deletion_receipt',
    'producer_catalog', 'catalog_probe', 'authority', 'transfer_observed', 'zone_answered'];
  const reason = 'dns_peer_owner_edit_unknown';
  for (const detail of checks) {
    assert.deepEqual(await readDomainDeletionOutcome(response(202, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason, detail,
    })), { state: 'pending', reason, detail });
    assert.deepEqual(await readSavedDomainDeletionState(response(200, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason, detail,
    })), { reason, detail });
    const key = domainDeletionDetailKey(reason, detail);
    assert.equal(key, `domains.peerOwnerEditDetail.${detail}`);
    assert.match(enScreens[key], /^What differed: /);
    assert.match(trScreens[key], /^Farklı olan: /);
    assert.doesNotMatch(trScreens[key], /\?/);
    assert.doesNotMatch(enScreens[key] + trScreens[key], /secret|password|parola|\d{6,}/i);
  }
  // A token is accepted only under the reason it refines.
  for (const [r, detail] of [
    ['dns_peer_owner_edit_unknown', 'named_unavailable'],
    ['dns_peer_owner_edit_unknown', 'serial 1790745288'],
    ['dns_peer_inspection_unknown', 'producer_catalog'],
    ['dns_peer_native_unknown', 'producer_catalog'],
  ]) {
    assert.deepEqual(await readDomainDeletionOutcome(response(202, {
      status: 'deletion_pending', stage: 'dns_cleanup', reason: r, detail,
    })), { state: 'pending', reason: r });
    assert.equal(domainDeletionDetailKey(r, detail), null);
  }
  assert.equal(domainDeletionDetailKey('dns_peer_inspection_unknown', 'named_unavailable'),
    'domains.peerInspectorDetail.named_unavailable');
  // The reason text and its retry of the same publication are unchanged.
  assert.match(en['err.DNS_PUBLICATION_FAILED.dns_peer_owner_edit_unknown'], /same publication/);
  assert.match(tr['err.DNS_PUBLICATION_FAILED.dns_peer_owner_edit_unknown'], /aynı yayını/);
});
