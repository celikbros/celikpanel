import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// R-065. R-057 gave the panel an account of its own on a database engine so it
// never has to touch the operator's root. That is only true for the operator
// if the screen says so - and the refusal an engine returns says "on this
// server's page", so the page has to carry it.
//
// These are the claims the strip makes. They are pinned here because each of
// them is a promise about a credential, and a promise about a credential that
// nothing checks is the kind that quietly stops being true.
//
// R-065. R-057 panele motorda kendi hesabini verdi; boylece operatorun kok
// hesabina hic dokunmasi gerekmez. Bu, ancak ekran soylerse operator icin
// dogrudur. Buradaki iddialarin her biri bir kimlik bilgisi hakkinda bir
// sozdur ve hicbir seyin denetlemedigi boyle bir soz, sessizce dogru olmaktan
// cikan turdendir.
const strip = readFileSync(
  new URL('../src/components/DatabaseAccountStrip.tsx', import.meta.url),
  'utf8',
);
const screen = readFileSync(
  new URL('../src/components/DatabaseManagementV2.tsx', import.meta.url),
  'utf8',
);
const en = readFileSync(new URL('../src/i18n/screens/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/screens/tr.ts', import.meta.url), 'utf8');

// A tenant has nothing to do with the account the panel connects as, and the
// server list does not even tell them its name. The screen must not render a
// strip whose every control would be refused.
// Bir kiracinin, panelin bagli oldugu hesapla isi yoktur.
test('the account is administrator business on the screen as well as in the panel', () => {
  assert.match(
    screen,
    /\{selectedServer && isAdmin && \(\s*<DatabaseAccountStrip/,
    'the account strip is rendered without checking that the caller is an administrator',
  );
});

// The panel can only open an account through the machine's own privileged
// door, which exists on the machine the agent runs on. Offering the button for
// an engine somewhere else would be offering something that cannot work.
// Panel, hesabi yalnizca makinenin kendi ayricalikli kapisindan acabilir.
test('an engine on another machine is not offered a button that cannot work', () => {
  const missingState = strip.slice(strip.indexOf('if (!account)'), strip.indexOf('return (\n        <>'));
  assert.match(
    missingState,
    /\{server\.is_local && \(\s*<Button/,
    'the open-account button is offered without checking the engine is on this machine',
  );
  assert.match(
    missingState,
    /server\.is_local\s*\?\s*'databases\.account\.missingHint'\s*:\s*'databases\.account\.missingRemote'/,
    'a remote engine is not told what to do instead',
  );
});

// Reading the password is audited on the panel side. An operator who is not
// told that is being recorded without being told, which is the part that makes
// an audit trail a courtesy rather than a trap.
// Parolayi okumak panel tarafinda denetim kaydina yazilir.
test('the operator is told that opening the password is recorded, because it is', () => {
  assert.match(
    strip,
    /description=\{t\('databases\.account\.passwordRecorded'\)\}/,
    'the password dialogue does not say that reading it is recorded',
  );
  for (const [name, catalogue] of [['English', en], ['Turkish', tr]]) {
    const line = catalogue
      .split('\n')
      .find((l) => l.includes("'databases.account.passwordRecorded'"));
    assert.ok(line, `the ${name} catalogue has no sentence about the recording`);
    assert.match(
      line,
      /audit|denetim/i,
      `the ${name} sentence does not name the record it is about`,
    );
  }
});

// The password reaches the screen only when it is asked for, and it is drawn
// only inside the dialogue that asking opens. A credential rendered anywhere
// else is a credential on a screen somebody left open.
// Parola ekrana yalnizca istendiginde ulasir.
test('the password is drawn only inside the dialogue that asking opens', () => {
  const occurrences = strip.split('{password}').length - 1;
  assert.equal(occurrences, 1, 'the password is rendered somewhere other than the dialogue');
  const dialogueAt = strip.indexOf('<Dialog');
  assert.ok(dialogueAt > 0, 'the password is no longer shown in the shared dialogue');
  assert.ok(
    strip.indexOf('{password}') > dialogueAt,
    'the password is rendered before the dialogue that is supposed to contain it',
  );
  assert.match(
    strip,
    /password !== null && \(/,
    'the dialogue opens on something other than having been given a password',
  );
});

// Removing the account stops the panel managing databases here. An operator
// who is asked to confirm that deserves the consequence in the question, and
// deserves to be told the thing this whole design protects: their own access
// is not what is being removed.
// Hesabi kaldirmak, panelin burada veritabani yonetmesini durdurur.
test('removing the account says what stops and what does not', () => {
  for (const [name, catalogue, mustSay] of [
    ['English', en, [/stop working|will not be able/i, /your own access/i]],
    ['Turkish', tr, [/çalışmayacak/i, /kendi erişiminiz/i]],
  ]) {
    const line = catalogue
      .split('\n')
      .find((l) => l.includes("'databases.account.confirmRemove'"));
    assert.ok(line, `the ${name} catalogue has no confirmation for removing the account`);
    for (const pattern of mustSay) {
      assert.match(line, pattern, `the ${name} confirmation does not say ${pattern}`);
    }
  }
});

// Every string the strip reaches for exists in both catalogues. A missing key
// renders as the key, and a screen about a credential is a bad place to find
// that out.
// Seridin kullandigi her dizge iki katalogda da vardir.
test('every sentence the strip uses exists in both languages', () => {
  const keys = [...strip.matchAll(/'(databases\.account\.[a-zA-Z]+)'/g)].map((m) => m[1]);
  assert.ok(keys.length >= 12, `the strip uses only ${keys.length} of its own sentences`);
  for (const key of new Set(keys)) {
    assert.ok(en.includes(`'${key}'`), `the English catalogue is missing ${key}`);
    assert.ok(tr.includes(`'${key}'`), `the Turkish catalogue is missing ${key}`);
  }
});

// A guard for a mistake that was actually made writing this screen, not a
// hypothetical one. Turkish text inserted through an escape round-trip landed
// as its own UTF-8 bytes read back as Latin-1: 'çalışmayacak' became
// 'Ã§alÄ±ÅŸmayacak' on disk. Nothing caught it - the build passed, the types
// passed, and the only reason it surfaced was a test that happened to match on
// a Turkish word.
//
// It is invisible in a diff on a console that cannot print the characters
// anyway, and it renders as gibberish for exactly the operators the Turkish is
// for. So the sequences are refused outright, everywhere, once.
//
// Bu ekrani yazarken gercekten yapilan bir hatanin muhafizi. Kacis dizisiyle
// eklenen Turkce metin, kendi UTF-8 baytlari Latin-1 olarak okunmus halde
// diske dustu. Hicbir sey yakalamadi.
test('no catalogue carries text that was encoded twice', () => {
  const doubled = /[\u00c2\u00c3][\u0080-\u00bf]/g;
  for (const [name, catalogue] of [
    ['English screens', en],
    ['Turkish screens', tr],
    ['English base', readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8')],
    ['Turkish base', readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8')],
  ]) {
    const found = [...catalogue.matchAll(doubled)];
    assert.equal(
      found.length,
      0,
      `${name} carries ${found.length} doubly-encoded sequences, the first near: ` +
        (found[0] ? catalogue.slice(Math.max(0, found[0].index - 60), found[0].index + 20) : ''),
    );
  }
});

// R-068. A refusal that names which of several causes it was must reach words
// for that cause. The mechanism is general - code, then code plus reason - so
// what is pinned is that the refinement exists and that every reason the panel
// sends has words in both languages.
//
// R-068. Hangi sebep oldugunu adlandiran bir ret, o sebebin sozlerine
// ulasmalidir. Mekanizma geneldir; sabitlenen sey inceltmenin var oldugu ve
// panelin gonderdigi her gerekcenin iki dilde de sozu oldugudur.
test('a refusal that names its reason reaches words for that reason', () => {
  const apiError = readFileSync(new URL('../src/lib/apiError.ts', import.meta.url), 'utf8');
  assert.match(
    apiError,
    /if \(e\.reason\) \{[\s\S]{0,200}?'err\.' \+ e\.code \+ '\.' \+ e\.reason/,
    'apiErrorText no longer prefers the sentence for the named reason',
  );
  assert.match(
    apiError,
    /reason: typeof d\.reason === 'string'/,
    'the response parser drops the reason the server sent',
  );

  const enBase = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
  const trBase = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');
  for (const reason of [
    'package_manager_active',
    'agent_mutation_active',
    'panel_operation_active',
    'host_lock_busy',
  ]) {
    const key = `'err.HOST_MUTATION_BUSY.${reason}'`;
    assert.ok(enBase.includes(key), `the English catalogue has no words for ${reason}`);
    assert.ok(trBase.includes(key), `the Turkish catalogue has no words for ${reason}`);
  }

  // The one that does not end by waiting must not be told to wait, in either
  // language. It is the only one of the four where "try again" is wrong.
  // Beklemekle geçmeyene beklemesi söylenmemeli.
  for (const [name, catalogue, wrong] of [
    ['English', enBase, /try again in a minute/i],
    ['Turkish', trBase, /yeniden deneyin/i],
  ]) {
    const line = catalogue
      .split('\n')
      .find((l) => l.includes("'err.HOST_MUTATION_BUSY.host_lock_busy'"));
    assert.ok(line, `${name} is missing the held-lock sentence`);
    assert.doesNotMatch(line, wrong, `the ${name} held-lock sentence tells the operator to wait`);
  }
});
