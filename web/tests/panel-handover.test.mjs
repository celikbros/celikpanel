import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  handoverAddress, handoverHost, handoverSettled, plannedHandoverDrop, recoveryHandover,
  savedSetupHandoverHost, scanRefusedBySetup, setupHandover, setupStartMarkerKey,
} from '../src/lib/panelHandover.ts';
import { en } from '../src/i18n/en.ts';
import { tr } from '../src/i18n/tr.ts';
import { enScreens } from '../src/i18n/screens/en.ts';
import { trScreens } from '../src/i18n/screens/tr.ts';

// 2026-10-08, installed Ubuntu server, v0.1.0-alpha.81: during setup the Panel
// obtained its certificate and was restarted once to serve it. The owner, on
// the server's IP address, saw an overlay naming a finished step ("installing
// certbot") with "connection interrupted / the latest server state is not
// confirmed", then a full page "panel readiness could not be checked" above an
// unrelated update result. Nothing was wrong: the restart is a planned step
// whose outcome the product knows in advance (D-024).

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const HOST = 'boston.example.com';
const step = (kind, status, target = kind === 'panel_certificate' ? HOST : kind) => ({ kind, target, status });
const run = (...steps) => ({ status: 'running', steps });

test('the secured host is compared with the address the browser is on', () => {
  assert.equal(handoverHost('Boston.Example.com.'), HOST);
  for (const value of ['203.0.113.7', 'localhost', '', 'bad_host.example.com', '-a.example.com', 'a..example.com', undefined, 7, `${'a'.repeat(64)}.example.com`]) {
    assert.equal(handoverHost(value), null, String(value));
  }
  const plan = [step('service', undefined, 'nginx'), step('panel_certificate', undefined)];
  // On the IP address or on another name the secure address is worth linking.
  assert.deepEqual(setupHandover(plan, '203.0.113.7'), { host: HOST, phase: 'ahead', elsewhere: true });
  assert.equal(setupHandover(plan, 'other.example.com').elsewhere, true);
  // Already on the host being secured: no link is needed.
  assert.equal(setupHandover(plan, 'BOSTON.example.com').elsewhere, false);
  assert.equal(setupHandover([step('panel_certificate', 'running')], '203.0.113.7').phase, 'now');
  assert.equal(setupHandover([step('panel_certificate', 'succeeded')], '203.0.113.7').phase, 'done');
  // No certificate step, a failed one, or an unusable target: nothing is promised.
  assert.equal(setupHandover([step('service', 'running', 'nginx')], '203.0.113.7'), null);
  assert.equal(setupHandover([step('panel_certificate', 'failed')], '203.0.113.7'), null);
  assert.equal(setupHandover([step('panel_certificate', 'pending', '203.0.113.7')], '203.0.113.7'), null);
  assert.equal(setupHandover(undefined, '203.0.113.7'), null);
});

test('a dropped connection is the planned restart only around the certificate step', () => {
  const ip = '203.0.113.7';
  const planned = [
    // The step is running.
    run(step('service', 'succeeded', 'certbot'), step('panel_certificate', 'running'), step('mail_profile', 'pending')),
    // The step before it had just completed; the certificate step is next.
    run(step('service', 'succeeded', 'certbot'), step('firewall', 'succeeded'), step('panel_certificate', 'pending')),
    // The step finished and the restart follows it; nothing later has finished.
    run(step('panel_certificate', 'succeeded'), step('mail_profile', 'running'), step('verify', 'pending')),
    run(step('panel_certificate', 'succeeded'), step('verify', 'pending')),
  ];
  for (const execution of planned) {
    assert.equal(plannedHandoverDrop(execution, ip)?.host, HOST, JSON.stringify(execution.steps));
  }
  const unknown = [
    null,
    // An earlier step is still running: the restart is not due yet.
    run(step('service', 'running', 'certbot'), step('panel_certificate', 'pending')),
    // A later step has finished: the handover is behind this setup.
    run(step('panel_certificate', 'succeeded'), step('mail_profile', 'succeeded'), step('service', 'running', 'nginx')),
    run(step('service', 'running', 'nginx')),
    { status: 'waiting', steps: [step('panel_certificate', 'running')] },
    { status: 'failed', steps: [step('panel_certificate', 'failed')] },
    { status: 'succeeded', steps: [step('panel_certificate', 'succeeded')] },
  ];
  for (const execution of unknown) {
    assert.equal(plannedHandoverDrop(execution, ip), null, JSON.stringify(execution));
  }
  // On the host name itself the restart is still planned; only the link is dropped.
  assert.deepEqual(plannedHandoverDrop(planned[0], HOST), { host: HOST, phase: 'now', elsewhere: false });
});

