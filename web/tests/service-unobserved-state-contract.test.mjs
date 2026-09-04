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
const dashboard = readFileSync(new URL('../src/components/Dashboard.tsx', import.meta.url), 'utf8');
const layout = readFileSync(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');

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

// The same claim, one screen over. The components list learned to say "not
// checked yet"; the dashboard card and the sidebar badge were still counting
// `is_installed === true` and reporting "0 installed" for a host nobody had
// scanned. A count is a statement about a census — where no census exists,
// there is no number to show.
//
// Aynı iddia, bir ekran ötede. Sayım yapılmamış bir makinede gösterilecek
// sayı yoktur.

test('an unobserved host gets no count on the dashboard, and the check instead', () => {
  assert.match(dashboard, /const uncheckedServices = services\.filter\(\(s\) => s\.is_installed === null\);/);
  assert.match(
    dashboard,
    /const hostNeverChecked = services\.length > 0 && uncheckedServices\.length === services\.length;/,
  );

  const guard = dashboard.indexOf('{hostNeverChecked ? (');
  assert.ok(guard >= 0, 'the services card must branch on whether anything was observed');
  const ratio = dashboard.indexOf('${running.length} / ${systemServices.length}');
  assert.ok(ratio > guard, 'a ratio must be reachable only after an observation is proven');

  const card = dashboard.slice(guard, dashboard.indexOf('</section>', guard));
  assert.ok(!card.includes('running.length'), 'the unobserved card must render no count at all');
  assert.match(card, /role="status"/);
  assert.match(card, /t\('services\.notChecked'\)/);
  assert.match(card, /t\('dashboard\.componentsNotCheckedHint'\)/);
  // The same check the Components page offers, and a label that is true
  // because the button really runs it.
  assert.match(card, /onClick=\{scanComponents\}/);
  assert.match(card, /t\('services\.scanNow'\)/);
  // Neutral: a host that has simply not been looked at is not a fault.
  assert.doesNotMatch(card, /warning|danger/);
});

test('the dashboard check runs the same scan and stays fail-closed on a payload it cannot read', () => {
  const scan = dashboard.slice(
    dashboard.indexOf('const scanComponents = async () => {'),
    dashboard.indexOf('// Known installed, explicitly'),
  );
  assert.ok(scan.length > 0, 'the dashboard needs the check it offers');
  assert.match(scan, /'\/api\/v1\/managed-services\/scan'/);
  assert.match(scan, /method: 'POST'/);
  // The decoder that refuses a malformed payload is the same one the initial
  // load uses, and nothing reaches state before it agrees.
  const decode = scan.indexOf('decodeDashboardServices(await response.json())');
  const refuse = scan.indexOf('if (!snapshot) {');
  const apply = scan.indexOf('setServices(snapshot.services);');
  assert.ok(decode >= 0 && refuse > decode && apply > refuse, 'the scan must fail closed before it writes state');
});

test('a partly observed host counts what is known and names what is unchecked', () => {
  // Known installed only. An unchecked row joins neither side of the ratio:
  // not the numerator, not the denominator, not the installed set below it.
  assert.match(dashboard, /const installed = services\.filter\(\(s\) => s\.is_installed === true\);/);
  assert.match(dashboard, /const systemServices = serviceScanFresh[\s\S]*installed\.filter\(\(s\) => s\.kind === 'service'\)/);

  const noteStart = dashboard.indexOf('{uncheckedServices.length > 0 && (');
  const note = dashboard.slice(noteStart, dashboard.indexOf('</button>', noteStart));
  assert.ok(noteStart >= 0 && note.length > 0, 'the unchecked rows must be stated separately, not folded away');
  assert.match(note, /t\('dashboard\.componentsUncheckedOne'\)/);
  assert.match(note, /t\('dashboard\.componentsUnchecked', \{ n: uncheckedServices\.length \}\)/);
  // fg-subtle is for placeholder and disabled text; this is a state the
  // operator acts on.
  assert.match(note, /text-fg-muted/);
});

test('the sidebar badge carries no number for a census nobody took', () => {
  assert.match(layout, /type Counts = Partial<Record<'domains' \| 'databases' \| 'services', number \| null>>;/);
  // Unobserved is null, not zero; a partly observed host still counts what
  // is known, so the unchecked rows are excluded rather than counted absent.
  assert.match(layout, /const observed = s\.filter\(\(service\) => service\.is_installed !== null\);/);
  assert.match(layout, /s\.length > 0 && observed\.length === 0\s*\n\s*\? null/);
  assert.match(layout, /observed\.filter\(\(service\) => service\.is_installed\)\.length/);

  const nullBranch = layout.indexOf('{count === null ? (');
  assert.ok(nullBranch >= 0, 'the badge must distinguish "no answer" from "zero"');
  const numberBranch = layout.indexOf('count !== undefined && count > 0 ?', nullBranch);
  assert.ok(numberBranch > nullBranch, 'a number must be reachable only after "no answer" is ruled out');
  const badge = layout.slice(nullBranch, numberBranch);
  assert.ok(!badge.includes('{count}'), 'the unobserved badge must not print a count');
  assert.match(badge, /t\('services\.notChecked'\)/);
  assert.match(badge, /sr-only/);
});

test('the setup journey asks for the check before suggesting anything off the census', () => {
  assert.match(dashboard, /const componentCensusComplete = uncheckedServices\.length === 0;/);
  assert.match(
    dashboard,
    /key: 'dashboard\.step\.serviceScan'[\s\S]*done: serviceScanFresh && componentCensusComplete,/,
  );
  // Everything the journey decides from installed state stays behind a scan
  // it can trust: an unchecked row keeps the check step open, and only the
  // first open step carries a call to action.
  assert.match(dashboard, /done: serviceScanFresh && dnsServer !== '' && serviceRunning\(dnsServer\)/);
  assert.match(dashboard, /if \(!serviceScanFresh\) return false/);
});

test('the dashboard unchecked copy exists in both locales, in the components screen voice', () => {
  for (const [locale, catalogue] of [['en', english], ['tr', turkish]]) {
    for (const key of [
      'dashboard.componentsNotCheckedHint',
      'dashboard.componentsUncheckedOne',
      'dashboard.componentsUnchecked',
    ]) {
      assert.match(catalogue, new RegExp(`'${key.replace(/\./g, '\\.')}': '`), `${locale} is missing ${key}`);
    }
  }
  const englishHint = english.match(/'dashboard\.componentsNotCheckedHint': '([^']+)'/)?.[1] ?? '';
  const turkishHint = turkish.match(/'dashboard\.componentsNotCheckedHint': '([^']+)'/)?.[1] ?? '';
  assert.match(englishHint, /has been checked yet/);
  assert.match(englishHint, /is not known/);
  assert.match(turkishHint, /henüz hiçbir şeye bakılmadı/);
  assert.match(turkishHint, /bilinmiyor/);
  // It must not restate the claim it exists to remove.
  assert.doesNotMatch(englishHint, /not installed/i);
  assert.doesNotMatch(turkishHint, /kurulu değil/i);

  assert.match(
    english.match(/'dashboard\.componentsUnchecked': '([^']+)'/)?.[1] ?? '',
    /^\{n\} components have not been checked yet$/,
  );
  assert.match(
    turkish.match(/'dashboard\.componentsUnchecked': '([^']+)'/)?.[1] ?? '',
    /^\{n\} bileşene henüz bakılmadı$/,
  );
});
