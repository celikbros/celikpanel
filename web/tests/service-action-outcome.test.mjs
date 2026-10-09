import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import { apiErrorText } from '../src/lib/apiError.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';

// 10 Oct 2026. Three things that were said wrongly or not at all:
//
//   - a Start, Stop, Restart or Reload that the Panel answers with what the
//     service showed (SERVICE_ACTION_FAILED with its stage, or
//     SERVICE_ACTION_UNKNOWN) had no sentence in the catalogues;
//   - a change that was made while its one-time result did not arrive (a VPN
//     device's configuration, a database user's password) said only "create a
//     new one";
//   - a configuration reload whose result is unknown was drawn as a failure,
//     and a line of the service manager was labelled as the service's own.
//
// 10 Eki 2026. Yanlış söylenen ya da hiç söylenmeyen üç şey: hizmet işlemi
// sonuçları, tek seferlik sonucu ulaşmayan değişiklikler ve sonucu bilinmeyen
// yapılandırma yeniden yüklemesi.

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const flat = (text) => text.replace(/\s+/g, ' ');
const say = (catalogue) => (key, vars) => {
  const text = catalogue[key];
  return text === undefined ? key : text.replace(/\{(\w+)\}/g, (_, name) => String(vars?.[name] ?? `{${name}}`));
};
const screens = { en: read('../src/i18n/screens/en.ts'), tr: read('../src/i18n/screens/tr.ts') };
const server = { en: read('../src/i18n/screens/server/en.ts'), tr: read('../src/i18n/screens/server/tr.ts') };
const valueOf = (source, key) => {
  const match = source.match(new RegExp(`^ {4}'${key.replace(/\./g, '\\.')}': ("(?:[^"\\\\]|\\\\.)*"),$`, 'm'));
  assert.ok(match, `${key} is not in the catalogue`);
  return JSON.parse(match[1]);
};

const reasons = ['check', 'reload', 'start', 'stop', 'verify', 'command'];
const vars = { unit: 'postfix', action: 'reload', command: 'sudo postfix check', detail: 'postfix: fatal: bad value', owner_unit: 'postfix@-.service' };

test('every service-action answer has its sentence in English and Turkish, with the unit and the command', () => {
  for (const [name, catalogue] of [['English', en], ['Turkish', tr]]) {
    const seen = new Set();
    for (const reason of [undefined, ...reasons]) {
      const text = apiErrorText({ message: 'server English', code: 'SERVICE_ACTION_FAILED', reason, vars }, say(catalogue));
      assert.notEqual(text, 'server English', `${name} has no sentence for the stage ${reason}`);
      assert.ok(text.includes('postfix') && text.includes('sudo postfix check'), `${name} ${reason} does not name the unit and the command`);
      assert.doesNotMatch(text, /\{\w+\}|SERVICE_ACTION/, `${name} ${reason} shows a placeholder or an internal name`);
      seen.add(text);
    }
    assert.equal(seen.size, reasons.length + 1, `${name}: two stages share one sentence`);
    // A stage this page does not know falls back to the sentence of the code.
    assert.equal(apiErrorText({ message: 'x', code: 'SERVICE_ACTION_FAILED', reason: 'later_stage', vars }, say(catalogue)),
      apiErrorText({ message: 'x', code: 'SERVICE_ACTION_FAILED', vars }, say(catalogue)));
    const unknown = apiErrorText({ message: 'server English', code: 'SERVICE_ACTION_UNKNOWN', vars }, say(catalogue));
    assert.notEqual(unknown, 'server English');
    assert.ok(unknown.includes('postfix') && unknown.includes('sudo postfix check'));
  }
  // Unknown is said as unknown, and never as done or as a failure.
  assert.match(en['err.SERVICE_ACTION_UNKNOWN'], /not shown as done\. This is not a verified failure/);
  assert.match(tr['err.SERVICE_ACTION_UNKNOWN'], /Bu doğrulanmış bir hata değildir/);
  assert.match(en['err.SERVICE_ACTION_FAILED.check'], /^Nothing was changed/);
  assert.match(tr['err.SERVICE_ACTION_FAILED.check'], /^Hiçbir şey değiştirilmedi/);
  for (const language of ['en', 'tr']) {
    assert.ok(valueOf(server[language], 'services.action.said').includes('{detail}'));
    const owner = valueOf(server[language], 'services.action.ownerUnit');
    assert.ok(owner.includes('{owner_unit}') && owner.includes('{unit}'));
  }
});

test('the sentences on screen are the ones the operation guidance records, in both languages', () => {
  const guidance = { en: flat(read('../../docs/OPERATION-GUIDANCE.md')), tr: flat(read('../../docs/OPERATION-GUIDANCE.tr.md')) };
  const keys = ['err.SERVICE_ACTION_FAILED', ...reasons.map((reason) => `err.SERVICE_ACTION_FAILED.${reason}`), 'err.SERVICE_ACTION_UNKNOWN'];
  for (const [language, document] of Object.entries(guidance)) {
    for (const key of keys) {
      assert.ok(document.includes(`"${en[key]}"`), `${language}: the English sentence of ${key} is not the recorded one`);
      assert.ok(document.includes(`"${tr[key]}"`), `${language}: the Turkish sentence of ${key} is not the recorded one`);
    }
    for (const key of ['services.action.said', 'services.action.ownerUnit']) {
      assert.ok(document.includes(`"${valueOf(server.en, key)}"`), `${language}: ${key} (English)`);
      assert.ok(document.includes(`"${valueOf(server.tr, key)}"`), `${language}: ${key} (Turkish)`);
    }
  }
});

