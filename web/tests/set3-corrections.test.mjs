import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { apiErrorText } from '../src/lib/apiError.ts';
import { readServiceActionNote } from '../src/lib/serviceActionNote.ts';
import { isSiteWebServerRefused, siteWebServerRefusedIn } from '../src/lib/siteWebServerRefused.ts';

// 12 Oct 2026, corrections from the final native round that have a screen:
//
//   - a site the web server refused was "internal server error"; it is a plain
//     sentence that says what was removed again and whether that was
//     confirmed, with the command to run and nginx's own line;
//   - an import left an archive entry out without a word; the entry is listed
//     as not imported, and a result whose only missing entries are such
//     entries says that the domain is in service;
//   - a Stop that succeeded and left the unit marked as failed says so;
//   - the update card says, before Start, that the offered version was already
//     tried here and rolled back, with the cause or that none was recorded.
//
// 12 Eki 2026, son yerel turun ekranı olan düzeltmeleri.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const flat = (text) => text.replace(/\s+/g, ' ');
const screens = { en: read('../src/i18n/screens/en.ts'), tr: read('../src/i18n/screens/tr.ts') };
const server = { en: read('../src/i18n/screens/server/en.ts'), tr: read('../src/i18n/screens/server/tr.ts') };
const valueOf = (source, key) => {
  const match = source.match(new RegExp(`^ {4}'${key.replace(/\./g, '\\.')}': ("(?:[^"\\\\]|\\\\.)*"|'(?:[^'\\\\]|\\\\.)*'),$`, 'm'));
  assert.ok(match, `${key} is not in the catalogue`);
  return match[1].startsWith('"') ? JSON.parse(match[1]) : match[1].slice(1, -1);
};
const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().join(',');
const say = (source) => (key, vars) => {
  let text;
  try { text = valueOf(source, key); } catch { return key; }
  for (const [name, value] of Object.entries(vars ?? {})) text = text.replaceAll(`{${name}}`, String(value));
  return text;
};
const guidance = { en: flat(read('../../docs/OPERATION-GUIDANCE.md')), tr: flat(read('../../docs/OPERATION-GUIDANCE.tr.md')) };

const refusedKeys = [
  'domains.add.webServerRefused.removed', 'domains.add.webServerRefused.unconfirmed',
  'import.webServerRefused.removed', 'import.webServerRefused.unconfirmed',
];
const importKeys = ['import.partial.entriesBody', 'import.partial.entriesNext', 'import.part.member', 'import.part.moreMembers', 'import.detail.absoluteMember'];
const noteKeys = ['services.action.note.unit_marked_failed', 'services.action.note.unit_marked_failed_config'];
const updateKeys = ['panelUpdate.previousAttempt.rolledBackTitle', 'panelUpdate.previousAttempt.recovered', 'panelUpdate.previousAttempt.noCause', 'panelUpdate.previousAttempt.again'];

test('every new sentence exists in English and Turkish with the same placeholders, and the guidance records it', () => {
  for (const [catalogue, keys] of [[screens, [...refusedKeys, ...importKeys]], [server, [...noteKeys, ...updateKeys]]]) {
    for (const key of keys) {
      const [english, turkish] = [valueOf(catalogue.en, key), valueOf(catalogue.tr, key)];
      assert.ok(english && turkish && english !== turkish, `${key}: one language repeats the other`);
      assert.equal(placeholders(turkish), placeholders(english), `${key}: the placeholders differ between the languages`);
      for (const [language, document] of Object.entries(guidance)) {
        assert.ok(document.includes(`"${english}"`), `${language}: the English sentence of ${key} is not recorded`);
        assert.ok(document.includes(`"${turkish}"`), `${language}: the Turkish sentence of ${key} is not recorded`);
      }
    }
  }
});

