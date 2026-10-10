#!/bin/bash
# set8: list what this run leaves on the WSL host and confirm nothing of it is running (removes nothing).
R=/var/tmp/cp-set8-run
echo "processes: qemu=$(pgrep -c qemu) hold=$(pgrep -fc 'set8/tools/hold.sh') set8driver=$(pgrep -fc set8_trial.py) jobs=$(pgrep -fc 'cp-set8-run/job-')"
du -sh $R /var/tmp/cp-release-drill-s8-* /var/tmp/cp-upd1-build/20261010t154207z 2>/dev/null
echo "overlay disks left in set8 labs: $(find /var/tmp/cp-release-drill-s8-* -name 'overlay.qcow2' | wc -l)"
ls -d /var/tmp/cp-release-drill-dry-* 2>/dev/null || echo "no dry-run lab"
