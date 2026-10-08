import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { patternNames, patterns, readAllowList, scanSource, scanTree, totals } from './remote-state-ratchet.mjs';

// NO NEGATIVE UI UNLESS KNOWN (9 Oct 2026, D-024).
//
// An owner reported "too many surprises": screens that showed a negative state
// - "choose a DNS engine", "no database server installed", "no domains yet" -
// while the answer was simply not known yet, or after one read had failed. The
// cause was not one screen. 68 files read the server with a raw `fetch` into
// `useState`, and most of them turned a failed or unfinished read into a value.
//
// The shared layer for reading is src/lib/remote.ts. This test is what keeps
// the old way from growing while the remaining screens are moved: it counts the
// patterns per file against remote-state-ratchet.json, and that list may only
// shrink.
//
//   - A file not on the list must have none of the patterns.
//   - A file on the list may not exceed its numbers.
//   - A file that got better must have its numbers lowered in the same change
//     (`node tests/remote-state-ratchet.mjs --tighten`), so the room it freed
//     cannot be used up again.
//   - The totals may not exceed the ceilings pinned below. Raising a ceiling
//     means editing this file, where a reviewer sees it.
//
// HOW TO MIGRATE A SCREEN: read with `useRemote(url, decode)` (or
// `useHostingCapabilities()`), draw through `<RemoteGate>` or an explicit
// switch on `remote.state` with `<Checking>` and `<CouldNotCheck onRetry>`,
// draw "empty" with `<KnownEmpty of={…}>`, compute a blocker with `gateOn`,
// keep every save and delete disabled unless the state is `known`, then run
// `--tighten`. docs/OPERATION-GUIDANCE.md, entry of 2026-10-09, has the texts.
const webDir = fileURLToPath(new URL('../', import.meta.url));
const allowList = readAllowList();
const actual = scanTree(webDir);

// The totals of the tree this ratchet was introduced on, after its first batch
// (hosting capabilities, Add domain, Domains, Databases, domain connection).
// Before that batch: 46 / 68 / 16 / 126 / 32 in 68 files.
const ceilings = {
  valueFromFailure: 37,
  swallowedFailure: 58,
  ignoredFailure: 14,
  rawRead: 109,
  unprovenEmptyState: 29,
};
const fileCeiling = 62;

const advice = 'Read through useRemote/readRemote (src/lib/remote.ts) and draw the three states; '
  + 'see docs/OPERATION-GUIDANCE.md, entry of 2026-10-09.';

test('the scanner still sees every pattern it counts, and only those', () => {
  const count = (source, path = 'src/components/Sample.tsx') => scanSource(path, source);
  const none = Object.fromEntries(patternNames.map((name) => [name, 0]));

  // A failed read becomes a value.
  for (const source of [
    "fetch('/a').then((r) => (r.ok ? r.json() : null));",
    "async function f() { const res = await fetch('/a'); setRows(res.ok ? (await res.json()) || [] : []); }",
    "const body = response.ok ? await response.json() : {};",
  ]) {
    assert.equal(count(source).valueFromFailure, 1, source);
  }
  assert.equal(count("const label = item.ok ? 'yes' : 'no';").valueFromFailure, 0);

  // A failed read is swallowed.
  for (const source of [
    "async function f() { try { const r = await fetch('/a'); use(await r.json()); } catch { /* quiet */ } }",
    "async function f() { try { await load(); } catch (error) {} }",
    "fetch('/a').then(use).catch(() => {});",
    "fetch('/a').then(use).catch(() => setCaps(null));",
    "fetch('/a').then(use).catch(() => null);",
    "fetch('/a').then(use).catch(() => { setRows([]); });",
  ]) {
    assert.equal(count(source).swallowedFailure, 1, source);
  }
  for (const source of [
    "function f() { try { localStorage.setItem('a', 'b'); } catch { /* storage may be blocked */ } }",
    "async function f() { try { await load(); } catch { showToast('error', t('x')); } }",
    "fetch('/a').then(use).catch((error) => report(error));",
    "fetch('/a').then(use).catch(() => setState({ state: 'unknown' }));",
  ]) {
    assert.equal(count(source).swallowedFailure, 0, source);
  }

  // A failed read is ignored.
  for (const source of [
    "async function f() { const res = await fetch('/a'); if (res.ok) setC(await res.json()); }",
    "async function f() { const res = await fetch('/a'); if (res.ok) { setC(await res.json()); } }",
    "async function f() { const res = await fetch('/a'); if (!res.ok) return; setC(await res.json()); }",
    "async function f() { for (const id of ids) { const res = await fetch('/a/' + id); if (!res.ok) continue; use(res); } }",
  ]) {
    assert.equal(count(source).ignoredFailure, 1, source);
  }
  for (const source of [
    "async function f() { const res = await fetch('/a'); if (res.ok) { setC(await res.json()); } else { fail(); } }",
    "async function f() { const res = await fetch('/a'); if (res.ok) { setC(await res.json()); return; } fail(); }",
    "async function f() { const res = await fetch('/a'); if (!res.ok) throw new Error(); setC(await res.json()); }",
    "async function f() { const res = await fetch('/a'); if (!res.ok) { fail(); return; } setC(await res.json()); }",
    // A write is another class of defect; this ratchet is about reads.
    "async function f() { const res = await fetch('/a', { method: 'POST' }); if (res.ok) done(); }",
  ]) {
    assert.equal(count(source).ignoredFailure, 0, source);
  }

  // A raw read outside src/lib.
  assert.equal(count("fetch('/a'); fetch('/b', { cache: 'no-store' }); fetch('/c', { method: 'GET' });").rawRead, 3);
  assert.equal(count("fetch('/a', { method: 'DELETE' }); fetch('/b', { method: 'POST', body });").rawRead, 0);
  assert.equal(count("fetch('/a');", 'src/lib/remote.ts').rawRead, 0);
  assert.equal(count("// fetch('/a') was here\nconst text = \"fetch('/a')\";").rawRead, 0, 'comments and strings are not code');

  // An empty state without the answer that proves it.
  assert.equal(count('const a = <EmptyState icon={X} title="none" />;').unprovenEmptyState, 1);
  assert.equal(count('const a = <KnownEmpty of={shown} icon={X} title="none" />;').unprovenEmptyState, 0);
  assert.equal(count('const a = <EmptyState icon={X} title="none" />;', 'src/components/ui.tsx').unprovenEmptyState, 0);

  assert.deepEqual(count('const a = 1;'), none);
});