test('a site the web server refused is said in plain words, with only what was verified', async () => {
  const body = (reason) => ({
    message: 'the server sentence', code: 'SITE_WEB_SERVER_REFUSED', reason,
    vars: { domain: 'set3-php.test', command: 'sudo nginx -t' },
    details: ['nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27'],
  });
  const keyOf = { removed: refusedKeys[0], cleanup_unconfirmed: refusedKeys[1], import_removed: refusedKeys[2], import_cleanup_unconfirmed: refusedKeys[3] };
  for (const language of ['en', 'tr']) {
    const t = say(screens[language]);
    for (const [reason, key] of Object.entries(keyOf)) {
      const refusal = siteWebServerRefusedIn(body(reason), t);
      const expected = valueOf(screens[language], key).replaceAll('{domain}', 'set3-php.test').replaceAll('{command}', 'sudo nginx -t');
      assert.equal(refusal.message, expected, `${language} ${reason}`);
      // The one renderer of refusals draws it: no boot-copy sentence stands in
      // front of it, and nginx's line and the reason are kept.
      assert.equal(apiErrorText(refusal, (k) => (language === 'en' ? en : tr)[k] ?? k), expected);
      assert.deepEqual(refusal.details, body(reason).details);
      assert.equal(refusal.reason, reason);
      assert.doesNotMatch(refusal.message, /\{\w+\}|internal server error|SITE_WEB_SERVER|\/etc\/nginx/);
      assert.ok(refusal.message.includes('sudo nginx -t') && refusal.message.includes('set3-php.test'));
    }
    const sentence = (key) => valueOf(screens[language], key);
    // Removed and confirmed, or not confirmed: never both, never "nothing was left".
    const confirmed = language === 'en' ? /the removal was confirmed/ : /bu kaldırma doğrulandı/;
    const unconfirmed = language === 'en' ? /was not confirmed, so parts of it may remain/ : /doğrulanamadı; bu yüzden bir bölümü sunucuda kalmış olabilir/;
    for (const key of [refusedKeys[0], refusedKeys[2]]) { assert.match(sentence(key), confirmed); assert.doesNotMatch(sentence(key), unconfirmed); }
    for (const key of [refusedKeys[1], refusedKeys[3]]) { assert.match(sentence(key), unconfirmed); assert.doesNotMatch(sentence(key), confirmed); }
    for (const key of refusedKeys) {
      assert.doesNotMatch(sentence(key), /nothing was left behind|hiçbir şey kalmadı/i);
      assert.match(sentence(key), language === 'en' ? /nothing (retries by itself|starts it again automatically)\.$/ : /hiçbir şey (kendiliğinden yeniden denemez|onu kendiliğinden yeniden başlatmaz)\.$/);
    }
    // The import's two also say that nothing of the archive was imported.
    for (const key of refusedKeys.slice(2)) {
      assert.match(sentence(key), language === 'en' ? /^The import did not start, and no file, mailbox, DNS record or database of the archive was imported\./ : /^İçe aktarma başlamadı ve arşivden hiçbir dosya, posta kutusu, DNS kaydı ya da veritabanı içe aktarılmadı\./);
    }
  }
  // A reason this build has no words for, and another refusal, keep what they had.
  const later = { ...body('later_reason') };
  assert.equal(siteWebServerRefusedIn(later, say(screens.en)).message, 'the server sentence');
  const other = { message: 'x', code: 'DNS_SERVER_REQUIRED' };
  assert.equal(siteWebServerRefusedIn(other, say(screens.en)), other);
  assert.equal(isSiteWebServerRefused(null), false);
  // The sentences are not in the boot copy.
  for (const catalogue of [en, tr]) assert.equal(Object.keys(catalogue).filter((key) => key.includes('WEB_SERVER_REFUSED')).length, 0);

  // Both screens that create a site keep it on the page.
  const dialog = read('../src/components/AddDomainModal.tsx');
  assert.match(dialog, /if \(isSiteWebServerRefused\(apiErr\)\) \{[^}]*setError\(siteWebServerRefusedIn\(apiErr, t\)\);\s*return;\s*\}/);
  assert.ok(dialog.indexOf('isSiteWebServerRefused(apiErr)') < dialog.indexOf("showToast('error', apiErrorText(apiErr, t, 'domains.add.failed'))"), 'the refusal still leaves with a toast');
  assert.match(read('../src/components/ImportPage.tsx'), /setRefusal\(siteWebServerRefusedIn\(await readApiError\(res\), t\)\);/);
  // ErrorBanner draws the server's line under the sentence.
  assert.match(read('../src/components/ui.tsx'), /error\.details && error\.details\.length > 0 && \(/);
});

