# set4b: extend web/tools/browser-inspect batch 7 for the 9 Oct 2026 corrections (run from that directory).
import io

p = 'scenarios-batch7.mjs'
s = io.open(p, encoding='utf-8', newline='').read()
nl = '\r\n' if '\r\n' in s else '\n'


def rep(a, b):
    global s
    a = a.replace('\n', nl)
    b = b.replace('\n', nl)
    assert s.count(a) == 1, (s.count(a), a[:80])
    s = s.replace(a, b)


rep("""    imported: ['domain', 'files', 'mail', 'forwarders', 'dns', 'database:olduser_shop'],
    not_imported: ['member:/etc/set3-escape-absolute.txt', `member:${LONG_ENTRY}`, 'members:44'],""",
    """    imported: ['domain', 'files', 'mail', 'database:olduser_shop'],
    not_imported: ['member:/etc/set3-escape-absolute.txt', `member:${LONG_ENTRY}`, 'members:44'],
    left_out: ['forwarders', 'dns'],""")
rep("""        { step: 'forwarders', ok: true, detail: '0 forwarders' },
        { step: 'dns', ok: true, detail: 'panel DNS template created; archive DNS import was not selected' },""",
    """        { step: 'forwarders', ok: true, state: 'none_in_archive', detail: '0 forwarders' },
        { step: 'dns', ok: true, state: 'not_chosen', detail: 'panel DNS template created; archive DNS import was not selected' },""")
rep("""const POSTFIX_LINE =""", """// 9 Oct 2026: an import that ended complete on a server whose DNS is the
// owner's external provider. The `dns` step ended without an error and
// imported nothing (`state: left_to_owner`); so did `forwarders`, of which the
// archive holds none.
const EXTERNAL_DNS_DETAIL = 'external DNS ownership preserved; verify provider records before publishing the site';
const LEFT_OUT = {
    domain_id: 9, site_id: 4, domain: 'old.example', status: 'active', domain_status: 'active',
    imported: ['domain', 'files', 'mail', 'database:olduser_shop'], not_imported: [], left_out: ['forwarders', 'dns'],
    steps: [
        { step: 'domain', ok: true, detail: 'old.example (id 9, site 4) → /var/www/celikpanel/subscriptions/3/sites/9/public_html' },
        { step: 'files', ok: true, detail: '412 files, 48234496 bytes' },
        { step: 'mail', ok: true, detail: '1 accounts imported with original passwords (mailbox CONTENTS are not migrated in v1)' },
        { step: 'forwarders', ok: true, state: 'none_in_archive', detail: '0 forwarders' },
        { step: 'dns', ok: true, state: 'left_to_owner', detail: EXTERNAL_DNS_DETAIL },
        { step: 'database:olduser_shop', ok: true, detail: 'created exclusively and dump imported (db USERS are not migrated; repoint app configs)' },
    ],
};
const POSTFIX_LINE =""")
rep("""    const START = ['Start import', 'İçe aktarmayı başlat'];""",
    """    const START = ['Start import', 'İçe aktarmayı başlat'];
    // Each row of the result's step list: its label, the words a screen reader
    // gets after it, its own line, and which of the three marks is drawn.
    const stepRows = (page) => page.evaluate(() => Array.from(document.querySelectorAll('[data-import-result] ul.space-y-2 > li')).map((row) => {
        const icon = row.querySelector('svg');
        const tone = icon ? ['text-success', 'text-danger', 'text-fg-muted'].find((name) => icon.classList.contains(name)) || null : null;
        const said = row.querySelector('.sr-only');
        const label = said ? said.parentElement.cloneNode(true) : null;
        if (label) label.querySelector('.sr-only')?.remove();
        return {
            label: label ? label.textContent.trim() : null, said: said ? said.textContent.replace(/^:\\s*/, '').trim() : null,
            detail: row.querySelector('.text-xs')?.textContent.trim() || null, tone, iconColor: icon ? getComputedStyle(icon).color : null,
            iconVisible: Boolean(icon && icon.getClientRects().length > 0),
        };
    }));
    const NOTHING = ['Nothing imported, nothing failed', 'İçe aktarılan yok, hata da yok'];
    const DNS = ['DNS records', 'DNS kayıtları'];
    const FORWARDERS = ['Forwarders', 'Yönlendirmeler'];
    const leftOutRows = (name, rows) => {
        for (const labels of [DNS, FORWARDERS]) {
            const row = rows.find((item) => labels.includes(item.label));
            must(row, `${name}: the step ${labels[0]} is not in the list: ${JSON.stringify(rows.map((item) => item.label))}`);
            must(row.tone === 'text-fg-muted' && row.iconVisible, `${name}: ${labels[0]} is drawn with the mark "${row.tone}", not the neutral one`);
            must(NOTHING.includes(row.said), `${name}: ${labels[0]} is read out as "${row.said}"`);
        }
        const imported = rows.filter((item) => item.tone === 'text-success');
        must(imported.length > 0 && imported.every((item) => ['Imported', 'İçe aktarıldı'].includes(item.said)), `${name}: an imported step is not read out as imported`);
        must(rows.filter((item) => item.tone === 'text-danger').every((item) => ['Not imported', 'İçe aktarılmadı'].includes(item.said)), `${name}: a step that was not imported is not read out as that`);
        const tones = new Set(rows.map((item) => item.tone));
        const colours = new Set(rows.map((item) => item.iconColor));
        must(!tones.has(null) && colours.size === tones.size, `${name}: two marks share a colour: ${JSON.stringify(rows.map((item) => [item.tone, item.iconColor]))}`);
    };""")
