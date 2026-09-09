import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';

// Register R-060: the critical-boot payload had 31 bytes of headroom, and 38%
// of it was one file — the whole product's copy, 2092 keys, of which 74 could
// be reached before a route had loaded. The catalogue is two halves now: the
// shell half boots with the application, the screen half arrives beside the
// screen that needs it.
//
// That split is only safe while the shell half really does hold everything an
// eagerly loaded module can ask for. A key that slips into the screen half but
// is read by the login form or the navigation rail renders as its own name, on
// the first screen an operator sees. This file is the guard: it recomputes what
// the eager graph reaches and fails if any of it is on the wrong side.
//
// R-060 defteri: açılış yükünün 31 baytlık boşluğu vardı ve %38'i tek dosyaydı.
// Katalog artık iki yarımdır. Bu bölünme, yalnızca kabuk yarısı istekli
// yüklenen bir modülün isteyebileceği her şeyi tuttuğu sürece güvenlidir. Bu
// dosya o güvencedir.
const web = new URL('../', import.meta.url);
const read = (path) => readFileSync(new URL(path, web), 'utf8');

const shellEn = read('src/i18n/en.ts');
const shellTr = read('src/i18n/tr.ts');
const screensEn = read('src/i18n/screens/en.ts');
const screensTr = read('src/i18n/screens/tr.ts');

function keysOf(source) {
  const keys = new Set();
  for (const match of source.matchAll(/^ {4}'((?:[^'\\]|\\.)+)':/gm)) keys.add(match[1]);
  return keys;
}

const shellKeys = keysOf(shellEn);
const screenKeys = keysOf(screensEn);

// The modules the browser runs before any route bundle has been fetched: the
// application entry and everything it imports statically, plus the two overlays
// an eagerly mounted provider can put on screen during a boot that interrupted
// an operation.
//
// Tarayıcının, herhangi bir sayfa paketi getirilmeden önce çalıştırdığı
// modüller.
const eagerModules = [
  'src/main.tsx',
  'src/App.tsx',
  'src/components/LicenseOnboarding.tsx',
  'src/router.tsx',
  'src/router-core.ts',
  'src/router-history.ts',
  'src/nav.ts',
  'src/auth/AuthContext.tsx',
  'src/auth/domainAccess.ts',
  'src/i18n/index.tsx',
  'src/theme/ThemeProvider.tsx',
  'src/lib/api.ts',
  'src/lib/apiError.ts',
  'src/lib/componentCensus.ts',
  'src/lib/systemUpdateAuthSignal.ts',
  'src/lib/systemUpdateLease.ts',
  'src/lib/systemUpdateWatchdog.ts',
  'src/lib/useSystemUpdateNavigationLease.ts',
  'src/lib/panelUpdateAdmission.ts',
  'src/components/BrandMark.tsx',
  'src/components/Login.tsx',
  'src/components/Layout.tsx',
  'src/components/PageHeader.tsx',
  'src/components/pageHeaderSlot.ts',
  'src/components/ChangePasswordModal.tsx',
  'src/components/ThemeSwitcher.tsx',
  'src/components/SkinSwitcher.tsx',
  'src/components/LanguageSwitcher.tsx',
  'src/components/Toast.tsx',
  'src/components/RootErrorBoundary.tsx',
  'src/components/ui.tsx',
  'src/components/ComponentOperation.tsx',
  'src/components/OperationOverlay.tsx',
  'src/components/SystemUpdateOperation.tsx',
];

// Groups whose keys are assembled at runtime rather than written out, so no
// scan can prove one of them unreachable. `apiErrorText` reaches the whole of
// err.* as ('err.' + code) from a module that is always loaded.
//
// Anahtarları çalışma anında kurulan gruplar; hiçbir tarama birinin
// erişilemez olduğunu kanıtlayamaz.
const runtimeBuiltGroups = ['err', 'common', 'app', 'nav', 'role', 'theme', 'lang', 'login', 'profile'];

test('the two halves are one catalogue: no key is missing, duplicated or untranslated', () => {
  const overlap = [...shellKeys].filter((key) => screenKeys.has(key));
  assert.deepEqual(overlap, [], `a key is in both halves: ${overlap.join(', ')}`);

  // Every English key has its Turkish twin, in the same half.
  assert.deepEqual(
    [...shellKeys].filter((key) => !keysOf(shellTr).has(key)),
    [],
    'the Turkish shell half is missing keys the English one has',
  );
  assert.deepEqual(
    [...screenKeys].filter((key) => !keysOf(screensTr).has(key)),
    [],
    'the Turkish screen half is missing keys the English one has',
  );
});

