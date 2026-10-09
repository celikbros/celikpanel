#!/usr/bin/env bash
# set4b: read-only host check before the run.
date -u +%FT%TZ
echo "--- memory"; free -m
echo "--- cpus"; nproc
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set4b*"; ls -d /var/tmp/cp-set4b* /var/tmp/cp-release-drill-s4b* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py|set4_|set4b_' | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD 'HEAD^{tree}'
echo "--- set4 builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null | tail -n 6; ls -la /var/tmp/cp-upd1-build/20261009t101529z/upd1-artifacts.json
echo "--- var/tmp count"; ls /var/tmp | wc -l
echo "--- var/tmp disk"; df -h /var/tmp | tail -1
echo "--- listening ports 4xxx / 18xxx"; ss -ltn 2>/dev/null | grep -E ':(4[0-9]{3}|18[0-9]{3})\b' || echo none
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | wc -l
echo "--- set4 run dir"; ls /var/tmp/cp-set4-run | head -40
echo "--- whoami"; id -un
