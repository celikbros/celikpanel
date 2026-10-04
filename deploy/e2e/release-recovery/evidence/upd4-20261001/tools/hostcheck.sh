#!/usr/bin/env bash
# Read-only host check before upd4.
date -u
echo "--- memory"; free -m
echo "--- disk"; df -h /var/tmp / 2>/dev/null
echo "--- qemu"; pgrep -a qemu || echo "no qemu process"
echo "--- kvm"; ls -l /dev/kvm
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- node"; node --version; npm --version
echo "--- python"; python3 --version
echo "--- images"; ls -la /var/tmp/cp-v3n28/images
echo "--- existing cp-upd4*"; ls -d /var/tmp/cp-upd4* /var/tmp/cp-release-drill-upd4* 2>/dev/null || echo none
echo "--- cp-upd1-build"; ls -la /var/tmp/cp-upd1-build 2>/dev/null
echo "--- pair dist"; ls /var/tmp/cp-pair-accept/dist 2>/dev/null | wc -l
echo "--- var/tmp"; du -sh /var/tmp 2>/dev/null
echo "--- repo head"; git -c safe.directory='*' -C '/mnt/c/CELIKBROS PROJECTS/celikpanel' rev-parse HEAD
echo "--- PATH go"; command -v go || echo "go not on PATH"