test('the handover flag is dropped once the setup has moved past it', () => {
  assert.equal(handoverSettled(null), false);
  assert.equal(handoverSettled(run(step('service', 'succeeded', 'certbot'), step('panel_certificate', 'pending'))), false);
  assert.equal(handoverSettled(run(step('panel_certificate', 'running'))), false);
  assert.equal(handoverSettled(run(step('panel_certificate', 'succeeded'), step('mail_profile', 'running'))), false);
  assert.equal(handoverSettled(run(step('panel_certificate', 'succeeded'), step('mail_profile', 'succeeded'))), true);
  assert.equal(handoverSettled(run(step('panel_certificate', 'failed'))), true);
  assert.equal(handoverSettled({ status: 'succeeded', steps: [step('panel_certificate', 'succeeded')] }), true);
  assert.equal(handoverSettled({ status: 'failed', steps: [step('panel_certificate', 'pending')] }), true);
  assert.equal(handoverSettled(run(step('service', 'running', 'nginx'))), true);
});

test('the secure address is an https link on the Panel port', () => {
  assert.equal(handoverAddress(HOST, '2083'), `https://${HOST}:2083`);
  assert.equal(handoverAddress(HOST, ''), `https://${HOST}`);
  assert.equal(handoverAddress(HOST, '443'), `https://${HOST}`);
  assert.equal(handoverAddress(HOST, 'x'), `https://${HOST}`);
  // The server's own URL wins only when it names exactly this host over HTTPS.
  assert.equal(handoverAddress(HOST, '2083', `https://${HOST}:8443/`), `https://${HOST}:8443`);
  for (const url of [`http://${HOST}:8443/`, 'https://evil.example.net:8443/', `https://user:pw@${HOST}:8443/`, 'not a url']) {
    assert.equal(handoverAddress(HOST, '2083', url), `https://${HOST}:2083`, url);
  }
});

test('the recovery page names the handover only on server evidence', () => {
  assert.equal(setupStartMarkerKey('admin'), 'celikpanel.setup.start.admin');
  const marker = (extra) => JSON.stringify({ request_id: 'a'.repeat(32), plan_id: 'b'.repeat(32), panel_domain: HOST, ...extra });
  assert.equal(savedSetupHandoverHost(marker({ handover: true })), HOST);
  // A setup whose plan had no certificate step, or any other stored value.
  for (const raw of [marker({}), marker({ handover: 'true' }), JSON.stringify({ handover: true, panel_domain: '203.0.113.7' }), 'garbage', '[]', 'null', null, ' '.repeat(5000)]) {
    assert.equal(savedSetupHandoverHost(raw), null, String(raw).slice(0, 40));
  }
  // The Panel reports a managed certificate for that host: the handover is named.
  assert.deepEqual(recoveryHandover(HOST, HOST, '203.0.113.7'), { host: HOST, elsewhere: true });
  assert.deepEqual(recoveryHandover(HOST, HOST, HOST), { host: HOST, elsewhere: false });
  // No server report, another host, or no saved setup: the page cannot know.
  assert.equal(recoveryHandover(HOST, '', '203.0.113.7'), null);
  assert.equal(recoveryHandover(HOST, 'other.example.com', '203.0.113.7'), null);
  assert.equal(recoveryHandover(null, HOST, '203.0.113.7'), null);
  assert.equal(recoveryHandover(HOST, undefined, '203.0.113.7'), null);
});