test('the Services screens keep the outcome on the page, and an unknown result is not coloured as a failure', () => {
  const notice = read('../src/components/ServiceActionNotice.tsx');
  assert.match(notice, /const unknown = outcome\.code === 'SERVICE_ACTION_UNKNOWN';/);
  assert.match(notice, /unknown \? 'border-warning-mark\/50 bg-warning-mark\/20' : 'border-danger\/30 bg-danger\/10'/);
  assert.match(notice, /t\('services\.action\.said', \{ detail: '\\u0000' \}\)/);
  assert.match(notice, /vars\.owner_unit && vars\.owner_unit !== vars\.unit/);
  for (const file of ['ServiceShell.tsx', 'ServiceList.tsx']) {
    const screen = read(`../src/components/${file}`);
    assert.match(screen, /<ServiceActionNotice outcome=\{actionOutcome\} onClose=\{\(\) => setActionOutcome\(null\)\}/, `${file} does not draw the outcome`);
    const routed = [...screen.matchAll(/if \(isServiceActionOutcome\((\w+)\)\) \{[^}]*setActionOutcome\(\1\);/g)].length;
    assert.equal(routed, file === 'ServiceList.tsx' ? 2 : 1, `${file}: an action's outcome still goes to a toast`);
    // No answer at all is not a failure either. (The list has other flows,
    // uninstall and repositories, that are not part of this.)
    const actions = file === 'ServiceList.tsx'
      ? screen.slice(screen.indexOf('const handleAction = async'), screen.indexOf('const isRunning ='))
      : screen.slice(screen.indexOf('const act = async'), screen.indexOf('return (', screen.indexOf('const act = async')));
    assert.ok(actions.length > 500, `${file}: the action handlers were not found`);
    assert.doesNotMatch(actions, /showToast\('error', t\('(common\.resultUnknown|services\.actionFailed)'\)\)/, `${file} colours an unanswered action as a failure`);
  }
  // A component's page is drawn inside the shell, so it shows the same notice.
  assert.match(read('../src/components/ComponentDetail.tsx'), /<ServiceShell\b/);
});

test('a change whose one-time result is not kept says what was made and what to do', () => {
  for (const language of ['en', 'tr']) {
    const peer = valueOf(server[language], 'vpn.configNotShown');
    const password = valueOf(screens[language], 'databases.passwordNotShown');
    assert.ok(peer.includes('{name}') && password.includes('{name}') && password.includes('{user}'));
  }
  // A VPN device: it exists, its configuration cannot be shown again; remove it and add it again.
  assert.match(valueOf(server.en, 'vpn.configNotShown'), /was added; it was not added a second time\..*cannot be shown again\..*remove it; then add the device again/);
  assert.match(valueOf(server.tr, 'vpn.configNotShown'), /eklendi; ikinci kez eklenmedi\..*yeniden gösterilemez\..*kaldırın; sonra .* cihazı yeniden ekleyin/);
  // A database password: the database exists, the password cannot be shown again; set a new one.
  assert.match(valueOf(screens.en, 'databases.passwordNotShown'), /was created; it was not created a second time\..*cannot be shown again\..*set a new password for \{user\}/);
  assert.match(valueOf(screens.tr, 'databases.passwordNotShown'), /oluşturuldu; ikinci kez oluşturulmadı\..*yeniden gösterilemez\..*yeni bir parola belirleyin/);

  const once = read('../src/components/OnceOnlyNotice.tsx');
  assert.match(once, /problem\.code === 'REQUEST_COMPLETED_RESULT_NOT_RETAINED' && !problem\.reason/);
  assert.match(once, /border-warning-mark\/50 bg-warning-mark\/20/);
  assert.doesNotMatch(once, /danger/, 'a change that was made is drawn as a failure');
  const vpn = read('../src/components/VPNPage.tsx');
  assert.match(vpn, /if \(!resultNotKept\(problem\)\) throw problem;[\s\S]*?setConfigNotShown\(name\);\s+setNewName\(''\);\s+await load\(\);/);
  assert.match(vpn, /text=\{configNotShown === null \? null : t\('vpn\.configNotShown', \{ name: configNotShown \}\)\}/);
  // One press of Enter while a create is running, or while its result is being read, creates nothing.
  assert.match(vpn, /if \(busyAction !== null \|\| answer\.holding\) return;\s+setBusyAction\('create'\);/);
  const dialog = read('../src/components/AddDatabaseModalV2.tsx');
  assert.match(dialog, /if \(resultNotKept\(problem\)\) \{\s+onPasswordNotShown\(name, userMode === 'new' \? newUsername : ''\);/);
  assert.match(read('../src/components/DatabaseManagementV2.tsx'), /t\('databases\.passwordNotShown', passwordNotShown\)/);
  // The engine account's password is read with "Show password": that answer is the success it was.
  assert.match(read('../src/components/DatabaseAccountStrip.tsx'), /problem\.code === 'REQUEST_COMPLETED_RESULT_NOT_RETAINED' && !problem\.reason/);
});

test('a configuration reload whose result is unknown is not a failure, and the line is labelled by its source', () => {
  const notices = read('../src/components/ConfigFileNotices.tsx');
  assert.match(notices, /failure: refusal\.reason !== 'restored_running_unknown',/);
  assert.match(notices, /const said = reloadFailed\s+\? t\('dbconf\.reloadSaid', \{ unit: refusal\.vars\?\.unit \|\| service \}\)/);
  assert.equal(valueOf(server.en, 'dbconf.reloadSaid'), 'Reported when {unit} was reloaded:');
  assert.equal(valueOf(server.tr, 'dbconf.reloadSaid'), '{unit} yeniden yüklenirken bildirilen:');
});
