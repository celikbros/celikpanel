#!/usr/bin/env bash
# set4: read-only host check before the run.
date -u +%FT%TZ
echo "--- memory"; free -m
echo "--- cpus"; nproc
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set4*"; ls -d /var/tmp/cp-set4* /var/tmp/cp-release-drill-set4* /var/tmp/cp-release-drill-s4* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py|set4_' | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- 557b554eb'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse '557b554eb^{commit}' '557b554eb^{tree}'
echo '--- tag alpha.81'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'v0.1.0-alpha.81^{commit}' 'v0.1.0-alpha.81^{tree}'
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null | tail -n 8
echo "--- dist dirs"; ls /var/tmp/cp-pair-accept/dist/ 2>/dev/null | wc -l; ls -d /var/tmp/cp-pair-accept/dist/a0beb7263* /var/tmp/cp-pair-accept/dist/557b554eb* 2>/dev/null || echo "no dist of the tag commit"
echo "--- git local config key list"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
echo "--- var/tmp count"; ls /var/tmp | wc -l
echo "--- var/tmp disk"; df -h /var/tmp | tail -1
echo "--- listening ports 4xxx / 184xx"; ss -ltn 2>/dev/null | grep -E ':(4[0-9]{3}|184[0-9]{2})\b' || echo none
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l
echo "--- set3 run dir"; ls /var/tmp/cp-set3-run | head -40
