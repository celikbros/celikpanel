#!/usr/bin/env bash
# set6: read-only host check before the run.
date -u +%FT%TZ
echo "--- whoami"; id -un
echo "--- uptime"; uptime
echo "--- memory"; free -m
echo "--- cpus"; nproc
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
qemu-system-x86_64 --version | head -n 1
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set6*"; ls -d /var/tmp/cp-set6* /var/tmp/cp-release-drill-s6* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py|set4_|set4b_|set5_|set6_|go test|go build' | grep -v pgrep | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- 72b879eea'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse '72b879eea^{commit}' '72b879eea^{tree}'
echo '--- tag alpha.81'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'v0.1.0-alpha.81^{commit}' 'v0.1.0-alpha.81^{tree}'
echo "--- /var/tmp entries"; ls -la /var/tmp
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null | tail -n 12 || echo "no /var/tmp/cp-upd1-build"
echo "--- pair-accept"; ls -la /var/tmp/cp-pair-accept/ /var/tmp/cp-pair-accept/dist/ 2>/dev/null
echo "--- a81 dist"; ls -la /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/ 2>/dev/null; sha256sum /var/tmp/cp-pair-accept/dist/a0beb7263d1f4ca72258f6b306f9111ba4e2a334-acceptance-license/*.tar* 2>/dev/null
echo "--- var/tmp disk"; df -h /var/tmp | tail -1; df -B1 /var/tmp | tail -1
echo "--- listening ports 4xxx / 18xxx"; ss -ltn 2>/dev/null | grep -E ':(4[0-9]{3}|18[0-9]{3})\b' || echo none
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l
echo "--- tmpfs mounts under /var/tmp"; findmnt -rn -t tmpfs -o TARGET | grep '^/var/tmp' || echo none
