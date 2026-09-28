import assert from 'node:assert/strict';
import test from 'node:test';
import { readDomainDeletionPending, readDomainDeletionOutcome, readSavedDomainDeletionStatus } from '../src/lib/domainDeletionPending.ts';
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