test('no file reads the server the old way more than the allow-list records', () => {
  const problems = [];
  for (const [path, counts] of Object.entries(actual)) {
    const allowed = allowList.files[path];
    if (!allowed) {
      const found = Object.entries(counts).map(([name, n]) => `${n} × ${patterns[name]}`).join('; ');
      problems.push(`${path} is not on the allow-list and has: ${found}`);
      continue;
    }
    for (const [name, n] of Object.entries(counts)) {
      if (n > (allowed[name] ?? 0)) {
        problems.push(`${path}: ${name} rose from ${allowed[name] ?? 0} to ${n} (${patterns[name]})`);
      }
    }
  }
  assert.deepEqual(problems, [], `\n${problems.join('\n')}\n\n${advice}\n`);
});

test('the allow-list is exactly what the tree still has: it only shrinks', () => {
  const stale = [];
  for (const [path, allowed] of Object.entries(allowList.files)) {
    if (!existsSync(new URL(path, new URL('../', import.meta.url)))) {
      stale.push(`${path} no longer exists`);
      continue;
    }
    for (const name of Object.keys(allowed)) {
      assert.ok(patternNames.includes(name), `${path}: ${name} is not a pattern this ratchet counts`);
      const now = actual[path]?.[name] ?? 0;
      if (allowed[name] <= 0) stale.push(`${path}: ${name} is recorded as ${allowed[name]}`);
      else if (now < allowed[name]) stale.push(`${path}: ${name} is now ${now}, the list still allows ${allowed[name]}`);
    }
  }
  assert.deepEqual(
    stale,
    [],
    `\n${stale.join('\n')}\n\nLower the allow-list in the same change: node tests/remote-state-ratchet.mjs --tighten\n`,
  );
});

test('the totals never exceed the pinned ceilings', () => {
  const now = totals(actual);
  for (const name of patternNames) {
    assert.ok(
      now[name] <= ceilings[name],
      `${name}: ${now[name]} in the tree, ceiling ${ceilings[name]} (${patterns[name]}). ${advice}`,
    );
  }
  assert.ok(
    Object.keys(allowList.files).length <= fileCeiling,
    `the allow-list has ${Object.keys(allowList.files).length} files, ceiling ${fileCeiling}`,
  );
  assert.deepEqual(totals(allowList.files), now, 'the allow-list and the tree disagree; run --tighten');
});

test('the screens of the first batch are off the list for good', () => {
  for (const name of [
    'AddDomainModal', 'Domains', 'DatabaseManagementV2', 'DomainDatabaseManager', 'DomainConnection', 'CurrentSettings',
  ]) {
    const path = `src/components/${name}.tsx`;
    assert.equal(actual[path], undefined, `${path} reads the old way again: ${JSON.stringify(actual[path])}. ${advice}`);
    assert.equal(allowList.files[path], undefined, `${path} is back on the allow-list`);
  }
});
