import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

// 11 Oct 2026, corrections from the second native measurement that have a
// screen:
//
//   - the import preview received each mailbox's password hash; it now gets,
//     and shows, only whether the archive holds a password the import keeps;
//   - an import that ended with a part not imported was shown under "Import
//     finished" with the server's word "pending"; it is a verified partial
//     result, with what was imported, what was not and what to do;
//   - a failed reload said "keeps running with the settings it had" without
//     anyone having read that;
//   - a certificate request certbot did not fulfil was "internal server error"
//     in a toast.
//
// 11 Eki 2026, ikinci yerel ölçümün ekranı olan düzeltmeleri.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const flat = (text) => text.replace(/\s+/g, ' ');
const screens = { en: read('../src/i18n/screens/en.ts'), tr: read('../src/i18n/screens/tr.ts') };
const valueOf = (source, key) => {
  const match = source.match(new RegExp(`^ {4}'${key.replace(/\./g, '\\.')}': ("(?:[^"\\\\]|\\\\.)*"),$`, 'm'));
  assert.ok(match, `${key} is not in the catalogue`);
  return JSON.parse(match[1]);
};
const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().join(',');
const importPage = read('../src/components/ImportPage.tsx');
const guidance = { en: flat(read('../../docs/OPERATION-GUIDANCE.md')), tr: flat(read('../../docs/OPERATION-GUIDANCE.tr.md')) };

const importKeys = [
  'import.mailPasswords.all', 'import.mailPasswords.some', 'import.inspectUnreadable', 'import.siteNotCreated', 'import.result.complete',
  'import.partial.title', 'import.partial.body', 'import.partial.unfinished', 'import.partial.imported', 'import.partial.notImported',
  'import.partial.next', 'import.partial.domains', 'import.stepsTitle', 'import.step.done', 'import.step.notDone', 'import.part.domain',
  'import.part.files', 'import.part.mail', 'import.part.mailbox', 'import.part.forwarders', 'import.part.forwarder', 'import.part.dns',
  'import.part.databases', 'import.part.database', 'import.part.finalize', 'import.detail.noPassword',
];
const certificateKinds = ['authority_unreachable', 'validation', 'rate_limited', 'timeout', 'tool'];
const certificateKeys = [...certificateKinds.map((kind) => `ssl.issueFailure.${kind}`), 'ssl.issueFailure.said'];
const reloadKeys = ['reload', 'reload_reread', 'reload_not_reread', 'not_running'].map((reason) => `err.SERVICE_ACTION_FAILED.${reason}`);