test('an archive entry that was refused is listed, and it does not leave the domain unfinished', () => {
  const page = read('../src/components/ImportPage.tsx');
  assert.match(page, /member: 'import\.part\.member',\s*members: 'import\.part\.moreMembers',/);
  assert.match(page, /const refusedEntry = \(step: string\) => step\.startsWith\('member:'\) \|\| step\.startsWith\('members:'\);/);
  assert.match(page, /result\.notImported\.length === 0 \? 'import\.partial\.unfinished'\s*: result\.notImported\.every\(refusedEntry\) \? 'import\.partial\.entriesBody'\s*: 'import\.partial\.body'/);
  // The server's line for an absolute name has the page's own words, and it is
  // the line the server sends.
  const line = page.match(/const absoluteMemberDetail = "([^"]+)";/)?.[1];
  assert.ok(line, 'the page does not know the line');
  assert.ok(flat(read('../../cmd/panel/import_handlers.go')).includes(`const importRefusedMemberAbsolute = "${line}"`), 'the page and the server disagree on the line');
  assert.match(page, /\[absoluteMemberDetail\]: 'import\.detail\.absoluteMember',/);
  assert.match(page, /\{detailKeys\[s\.detail\] \? t\(detailKeys\[s\.detail\]\) : s\.detail\}/);
  for (const language of ['en', 'tr']) {
    const value = (key) => valueOf(screens[language], key);
    assert.ok(value('import.part.member').includes('{name}') && value('import.part.moreMembers').includes('{name}'));
    assert.ok(value('import.partial.entriesBody').includes('{domain}') && value('import.partial.entriesNext').includes('{domain}'));
    assert.doesNotMatch(value('import.partial.entriesBody'), /not finished|bitmemiş/);
    assert.match(value('import.partial.entriesBody'), language === 'en' ? /Everything you chose was imported, and \{domain\} is in service/ : /Seçtiğiniz her şey içe aktarıldı ve \{domain\} yayında/);
    assert.match(value('import.detail.absoluteMember'), language === 'en' ? /Nothing was written for it\.$/ : /hiçbir şey yazılmadı\.$/);
  }
});

