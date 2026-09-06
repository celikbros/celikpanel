import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';

// R-064. This spinner was hand-written twenty-eight times across the product,
// in three spellings of the same six classes. Nothing was wrong with any one
// of them - each copy was locally correct, and no test could have caught a
// missed one, because a missed one still looks like a spinner.
//
// What it cost was that a change to a loading state - its size, its colour,
// its accessible name, a reduced-motion fallback - had to be made twenty-eight
// times or made inconsistently. The design hook kept reporting it as a false
// positive one file at a time, and the sanctioned-exception list grew an entry
// every time somebody edited another screen. The list was the measurement.
//
// So this pins the shape where it now lives, and pins that there are exactly
// two: the shared one, and the boot one that cannot use it.
//
// R-064. Bu gosterge urun genelinde yirmi sekiz kez elle yazilmisti. Her kopya
// kendi icinde dogruydu ve kacirilan biri hala gosterge gibi gorunecegi icin
// hicbir test onu yakalayamazdi. Bedeli, bir yukleme durumuna yapilacak her
// degisikligin yirmi sekiz kez ya da tutarsiz yapilmasiydi.
const componentsDir = new URL('../src/components/', import.meta.url);
const srcDir = new URL('../src/', import.meta.url);
const ui = readFileSync(new URL('ui.tsx', componentsDir), 'utf8');

// The shape, in any spelling: a spinning circle whose one-sided border is the
// arc. Matched on the tokens appearing near each other rather than on one
// literal attribute, because the classes can be a plain string or built in a
// template - and a copy written the second way is still a copy.
// Bicim, herhangi bir yazimda; siniflar duz bir dizge de olabilir, bir sablonda
// kurulmus da - ikinci yolla yazilmis bir kopya yine kopyadir.
function buildsItsOwnSpinner(source) {
  let at = source.indexOf('animate-spin');
  while (at >= 0) {
    const window = source.slice(Math.max(0, at - 140), at + 140);
    if (window.includes('rounded-full') && /border-[a-z]-\d/.test(window)) return true;
    at = source.indexOf('animate-spin', at + 1);
  }
  return false;
}

function everyTsxUnder(dir, prefix = '') {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      out.push(...everyTsxUnder(new URL(entry.name + '/', dir), prefix + entry.name + '/'));
    } else if (entry.name.endsWith('.tsx')) {
      out.push([prefix + entry.name, readFileSync(new URL(entry.name, dir), 'utf8')]);
    }
  }
  return out;
}

test('the product has exactly two spinners, and one of them is shared', () => {
  const handWritten = everyTsxUnder(srcDir)
    .filter(([, source]) => buildsItsOwnSpinner(source))
    .map(([name]) => name)
    .sort();

  assert.deepEqual(handWritten, ['components/ui.tsx', 'i18n/index.tsx'], String(
    'a screen builds its own spinner instead of using the shared one. ' +
    'Import Spinner from the shared primitives; ui.tsx is where it is decided.',
  ));
});

// The exception is an exception for a reason, and the reason is checkable: the
// boot spinner renders before I18nProvider has a value, so a component that
// reads useI18n would throw there and the first load would be a white screen.
// Istisnanin sebebi denetlenebilir.
test('the one spinner that is not shared says why it cannot be', () => {
  const boot = readFileSync(new URL('i18n/index.tsx', srcDir), 'utf8');
  assert.match(
    boot,
    /NOT the shared one, and[\s\S]{0,400}?before I18nProvider/,
    'the boot spinner no longer explains why it is not the shared one',
  );
  assert.doesNotMatch(
    boot,
    /import \{[^}]*\bSpinner\b[^}]*\} from '\.\.\/components\/ui'/,
    'the boot spinner imports the shared one, which throws outside the provider',
  );
});

// A spinner that announces nothing is a page that, to a screen reader, simply
// stops. Twenty-four of the copies sat in a wrapper that named them; the rest
// were a bare spinning div. The shared one names itself, so all of them do.
// Hicbir sey duyurmayan bir gosterge, ekran okuyucu icin duran bir sayfadir.
test('the shared spinner names itself', () => {
  const start = ui.indexOf('export function Spinner(');
  assert.ok(start > 0, 'the shared Spinner is gone');
  const body = ui.slice(start, ui.indexOf('\nexport function ', start + 1));
  assert.match(body, /role="status"/, 'the shared spinner is not a status region');
  assert.match(body, /aria-label=\{label \?\? t\('common\.loading'\)\}/,
    'the shared spinner has no accessible name');
});

// A screen that names its own wait must not end up inside a second status
// region: two nested live regions is worse than one, not better.
// Kendi beklemesini adlandiran bir ekran, ikinci bir durum bolgesinin icinde
// kalmamalidir.
test('no shared spinner is nested inside another status region', () => {
  for (const [name, source] of everyTsxUnder(srcDir)) {
    let at = source.indexOf('<Spinner');
    while (at >= 0) {
      const before = source.slice(Math.max(0, at - 240), at);
      const openTag = before.lastIndexOf('<div');
      if (openTag >= 0) {
        const wrapper = before.slice(openTag);
        assert.ok(
          !/role=['"]status['"]/.test(wrapper),
          `${name} wraps the shared spinner in a second status region`,
        );
      }
      at = source.indexOf('<Spinner', at + 1);
    }
  }
});
