import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const ui = readFileSync(new URL('../src/components/ui.tsx', import.meta.url), 'utf8');

test('desktop shell exposes a page-header target before the global controls', () => {
  assert.match(layout, /DesktopPageHeaderTargetContext\.Provider value=\{desktopPageHeaderSlot\}/);
  assert.match(layout, /ref=\{setDesktopPageHeaderTarget\}/);
  assert.match(layout, /data-shell-page-header-target/);

  const target = layout.indexOf('data-shell-page-header-target');
  const controls = layout.indexOf('<LanguageSwitcher />', target);
  assert.ok(target >= 0 && controls > target, 'page heading slot must precede global controls');
});

test('PageHeader portals one heading and its actions only on wide desktop', () => {
  assert.match(ui, /createPortal\(headerContent, desktopTarget\)/);
  assert.match(ui, /matchMedia\('\(min-width: 1280px\)'\)/);
  assert.match(ui, /const headerContent = \(/);
  assert.equal((ui.match(/\{actions &&/g) ?? []).length, 1);
  assert.match(ui, /<div className="min-w-0 flex-1">/);
  assert.match(ui, /<h1 className="break-words/);
  assert.match(ui, /flex shrink-0 items-center gap-2/);
});

test('narrow screens and pages outside the shell keep the in-page heading', () => {
  assert.match(
    ui,
    /createContext<DesktopPageHeaderSlot \| undefined>\(undefined\)/,
  );
  assert.match(ui, /useLayoutEffect\(\(\) => desktopSlot\?\.register\(\)/);
  assert.match(ui, /if \(isDesktop && desktopSlot && desktopTarget === null\) return null/);
  assert.match(ui, /return <div className="mb-6">\{headerContent\}<\/div>/);
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
