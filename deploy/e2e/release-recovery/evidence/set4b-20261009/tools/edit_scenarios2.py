# set4b: the stopnote scenario also shows a Stop whose unit was not read as settled (run from web/tools/browser-inspect).
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


rep("""    scenarios.stopnote = async () => {""",
    """    // 9 Oct 2026: a Stop that succeeded while the unit's own stop was not read
    // (systemd was still stopping the unit when the wait ended, or the unit
    // could not be read). It is said, on the attention surface, and it claims
    // the unit neither failed nor clean.
    const pendingCase = async (page, name, answer, press) => {
        await ctl({ override: { '/api/v1/service/action': { method: 'POST', status: 200, body: answer } } });
        await press(page);
        await waitFor(page, () => Boolean(document.querySelector('[data-service-action]')), 15000);
        await quiet(page);
        await pause(5600);
        const seen = await record(page, name, '[data-service-action]');
        const notice = seen.serviceAction;
        const vars = answer.note.vars;
        must(notice, `${name}: the note is not on the page`);
        must(notice.tone === 'note' && notice.role === 'status', `${name}: drawn as "${notice.tone}" with role "${notice.role}"`);
        must(!notice.surface.redder, `${name}: a successful Stop stands on the failure surface (${notice.surface.background})`);
        must(/was stopped and is not running|durduruldu ve çalışmıyor/.test(notice.text), `${name}: it is not said that the service stopped: ${notice.text.slice(0, 200)}`);
        must(notice.text.includes(vars.pending_unit), `${name}: the unit is not named`);
        must(vars.state
            ? notice.text.includes(vars.state) && /had not finished stopping|durdurmayı bitirmemişti/.test(notice.text) && /may end marked as failed|failed olarak işaretlenmiş olabilir/.test(notice.text)
            : /could not be read from systemd|systemd’den okunamadı/.test(notice.text) && /it is not known whether|bilinmiyor/.test(notice.text),
        `${name}: it is not said what was not read: ${notice.text.slice(0, 300)}`);
        must(notice.code === vars.command && /^systemctl status /.test(notice.code), `${name}: the command is not set apart, or it is not one that only shows the unit: ${notice.code}`);
        must(/nothing looks again by itself|hiçbir şey kendiliğinden yeniden bakmaz/.test(notice.text), `${name}: it is not said that nothing looks again`);
        must(!/now shows .* as failed|olarak gösteriyor|reset-failed|leaves it as it is|olduğu gibi bırakır/.test(notice.text), `${name}: the note claims a mark that was not read: ${notice.text.slice(0, 300)}`);
        must(!notice.text.includes(answer.note.error), `${name}: the server's English sentence is shown in place of the catalogue's`);
        must(!/\\{\\w+\\}|SERVICE_ACTION|unit_not_settled|unit_state_not_read|did not stop|durmadı/.test(notice.text), `${name}: a placeholder, an internal name or a failure is on screen`);
        must(notice.inView, `${name}: the note is outside the window`);
        must(seen.toasts.length === 0, `${name}: the note is also a toast: ${JSON.stringify(seen.toasts)}`);
        await clickExact(page, ['Close', 'Kapat']);
        await pause(250);
        must(!(await facts(page)).serviceAction, `${name}: the note stayed after Close`);
    };
    scenarios.stopnote = async () => {""")
rep("""            await noteCase(page, '142b-stop-left-the-unit-marked-failed-without-a-line', stopped('unit_marked_failed', 'redis-server', 'redis-server.service', 'timeout', ''), stop);
            await closePage(page);""",
    """            await noteCase(page, '142b-stop-left-the-unit-marked-failed-without-a-line', stopped('unit_marked_failed', 'redis-server', 'redis-server.service', 'timeout', ''), stop);
            await closePage(page);
            page = await open('/services/postfix', ready);
            await pendingCase(page, '142c-postfix-stop-the-unit-had-not-settled', pending('unit_not_settled', 'postfix', 'postfix@-.service', 'deactivating'), stop);
            await closePage(page);
            page = await open('/services/postfix', ready);
            await pendingCase(page, '142d-postfix-stop-the-unit-could-not-be-read', pending('unit_state_not_read', 'postfix', 'postfix@-.service', ''), stop);
            await closePage(page);""")
rep("""const TARGET = { version: 'v0.1.0-alpha.82',""",
    """const pending = (reason, unit, pendingUnit, state) => ({
    success: true, outcome: 'verified', applied: 'stopped',
    note: {
        error: 'The service was stopped and is not running. How its unit ended was not read.', code: 'SERVICE_ACTION_NOTE', reason,
        vars: { unit, pending_unit: pendingUnit, command: `systemctl status ${pendingUnit}`, ...(state ? { state } : {}) },
    },
});
const TARGET = { version: 'v0.1.0-alpha.82',""")
rep("""//   stopnote          a Stop that succeeded and left the unit marked as failed
//                     says so on the attention surface, never as a failure.""",
    """//   stopnote          a Stop that succeeded and left the unit marked as failed
//                     says so on the attention surface, never as a failure.
//                     Since 9 Oct 2026 also a Stop whose unit was not read as
//                     settled: it says that, and claims no mark.""")
io.open(p, 'w', encoding='utf-8', newline='').write(s)

p = 'README.md'
s = io.open(p, encoding='utf-8', newline='').read()
nl = '\r\n' if '\r\n' in s else '\n'
a = "| `stopnote` | a Stop that succeeded and left the unit marked as failed, with and without a line of the service's own (142a, 142b). Fails when it stands on the failure surface or is not announced as a status |"
assert s.count(a) == 1
s = s.replace(a, a[:-2] + ". Since 2026-10-09 also a Stop whose unit had not settled when the wait ended, and one whose unit could not be read (142c, 142d): fails when the note claims a mark that was not read, or offers a command that changes the unit |")
io.open(p, 'w', encoding='utf-8', newline='').write(s)
print('edited')
