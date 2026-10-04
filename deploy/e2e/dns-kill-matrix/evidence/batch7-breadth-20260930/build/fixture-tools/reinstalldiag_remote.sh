# Read-only diagnostics for the reinstall cell (no writes outside /tmp).
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
echo "== /var/lib/dpkg/statoverride"; ls -la --time-style=full-iso /var/lib/dpkg/statoverride*; cat /var/lib/dpkg/statoverride
echo "== getent group bind"; getent group bind; echo "rc=$?"
echo "== getent passwd bind"; getent passwd bind; echo "rc=$?"
echo "== /var/cache/bind"; ls -la --time-style=full-iso /var/cache/bind 2>&1; stat -c '%n %a %U:%G %u:%g' /var/cache/bind 2>&1
echo "== dpkg -l bind9*"; COLUMNS=200 dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}\n' 'bind9*' 2>&1
echo "== dpkg --audit (read-only)"; dpkg --audit 2>&1; echo "rc=$?"
echo "== apt history (last 60 lines)"; tail -n 60 /var/log/apt/history.log 2>&1
echo "== dpkg.log bind lines"; grep -n 'bind9\|statoverride' /var/log/dpkg.log 2>&1 | tail -n 40
echo "== private evidence"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/; (cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
if [ -f /var/lib/celikpanel-agent-private/dns-engine-switch-journal.json ]; then echo "== journal"; python3 -c 'import json;d=json.load(open("/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json"));print(json.dumps({k:d.get(k) for k in ("schema","phase","request_id","mode","source_engine","target_engine")}))'; else echo "== journal absent"; fi
echo "== ledger jobs"; python3 -c 'import json;d=json.load(open("/var/lib/celikpanel-agent-private/service-mutations.json"));j=d.get("jobs") or d.get("Jobs") or {};print(json.dumps(j,indent=1)[:5000])' 2>&1
echo "== install-ownership receipts"; for f in /var/lib/celikpanel-agent-private/*ownership*; do echo "-- $f"; cat "$f"; echo; done
echo "== units"; for u in named.service bind9.service pdns.service celikpanel-agent.service celikpanel-panel.service; do echo "-- $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID 2>&1; done
echo "== mask links"; ls -la /etc/systemd/system/named.service /etc/systemd/system/bind9.service /run/systemd/system/named.service /run/systemd/system/bind9.service 2>&1
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== agent journal (all)"; journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise 2>&1 | tail -n 40 | cut -c1-600
