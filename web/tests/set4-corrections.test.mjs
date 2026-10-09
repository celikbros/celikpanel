import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// 9 Oct 2026, corrections after the set4 native measurement that have a screen:
//
//   - an import listed `dns` as imported although DNS was not chosen and the
//     server's DNS is the owner's external provider. A step that ended
//     without an error and imported nothing is now neither imported nor
//     missing, on the page as in the answer;
//   - the update card's sentence put the attempt's end time beside "started"
//     (pinned with the card's other sentences in set3-corrections.test.mjs);
//   - a Stop is now answered once the unit has settled; when it has not
//     settled within the wait, or cannot be read, the success says so in a
//     note of its own instead of saying nothing.
//
// 9 Eki 2026, set4 yerel ölçümünden sonra ekranı olan düzeltmeler.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const flat = (text) => text.replace(/\s+/g, ' ');
const screens = { en: read('../src/i18n/screens/en.ts'), tr: read('../src/i18n/screens/tr.ts') };
const server = { en: read('../src/i18n/screens/server/en.ts'), tr: read('../src/i18n/screens/server/tr.ts') };
const guidance = { en: flat(read('../../docs/OPERATION-GUIDANCE.md')), tr: flat(read('../../docs/OPERATION-GUIDANCE.tr.md')) };
const importPage = read('../src/components/ImportPage.tsx');
const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().filter((name, index, all) => all.indexOf(name) === index).join(',');
const valueOf = (source, key) => {
  const match = source.match(new RegExp(`^ {4}'${key.replace(/\./g, '\\.')}': ("(?:[^"\\\\]|\\\\.)*"|'(?:[^'\\\\]|\\\\.)*'),$`, 'm'));
  assert.ok(match, `${key} is not in the catalogue`);
  return match[1].startsWith('"') ? JSON.parse(match[1]) : match[1].slice(1, -1).replace(/\\'/g, "'");
};

test('a step that imported nothing is neither listed as imported nor as missing', () => {
  // The answer's `state` is kept per step; anything that is not a string is no state.
  assert.match(importPage, /state: typeof \(s as StepResult\)\?\.state === 'string' \? \(s as StepResult\)\.state : '',/);
  // Imported: ended without an error and has no state. Missing: did not end well.
  assert.match(importPage, /imported: parts\.filter\(\(s\) => s\.ok && !s\.state\)\.map\(\(s\) => s\.step\),/);
  assert.match(importPage, /notImported: parts\.filter\(\(s\) => !s\.ok\)\.map\(\(s\) => s\.step\),/);
  // Such a step does not make the result partial: only a step that did not end well does.
  assert.match(importPage, /partial: data\.status === 'partial' \|\| steps\.some\(\(s\) => !s\.ok\),/);
  // In the list of steps it has its own mark and its own words for a screen
  // reader; a failure is decided first, so a failed step is never shown as this.
  const list = importPage.slice(importPage.indexOf('{result.steps.map((s, i) => ('));
  assert.match(list, /\{!s\.ok \? \(\s*<XCircle [^>]*text-danger[^>]*\/>\s*\) : s\.state \? \(\s*<MinusCircle [^>]*text-fg-muted[^>]*aria-hidden="true" \/>\s*\) : \(\s*<CheckCircle2 [^>]*text-success[^>]*\/>\s*\)\}/);
  assert.match(list, /<span className="sr-only">: \{t\(!s\.ok \? 'import\.step\.notDone' : s\.state \? 'import\.step\.nothing' : 'import\.step\.done'\)\}<\/span>/);
});

test('a Stop whose unit was not read as settled has its own words, on the attention surface', () => {
  const notice = read('../src/components/ServiceActionNotice.tsx');
  // The two reasons the server sends when the unit's own stop was not read.
  assert.match(notice, /unit_not_settled: 'services\.action\.note\.unit_not_settled',/);
  assert.match(notice, /unit_state_not_read: 'services\.action\.note\.unit_state_not_read',/);
  // A note is never drawn as a failure, whatever its reason.
  assert.match(notice, /const note = outcome\.code === 'SERVICE_ACTION_NOTE';/);
  assert.match(notice, /const attention = unknown \|\| note;/);
  assert.match(notice, /role=\{note \? 'status' : 'alert'\}/);
  for (const language of ['en', 'tr']) {
    const settled = valueOf(server[language], 'services.action.note.unit_not_settled');
    const unread = valueOf(server[language], 'services.action.note.unit_state_not_read');
    assert.equal(placeholders(settled), 'command,pending_unit,state,unit');
    assert.equal(placeholders(unread), 'command,pending_unit,unit');
    for (const sentence of [settled, unread]) {
      // What is known is said (the service stopped); the unit is claimed neither failed nor clean.
      assert.match(sentence, language === 'en' ? /^\{unit\} was stopped and is not running\. / : /^\{unit\} durduruldu ve çalışmıyor\. /);
      assert.match(sentence, language === 'en' ? /run \{command\} on the server/ : /sunucuda \{command\} komutunu çalıştırın/);
      assert.match(sentence, language === 'en' ? /nothing looks again by itself\.$/ : /hiçbir şey kendiliğinden yeniden bakmaz\.$/);
      assert.doesNotMatch(sentence, /now shows|şimdi .* gösteriyor|reset-failed|is clean|temiz/);
    }
    assert.match(settled, language === 'en' ? /was not read; the unit may end marked as failed/ : /okunmadı ve birim failed olarak işaretlenmiş olabilir/);
    assert.match(unread, language === 'en' ? /it is not known whether the unit ended marked as failed/ : /işaretlenip işaretlenmediği bilinmiyor/);
  }
});

test('the new sentences exist in English and Turkish, and the guidance records them', () => {
  for (const [catalogue, key] of [[screens, 'import.step.nothing'], [server, 'panelUpdate.previousAttempt.recovered'],
    [server, 'services.action.note.unit_not_settled'], [server, 'services.action.note.unit_state_not_read']]) {
    const [english, turkish] = [valueOf(catalogue.en, key), valueOf(catalogue.tr, key)];
    assert.ok(english && turkish && english !== turkish, `${key}: one language repeats the other`);
    assert.equal(placeholders(turkish), placeholders(english), `${key}: the placeholders differ between the languages`);
    for (const [language, document] of Object.entries(guidance)) {
      assert.ok(document.includes(`"${english}"`), `${language}: the English sentence of ${key} is not recorded`);
      assert.ok(document.includes(`"${turkish}"`), `${language}: the Turkish sentence of ${key} is not recorded`);
    }
  }
  // The words say both facts: nothing came in, and nothing went wrong.
  assert.equal(valueOf(screens.en, 'import.step.nothing'), 'Nothing imported, nothing failed');
  assert.equal(valueOf(screens.tr, 'import.step.nothing'), 'İçe aktarılan yok, hata da yok');
});
