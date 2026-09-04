import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';

// The browser round of 4 September 2026, run against a real panel on a host
// this project had never scanned, found four things no test could see. This
// file pins them.
//
// 1. The components list probes the host when the operator opens it. That is
//    correct and stays: this IS the inventory page. The rule it makes true is
//    that NO OTHER screen may probe as a side effect of being opened.
// 2. On a fresh host, the per-component page was reachable only by typing its
//    URL: the only link to it was Manage, drawn for an installed non-tool.
// 3. The sidebar badge asked the API once, on mount, so a check the operator
//    ran on another screen left it showing a dash until a full page load.
// 4. The dashboard mail card told the operator the scan was older than five
//    minutes on a host where no scan had ever run.
//
// 4 Eylul 2026 tarayici turu, bu projenin hic taramadigi gercek bir makinede
// kosuldu ve hicbir testin goremedigi dort sey buldu. Bu dosya onlari civiler.

const src = new URL('../src/', import.meta.url);
const read = (relative) => readFileSync(new URL(relative, src), 'utf8');

const serviceList = read('components/ServiceList.tsx');
const serviceShell = read('components/ServiceShell.tsx');
const dashboard = read('components/Dashboard.tsx');
const layout = read('components/Layout.tsx');
const componentDetail = read('components/ComponentDetail.tsx');
const componentOperation = read('components/ComponentOperation.tsx');
const census = read('lib/componentCensus.ts');
const english = read('i18n/en.ts');
const turkish = read('i18n/tr.ts');

const SCAN = '/api/v1/managed-services/scan';

/**
 * Every React effect body in a source file, matched by walking parentheses
 * from `useEffect(` / `useLayoutEffect(` to the call's own closing paren.
 * Crude on purpose: it reads what actually runs when a component mounts.
 */
