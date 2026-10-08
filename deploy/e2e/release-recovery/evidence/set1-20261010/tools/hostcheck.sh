#!/usr/bin/env bash
# Read-only host check before set1.
date -u
echo "--- memory"; free -m
echo "--- qemu"; pgrep -a qemu | cut -c1-160 || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-set1*"; ls -d /var/tmp/cp-set1* /var/tmp/cp-release-drill-set1* 2>/dev/null || echo none
echo "--- other jobs"; pgrep -af 'run-upd1|owner_update_trial|settings_writes|runall|lab.py' | cut -c1-160 || echo none
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo '--- c4cf7fd9'; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse 'c4cf7fd9^{commit}'
echo "--- builds"; ls /var/tmp/cp-upd1-build/ 2>/dev/null
echo "--- git local config key list"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' config --list --local | sed 's/=.*//' | sort | md5sum
echo "--- var/tmp listing"; ls /var/tmp | head -200
echo "--- go build cache"; du -sh /root/.cache/go-build 2>/dev/null
