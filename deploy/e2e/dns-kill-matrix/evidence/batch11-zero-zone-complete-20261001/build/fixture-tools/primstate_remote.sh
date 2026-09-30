# Read-only capture of the PowerDNS primary guest (Debian): units, listeners, config digests, state receipt, pdns journal lines.
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
for u in pdns.service named.service bind9.service celikpanel-agent.service celikpanel-panel.service; do
  echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainStartTimestamp,NRestarts 2>&1
done
echo "== pdns.service mask symlinks"; for d in /etc/systemd/system /run/systemd/system; do p=$d/pdns.service; if [ -e "$p" ] || [ -L "$p" ]; then stat -c '%N mtime=%y' "$p"; else echo "$p absent"; fi; done
echo "== pdns_server processes"; pgrep -a -x pdns_server || echo none
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== /etc/powerdns"; ls -la --time-style=full-iso /etc/powerdns /etc/powerdns/pdns.d 2>&1; sha256sum /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/* 2>&1
echo "== pdns.conf (non-comment lines; values of key/secret/password settings redacted)"
grep -vE '^\s*(#|$)' /etc/powerdns/pdns.conf 2>/dev/null | sed -E 's/^(\s*[^=]*(key|secret|password)[^=]*=).*/\1<redacted>/I'
echo "== /var/lib/powerdns"; ls -la --time-style=full-iso /var/lib/powerdns 2>&1
echo "== state receipt"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/dns-engine-state.json 2>&1
python3 -c 'import json;d=json.load(open("/var/lib/celikpanel-agent-private/dns-engine-state.json"));print(json.dumps({k:d.get(k) for k in ("schema","request_id")}));print("semantic:",json.dumps(d.get("semantic"),sort_keys=True)[:1500])' 2>&1
echo "== pdns journal (notify/xfr/catalog/start lines, all boots)"
journalctl -u pdns.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'xfr|transfer|notif|catalog|producer|serial|start|stop|listen|ready|exit' | tail -n 80
