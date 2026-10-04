# Read-only: OS, kernel and DNS software versions on one guest (Debian or Arch).
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
. /etc/os-release; echo "os=$PRETTY_NAME kernel=$(uname -r)"
if command -v dpkg-query >/dev/null 2>&1; then
  echo "== dpkg-query"
  LC_ALL=C dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}\n' 'bind9*' 'pdns*' python3 systemd 2>&1
fi
if command -v pacman >/dev/null 2>&1; then
  echo "== pacman -Q"; pacman -Q bind powerdns python systemd linux 2>&1
fi
echo "== named -v"; (command -v named >/dev/null && named -v) 2>&1
echo "== pdns_server --version"; (command -v pdns_server >/dev/null && pdns_server --version 2>&1 | head -2) 2>&1
echo "== management binaries"; sha256sum /opt/celikpanel/bin/* /usr/libexec/celikpanel/recovery 2>&1
