#!/usr/bin/env bash
# Read-only host check before set3.
date -u
echo "--- memory"; free -m
echo "--- cpus"; nproc
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set3*"; ls -d /var/tmp/cp-set3* /var/tmp/cp-release-drill-set3* /var/tmp/cp-release-drill-rid3* /var/tmp/cp-release-drill-upd14* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py' | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- cfa329676'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'cfa329676^{commit}' 'cfa329676^{tree}'
echo '--- tag alpha.81'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'v0.1.0-alpha.81^{commit}' 'v0.1.0-alpha.81^{tree}'
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null | tail -n 12
echo "--- dist dirs"; ls /var/tmp/cp-pair-accept/dist/ 2>/dev/null | wc -l; ls -d /var/tmp/cp-pair-accept/dist/a0beb7263* 2>/dev/null || echo "no dist of the tag commit"
echo "--- git local config key list"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
echo "--- var/tmp count"; ls /var/tmp | wc -l
echo "--- listening ports 41xx-49xx / 184xx"; ss -ltn 2>/dev/null | grep -E ':(4[0-9]{3}|184[0-9]{2})\b' || echo none
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l
