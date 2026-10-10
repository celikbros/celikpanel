#!/bin/bash
# set8: read-only look at the WSL QEMU host before anything is created.
echo "date_utc=$(date -u +%FT%TZ) user=$(id -un) kernel=$(uname -r)"
echo "qemu procs: $(pgrep -c qemu)"; pgrep -af qemu | cut -c1-160
echo "--- /var/tmp"; ls -la /var/tmp | cut -c1-160
echo "--- set8 run dir"; ls -la /var/tmp/cp-set8-run 2>&1 | head
echo "--- image cache"; ls -la /var/tmp/cp-v3n28/images 2>&1
echo "--- drills"; ls -d /var/tmp/cp-release-drill-* 2>/dev/null
echo "--- dist"; ls -la /var/tmp/cp-pair-accept/dist 2>&1 | tail -n 20
echo "--- builds"; ls -la /var/tmp/cp-upd1-build 2>&1 | tail -n 12
echo "--- go"; /opt/celikpanel-test-toolchains/go1.26.5/go/bin/go version
echo "--- df"; df -h /var/tmp | tail -1
free -m | sed -n 2p
qemu-system-x86_64 --version | head -n1
python3 --version
