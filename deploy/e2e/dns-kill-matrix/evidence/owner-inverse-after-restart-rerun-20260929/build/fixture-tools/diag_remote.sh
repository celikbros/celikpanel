echo "wall=$(date -u +%FT%T.%NZ)"
for u in named.service bind9.service; do
  echo "== systemctl show $u (full)"; systemctl show "$u" --no-pager 2>&1 | grep -E '^(Id|Names|LoadState|LoadError|FragmentPath|SourcePath|DropInPaths|UnitFileState|UnitFilePreset|ActiveState|SubState|MainPID|ControlPID|ExecMainPID|NeedDaemonReload|Type|ExecStart|EnvironmentFiles|User|Following|Transient)=' 
  echo "== systemctl status $u"; systemctl status "$u" --no-pager 2>&1 | head -8
done
echo "== unit files"; ls -la --time-style=full-iso /etc/systemd/system/named.service /etc/systemd/system/bind9.service /run/systemd/system/named.service /run/systemd/system/bind9.service /usr/lib/systemd/system/named.service /usr/lib/systemd/system/bind9.service /lib/systemd/system/named.service 2>&1
readlink -v /etc/systemd/system/named.service /etc/systemd/system/bind9.service 2>&1
echo "== list-unit-files"; systemctl list-unit-files 'named*' 'bind9*' --no-pager 2>&1
echo "== dpkg -S"; dpkg -S /usr/lib/systemd/system/named.service 2>&1
echo "== /etc/bind"; ls -la --time-style=full-iso /etc/bind 2>&1
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== apt history tail"; tail -n 30 /var/log/apt/history.log 2>&1
