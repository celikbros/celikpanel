# Read-only capture after the controller finished (after the owner command and its re-run). Arg: REQUEST_ID
RID=$1
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id) request_id=$RID"
for u in pdns.service named.service bind9.service; do
  echo "== systemctl show $u"
  systemctl show "$u" -p Id,LoadState,LoadError,FragmentPath,UnitFileState,UnitFilePreset,ActiveState,SubState,MainPID,ControlPID,ExecMainPID,ExecMainStartTimestamp,NRestarts,InvocationID,ActiveEnterTimestamp,InactiveEnterTimestamp,NeedDaemonReload 2>&1
  echo "== systemctl is-enabled $u"; systemctl is-enabled "$u" 2>&1; echo "rc=$?"
  echo "== systemctl is-active $u"; systemctl is-active "$u" 2>&1; echo "rc=$?"
done
echo "== mask symlinks (persistent and runtime)"
for d in /etc/systemd/system /run/systemd/system; do for u in named.service bind9.service; do
  p=$d/$u
  if [ -e "$p" ] || [ -L "$p" ]; then stat -c '%N owner=%U:%G mode=%a mtime=%y' "$p"; else echo "$p absent"; fi
done; done
echo "== named processes"; pgrep -a -x named || echo "no named process"
echo "== pdns_server processes"; pgrep -a -x pdns_server || echo "no pdns_server process"
echo "== journal entry counts"; for u in named.service bind9.service; do echo "$u $(journalctl -u $u --no-pager -q -o cat 2>/dev/null | wc -l)"; done
echo "== dpkg -l bind9*"; COLUMNS=200 dpkg -l 'bind9*' 2>&1
echo "== dpkg -l pdns*"; COLUMNS=200 dpkg -l 'pdns*' 2>&1
echo "== dpkg --verify bind9 bind9-utils bind9-libs bind9-host bind9-dnsutils (read-only; no output = unchanged)"; dpkg --verify bind9 bind9-utils bind9-libs bind9-host bind9-dnsutils 2>&1; echo "rc=$?"
echo "== install-ownership receipt"; ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/dns-engine-install-ownership-bind.json 2>&1
echo "== /var/cache/bind tree (managed BIND root parent)"
find /var/cache/bind -xdev -printf '%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p%l\n' 2>&1 | sort -k5
echo "== /var/cache/bind file hashes"; find /var/cache/bind -xdev -type f -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum
echo "== /var/lib/bind tree"
find /var/lib/bind -xdev -printf '%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p %l\n' 2>&1 | sort -k5
echo "== /etc/bind tree"
find /etc/bind -xdev -printf '%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p %l\n' 2>&1 | sort -k5
echo "== /etc/bind file hashes"; find /etc/bind -xdev -type f -print0 2>/dev/null | sort -z | xargs -0 -r sha256sum
echo "== /etc/bind package ownership"; for f in $(find /etc/bind -xdev -type f | sort); do o=$(dpkg-query -S "$f" 2>/dev/null | cut -d: -f1); echo "$f ${o:-not-owned-by-a-package}"; done
echo "== bind9 conffiles (package md5)"; dpkg-query -W -f='${Conffiles}\n' bind9 2>&1
echo "== md5 of /etc/bind files"; find /etc/bind -xdev -type f -print0 2>/dev/null | sort -z | xargs -0 -r md5sum
echo "== apt history (bind9 transactions)"; grep -n -B1 -A4 'bind9' /var/log/apt/history.log 2>&1 | tail -n 30
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== recovery dns-switch-status --quiesced (after owner command and re-run)"
cd /
/usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C.UTF-8 /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id "$RID" > /tmp/b4-status.out 2>&1
rc=$?
cat /tmp/b4-status.out
echo "STATUS_RC=$rc"
sha256sum /tmp/b4-status.out
echo "== private evidence after status"
ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/
(cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
