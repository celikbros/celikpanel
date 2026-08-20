import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const ui = readFileSync(new URL('../src/components/ui.tsx', import.meta.url), 'utf8');
const pageHeader = readFileSync(
  new URL('../src/components/PageHeader.tsx', import.meta.url),
  'utf8',
);
const slot = readFileSync(
  new URL('../src/components/pageHeaderSlot.ts', import.meta.url),
  'utf8',
);

test('desktop shell exposes a page-header target before the global controls', () => {
  assert.match(layout, /DesktopPageHeaderTargetContext\.Provider value=\{desktopPageHeaderSlot\}/);
  assert.match(layout, /ref=\{setDesktopPageHeaderTarget\}/);
  assert.match(layout, /data-shell-page-header-target/);

  const target = layout.indexOf('data-shell-page-header-target');
  const controls = layout.indexOf('<LanguageSwitcher />', target);
  assert.ok(target >= 0 && controls > target, 'page heading slot must precede global controls');
});

test('PageHeader portals one heading and its actions only on wide desktop', () => {
  assert.doesNotMatch(ui, /PageHeader/);
  assert.match(pageHeader, /createPortal\(headerContent, desktopTarget\)/);
  assert.match(pageHeader, /matchMedia\('\(min-width: 1280px\)'\)/);
  assert.match(pageHeader, /const headerContent = \(/);
  assert.equal((pageHeader.match(/\{actions &&/g) ?? []).length, 1);
  assert.match(pageHeader, /<div className="min-w-0 flex-1">/);
  assert.match(pageHeader, /<h1 className="break-words/);
  assert.match(pageHeader, /flex shrink-0 items-center gap-2/);
});

test('narrow screens and pages outside the shell keep the in-page heading', () => {
  assert.match(
    slot,
    /createContext<DesktopPageHeaderSlot \| undefined>\(undefined\)/,
  );
  assert.match(layout, /from '\.\/pageHeaderSlot'/);
  assert.match(pageHeader, /from '\.\/pageHeaderSlot'/);
  assert.match(pageHeader, /useLayoutEffect\(\(\) => desktopSlot\?\.register\(\)/);
  assert.match(pageHeader, /if \(isDesktop && desktopSlot && desktopTarget === null\) return null/);
  assert.match(pageHeader, /return <div className="mb-6">\{headerContent\}<\/div>/);
  assert.match(layout, /className="hidden min-w-0 flex-1 self-stretch xl:flex xl:items-center"/);
});

test('desktop heading row and sidebar brand share the same compact height', () => {
  assert.match(layout, /xl:min-h-\[90px\]/);
  assert.match(layout, /xl:h-\[90px\]/);
  assert.match(layout, /const hasDesktopPageHeader = desktopPageHeaderCount > 0/);
  assert.match(layout, /hasDesktopPageHeader \? 'xl:h-auto xl:min-h-\[90px\] xl:py-2' : ''/);
  assert.match(layout, /expandedHeader \? 'xl:h-\[90px\]' : ''/);
});

test('detail pages without PageHeader keep the compact shell row', () => {
  assert.match(layout, /const \[desktopPageHeaderCount, setDesktopPageHeaderCount\] = useState\(0\)/);
  assert.match(layout, /setDesktopPageHeaderCount\(\(count\) => count \+ 1\)/);
  assert.match(layout, /Math\.max\(0, count - 1\)/);
});
