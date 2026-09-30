#!/usr/bin/env python3
"""Resolver diagnosis on a fresh disposable Arch lab guest. Never looks up celikpanel.net."""
import sys, subprocess
from pathlib import Path
HERE = Path("/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery")
sys.path.insert(0, str(HERE))
import lab
root = lab.checked_root(sys.argv[1]); node = sys.argv[2]
record, plan = lab.load(root)
BODY = r'''
set +eu
exec 2>&1
N=upd2-diag.example.net
echo "== os"; . /etc/os-release; echo "$ID $VERSION_ID"; uname -r
echo "== nsswitch"; grep -E '^hosts:' /etc/nsswitch.conf
echo "== resolved"; systemctl is-active systemd-resolved; ls -la /etc/resolv.conf /etc/hosts; cat /etc/hosts
echo "== add test entry"; printf '\n127.0.0.1 %s # upd2 resolver diagnosis\n' "$N" >> /etc/hosts; tail -2 /etc/hosts
sleep 2
for cmd in "getent hosts $N" "getent ahostsv4 $N" "getent ahosts $N" "getent ahostsv6 $N" "resolvectl query $N" "python3 -c 'import socket;print(socket.getaddrinfo(\"$N\",443,0,socket.SOCK_STREAM))'"; do
  echo "== $cmd"; s=$(date +%s.%N); timeout 25 bash -c "$cmd"; rc=$?; e=$(date +%s.%N); echo "rc=$rc elapsed=$(python3 -c "print(round($e-$s,2))")"
done
echo "== same via the helper environment (PATH only, LC_ALL=C)"; s=$(date +%s.%N); env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C timeout 25 getent hosts $N; echo "rc=$? elapsed=$(python3 -c "print(round($(date +%s.%N)-$s,2))")"
'''
res = lab.guarded_script(root, record, plan, node, BODY, timeout=240)
print(res.stdout)
