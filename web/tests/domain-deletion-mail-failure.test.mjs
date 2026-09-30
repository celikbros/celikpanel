import assert from 'node:assert/strict';
import test from 'node:test';
import {
  domainDeletionReasonKey,
  readDomainDeletionOutcome,
  readDomainDeletionPending,
  readSavedDomainDeletionState,
  readSavedDomainDeletionStatus,
} from '../src/lib/domainDeletionPending.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// P4-2 (pair 4 t2): a verified mail-stage failure of a domain deletion.
const REASON = 'mail_runtime_cleanup_failed';
const KEY = `err.DOMAIN_DELETION_FAILED.${REASON}`;

function response(status, body) {
  return new Response(JSON.stringify(body), {
    status, headers: { 'content-type': 'application/json' },
  });
}

test('202 at the mail stage returns the reviewed failure reason only for that stage', async () => {
  const body = {
    status: 'deletion_pending', stage: 'mail_runtime_cleanup', reason: REASON,
    error_line: 'open mail root for domain quarantine: too many levels of symbolic links',
  };
  assert.equal(await readDomainDeletionPending(response(202, body)), REASON);
  assert.deepEqual(await readDomainDeletionOutcome(response(202, body)), { state: 'pending', reason: REASON });
  for (const other of [
    { ...body, stage: 'site_cleanup' },
    { ...body, stage: 'dns_cleanup' },
    { ...body, reason: 'made_up' },
    { status: 'deletion_pending', stage: 'mail_runtime_cleanup' },
  ]) {
    assert.equal(await readDomainDeletionPending(response(202, other)), '');
  }
});

test('the saved status reports a recorded failure, not unknown', async () => {
  const saved = { status: 'failed', stage: 'mail_runtime_cleanup', reason: REASON, error_line: 'x' };
  assert.deepEqual(await readSavedDomainDeletionState(response(200, saved)), { reason: REASON, detail: '' });
  assert.equal(await readSavedDomainDeletionStatus(response(200, saved)), REASON);
  // A failed status with an unreviewed reason keeps the marker but shows the generic text.
  assert.equal(await readSavedDomainDeletionStatus(response(200, { ...saved, reason: 'other' })), '');
  assert.equal(await readSavedDomainDeletionStatus(response(200, { ...saved, stage: 'dns_cleanup' })), '');
});

test('reason keys: mail failure has its own key, DNS reasons keep theirs', () => {
  assert.equal(domainDeletionReasonKey(REASON), KEY);
  assert.equal(domainDeletionReasonKey('bind_rndc_unavailable'), 'err.DNS_PUBLICATION_FAILED.bind_rndc_unavailable');
  assert.equal(domainDeletionReasonKey(''), null);
});

test('mail failure guidance names what failed, the actor, the action and the same retry (EN/TR)', () => {
  const english = en[KEY];
  const turkish = tr[KEY];
  assert.ok(english && turkish);
  // What failed, on which server, and what is untouched.
  assert.match(english, /removing this domain’s mail from this server/);
  assert.match(english, /DNS zone and the website are unchanged/);
  assert.match(turkish, /postası bu sunucudan kaldırılırken durdu/);
  assert.match(turkish, /DNS bölgesi ve web sitesi değişmedi/);
  // Who acts.
  assert.match(english, /server owner/);
  assert.match(turkish, /sunucunun sahibi/);
  // The concrete action.
  for (const text of [english, turkish]) {
    assert.match(text, /\/var\/mail\/vhosts/);
    assert.match(text, /\/var\/spool\/mail\/vhosts/);
    assert.match(text, /journalctl -u celikpanel-panel/);
    assert.doesNotMatch(text, /secret|password|parola/i);
  }
  // How work resumes: the existing retry label and the same deletion.
  assert.ok(english.includes(`“${enScreens['domains.retryDeletion']}”`));
  assert.ok(turkish.includes(`“${trScreens['domains.retryDeletion']}”`));
  assert.match(english, /this same deletion/);
  assert.match(turkish, /aynı silme işlemini/);
  assert.doesNotMatch(turkish, /\?/);
});