test('the import page never holds a password hash: it reads and shows one fact per mailbox', () => {
  assert.doesNotMatch(importPage, /crypt_hash|CryptHash|password_hash/, 'the page names a hash field');
  assert.match(importPage, /mail_accounts: \{ domain: string; user: string; quota_mb: number; has_password: boolean \}\[\];/);
  // The preview is rebuilt field by field from the answer, so a field the
  // server were to send besides these never reaches the page's state.
  assert.match(importPage, /has_password: m\?\.has_password === true,/);
  assert.match(importPage, /p = readPreview\(await res\.json\(\)\);/);
  assert.doesNotMatch(importPage, /const p: Preview = await res\.json\(\)/, 'the answer is kept as it came');
  // An answer that is not a preview is not drawn as an empty archive.
  assert.match(importPage, /if \(!lists\.every\(Array\.isArray\) \|\| !p\.dns_zones \|\| typeof p\.dns_zones !== 'object'\) return null;/);
  assert.match(importPage, /showToast\('error', t\('import\.inspectUnreadable'\)\);/);
  // What the apply sends: the archive's path and the owner's choices.
  const sent = importPage.slice(importPage.indexOf('body: JSON.stringify({', importPage.indexOf('const runImport')), importPage.indexOf('applyRequest.current = request;'));
  assert.deepEqual([...sent.matchAll(/^\s+(\w+)[:,]/gm)].map((match) => match[1]),
    ['path', 'subscription_id', 'domain', 'do_files', 'do_mail', 'do_dns', 'do_databases']);
  assert.match(importPage, /t\('import\.mailPasswords\.all'\)/);
  assert.match(importPage, /t\('import\.mailPasswords\.some', \{/);
});

test('an import that ended is complete or a verified partial result, never "pending"', () => {
  assert.doesNotMatch(importPage, /[=!]==? ?['"]pending['"]|: ?'pending'/, 'the page still decides on a pending import');
  assert.match(importPage, /partial: data\.status === 'partial' \|\| steps\.some\(\(s\) => !s\.ok\),/);
  // The summary stands before the steps and is announced; it is the attention
  // surface, not the failure surface: most of the archive is on the server.
  const result = importPage.slice(importPage.indexOf("{stage === 'result' && result && ("));
  assert.ok(result.indexOf("t('import.partial.title'") < result.indexOf("t('import.stepsTitle')"), 'the steps come before what the import came to');
  assert.match(result, /<div role="alert" className="rounded-lg border border-warning-mark\/50 bg-warning-mark\/20/);
  // What to do next: the sentence of a missing part, or (12 Oct 2026) the one
  // for a result whose only missing entries the archive names in a way no
  // import places.
  assert.match(result, /t\(result\.notImported\.length > 0 && result\.notImported\.every\(refusedEntry\) \? 'import\.partial\.entriesNext' : 'import\.partial\.next', \{ domain: result\.domain \}\)/);
  assert.match(result, /navigate\(`\/domains\/\$\{encodeURIComponent\(result\.domain\)\}`\)/);
  // "Import finished" is the heading of a complete import only.
  assert.equal([...importPage.matchAll(/t\('import\.resultTitle'\)/g)].length, 1);
  assert.ok(result.indexOf("t('import.resultTitle')") > result.indexOf(') : ('), '"Import finished" heads a partial result');
  // A step is named in the page's own words, in both languages.
  for (const step of ['domain', 'files', 'mail', 'forwarders', 'dns', 'databases', 'finalize']) {
    assert.match(importPage, new RegExp(`${step}: 'import\\.part\\.${step}'`));
  }
  // A refusal of the Panel stays on the page.
  assert.doesNotMatch(importPage, /showToast\('error', apiErrorText\(/, 'a refusal with something to correct still leaves with a toast');
  assert.equal([...importPage.matchAll(/<ErrorBanner error=\{/g)].length, 2);
});

test('every new sentence exists in English and Turkish with the same placeholders, and the guidance records it', () => {
  for (const key of [...importKeys, ...certificateKeys]) {
    const [english, turkish] = [valueOf(screens.en, key), valueOf(screens.tr, key)];
    assert.ok(english && turkish && english !== turkish, `${key}: one language repeats the other`);
    assert.equal(placeholders(turkish), placeholders(english), `${key}: the placeholders differ between the languages`);
    for (const [language, document] of Object.entries(guidance)) {
      assert.ok(document.includes(`"${english}"`), `${language}: the English sentence of ${key} is not recorded`);
      assert.ok(document.includes(`"${turkish}"`), `${language}: the Turkish sentence of ${key} is not recorded`);
    }
  }
  for (const key of reloadKeys) {
    assert.equal(placeholders(tr[key]), placeholders(en[key]), `${key}: the placeholders differ between the languages`);
    assert.ok(en[key].includes('{unit}') && en[key].includes('{command}'), `${key} does not name the unit and the command`);
  }
});

test('a failed reload claims a state only when the server was asked', () => {
  const claims = /keeps running with the settings it had|önceki ayarlarıyla çalışmayı sürdürüyor/;
  for (const catalogue of [en, tr]) {
    for (const [key, sentence] of Object.entries(catalogue)) {
      if (key.startsWith('err.SERVICE_ACTION')) assert.doesNotMatch(sentence, claims, `${key} still says it`);
    }
  }
  assert.match(en['err.SERVICE_ACTION_FAILED.reload'], /says neither that it kept the settings it had nor that it took the files on disk/);
  assert.match(tr['err.SERVICE_ACTION_FAILED.reload'], /ne önceki ayarlarını koruduğunu ne de diskteki dosyaları aldığını söylüyor/);
  assert.match(en['err.SERVICE_ACTION_FAILED.reload_reread'], /PostgreSQL itself re-read its configuration files/);
  assert.match(en['err.SERVICE_ACTION_FAILED.reload_not_reread'], /did not re-read its configuration files: it is running with the settings it had before/);
  // Not running: nothing to reload, nothing changed, and Start is the action.
  assert.match(en['err.SERVICE_ACTION_FAILED.not_running'], /^\{unit\} is not running, so there was nothing to reload and nothing was changed\. If it should run, use Start here/);
  assert.match(tr['err.SERVICE_ACTION_FAILED.not_running'], /^\{unit\} çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu ve hiçbir şey değiştirilmedi\. Çalışması gerekiyorsa burada Başlat’ı kullanın/);
});

test('a certificate request certbot did not fulfil stays on the page with its kind', () => {
  const notice = read('../src/components/CertificateIssueNotice.tsx');
  const settings = read('../src/components/DomainSSLSettings.tsx');
  assert.match(notice, /return error\.code === 'CERTIFICATE_ISSUE_FAILED';/);
  assert.match(notice, /role="alert"/);
  for (const kind of certificateKinds) assert.match(notice, new RegExp(`${kind}: 'ssl\\.issueFailure\\.${kind}'`));
  // A kind this page has no words for gets the general sentence, never a key.
  assert.match(notice, /t\(sentences\[failure\.reason \?\? ''\] \?\? 'ssl\.issueFailure\.tool', \{ domain: failure\.vars\?\.domain \?\? '' \}\)/);
  assert.match(settings, /if \(isCertificateIssueFailure\(apiError\)\) \{\s*setIssueFailure\(apiError\);\s*return;/);
  assert.match(settings, /<CertificateIssueNotice failure=\{issueFailure\} onClose=\{\(\) => setIssueFailure\(null\)\}/);
  // The next request clears the notice of the one before.
  assert.match(settings, /setIssuing\(true\);\s*setIssueFailure\(null\);/);
  for (const language of ['en', 'tr']) {
    for (const kind of certificateKinds) {
      const sentence = valueOf(screens[language], `ssl.issueFailure.${kind}`);
      assert.ok(sentence.includes('{domain}'), `${language} ${kind} does not name the domain`);
      assert.doesNotMatch(sentence, /internal server error|CERTIFICATE_ISSUE/);
      assert.match(sentence, /othing asks again automatically|kendiliğinden yeniden istemez/, `${language} ${kind} does not say that nothing asks again`);
    }
    assert.ok(valueOf(screens[language], 'ssl.issueFailure.said').includes('{detail}'));
  }
});

test('a password the caller typed is not expected back from the server', () => {
  assert.match(read('../src/components/AddDatabaseModalV2.tsx'), /typeof data\.password === 'string' \? `User: \$\{data\.user\}, Password: \$\{data\.password\}` : `User: \$\{data\.user\}`/);
  assert.match(read('../src/components/AddUserModalV2.tsx'), /if \(typeof data\.password === 'string'\) showToast\('info', `Password: \$\{data\.password\}`\);/);
});