test('every key the eager boot graph can reach is in the half that boots with it', () => {
  const literal = new Set();
  const prefixes = new Set();
  for (const module of eagerModules) {
    const source = read(module);
    for (const m of source.matchAll(/['"]([a-zA-Z][a-zA-Z0-9]*(?:\.[a-zA-Z0-9_]+)+)['"]/g)) {
      literal.add(m[1]);
    }
    for (const m of source.matchAll(/`([a-zA-Z][a-zA-Z0-9]*(?:\.[a-zA-Z0-9_]+)*\.)\$\{/g)) {
      prefixes.add(m[1]);
    }
  }

  const misplaced = [...screenKeys].filter((key) => {
    if (runtimeBuiltGroups.includes(key.split('.')[0])) return true;
    if (literal.has(key)) return true;
    for (const prefix of prefixes) if (key.startsWith(prefix)) return true;
    return false;
  });

  assert.deepEqual(
    misplaced,
    [],
    'these are read before any route loads and would render as their own names: '
      + misplaced.join(', '),
  );
});

// The split's whole purpose. If the screen half ever became small, or the shell
// half large, the boot payload would be back where R-060 found it — and the
// bundle budget would be the only thing left saying so, at the moment someone
// unrelated tripped it.
test('the shell half stays the small one', () => {
  const shellBytes = Buffer.byteLength(shellEn) + Buffer.byteLength(shellTr);
  const screenBytes = Buffer.byteLength(screensEn) + Buffer.byteLength(screensTr);
  assert.ok(
    shellBytes < screenBytes / 4,
    `the boot copy is no longer the small half: ${shellBytes} vs ${screenBytes} bytes`,
  );
});

// A screen must not paint against a catalogue that has only half arrived: a key
// rendering as its own name is worse than a spinner that lasts as long as the
// route bundle being fetched beside it.
test('a route waits for its own copy the way it waits for its own bundle', () => {
  const app = read('src/App.tsx');
  assert.match(app, /<ScreenCopyGate>\{children\}<\/ScreenCopyGate>/);
  assert.match(app, /const \{ screensReady, screensFailed, t \} = useI18n\(\);/);
  assert.match(app, /if \(!screensReady\) return <PageLoading \/>;/);
  assert.match(app, /if \(screensFailed\) return <PageLoadFailed/);

  const i18n = read('src/i18n/index.tsx');
  assert.match(i18n, /await import\('\.\/screens\/tr'\)\)\.trScreens/);
  assert.match(i18n, /await import\('\.\/screens\/en'\)\)\.enScreens/);
  // The shell is what the application waits for before it renders at all; the
  // screen half is merged in behind it.
  assert.match(i18n, /screensReady: loaded\.screens/);
});

// The eager list above is only true while it matches the application. A new
// static import into the boot graph that this file does not know about would
// silently widen what must be in the shell half.
test('the eager module list still matches what the application imports statically', () => {
  const known = new Set(eagerModules);
  const seen = new Set();
  const srcDir = fileURLToPath(new URL('src/', web));

  const resolve = (fromFile, specifier) => {
    if (!specifier.startsWith('.')) return null;
    const base = join(fromFile, '..', specifier).replaceAll('\\', '/');
    const candidates = /\.(tsx?|css)$/.test(base)
      ? [base]
      : [`${base}.tsx`, `${base}.ts`, `${base}/index.tsx`, `${base}/index.ts`];
    for (const candidate of candidates) {
      try {
        if (statSync(candidate).isFile()) return candidate;
      } catch {
        /* not this extension */
      }
    }
    return null;
  };

  // Static imports only. A route reached through import() is not here, which is
  // the point — its copy travels with it.
  // Yalnizca statik import'lar. import() ile ulasilan bir sayfa burada yoktur.
  const walk = (file) => {
    if (seen.has(file) || file.endsWith('.css')) return;
    seen.add(file);
    const source = readFileSync(file, 'utf8');
    for (const m of source.matchAll(/^import\s+(?!type\b)[\s\S]*?from\s+'([^']+)'/gm)) {
      const resolved = resolve(file, m[1]);
      if (resolved) walk(resolved);
    }
    for (const m of source.matchAll(/^import\s+'([^']+)'/gm)) {
      const resolved = resolve(file, m[1]);
      if (resolved) walk(resolved);
    }
  };
  walk(join(srcDir, 'main.tsx'));
  assert.ok(seen.size > 20, `the import walk found only ${seen.size} modules, so it is not walking`);

  // The lazily loaded routes are reached through import() and never appear.
  const reached = [...seen]
    .map((file) => file.replaceAll('\\', '/').slice(file.replaceAll('\\', '/').indexOf('/src/') + 1))
    .filter((path) => !path.endsWith('.css'))
    .sort();

  const unknown = reached.filter((path) => !known.has(path));
  assert.deepEqual(
    unknown,
    [],
    'these are loaded at boot but are not in this file\'s eager list, so their '
      + `copy is not being checked: ${unknown.join(', ')}`,
  );
});

// The Suspense-lazy route components are the reason the screen half can be
// deferred at all. If a screen became a static import it would drag its copy
// into the boot payload with it.
test('every screen is still reached lazily', () => {
  const app = read('src/App.tsx');
  const componentsDir = fileURLToPath(new URL('src/components/', web));
  const routeComponents = readdirSync(componentsDir)
    .filter((name) => name.endsWith('.tsx'))
    .map((name) => name.replace(/\.tsx$/, ''));

  for (const name of ['Dashboard', 'Domains', 'Settings', 'ServiceList', 'UsersPage', 'VPNPage']) {
    assert.ok(routeComponents.includes(name), `${name} is no longer a component`);
    assert.match(
      app,
      new RegExp(`lazyNamed\\(\\(\\) => import\\('\\./components/${name}'\\), '${name}'\\)`),
      `${name} must stay behind a lazy import or its copy returns to the boot payload`,
    );
  }
});
