import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const mailSource = readFileSync(new URL('../src/components/DomainMailManager.tsx', import.meta.url), 'utf8');
const detailSource = readFileSync(new URL('../src/components/DomainDetail.tsx', import.meta.url), 'utf8');
const enSource = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const trSource = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

function sourceBetween(source, startNeedle, endNeedle) {
  const start = source.indexOf(startNeedle);
  const end = source.indexOf(endNeedle, start + startNeedle.length);
  assert.notEqual(start, -1, 'missing source start: ' + startNeedle);
  assert.notEqual(end, -1, 'missing source end: ' + endNeedle);
  return source.slice(start, end);
}

function occurrences(source, needle) {
  return source.split(needle).length - 1;
}

const rotateSource = sourceBetween(
  mailSource,
  'const rotateAccountPassword = async () =>',
  'const createForwarding = async () =>',
);
const dialogStart = mailSource.indexOf('{passwordAccount && !readOnly && (');
assert.notEqual(dialogStart, -1, 'password dialog must be read-only gated');
const dialogSource = mailSource.slice(dialogStart);

test('mailbox password rotation uses the exact endpoint and minimal secret-bearing body', () => {
  assert.ok(
    rotateSource.includes(
      "fetch(`/api/v1/domains/${domainId}/mail/accounts/password`, {",
    ),
  );
  assert.ok(rotateSource.includes("method: 'PUT'"));
  assert.ok(
    rotateSource.includes(
      'body: JSON.stringify({ id: passwordAccount.id, new_password: newPassword })',
    ),
  );
  assert.ok(!rotateSource.includes('/mail/accounts/password?'));
});

test('mailbox password action and handler both fail closed for read-only access', () => {
  assert.ok(
    rotateSource.includes(
      'if (readOnly || passwordSaving || !passwordAccount || passwordRequestRef.current) return;',
    ),
  );
  const requestGuardIndex = rotateSource.indexOf('passwordRequestRef.current) return;');
  const requestAssignmentIndex = rotateSource.indexOf('passwordRequestRef.current = request;');
  const firstAwaitIndex = rotateSource.indexOf('await ');
  const fetchIndex = rotateSource.indexOf('fetch(');
  assert.ok(requestGuardIndex !== -1 && requestGuardIndex < requestAssignmentIndex);
  assert.ok(requestAssignmentIndex !== -1 && requestAssignmentIndex < firstAwaitIndex);
  assert.ok(requestAssignmentIndex < fetchIndex);
  assert.ok(
    mailSource.includes(
      'const openPasswordDialog = (account: EmailAccount) => {' +
        String.fromCharCode(10) +
        '        if (readOnly) return;',
    ),
  );
  const actionIndex = mailSource.indexOf('onClick={() => openPasswordDialog(a)}');
  const readOnlyGateIndex = mailSource.lastIndexOf('{!readOnly && (', actionIndex);
  assert.notEqual(actionIndex, -1);
  assert.ok(readOnlyGateIndex !== -1 && actionIndex - readOnlyGateIndex < 1000);
  assert.ok(detailSource.includes('readOnly={readOnly}'));
});

test('mailbox password validation enforces the backend byte contract and confirmation', () => {
  assert.ok(mailSource.includes('new TextEncoder().encode(value).byteLength'));
  assert.ok(
    rotateSource.includes(
      'passwordBytes < 8 || passwordBytes > 1024 || newPassword !== passwordConfirmation',
    ),
  );
  assert.equal(occurrences(dialogSource, 'type="password"'), 2);
  assert.equal(occurrences(dialogSource, 'autoComplete="new-password"'), 2);
  assert.equal(occurrences(dialogSource, 'minLength={8}'), 2);
  assert.equal(occurrences(dialogSource, 'maxLength={1024}'), 2);
  assert.ok(dialogSource.includes('noValidate'));
  const newPasswordInput = sourceBetween(dialogSource, 'value={newPassword}', '/>');
  const confirmationInput = sourceBetween(dialogSource, 'value={passwordConfirmation}', '/>');
  assert.ok(newPasswordInput.includes('disabled={passwordSaving}'));
  assert.ok(confirmationInput.includes('disabled={passwordSaving}'));
  assert.ok(
    dialogSource.includes(
      'disabled={readOnly || passwordSaving || !passwordInRange || !passwordMatches}',
    ),
  );
});

test('submitted mailbox secrets are cleared after every outcome and never echoed', () => {
  const finallyStart = rotateSource.indexOf('} finally {');
  assert.notEqual(finallyStart, -1);
  const finallySource = rotateSource.slice(finallyStart);
  assert.ok(finallySource.includes("setNewPassword('');"));
  assert.ok(finallySource.includes("setPasswordConfirmation('');"));
  assert.ok(finallySource.includes('if (succeeded) setPasswordAccount(null);'));

  assert.ok(rotateSource.includes('readApiError(res)'));
  assert.ok(
    rotateSource.includes(
      "apiErrorText({ ...apiError, message: '' }, t, 'mail.passwordUpdateFailed')",
    ),
  );
  const toastLines = rotateSource
    .split(String.fromCharCode(10))
    .filter((line) => line.includes('showToast'));
  for (const toastLine of toastLines) {
    assert.ok(!toastLine.includes('newPassword'));
    assert.ok(!toastLine.includes('passwordConfirmation'));
  }
  assert.ok(!mailSource.includes('localStorage'));
  assert.ok(!mailSource.includes('sessionStorage'));
  assert.ok(!mailSource.includes('console.'));

  const contextReset = sourceBetween(
    mailSource,
    '// A mailbox secret must not survive',
    '}, [domainId, readOnly, activeTab]);',
  );
  assert.ok(contextReset.includes('setPasswordAccount(null);'));
  assert.ok(contextReset.includes("setNewPassword('');"));
  assert.ok(contextReset.includes("setPasswordConfirmation('');"));
  assert.ok(contextReset.includes('passwordRequestRef.current?.abort();'));
  assert.ok(contextReset.includes('setPasswordSaving(false);'));
});

test('password dialog is accessible, identifies the mailbox, and warns about live sessions', () => {
  assert.ok(dialogSource.includes('role="dialog"'));
  assert.ok(dialogSource.includes('aria-modal="true"'));
  assert.ok(dialogSource.includes('aria-labelledby="mail-password-dialog-title"'));
  assert.ok(dialogSource.includes('passwordAccount.address'));
  assert.ok(dialogSource.includes("t('mail.passwordDialog.sessionWarning')"));
  assert.ok(dialogSource.includes("aria-label={t('mail.passwordDialog.close')}"));
});

test('mailbox password and deletion-pending messages stay in EN/TR parity', () => {
  const keys = [
    'err.DOMAIN_DELETION_PENDING',
    'mail.changePasswordFor',
    'mail.passwordDialog.title',
    'mail.passwordDialog.account',
    'mail.passwordDialog.close',
    'mail.passwordDialog.new',
    'mail.passwordDialog.confirm',
    'mail.passwordDialog.requirements',
    'mail.passwordDialog.mismatch',
    'mail.passwordDialog.sessionWarning',
    'mail.passwordDialog.submit',
    'mail.passwordDialog.saving',
    'mail.passwordUpdated',
    'mail.passwordUpdateFailed',
  ];
  for (const key of keys) {
    assert.ok(enSource.includes("'" + key + "':"), 'missing EN key ' + key);
    assert.ok(trSource.includes("'" + key + "':"), 'missing TR key ' + key);
  }
});
