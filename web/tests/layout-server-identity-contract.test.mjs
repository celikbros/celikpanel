import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const en = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const tr = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');

test('the admin shell reuses one bounded no-store runtime response', () => {
  assert.equal(
    (layout.match(/fetch\('\/api\/v1\/panel\/version'/g) ?? []).length,
    1,
    'Layout must make exactly one panel-version request',
  );
  assert.match(layout, /if \(role !== 'admin'\) \{\s*setPanelRuntime\(null\)/);
  assert.match(layout, /cache: 'no-store', credentials: 'same-origin'/);
  assert.doesNotMatch(layout, /api\.getSystemStats\(\)/);
  assert.match(layout, /raw\.hostname === 'string' && raw\.hostname\.length <= 253/);
  assert.match(layout, /raw\.ipv4 === 'string' && raw\.ipv4\.length <= 64/);
});

test('hostname and IPv4 remain visible in the desktop footer and narrow header', () => {
  assert.match(layout, /<section\s+data-server-identity="sidebar"/);
  assert.match(layout, /<section\s+data-server-identity="mobile"/);
  assert.match(layout, /placement="mobile"/);
  assert.match(layout, /max-w-\[10rem\] md:hidden/);
  assert.match(layout, /dir="ltr"/);
  assert.match(layout, /\{identity\.hostname\}/);
  assert.match(layout, /\{identity\.ipv4\}/);
  assert.match(layout, /<aside className="hidden shrink-0 md:block">/);

  const sidebarIdentity = layout.indexOf('placement="sidebar"');
  const buildStamp = layout.indexOf('<BuildStamp runtime={panelRuntime} />');
  assert.ok(
    sidebarIdentity >= 0 && buildStamp > sidebarIdentity,
    'desktop identity must appear above the build stamp',
  );

  const mobileHeader = layout.indexOf('<header');
  const mobileIdentity = layout.indexOf('placement="mobile"');
  const mobileHeaderEnd = layout.indexOf('</header>', mobileHeader);
  assert.ok(
    mobileHeader >= 0 && mobileIdentity > mobileHeader && mobileIdentity < mobileHeaderEnd,
    'compact identity must be rendered inside the narrow-screen header',
  );
});

test('the build stamp consumes the shared runtime without a second request', () => {
  const start = layout.indexOf('function BuildStamp');
  assert.ok(start >= 0, 'BuildStamp must exist');
  const buildStamp = layout.slice(start);
  assert.match(buildStamp, /function BuildStamp\(\{ runtime \}/);
  assert.match(buildStamp, /if \(!runtime\) return/);
  assert.doesNotMatch(buildStamp, /fetch\(/);
  assert.doesNotMatch(buildStamp, /useState/);
});

test('server identity uses existing localized labels', () => {
  assert.match(layout, /t\('dashboard\.serverInfo'\)/);
  for (const key of ['dashboard.serverInfo', 'dashboard.hostname', 'dashboard.ipv4']) {
    assert.ok(en.includes("'" + key + "':"), 'missing EN key ' + key);
    assert.ok(tr.includes("'" + key + "':"), 'missing TR key ' + key);
  }
});