rep("""            await record(page, '141b-import-refused-archive-entries-steps', '[data-import-result] ul.space-y-2');
            await closePage(page);""",
    """            // 9 Oct 2026: a part that imported nothing without failing (DNS that
            // was not chosen, forwarders the archive does not hold) is in
            // neither list of the summary.
            const lists = await page.evaluate(() => (document.querySelector('[data-import-result] [role="alert"] dl')?.innerText || '').replace(/\\s+/g, ' ').trim());
            must(/Website files|Site dosyaları/.test(lists) && /Archive entry|Arşiv girdisi/.test(lists), `141a: the two lists were not read: ${lists.slice(0, 200)}`);
            must(!DNS.some((label) => lists.includes(label)) && !FORWARDERS.some((label) => lists.includes(label)), `141a: a part that imported nothing is in a list of the summary: ${lists.slice(0, 400)}`);
            const entryRows = await stepRows(page);
            await record(page, '141b-import-refused-archive-entries-steps', '[data-import-result] ul.space-y-2', { stepRows: entryRows });
            leftOutRows('141b', entryRows);
            await closePage(page);""")
rep("""    const noteCase = async (page, name, answer, press) => {""",
    """    // 9 Oct 2026 (set4): an import that asked for no DNS on a server whose DNS
    // is the owner's external provider listed `dns` as imported.
    scenarios.importleftout = async () => {
        try {
            await fresh({ [GUARDED.importApply]: { fail: { status: 200, body: LEFT_OUT } } });
            const page = await inspect();
            await clickExact(page, START);
            await waitFor(page, () => Boolean(document.querySelector('[data-import-result]')), 20000);
            await quiet(page);
            const seen = await record(page, '144a-import-complete-dns-left-to-the-owner', '[data-import-result]');
            const result = seen.importResult;
            must(result && result.kind === 'complete' && !result.alert, `144a: the result is drawn as "${result?.kind}"${result?.alert ? ' with an alert' : ''}`);
            must(/Every part you chose was imported, and old\\.example is in service|Seçtiğiniz her parça içe aktarıldı ve old\\.example hizmette/.test(result.text), `144a: a complete import does not say so: ${result.text.slice(0, 200)}`);
            must(result.text.includes(EXTERNAL_DNS_DETAIL), '144a: the DNS step does not say that the records stay with the provider');
            const rows = await stepRows(page);
            must(rows.length === LEFT_OUT.steps.length, `144a: ${rows.length} step rows for ${LEFT_OUT.steps.length} steps`);
            leftOutRows('144a', rows);
            must(rows.filter((item) => item.tone === 'text-danger').length === 0, '144a: a step is drawn as failed');
            must(rows.find((item) => DNS.includes(item.label)).detail === EXTERNAL_DNS_DETAIL, '144a: the DNS row does not carry its own line');
            must(seen.toasts.length === 0, `144a: the result is also a toast: ${JSON.stringify(seen.toasts)}`);
            await record(page, '144b-import-complete-dns-left-to-the-owner-steps', '[data-import-result] ul.space-y-2', { stepRows: rows });
            await closePage(page);
        } finally {
            await done();
        }
    };

    const noteCase = async (page, name, answer, press) => {""")
rep("""                must(/which it runs now|şu an onu çalıştırıyor/.test(previous.lines[1]), `${name}: it is not said what the server runs now`);""",
    """                must(/which it runs now|şu an onu çalıştırıyor/.test(previous.lines[1]), `${name}: it is not said what the server runs now`);
                // 9 Oct 2026 (set4): the one time the server gives is when the
                // attempt ended. It is said as that and never beside "started".
                const ended = await page.evaluate((at, language) => new Date(at).toLocaleString(language === 'tr' ? 'tr-TR' : 'en-US'), attempt.finished_at, locale);
                must(previous.lines[1].includes(`that attempt ended on ${ended}.`) || previous.lines[1].includes(`o deneme ${ended} tarihinde sona erdi.`), `${name}: the time is not said as the end of the attempt (${ended}): ${previous.lines[1]}`);
                must(!/started (here |on this server )?on \\d|tarihinde başlatıldı/.test(previous.lines[1]), `${name}: the end time stands beside "started": ${previous.lines[1]}`);""")
rep("""    const { base, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet } = tools;""",
    """    const { base, ctl, reset, drainLog, newPage, closePage, shot, clickByText, waitFor, pause, quiet, locale } = tools;""")
rep("""//   updaterolledback  the update card says, above Start, that the offered
//                     version was already tried here and rolled back, with the
//                     recorded cause or that none was recorded; Start stays
//                     enabled.""", """//   updaterolledback  the update card says, above Start, that the offered
//                     version was already tried here and rolled back, with the
//                     recorded cause or that none was recorded; Start stays
//                     enabled. Since 9 Oct 2026 the sentence says its time as
//                     the end of that attempt.
//   importleftout     (9 Oct 2026) an import whose DNS was left to the owner's
//                     provider is complete, and the step is neither marked as
//                     imported nor as failed.""")
io.open(p, 'w', encoding='utf-8', newline='').write(s)

p = 'run.mjs'
s = io.open(p, encoding='utf-8', newline='').read()
nl = '\r\n' if '\r\n' in s else '\n'
a = "// `siterefused`, `importentries`, `stopnote` and `updaterolledback` live in"
assert s.count(a) == 1
s = s.replace(a, "// `siterefused`, `importentries`, `importleftout` (9 Oct 2026), `stopnote` and" + nl + "// `updaterolledback` live in")
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('edited')
