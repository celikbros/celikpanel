#!/usr/bin/env python3
"""Read-only side inspection of a running upd3 lab guest (observer only; no mutation).

usage: cpinspect.py LAB_ROOT NODE LABEL OUTDIR [light]
'light' = hosting-root probe only (stat/namei/receipt), for the before-first-site series.
"""
import sys, time, datetime as dt
from pathlib import Path
HERE = Path("/var/tmp/cp-upd3-run/harness/deploy/e2e/release-recovery")
sys.path.insert(0, str(HERE))
import lab  # noqa: E402

root_s, node, label, outdir = sys.argv[1:5]
light = len(sys.argv) > 5 and sys.argv[5] == "light"
root = lab.checked_root(root_s)
record, plan = lab.load(root)
HOSTROOT = r'''
echo "== hosting root: stat of the parents"
for d in /var /var/www /var/www/celikpanel /var/www/celikpanel/subscriptions; do [ -e "$d" ] && stat -c '%A %a %U:%G %n' "$d" || echo "absent $d"; done
echo "== hosting root: site directories"
for d in /var/www/celikpanel/subscriptions/*/sites/*; do [ -d "$d" ] && { namei -l "$d/public_html" 2>&1; }; done 2>/dev/null
echo "== hosting root: receipt /var/lib/celikpanel-agent-private/hosting-root-v1.json"
f=/var/lib/celikpanel-agent-private/hosting-root-v1.json; [ -f "$f" ] && { stat -c '%A %a %U:%G %n' "$f"; cat "$f"; } || echo "absent"
echo "== hosting root: agent log lines"; journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'hosting root|site refused' | tail -10
echo "== web server accounts"; getent passwd http www-data nginx 2>/dev/null
'''
FULL = r'''
echo "== os"; . /etc/os-release; echo "$ID $VERSION_ID"
echo "== cron units"; for u in cron.service cronie.service; do systemctl show "$u" -p Id -p LoadState -p ActiveState -p SubState -p UnitFileState -p NRestarts; echo; done
echo "== cron spool"; for d in /var/spool/cron/crontabs /var/spool/cron; do [ -d "$d" ] && ls -la "$d"; done
for d in /var/spool/cron/crontabs /var/spool/cron; do [ -d "$d" ] || continue; for f in "$d"/*; do [ -f "$f" ] || continue; echo "-- $f (owner $(stat -c %U "$f"))"; cat "$f"; done; done
echo "== stamp files"; find /var/www /home -maxdepth 6 -name upd1-cron-stamp.txt 2>/dev/null | while read f; do echo "$f mtime=$(stat -c %y "$f") content=$(cat "$f")"; done
echo "== cron journal tail"; journalctl -u cronie.service -u cron.service --no-pager -n 8 -o short-iso 2>/dev/null
echo "== origin unit"; systemctl show cp-lab-upd1-origin.service -p ActiveState -p SubState -p UnitFileState -p NRestarts
echo "== hosts celikpanel.net"; grep -n celikpanel /etc/hosts; getent hosts celikpanel.net
echo "== listeners"; ss -ltnpH | awk '{print $4, $6}' | grep -E ':(80|443|2083|587|25|53) ' || true
echo "== celikpanel units"; systemctl list-units --all --no-legend 'celikpanel*' | head -40
echo "== timers"; systemctl list-timers --all --no-legend | head -30
echo "== installed build identity"; for b in /usr/libexec/celikpanel/*/build-identity /opt/celikpanel/BUILD-IDENTITY; do [ -f "$b" ] && { echo "-- $b"; cat "$b"; }; done 2>/dev/null
echo "== recovery observation records"; ls -la /var/lib/celikpanel-recovery-observations 2>/dev/null; for f in /var/lib/celikpanel-recovery-observations/*.failure; do [ -f "$f" ] && { echo "-- $f"; cat "$f"; }; done 2>/dev/null
echo "== site: vhost files naming the domain"; grep -rln "upd1-owner.test" /etc/nginx 2>/dev/null
echo "== site: local requests"; curl -sS -o /tmp/.cpinspect-body -w 'host-header / -> %{http_code}\n' -H 'Host: upd1-owner.test' http://127.0.0.1/ ; head -c 300 /tmp/.cpinspect-body 2>/dev/null; echo; rm -f /tmp/.cpinspect-body
echo "== site: nginx error log tail"; for l in /var/log/nginx/error.log; do [ -f "$l" ] && { echo "-- $l"; tail -8 "$l"; }; done
echo "== panel unit"; systemctl show celikpanel-panel.service -p ActiveState -p SubState -p NRestarts -p ExecMainStatus -p ActiveEnterTimestamp -p InactiveEnterTimestamp
'''
BODY = "set +eu\nexec 2>&1\necho '== utc'; date -u +%FT%TZ; cat /proc/sys/kernel/random/boot_id\n" + HOSTROOT + ("" if light else FULL) + "\ntrue\n"
t0 = time.time()
try:
    res = lab.guarded_script(root, record, plan, node, BODY, timeout=90)
    text, rc = res.stdout, res.returncode
except Exception as exc:  # noqa: BLE001
    text, rc = f"unavailable: {type(exc).__name__}: {exc}", None
out = Path(outdir); out.mkdir(parents=True, exist_ok=True)
stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
p = out / f"inspect-{label}-{stamp}.txt"
p.write_text(f"# read-only side inspection lab={root.name} node={node} label={label} rc={rc} took={time.time()-t0:.1f}s\n" + text)
print(p)