function effectBodies(source) {
    const bodies = [];
    const opener = /use(?:Layout)?Effect\(/g;
    let match;
    while ((match = opener.exec(source)) !== null) {
        let depth = 0;
        let i = match.index + match[0].length - 1;
        for (; i < source.length; i += 1) {
            if (source[i] === '(') depth += 1;
            else if (source[i] === ')') {
                depth -= 1;
                if (depth === 0) break;
            }
        }
        bodies.push(source.slice(match.index, i + 1));
    }
    return bodies;
}

// ---------------------------------------------------------------------------
// 1. One rule about probing the host, true everywhere
// ---------------------------------------------------------------------------

test('the inventory page is the only screen that probes the host on open', () => {
    // Opening the components list runs a host-wide check when the cache is
    // missing or stale. This is deliberate and stays: the page's whole
    // purpose is "what is on this server", and the probe is read-only.
    // Bileşenler listesini açmak, önbellek yoksa ya da bayatsa sistem geneli
    // bir kontrol çalıştırır. Bu kasıtlıdır ve kalır.
    assert.match(
        serviceList,
        /`\/api\/v1\/managed-services\/scan\?max_age_seconds=\$\{AUTO_SCAN_MAX_AGE_SECONDS\}`/,
    );
    const listEffects = effectBodies(serviceList);
    assert.ok(
        listEffects.some((body) => body.includes('void loadServices()')),
        'the inventory page must still check the host when it is opened',
    );

    // And nowhere else. A page visit that probes the machine turns every
    // screen into an inventory page and makes the operator's own check
    // meaningless, because something already ran one.
    // Ve başka hiçbir yerde. Makineyi yoklayan bir sayfa ziyareti, her ekranı
    // envanter sayfasına çevirir ve operatörün kendi kontrolünü anlamsızlaştırır.
    const triggers = /managed-services\/scan|scanComponents\(|runCheck\(|\bscan\(\)/;
    for (const [name, source] of [
        ['Dashboard.tsx', dashboard],
        ['ServiceShell.tsx', serviceShell],
        ['ComponentDetail.tsx', componentDetail],
        ['Layout.tsx', layout],
    ]) {
        for (const body of effectBodies(source)) {
            assert.doesNotMatch(
                body,
                triggers,
                `${name} must not scan the host as a side effect of being opened`,
            );
        }
    }

    // The per-service page and the dashboard keep their checks; both are
    // reached from a control the operator presses, never from mount.
    assert.match(serviceShell, /onClick=\{\(\) => void runCheck\(\)\}/);
    assert.match(dashboard, /onClick=\{scanComponents\}/);
    // A component page must not scan on open, and never has to: its state
    // comes from the read-only cached snapshot.
    assert.doesNotMatch(componentDetail, /managed-services\/scan/);
});

test('the one scan outside a page open belongs to an operation already running', () => {
    // The operation controller rescans after a privileged operation reaches a
    // terminal state. That is not a page side effect: it is the operator's own
    // install or uninstall confirming what it did to the machine, and it is
    // gated on an operation being in flight.
    // İşlem denetleyicisi, ayrıcalıklı bir işlem sonlandığında yeniden tarar.
    // Bu bir sayfa yan etkisi değildir: operatörün kendi kurulumunun makineye
    // ne yaptığını doğrulamasıdır ve koşan bir işleme bağlıdır.
    const scanning = effectBodies(componentOperation).filter((body) => body.includes(SCAN));
    assert.equal(scanning.length, 1, 'exactly one operation effect may rescan');
    assert.match(scanning[0], /if \(!operation\?\.id\) return;/);
});

// ---------------------------------------------------------------------------
// 2. Every row is a door to its own page
// ---------------------------------------------------------------------------

test('every component row links to its component page, whatever its state', () => {
    // The link is on the row itself, unconditionally: not behind
    // `is_installed`, not behind `kind`, and not behind having been checked.
    // On a fresh host none of those are true of anything, and that is exactly
    // where the page has the most to say.
    // Bağlantı, koşulsuz olarak satırın kendisindedir. Taze bir makinede
    // bunların hiçbiri hiçbir şey için doğru değildir.
    const rowStart = serviceList.indexOf('{group.map((s) => {');
    assert.ok(rowStart > 0, 'the component rows must stay in one place');
    const rows = serviceList.slice(rowStart, serviceList.indexOf('</ul>', rowStart));

    const link = rows.indexOf('<Link');
    assert.ok(link > 0, 'each row needs a real link, not a click handler');
    assert.match(rows.slice(link), /to=\{`\/services\/\$\{s\.id\}`\}/);
    // A real anchor: the keyboard and middle-click reach it, which a div with
    // an onClick never does.
    assert.match(serviceList, /import \{ Link, useLocation, useNavigate \} from '\.\.\/router';/);
    assert.match(read('router.tsx'), /return <a \{\.\.\.rest\} href=\{to\}/);

    // The accessible name is the component's own name.
    assert.match(rows.slice(link), /\{s\.name\}\s*\n\s*<\/Link>/);
    // Keyboard users must see where they are.
    assert.match(rows.slice(link), /focus-visible:ring-2/);

    // The whole row is the target, and every real control still sits above it.
    assert.match(rows.slice(link), /after:absolute after:inset-0/);
    assert.match(rows, /<li key=\{s\.id\} className="relative /);
    assert.match(rows, /className="relative ml-auto flex min-w-0 items-center justify-end/);

    // Manage stays the primary action where it exists; the row link is not a
    // replacement for it.
    assert.match(rows, /onManageService\?\.\(s\.id, s\.versions\)/);
});

// ---------------------------------------------------------------------------
// 3. The badge follows the operator's check
// ---------------------------------------------------------------------------

test('the sidebar badge reads the census every screen publishes, and never fetches twice', () => {
    // One store, written by whichever screen received a host-wide payload.
    // Tek depo; sistem geneli yükü hangi ekran aldıysa o yazar.
    for (const [name, source] of [
        ['ServiceList.tsx', serviceList],
        ['ServiceShell.tsx', serviceShell],
        ['Dashboard.tsx', dashboard],
    ]) {
        assert.match(
            source,
            /publishComponentCensus\(/,
            `${name} already holds the answer and must publish it`,
        );
    }

    // The shell subscribes; it does not ask again and it does not poll.
    assert.match(layout, /const serviceCensus = useComponentCensus\(\);/);
    assert.match(layout, /services: serviceCensus/);
    assert.match(census, /useSyncExternalStore\(/);
    // Its one read seeds the store instead of owning a private count.
    assert.match(layout, /api\.getServices\(\)\s*\n\s*\.then\(publishComponentCensus\)/);
    assert.equal(
        layout.match(/managed-services|getServices\(/g)?.length,
        1,
        'the shell must not gain a second components request',
    );
    assert.doesNotMatch(layout, /setInterval[\s\S]{0,200}getServices/);
});

// ---------------------------------------------------------------------------
// 4. The mail card knows unknown from stale
// ---------------------------------------------------------------------------

test('the dashboard mail card says unknown on a host nobody has checked', () => {
    // "Older than five minutes" is a claim about a scan. On a host where no
    // scan has ever run there is no scan to be old, so the card says what is
    // true and carries the same check as the card beside it.
    // "Beş dakikadan eski" bir tarama hakkında iddiadır. Hiç tarama koşmamış
    // makinede eskiyecek tarama yoktur.
    assert.match(dashboard, /hostNeverChecked=\{hostNeverChecked\}/);
    assert.match(dashboard, /onCheck=\{scanComponents\}/);
    const card = dashboard.slice(dashboard.indexOf('function MailStackSummary'));
    // Unknown is decided before staleness, or the fold comes straight back.
    assert.match(card, /const statusKey: TranslationKey = hostNeverChecked\s*\n\s*\? 'services\.notChecked'/);
    assert.match(card, /const detail = hostNeverChecked\s*\n\s*\? t\('dashboard\.mailStacks\.notChecked'\)/);
    // Neutral, like the system-services card: not yet looked at is not a fault.
    assert.match(card, /hostNeverChecked \|\| !scanFresh \|\| availableOnly\s*\n\s*\? 'bg-surface-2 text-fg-muted'/);
    // The check runs here rather than pointing at another page.
    assert.match(card, /onClick=\{onCheck\}[\s\S]{0,600}t\('services\.scanNow'\)/);
});

// ---------------------------------------------------------------------------
// Copy
// ---------------------------------------------------------------------------

test('the new copy exists in both locales and claims nothing about the host', () => {
    for (const [locale, catalogue] of [['en', english], ['tr', turkish]]) {
        for (const key of [
            // the list's own check, while it runs and when it fails
            'services.checkingNow',
            'services.showingLastSeen',
            'services.autoCheckFailed',
            // the mail card's unknown state
            'dashboard.mailStacks.notChecked',
            // the two counts, reconciled
            'dashboard.svcNoneUnitlessOne',
            'dashboard.svcNoneUnitless',
        ]) {
            assert.match(
                catalogue,
                new RegExp(`'${key.replace(/\./g, '\\.')}': '`),
                `${locale} is missing ${key}`,
            );
        }
    }
    // Neither locale may describe a scan's age on a host that has none.
    assert.doesNotMatch(
        english.slice(english.indexOf("'dashboard.mailStacks.notChecked'")).split('\n')[0],
        /five minutes|older/,
    );
});

test('no screen was left behind: every managed-services scan caller is accounted for', () => {
    // A new screen that starts probing on open would otherwise slip in
    // unnoticed. This list is the audit, kept honest by failing when it grows.
    // Açılışta yoklamaya başlayan yeni bir ekran fark edilmeden sızardı.
    const allowed = new Set([
        'components/ServiceList.tsx',   // the inventory page: on open and on click
        'components/ServiceShell.tsx',  // the operator's check, from a button
        'components/Dashboard.tsx',     // the operator's check, from a button
        'components/ComponentOperation.tsx', // after an operation the operator started
        'lib/api.ts',                   // the client method those callers share
    ]);
    const found = new Set();
    const walk = (dir, prefix) => {
        for (const entry of readdirSync(new URL(dir, src), { withFileTypes: true })) {
            if (entry.isDirectory()) {
                walk(`${dir}${entry.name}/`, `${prefix}${entry.name}/`);
            } else if (/\.tsx?$/.test(entry.name)) {
                if (read(`${dir}${entry.name}`).includes('managed-services/scan')) {
                    found.add(`${prefix}${entry.name}`);
                }
            }
        }
    };
    walk('', '');
    assert.deepEqual([...found].sort(), [...allowed].sort());
});