test('a scan refused because setup owns the host is not a lost connection', async () => {
  const response = (status, body) => ({ status, clone: () => ({ json: async () => { if (body === undefined) throw new Error('not json'); return body; } }) });
  assert.equal(await scanRefusedBySetup(response(409, { code: 'server_setup_busy', error: 'Server setup is in progress.' })), true);
  assert.equal(await scanRefusedBySetup(response(409, { code: 'service_operation_busy' })), false);
  assert.equal(await scanRefusedBySetup(response(503, { code: 'server_setup_busy' })), false);
  assert.equal(await scanRefusedBySetup(response(409, undefined)), false);
  assert.equal(await scanRefusedBySetup(response(409, null)), false);
  // The Panel's refusal code this reads.
  assert.match(read('../../cmd/panel/service_operations.go'), /writeCodedError\(w, http\.StatusConflict, "server_setup_busy"/);
});

test('a finished setup step no longer holds an overlay that calls it installing', () => {
  // The behaviour is run in component-operation-setup-scan-runtime.test.mjs;
  // this holds the shape that keeps it fail-closed.
  const provider = read('../src/components/ComponentOperation.tsx');
  const poll = provider.slice(provider.indexOf('poll = async () => {'), provider.indexOf('        poll();'));
  assert.ok(poll.length > 0, 'the operation poll was not found');
  // The setup refusal is read in one place. It does not release anything: it
  // replaces the refused scan by a read of the scan the operation stored.
  assert.equal(provider.match(/scanRefusedBySetup\(/g)?.length, 1);
  assert.match(poll, /if \(await scanRefusedBySetup\(scanResponse\)\) \{\s*storedByOperation = true;\s*scanResponse = await fetch\('\/api\/v1\/managed-services', \{\s*cache: 'no-store',\s*\}\);\s*\}\s*\} catch \{\s*setConnectionInterrupted\(true\);\s*schedule\(poll, RETRY_DELAY_MS\);\s*return;/);
  assert.equal(provider.match(/storedByOperation = true;/g)?.length, 1);
  // One release, after the checks that judge the stored scan like a fresh one.
  assert.equal(poll.match(/clearStoredOperation\(\);/g)?.length, 1);
  assert.equal(poll.match(/setOperation\(null\);/g)?.length, 1);
  const refused = poll.indexOf('scanRefusedBySetup(');
  const checked = poll.indexOf('|| !snapshotConfirmsTerminalOperation(freshSnapshot, next, storedByOperation)');
  const published = poll.indexOf('setCatalogSnapshot(freshSnapshot);');
  const released = poll.indexOf('clearStoredOperation();');
  assert.ok(refused > 0 && refused < checked && checked < published && published < released, 'the stored scan must pass the snapshot checks before anything is released');
  // The stored scan is accepted only on the caller's word, and only when it is
  // no older than the second the operation started.
  const confirms = provider.slice(provider.indexOf('function snapshotConfirmsTerminalOperation('), provider.indexOf('interface VerifiedMailProfileResult'));
  assert.match(confirms, /storedByOperation = false,\n\): boolean \{/);
  assert.match(confirms, /if \(!Number\.isFinite\(scannedAt\) \|\| !Number\.isFinite\(finishedAt\)\) return false;\s*if \(scannedAt < finishedAt\) \{\s*const startedAt = Date\.parse\(operation\.started_at \|\| ''\);\s*if \(\s*!storedByOperation\s*\|\| !Number\.isFinite\(startedAt\)\s*\|\| scannedAt < Math\.floor\(startedAt \/ 1000\) \* 1000\s*\) \{\s*return false;\s*\}\s*\}/);
  // The Panel stores that scan to the second and refuses a requested scan while setup runs.
  assert.match(read('../../cmd/panel/managed_service_handlers.go'), /INSERT INTO service_scan_cache \(id, data, scanned_at\) VALUES \(1, \?, \?\)[\s\S]{0,200}time\.Now\(\)\.UTC\(\)\.Format\(time\.RFC3339\)\)/);
  // A failed operation never takes the stored scan.
  const failed = provider.slice(provider.indexOf('const refreshFailedSnapshot = async ('), provider.indexOf('let poll: () => Promise<void>;'));
  assert.ok(failed.length > 0);
  assert.doesNotMatch(failed, /scanRefusedBySetup|storedByOperation|fetch\('\/api\/v1\/managed-services',/);
  assert.match(failed, /\|\| !snapshotConfirmsTerminalOperation\(freshSnapshot, terminalOperation\)\n/);
});

test('the wizard states the restart in advance and names it when the connection drops', () => {
  const wizard = read('../src/components/ServerSetup.tsx');
  // Advance notice: only on another address, before the step has finished, on
  // the review and on the progress view; never while a failure is shown.
  assert.match(wizard, /const handoverAhead = reconnecting \|\| completed \|\| execution\?\.status === 'failed' \? null\s*: setupHandover\(execution \? execution\.steps : step === 'review' \? plan\?\.steps : undefined, currentHost\);/);
  assert.match(wizard, /const handoverNotice = handoverAhead\?\.elsewhere && handoverAhead\.phase !== 'done' \? handoverAhead : null;/);
  assert.equal(wizard.match(/\{handoverNotice && <SetupHandoverNotice host=\{handoverNotice\.host\} url=\{handoverURL\} \/>\}/g)?.length, 2);
  // The drop: recognised from the last execution read, replaces the unknown text.
  assert.match(wizard, /const handoverDrop = reconnecting \? plannedHandoverDrop\(execution, currentHost\) : null;/);
  assert.match(wizard, /t\(handoverDrop \? 'setup\.handover\.dropTitle' : reconnecting \? 'setup\.reconnecting'/);
  assert.match(wizard, /\{handoverDrop \? <div role="status"[\s\S]*?t\('setup\.handover\.drop', \{ host: handoverDrop\.host \}\)[\s\S]*?: reconnecting && <p[^>]*>\{t\('setup\.uncertain'\)\}<\/p>\}/);
  // The address is a real link to the wizard on the secure origin.
  assert.match(wizard, /<AddressLink href=\{`\$\{url\}\/setup`\} address=\{url\} \/>/);
  assert.doesNotMatch(wizard.slice(wizard.indexOf('function SetupHandoverAddress(')), /break-all[^\n]*\{url\}/);
  // Browser inspection, 2026-10-08: on the review the notice was after the step
  // list and below the fold. It now leads the review and stays above the step
  // list on the progress view, and the step row itself carries the short note.
  const review = wizard.slice(wizard.indexOf('{step === \'review\' && plan && <section'));
  assert.ok(review.indexOf('<SetupHandoverNotice ') > 0 && review.indexOf('<SetupHandoverNotice ') < review.indexOf('plan.steps.map('), 'the review notice is not above the step list');
  const progress = wizard.slice(wizard.indexOf('<section aria-labelledby="setup-progress-title">'), wizard.indexOf('{step === \'review\' && plan && <section'));
  assert.ok(progress.indexOf('<SetupHandoverNotice ') > 0 && progress.indexOf('<SetupHandoverNotice ') < progress.indexOf('execution?.steps.map('), 'the progress notice is not above the step list');
  assert.equal(wizard.match(/\{handoverNotice && item\.kind === 'panel_certificate' && <p [^>]*>\{t\('setup\.handover\.stepNote'\)\}<\/p>\}/g)?.length, 2);
  // No alarm styling on either surface.
  for (const name of ['SetupHandoverAddress', 'SetupHandoverNotice']) {
    const body = wizard.slice(wizard.indexOf(`function ${name}(`), wizard.indexOf('\n}\n', wizard.indexOf(`function ${name}(`)));
    assert.doesNotMatch(body, /danger|warning|role="alert"/, name);
  }
  // The marker records that the reviewed plan contained the step.
  assert.match(wizard, /plan\.steps\.some\(item => item\.kind === 'panel_certificate'\) \? \{ handover: true \} : \{\}/);
  assert.match(wizard, /if \(!marker\?\.handover \|\| !handoverSettled\(execution\)\) return;/);
});

test('the recovery page explains the restart and keeps the update result one step away', () => {
  const page = read('../src/components/RecoveryAccess.tsx');
  assert.match(page, /usePanelHandover\(user\?\.username, cause === 'availability' \|\| cause === 'starting', checking\)/);
  assert.match(page, /handover \? 'recovery\.handoverTitle' : `recovery\.\$\{cause\}Title`/);
  // Only an unfinished operation is drawn, and during the handover it is closed under its own title.
  assert.match(page, /<RecoveryStatus [^>]*unfinishedOnly=\{cause !== 'bundle'\} disclosed=\{!!handover\} \/>/);
  assert.match(page, /return disclosed \? <details[^>]*><summary[^>]*>\{t\('recovery\.operationTitle'\)\}<\/summary>\{status\}<\/details> : status;/);
  // The release-recovery harness renders this screen from the build's own source
  // (deploy/e2e/release-recovery/web_source_eval.py, component_renderings). It
  // reads two shapes at the top level of RecoveryStatus: one `return <...>;`, or
  // a `const status = <...>;` shown plainly or as the one direct `{status}`
  // child of a wrapper. Any other shape makes the recovery screen unknown there.
  assert.match(page, /\n    const status = <section [\s\S]*?\n    <\/section>;\n    return disclosed \? <details/,
    'RecoveryStatus changed shape: teach web_source_eval.component_renderings the new one and run deploy/e2e/release-recovery/test_owner_update_trial.py (WSL) in the same change');
  // Reads only: the public address metadata and this browser's own marker.
  const hook = page.slice(page.indexOf('function usePanelHandover('), page.indexOf('export function RecoveryStatus('));
  // Through the shared reader (2026-10-10): an unreadable address is unknown and names no handover.
  assert.match(hook, /readRemote\('\/api\/v1\/panel\/access-address', decodeServedHost, undefined, \{ cache: 'no-store', signal: controller\.signal \}\)/);
  assert.match(hook, /result\.state === 'known' && !controller\.signal\.aborted/);
  assert.doesNotMatch(hook, /method:|setItem|removeItem/);
});

test('handover texts exist in both languages, in the half that can show them', () => {
  const screenKeys = ['setup.handover.title', 'setup.handover.notice', 'setup.handover.address', 'setup.handover.addressHelp', 'setup.handover.stepNote', 'setup.handover.dropTitle', 'setup.handover.drop', 'setup.handover.dropAddress'];
  for (const key of screenKeys) assert.ok(enScreens[key] && trScreens[key], key);
  // The recovery page boots without the screen catalogue.
  const shellKeys = ['recovery.handoverTitle', 'recovery.handoverHelp', 'recovery.handoverAddress'];
  for (const key of shellKeys) assert.ok(en[key] && tr[key], key);
  for (const key of ['setup.handover.notice', 'setup.handover.drop']) {
    assert.match(enScreens[key], /\{host\}/, key);
    assert.match(trScreens[key], /\{host\}/, key);
  }
  assert.match(en['recovery.handoverHelp'], /\{host\}/);
  assert.match(tr['recovery.handoverHelp'], /\{host\}/);
  // D-024 order: what happens, that nobody needs to act, how it resumes.
  assert.match(enScreens['setup.handover.notice'], /restarts once.*loses its connection.*planned.*continues on the server.*reconnects by itself.*do not need to do anything/s);
  assert.match(trScreens['setup.handover.notice'], /bir kez yeniden başlar.*bağlantısı.*kesilir.*planlı.*sunucuda devam eder.*kendiliğinden yeniden bağlanır.*bir şey yapmanız gerekmez/s);
  // "You do not need to do anything" is followed by information, never by an instruction:
  // the secure address is where the Panel can also be opened, and its help tells no one to act.
  assert.match(enScreens['setup.handover.address'], /^Once that step has finished, the Panel can also be opened at its secure address:$/);
  assert.match(trScreens['setup.handover.address'], /^O adım bittikten sonra panel güvenli adresinden de açılabilir:$/);
  for (const key of ['setup.handover.address', 'setup.handover.addressHelp', 'setup.handover.stepNote']) {
    assert.doesNotMatch(enScreens[key], /\b(open the Panel|return to this page|wait|continue at|you must|you need to)\b/i, key);
    assert.doesNotMatch(trScreens[key], /açın|dönün|dönüp|bekleyin|devam edin|gerekir/i, key);
  }
  assert.match(enScreens['setup.handover.addressHelp'], /certificate.*has not finished yet, and this page keeps following setup by itself/s);
  assert.match(trScreens['setup.handover.addressHelp'], /sertifika uyarısı.*henüz bitmemiştir; bu sayfa kurulumu kendiliğinden izlemeyi sürdürür/s);
  assert.match(enScreens['setup.handover.drop'], /lost its connection.*expected.*restarts once.*continues on the server.*reconnects by itself.*Do not start setup again/s);
  assert.match(trScreens['setup.handover.drop'], /bağlantısı kesildi.*beklenen.*bir kez yeniden başlar.*sunucuda devam eder.*kendiliğinden yeniden bağlanır.*yeniden başlatmayın/s);
  // The planned state never borrows the unknown-result wording.
  for (const catalogue of [enScreens, trScreens]) {
    for (const key of screenKeys) assert.doesNotMatch(catalogue[key], /not confirmed|could not be verified|doğrulanamadı|doğrulanmadı/, key);
  }
});
