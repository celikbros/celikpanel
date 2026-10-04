#!/bin/bash
date -u +%FT%TZ
free -g | head -2
df -h /var/tmp / | cat
pgrep -a qemu || echo "no qemu"
ls -d /var/tmp/cp-upd3* /var/tmp/cp-release-drill-upd3* 2>/dev/null || echo "no upd3 paths"
ls /var/tmp/cp-upd1-build/ 2>/dev/null
ls /var/tmp/cp-v3n28/images 2>/dev/null
/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
command -v node npm python3 git
python3 --version; node --version
ls -la /dev/kvm
