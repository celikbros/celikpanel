#!/usr/bin/env python3
"""Read-only side inspection of a running upd2 lab guest (observer only; no mutation).

usage: inspect.py LAB_ROOT NODE LABEL OUTDIR
"""
import json, sys, time, datetime as dt
from pathlib import Path
HERE = Path("/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery")
sys.path.insert(0, str(HERE))
import lab  # noqa: E402

root_s, node, label, outdir = sys.argv[1:5]
root = lab.checked_root(root_s)
record, plan = lab.load(root)
BODY = r'''
set +eu
exec 2>&1
echo "== utc"; date -u +%FT%TZ; cat /proc/sys/kernel/random/boot_id
echo "== os"; . /etc/os-release; echo "$ID $VERSION_ID"
echo "== crontab binary"; command -v crontab || echo "crontab: absent"
echo "== cron packages"; (dpkg-query -W -f='${Package} ${Version} ${Status}\n' cron cron-daemon-common 2>/dev/null || true); (pacman -Q cronie 2>/dev/null || true)
echo "== cron units"; for u in cron.service cronie.service; do systemctl show "$u" -p Id -p LoadState -p ActiveState -p SubState -p UnitFileState -p ActiveEnterTimestamp -p NRestarts; echo; done
echo "== cron spool"; for d in /var/spool/cron/crontabs /var/spool/cron; do [ -d "$d" ] && ls -la "$d"; done
for d in /var/spool/cron/crontabs /var/spool/cron; do [ -d "$d" ] || continue; for f in "$d"/*; do [ -f "$f" ] || continue; echo "-- $f (owner $(stat -c %U "$f"))"; cat "$f"; done; done
echo "== cron.d (panel must write none)"; ls -la /etc/cron.d 2>/dev/null || echo "no /etc/cron.d"
echo "== stamp files"; for h in /home/* /var/www/*; do [ -f "$h/upd1-cron-stamp.txt" ] && { echo "$h/upd1-cron-stamp.txt $(stat -c %y "$h/upd1-cron-stamp.txt") $(cat "$h/upd1-cron-stamp.txt")"; }; done 2>/dev/null
echo "== origin unit"; systemctl show cp-lab-upd1-origin.service -p ActiveState -p SubState -p UnitFileState -p NRestarts -p MainPID -p ActiveEnterTimestamp
echo "== hosts celikpanel.net"; grep -n celikpanel /etc/hosts; getent hosts celikpanel.net
echo "== listeners 80/443/panel"; ss -ltnpH | awk '{print $4, $6}' | grep -E ':(80|443|8443|2083|2087|587|25|53) ' || true
echo "== all listeners"; ss -ltnH | awk '{print $4}' | sort -u | tr '\n' ' '; echo
echo "== nginx"; systemctl show nginx.service -p ActiveState -p SubState -p NRestarts 2>/dev/null; (nginx -T 2>/dev/null | grep -nE '^\s*listen ' | sort -u | head -40)
echo "== celikpanel units"; systemctl list-units --all --no-legend 'celikpanel*' | head -40
echo "== timers"; systemctl list-timers --all --no-legend | head -30
echo "== site: owner account"; getent passwd upd1_owner_test; id upd1_owner_test
echo "== site: nginx worker user"; ps -o user=,pid=,args= -C nginx | head -4
echo "== site: vhost files naming the domain"; grep -rln "upd1-owner.test" /etc/nginx 2>/dev/null
echo "== site: include lines of nginx.conf"; grep -nE "include|server_names_hash|user " /etc/nginx/nginx.conf
echo "== site: nginx -T blocks naming the domain (server block text)"
nginx -T 2>/dev/null | awk '/^# configuration file /{f=$4} /upd1-owner/{print f": "NR": "$0}' | head -20
for f in $(grep -rln "upd1-owner.test" /etc/nginx 2>/dev/null); do echo "---- $f"; cat "$f"; done
echo "== site: index.html files for the domain"; find / -xdev \( -path /proc -o -path /sys -o -path /run \) -prune -o -name index.html -newer /etc/hostname -print 2>/dev/null | head -20
for r in $(nginx -T 2>/dev/null | awk '/upd1-owner/{s=1} s&&/^\s*root /{gsub(";","",$2); print $2; s=0}' | sort -u); do echo "---- root $r"; namei -l "$r" 2>&1 | tail -8; ls -la "$r" 2>&1 | head; done
echo "== site: local requests"; curl -sS -o /dev/null -w 'host-header / -> %{http_code}\n' -H 'Host: upd1-owner.test' http://127.0.0.1/; curl -sS -o /dev/null -w 'host-header /index.html -> %{http_code}\n' -H 'Host: upd1-owner.test' http://127.0.0.1/index.html
echo "== site: nginx error log tail"; for l in /var/log/nginx/error.log /var/log/nginx/*upd1*; do [ -f "$l" ] && { echo "-- $l"; tail -15 "$l"; }; done
journalctl -u nginx --no-pager -n 15 2>/dev/null | tail -15
'''
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
