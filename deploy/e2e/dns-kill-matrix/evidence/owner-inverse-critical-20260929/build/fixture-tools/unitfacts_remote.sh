echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
for u in pdns.service named.service bind9.service celikpanel-agent.service celikpanel-panel.service; do echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainPID,ExecMainStartTimestamp,NRestarts,InvocationID,ActiveEnterTimestamp 2>&1; done
echo "== packages"; LC_ALL=C dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}\n' pdns-server pdns-backend-sqlite3 bind9 bind9-utils 2>&1
echo "== pdns_server --version"; /usr/sbin/pdns_server --version 2>&1 | head -3
echo "== state dir"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/
(cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
echo "== fixture dir"; ls -la --time-style=full-iso /var/lib/celikpanel-dns-kill-matrix/ 2>&1
echo "== recovery launcher"; ls -la --time-style=full-iso /usr/libexec/celikpanel/ 2>&1; sha256sum /usr/libexec/celikpanel/recovery 2>&1
echo "== owner files"; ls -la --time-style=full-iso /etc/powerdns /etc/powerdns/pdns.d /var/lib/powerdns 2>&1; sha256sum /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/* /var/lib/powerdns/pdns.sqlite3 2>&1
echo "== bind dirs"; ls -la --time-style=full-iso /etc/bind 2>&1 | head -30
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== pdns journal tail"; journalctl -u pdns.service --no-pager -o short-iso-precise 2>&1 | tail -n 30
