// Batch 9 (2026-10-10): the owner's reports after the alpha.82 update.
//
//   finalcheck   the setup page's final check: a run stopped because its plan
//                was reopened while the check waited for the mail host's
//                reverse DNS (the owner's state), the same check waiting for
//                the owner, an unmet prerequisite without a typed reason, and a
//                check that could not be read. Recorded: the lead above the
//                step list, which text is in the failure colour, whether the
//                reason is inside a folded section, and the final step's state.
//   updatephase  the update notice in the corner and the update card together,
//                on Settings, updates: the record running while this Panel is
//                already the target ("verifying"), a read that failed, and a
//                finished record (the page reloads once and no notice is left).
//                Then the update dialog's time line, which is local time named
//                with its zone.
//
// Like the batches before: the mock on 127.0.0.1 answers everything; nothing
// else is contacted, nothing is started.
//
// Dokuzuncu grup: kurulumun son denetimi ve guncelleme bildirimi ile karti.

const HOST = 'panel.example.com';
const REQ_ID = 'd'.repeat(32);
const UPDATE_ID = 'f'.repeat(32);
const TARGET = { version: 'v0.1.0-alpha.82', commit: '1'.repeat(40), sequence: '82', os: 'linux', arch: 'amd64', archive_sha256: '2'.repeat(64), archive_size: '1048576' };
const PTR = { id: 'mail_identity', state: 'action_required', code: 'mail_identity_required', reason: 'reverse_dns_mismatch', vars: { hostname: 'mail.example.com', ip: '203.0.113.10', ptr: 'static.10.113.0.203.provider.example' } };
const DONE = ['succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'succeeded', 'pending'];
const CONTEXT = { dns_mode: 'external', dns_role: '', dns_engine: 'bind', local_nameserver: '', local_ip: '', peer_nameserver: '', peer_ip: '', panel_domain: HOST, mail_hostname: 'mail.example.com', dns_hosting_management: '' };

// What stands above the step list, in which colour, and whether it is folded.
const finalFacts = page => page.evaluate(() => {
    const probe = document.createElement('p'); probe.className = 'text-danger'; document.body.appendChild(probe);
    const danger = getComputedStyle(probe).color; probe.remove();
    const section = document.querySelector('section[aria-labelledby="setup-progress-title"]');
    const list = section?.querySelector('ol');
    const before = node => !!list && !!(node.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING);
    const lines = Array.from(section?.querySelectorAll('h2,h3,p,li') || []).filter(before).filter(node => !node.closest('details') || node.closest('details').open);
    const reason = section?.querySelector('[data-setup-checks]');
    const steps = Array.from(list?.querySelectorAll('[data-step-state]') || []).map(node => `${node.getAttribute('data-step-state')}: ${node.innerText.replace(/\s+/g, ' ')}`);
    const r = reason?.getBoundingClientRect();
    return {
        leadText: lines.map(node => node.innerText.slice(0, 160)),
        dangerText: lines.filter(node => getComputedStyle(node).color === danger).map(node => node.innerText.slice(0, 80)),
        reasonFolded: reason ? !!reason.closest('details') : null,
        reasonAboveList: reason && list ? !!(reason.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING) : null,
        reasonTop: r ? Math.round(r.top) : null,
        fold: window.innerHeight,
        steps,
        foldedSummaries: Array.from(section?.querySelectorAll('summary') || []).map(node => node.innerText),
    };
});

// The notice in the corner and the card's lines, read together.
const updateFacts = page => page.evaluate(() => {
    const notice = document.querySelector('[aria-labelledby="system-update-background-title"]');
    const dialog = document.querySelector('[aria-labelledby="system-update-operation-title"]');
    const card = document.querySelector('[aria-labelledby="panel-update-title"]');
    return {
        notice: notice ? notice.innerText.split('\n').filter(Boolean) : null,
        dialog: dialog ? dialog.innerText.split('\n').filter(Boolean) : null,
        cardCurrent: card?.querySelector('dd')?.innerText ?? null,
        cardPhase: card?.querySelector('[data-update-phase]')?.innerText ?? null,
        url: location.pathname + location.search,
    };
});

export default function batch9(scenarios, { base, ctl, reset, newPage, closePage, shot, pause, waitFor }) {
    scenarios.finalcheck = async () => {
        const cases = {
            '90a-reopened-waiting-ptr': { setupStatus: 'draft', marker: false, extra: { status: 'failed', phase: 'verification', context: CONTEXT, checks: [PTR], error: { code: 'server_setup_plan_revised', message: 'The administrator reopened the plan. Completed host changes remain in place.' } } },
            '90b-waiting-owner-ptr': { setupStatus: 'waiting', marker: true, extra: { status: 'waiting', phase: 'verification', context: CONTEXT, checks: [PTR] } },
            '90c-waiting-prerequisite': { setupStatus: 'waiting', marker: true, extra: { status: 'waiting', phase: 'verification', context: CONTEXT, checks: [{ id: 'mail_delivery', state: 'action_required', code: 'mail_delivery_required' }] } },
            '90d-check-not-read': { setupStatus: 'waiting', marker: true, extra: { status: 'waiting', phase: 'verification', context: CONTEXT, checks: [{ id: 'mail_identity', state: 'unknown', code: 'mail_identity_unavailable' }] } },
            // Follow-up (2026-10-10): the Agent's reverse DNS lookup got no answer, and a plan whose mail host name is not a full host name.
            '90e-reverse-dns-unknown': { setupStatus: 'waiting', marker: true, extra: { status: 'waiting', phase: 'verification', context: CONTEXT, checks: [{ id: 'mail_identity', state: 'unknown', code: 'mail_identity_unavailable', reason: 'reverse_dns_unknown', vars: { ip: '203.0.113.10', error: 'timeout' } }] } },
            '90f-name-not-canonical': { setupStatus: 'waiting', marker: true, extra: { status: 'waiting', phase: 'verification', context: CONTEXT, checks: [{ id: 'mail_identity', state: 'action_required', code: 'mail_identity_required', reason: 'mail_name_not_canonical', vars: { name: 'mail' } }] } },
            '90g-reopened-ptr-unconfirmed': { setupStatus: 'draft', marker: false, extra: { status: 'failed', phase: 'verification', context: CONTEXT, checks: [{ ...PTR, vars: { hostname: 'mail.example.com', ip: '203.0.113.10' } }], error: { code: 'server_setup_plan_revised', message: 'The administrator reopened the plan. Completed host changes remain in place.' } } },
        };
        for (const [name, item] of Object.entries(cases)) {
            await reset();
            await ctl({ setupStatus: item.setupStatus, execution: { request_id: REQ_ID, statuses: DONE, extra: item.extra } });
            const storage = item.marker ? { 'celikpanel.setup.start.admin': JSON.stringify({ request_id: REQ_ID, plan_id: 'a'.repeat(32), panel_domain: HOST }) } : {};
            const page = await newPage(storage);
            await page.goto(`${base}/setup`, { waitUntil: 'networkidle0' });
            await page.waitForSelector('#setup-progress-title', { timeout: 20000 });
            await pause(500);
            await shot(page, name, await finalFacts(page));
            await closePage(page);
        }
    };

    scenarios.updatephase = async () => {
        const followed = (createdAgo) => ({ 'celikpanel.system-update-operation.v1': JSON.stringify({ state_version: 1, phase: 'active', marker: { marker_version: 1, request_id: UPDATE_ID, current_version: 'v0.1.0-alpha.81', current_commit: '3'.repeat(40), created_at: Date.now() - createdAgo, target: TARGET } }) });
        const installed = { '/api/v1/panel/version': { status: 200, body: { version: TARGET.version, commit: TARGET.commit, agent_commit: TARGET.commit, agent_matches: true, hostname: 'server1', ipv4: '203.0.113.10' } } };
        const settle = page => waitFor(page, () => !!document.querySelector('[aria-labelledby="system-update-background-title"]') && !!document.querySelector('[aria-labelledby="panel-update-title"] dd'), 20000).catch(() => {});

        // Verifying: past the two-minute lock, so the notice is in the corner beside the card.
        await reset();
        await ctl({ clear: ['/api/v1/panel/update/status', '/api/v1/panel/version'] });
        await ctl({ update: { request_id: UPDATE_ID, status: 'running', target: TARGET, phase: 'verifying' }, override: installed });
        let page = await newPage(followed(123_000));
        await page.goto(`${base}/settings?section=updates`, { waitUntil: 'domcontentloaded' });
        await settle(page);
        await pause(2500);
        await shot(page, '91a-verifying-notice-and-card', await updateFacts(page));
        await closePage(page);

        // The status read fails: unknown with its reason, never "being applied".
        await reset();
        await ctl({ clear: ['/api/v1/panel/update/status', '/api/v1/panel/version'] });
        await ctl({ update: { request_id: UPDATE_ID, status: 'running', target: TARGET }, override: { ...installed, '/api/v1/panel/update/status': { status: 503, body: { code: 'PANEL_UPDATE_STATUS_UNAVAILABLE', error: 'the update status is temporarily unavailable' } } } });
        page = await newPage(followed(123_000));
        await page.goto(`${base}/settings?section=updates`, { waitUntil: 'domcontentloaded' });
        await settle(page);
        await pause(2500);
        await shot(page, '91b-status-unread', await updateFacts(page));
        await closePage(page);

        // Finished: the page reloads once on the new version and no notice is left.
        await reset();
        await ctl({ clear: ['/api/v1/panel/update/status', '/api/v1/panel/version'] });
        await ctl({ update: { request_id: UPDATE_ID, status: 'succeeded', target: TARGET }, override: installed });
        page = await newPage(followed(123_000));
        await page.goto(`${base}/settings?section=updates`, { waitUntil: 'domcontentloaded' });
        await waitFor(page, () => location.search.includes('_cp_update'), 20000).catch(() => {});
        await pause(4000);
        await shot(page, '91c-finished-no-notice', await updateFacts(page));
        await closePage(page);

        // Inside the first two minutes: the dialog's time line is local time with its zone.
        await reset();
        await ctl({ clear: ['/api/v1/panel/update/status', '/api/v1/panel/version'] });
        await ctl({ update: { request_id: UPDATE_ID, status: 'running', target: TARGET } });
        page = await newPage(followed(5_000));
        await page.goto(`${base}/settings?section=updates`, { waitUntil: 'domcontentloaded' });
        await waitFor(page, () => /\d{1,2}[:.]\d{2}[:.]\d{2}/.test(document.querySelector('[aria-labelledby="system-update-operation-title"]')?.innerText || ''), 20000).catch(() => {});
        await pause(500);
        await shot(page, '91d-dialog-time-label', { ...(await updateFacts(page)), browserZone: await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone) });
        await closePage(page);
    };

    // Follow-up (2026-10-10): the hold layer while the Panel does not answer,
    // with this browser's update record started 5 minutes ago (the update is
    // named as a possible cause) and 31 minutes ago (the generic sentence);
    // then a route whose code is still loading after half a minute.
    scenarios.waitcopy = async () => {
        const holdFacts = page => page.evaluate(() => {
            const hold = document.querySelector('[aria-labelledby="access-hold-title"]');
            return { hold: hold ? hold.innerText.split('\n').filter(Boolean) : null };
        });
        for (const [name, ago] of [['92a-hold-update-recent', 5 * 60_000], ['92b-hold-update-stale', 31 * 60_000]]) {
            await reset();
            await ctl({ validity: 3, update: { request_id: UPDATE_ID, status: 'running', target: TARGET } });
            const record = { state_version: 1, phase: 'active', marker: { marker_version: 1, request_id: UPDATE_ID, current_version: 'v0.1.0-alpha.81', current_commit: '3'.repeat(40), created_at: Date.now() - ago, target: TARGET } };
            const page = await newPage({ 'celikpanel.system-update-operation.v1': JSON.stringify(record) });
            await page.goto(`${base}/domains`, { waitUntil: 'domcontentloaded' });
            await waitFor(page, () => !!document.querySelector('nav, aside'), 20000).catch(() => {});
            await pause(800);
            await ctl({ mode: 'down' });
            await page.waitForSelector('[aria-labelledby="access-hold-title"]', { timeout: 30000 }).catch(() => {});
            await pause(2500);
            await shot(page, name, await holdFacts(page));
            await ctl({ mode: 'ok' });
            await closePage(page);
        }

        await reset();
        const page = await newPage();
        await page.setRequestInterception(true);
        // The update tracker that wraps the whole interface never arrives: the first wait is the interface loading.
        page.on('request', request => { if (/\/assets\/SystemUpdateOperation-[^/]+\.js$/.test(request.url())) return; request.continue(); });
        await page.goto(`${base}/settings?section=updates`, { waitUntil: 'domcontentloaded' });
        await pause(3000);
        await shot(page, '93a-loading-wait', { text: await page.evaluate(() => document.body.innerText.split('\n').filter(Boolean)) });
        await pause(30_000);
        await shot(page, '93b-loading-prolonged', { text: await page.evaluate(() => document.body.innerText.split('\n').filter(Boolean)) });
        await closePage(page);
    };
}