test('a Stop that left the unit marked as failed is said as a note, not as a failure', async () => {
  const notice = read('../src/components/ServiceActionNotice.tsx');
  {
    const answer = {
      success: true, outcome: 'verified', applied: 'stopped',
      note: { code: 'SERVICE_ACTION_NOTE', reason: 'unit_marked_failed_config', error: 'server sentence',
        vars: { unit: 'postfix', failed_unit: 'postfix.service', result: 'exit-code', command: 'sudo systemctl reset-failed postfix.service', detail: 'postfix: fatal: bad numerical configuration', odd: 7 } },
    };
    assert.deepEqual(readServiceActionNote(answer), {
      message: 'server sentence', code: 'SERVICE_ACTION_NOTE', reason: 'unit_marked_failed_config',
      vars: { unit: 'postfix', failed_unit: 'postfix.service', result: 'exit-code', command: 'sudo systemctl reset-failed postfix.service', detail: 'postfix: fatal: bad numerical configuration' },
    });
    for (const none of [null, 'x', {}, { success: true }, { note: null }, { note: { code: 'SERVICE_ACTION_FAILED' } }]) assert.equal(readServiceActionNote(none), null);
  }
  assert.match(notice, /role=\{note \? 'status' : 'alert'\}/);
  assert.match(notice, /data-service-action=\{note \? 'note' : unknown \? 'unknown' : 'failed'\}/);
  for (const reason of ['unit_marked_failed', 'unit_marked_failed_config']) assert.match(notice, new RegExp(`${reason}: 'services\\.action\\.note\\.${reason}'`));
  // A reason this page has no words for keeps the server's sentence.
  assert.match(notice, /const noteKey = note \? noteSentences\[outcome\.reason \?\? ''\] : undefined;/);
  // Each screen that sends an action reads the note of a success; an answer
  // that cannot be read is an unknown result, not "no note".
  const list = read('../src/components/ServiceList.tsx');
  assert.equal([...list.matchAll(/const note = readServiceActionNote\(await res\.json\(\)\);\s*if \(note\) setActionOutcome\(note\);/g)].length, 2);
  assert.match(read('../src/components/ServiceShell.tsx'), /try \{\s*answer = await r\.json\(\);\s*\} catch \{[^}]*showToast\('warning', t\('common\.resultUnknown'\)\);\s*await load\(\);\s*return;\s*\}\s*const note = readServiceActionNote\(answer\);\s*if \(note\) setActionOutcome\(note\);/);
  for (const language of ['en', 'tr']) {
    for (const key of noteKeys) {
      const sentence = valueOf(server[language], key);
      assert.equal(placeholders(sentence), key.endsWith('config') ? 'command,failed_unit,result,unit,unit' : 'command,failed_unit,result,unit', key);
      assert.doesNotMatch(sentence, /reset-failed|did not stop|durdurulamadı/, `${language} ${key}`);
      assert.match(sentence, language === 'en' ? /^\{unit\} was stopped and is not running\./ : /^\{unit\} durduruldu ve çalışmıyor\./);
      assert.match(sentence, language === 'en' ? /CelikPanel leaves it as it is/ : /CelikPanel onu olduğu gibi bırakır/);
    }
  }
});

test('the update card says before Start that the offered version was already tried here and rolled back', () => {
  const card = read('../src/components/PanelUpdateCard.tsx');
  assert.match(card, /previousAttempt\.phase === 'recovered' \? 'panelUpdate\.previousAttempt\.rolledBackTitle'/);
  assert.match(card, /\{!previousStopped && \(\s*<p className="mt-1 text-fg-muted">\s*\{previousAttempt\.failure_code\s*\? t\('panelUpdate\.previousAttempt\.cause', \{ cause: t\(`recovery\.reason\.\$\{previousAttempt\.failure_code\}`\) \}\)\s*: t\('panelUpdate\.previousAttempt\.noCause'\)\}/);
  assert.match(card, /\{previousAttempt\.phase === 'recovered' && \(\s*<p className="mt-1 text-fg-muted">\{t\('panelUpdate\.previousAttempt\.again', \{ version: target\.version \}\)\}<\/p>/);
  // The notice is guidance: nothing in it disables Start or hides the version.
  const start = card.slice(card.indexOf('id="panel-update-start-button"'));
  assert.match(start, /disabled=\{starting \|\| readinessChecking \|\| readiness\?\.ready !== true\}/);
  assert.doesNotMatch(start.slice(0, 400), /previousAttempt/);
  for (const language of ['en', 'tr']) {
    const value = (key) => valueOf(server[language], key);
    assert.equal(placeholders(value('panelUpdate.previousAttempt.recovered')), 'current,time,version');
    assert.match(value('panelUpdate.previousAttempt.rolledBackTitle'), language === 'en' ? /already tried on this server and rolled back/ : /daha önce denendi ve geri alındı/);
    assert.match(value('panelUpdate.previousAttempt.recovered'), language === 'en' ? /did not complete, and the server was returned to \{current\}, which it runs now\.$/ : /tamamlanmadı ve sunucu \{current\} sürümüne döndürüldü; şu an onu çalıştırıyor\.$/);
    assert.match(value('panelUpdate.previousAttempt.again'), language === 'en' ? /The button below still starts \{version\}\.$/ : /Aşağıdaki düğme \{version\} sürümünü yine başlatır\.$/);
    assert.doesNotMatch(value('panelUpdate.previousAttempt.again'), /cannot|blocked|disabled|engellen|kapalı/);
  }
});
