#!/usr/bin/env bash
# Read-only host check before set2.
date -u
echo "--- memory"; free -m
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set2*"; ls -d /var/tmp/cp-set2* /var/tmp/cp-release-drill-set2* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|request_identity|runall|lab.py' | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- faa5ef085'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'faa5ef085^{commit}' 'faa5ef085^{tree}'
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null
echo "--- git local config key list"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
echo "--- var/tmp listing"; ls /var/tmp | head -200
echo "--- var/tmp sizes"; du -sh /var/tmp 2>/dev/null
