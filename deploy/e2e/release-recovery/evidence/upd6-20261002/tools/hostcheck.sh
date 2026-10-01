#!/usr/bin/env bash
# Read-only host check before upd6.
date -u
echo "--- memory"; free -m
echo "--- disk"; df -BG /var/tmp / 2>/dev/null
echo "--- qemu"; pgrep -a qemu || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-upd6*"; ls -d /var/tmp/cp-upd6* /var/tmp/cp-release-drill-upd6* 2>/dev/null || echo none
echo "--- cp-upd1-build"; ls -la /var/tmp/cp-upd1-build 2>/dev/null
echo "--- pair dist"; ls /var/tmp/cp-pair-accept/dist 2>/dev/null | wc -l
echo "--- var/tmp entries"; ls -1 /var/tmp | wc -l
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo "--- 6cda60b8"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse '6cda60b8^{commit}'
