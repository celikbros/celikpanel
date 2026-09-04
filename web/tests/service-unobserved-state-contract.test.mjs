import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// R-040. The components screen used to state, as fact, something the product
// had never checked: on a host with no scan row the API served the whole
// catalogue as `is_installed: false, status: "not_installed"`, and the browser
// drew "Not installed" beside every component. On 3 September 2026 the same
// host reported BIND installed on /api/v1/dns/engine at the same moment.
//
// These tests pin the fix on both sides of the wire: the API answers null for
// "never observed", and the screen says "not checked yet" and offers the check
// instead of reporting an inventory nobody took.
//
// R-040. Bileşenler ekranı, ürünün hiç bakmadığı bir şeyi olgu diye
// söylüyordu. Bu testler düzeltmeyi telin iki yakasında da çivilerler.

const serviceList = readFileSync(new URL('../src/components/ServiceList.tsx', import.meta.url), 'utf8');
const english = readFileSync(new URL('../src/i18n/en.ts', import.meta.url), 'utf8');
const turkish = readFileSync(new URL('../src/i18n/tr.ts', import.meta.url), 'utf8');
const panelHandlers = readFileSync(new URL('../../cmd/panel/managed_service_handlers.go', import.meta.url), 'utf8');

test('the API answers three states, so "never observed" cannot serialise as "absent"', () => {
  // The pointer is the contract. A plain bool here can only say yes or no,
  // and "we have not looked" would have to borrow one of them again.
  assert.match(panelHandlers, /IsInstalled \*bool `json:"is_installed"`/);
  assert.match(panelHandlers, /status = "unknown"/);
  // An unobserved row must not carry conflict or requirement claims either:
  // both are read off an installed set that is unknown, not empty.
  const unobserved = panelHandlers.slice(
    panelHandlers.indexOf('case !observed:'),
    panelHandlers.indexOf('case !o.IsInstalled:'),
  );
  assert.ok(unobserved.length > 0, 'the unobserved branch must stay explicit');
  assert.doesNotMatch(unobserved, /conflictWith = |RequirementsMissing\(/);
});

test('the browser decodes the three-state answer instead of failing the payload', () => {
  // The R-041 lesson: an API answer the browser cannot decode is an answer
  // the product does not have. Every fail-closed decoder of this payload has
  // to accept null, or a never-scanned host renders as a broken page.
  assert.match(serviceList, /is_installed: boolean \| null;/);
  assert.match(
    serviceList,
    /\(service\.is_installed === null \|\| typeof service\.is_installed === 'boolean'\)/,
  );
  for (const file of ['Dashboard.tsx', 'ComponentOperation.tsx']) {
    const source = readFileSync(new URL(`../src/components/${file}`, import.meta.url), 'utf8');
    assert.doesNotMatch(
      source,
      /typeof service\.is_installed !== 'boolean'\n|&& typeof service\.is_installed === 'boolean'\n/,
      `${file} must accept the null answer instead of rejecting the payload`,
    );
  }
});

test('an unchecked component reads "not checked yet", never "not installed"', () => {
  assert.match(serviceList, /const notChecked = \(s: ManagedService\) => s\.is_installed === null;/);

  const statusStart = serviceList.indexOf('<StatusDot ok={s.is_installed === true');
  assert.ok(statusStart >= 0, 'the status dot must not treat an unknown answer as installed');
  const statusCell = serviceList.slice(statusStart, serviceList.indexOf('</span>', statusStart));
  const uncheckedCopy = statusCell.indexOf(`t('services.notChecked')`);
  const absentCopy = statusCell.indexOf(`t('services.notInstalled')`);
  assert.ok(uncheckedCopy >= 0, 'the unchecked state needs its own copy');
  assert.ok(
    absentCopy > uncheckedCopy,
    '"not installed" must be reachable only after the unchecked state is ruled out',
  );
});

test('an unchecked component offers no install, conflict or requirement claim', () => {
  const controls = serviceList.slice(
    serviceList.indexOf('{!s.is_installed ? ('),
    serviceList.indexOf(') : s.requires_missing', serviceList.indexOf('{!s.is_installed ? (')),
  );
  const guard = controls.indexOf('{notChecked(s) ? null : ');
  assert.ok(guard >= 0, 'the not-installed control chain must be gated on an actual observation');
  assert.ok(
    guard < controls.indexOf(`s.conflict_with ? (`),
    'the unchecked guard must precede every badge that claims something about the host',
  );
});

test('the unchecked server is told so plainly, with the check one click away', () => {
  const noticeStart = serviceList.indexOf('{!loading && hostNeverChecked && (');
  assert.ok(noticeStart >= 0, 'a server with no observation at all needs its own notice');
  const notice = serviceList.slice(noticeStart, serviceList.indexOf('</section>', noticeStart));
  assert.match(notice, /role="status"/);
  assert.match(notice, /t\('services\.notCheckedTitle'\)/);
  assert.match(notice, /t\('services\.notCheckedHint'\)/);
  // The action is the scan this page already has, not a new endpoint.
  assert.match(notice, /onClick=\{scan\}/);
  assert.match(notice, /t\('services\.scanNow'\)/);
  // Neutral, not an alarm: a host nobody has looked at yet is the normal
  // first state of a fresh or restored server.
  assert.doesNotMatch(notice, /warning|danger/);

  assert.match(
    serviceList,
    /const hostNeverChecked = services\.length > 0 && services\.every\(notChecked\);/,
  );
  // The installed-only view must not hide what was never checked; hiding it
  // would restore the very claim this fix removes.
  assert.match(serviceList, /!hideNotInstalled \|\| s\.is_installed !== false/);
});

test('the unchecked copy exists in both locales and claims nothing about the host', () => {
  for (const [locale, catalogue] of [['en', english], ['tr', turkish]]) {
    for (const key of ['services.notChecked', 'services.notCheckedTitle', 'services.notCheckedHint']) {
      assert.match(
        catalogue,
        new RegExp(`'${key.replace('.', '\\.')}': '`),
        `${locale} is missing ${key}`,
      );
    }
  }
  const englishNotChecked = english.match(/'services\.notChecked': '([^']+)'/)?.[1] ?? '';
  const turkishNotChecked = turkish.match(/'services\.notChecked': '([^']+)'/)?.[1] ?? '';
  assert.equal(englishNotChecked, 'Not checked yet');
  assert.equal(turkishNotChecked, 'Henüz bakılmadı');
  assert.notEqual(englishNotChecked, english.match(/'services\.notInstalled': '([^']+)'/)?.[1]);
  assert.notEqual(turkishNotChecked, turkish.match(/'services\.notInstalled': '([^']+)'/)?.[1]);

  const englishHint = english.match(/'services\.notCheckedHint': '([^']+)'/)?.[1] ?? '';
  const turkishHint = turkish.match(/'services\.notCheckedHint': '([^']+)'/)?.[1] ?? '';
  assert.match(englishHint, /has been looked at on this server yet/);
  assert.match(englishHint, /known to be installed or missing/);
  assert.match(turkishHint, /henüz bakılmadı/);
  assert.match(turkishHint, /hangisinin kurulu, hangisinin eksik olduğu bilinmiyor/);
});
