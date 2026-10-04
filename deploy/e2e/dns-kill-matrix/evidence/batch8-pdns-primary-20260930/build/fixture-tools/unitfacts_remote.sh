echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
for u in pdns.service named.service bind9.service celikpanel-agent.service celikpanel-panel.service; do echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainPID,ExecMainStartTimestamp,NRestarts,InvocationID,ActiveEnterTimestamp 2>&1; done
echo "== named/pdns processes"; pgrep -a -x named; pgrep -a -x pdns_server
echo "== boots"; journalctl --list-boots --no-pager 2>&1 | tail -5
echo "== packages"; LC_ALL=C dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}\n' pdns-server pdns-backend-sqlite3 bind9 bind9-utils bind9-libs bind9-host 2>&1
echo "== pdns_server --version"; /usr/sbin/pdns_server --version 2>&1 | head -3
echo "== state dir"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/
(cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
echo "== fixture dir"; ls -la --time-style=full-iso /var/lib/celikpanel-dns-kill-matrix/ 2>&1
echo "== recovery launcher"; ls -la --time-style=full-iso /usr/libexec/celikpanel/ 2>&1; sha256sum /usr/libexec/celikpanel/recovery 2>&1
echo "== owner files"; ls -la --time-style=full-iso /etc/powerdns /etc/powerdns/pdns.d /var/lib/powerdns 2>&1; sha256sum /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/* /var/lib/powerdns/pdns.sqlite3 2>&1
echo "== bind dirs"; ls -la --time-style=full-iso /etc/bind 2>&1 | head -40
echo "== /etc/bind file hashes"; find /etc/bind -xdev -type f -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum
echo "== /etc/bind inodes"; find /etc/bind -xdev -type f -printf "%i %n %u:%g %m %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" 2>/dev/null | sort -k7
echo "== /var/cache/bind tree"; find /var/cache/bind -xdev -printf "%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" 2>&1 | sort -k5 | head -60
echo "== named-checkconf -l"; named-checkconf -l 2>&1 | head -20
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== pdns journal tail"; journalctl -u pdns.service --no-pager -o short-iso-precise 2>&1 | tail -n 30
if command -v pacman >/dev/null 2>&1; then
  echo "== pacman -Q (Arch)"; pacman -Q bind powerdns 2>&1
  echo "== /etc/named.conf"; ls -la --time-style=full-iso /etc/named.conf 2>&1; sha256sum /etc/named.conf 2>&1
  echo "== /var/named tree"; find /var/named -xdev -printf "%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p -> %l\n" 2>&1 | sort -k5 | head -80
fi
