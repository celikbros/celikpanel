#!/bin/bash
set -u
whoami; date -u +%FT%TZ
R=/var/tmp/cp-set9-run
if [ -e $R ]; then echo "exists:"; ls -la $R; tail -n 5 $R/progress.txt 2>/dev/null; else mkdir -p $R && echo "$(date -u +%FT%TZ) set9 run directory created" > $R/progress.txt && echo created; fi
echo "--- labs"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null | tail -n 12
echo "--- run dirs"; ls -d /var/tmp/cp-set*-run 2>/dev/null
echo "--- qemu"; pgrep -a qemu | cut -c1-200 || echo "no qemu"
df -h /var/tmp | tail -1
free -m | sed -n 2p
nproc
ls /opt/celikpanel-test-toolchains/go1.26.5/go/bin
python3 --version
ls -la /var/tmp/cp-v3n28/images
