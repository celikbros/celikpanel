#!/bin/bash
# set9: remove what this run created and no longer needs (the password copies kept for the scan), end the WSL hold, list what is left.
set -u
R=/var/tmp/cp-set9-run
rm -rf -- $R/scanvalues
touch $R/hold.stop
echo "secret dir: $(ls -A $R/secret 2>/dev/null | wc -l) file(s)"
echo "processes: qemu=$(pgrep -c qemu) set9driver=$(pgrep -fc set9_trial.py) bg-jobs=$(pgrep -fc 'job-cell')"
du -sh $R /var/tmp/cp-release-drill-s9-* 2>/dev/null
ls -d /var/tmp/cp-upd1-build/* 2>/dev/null | tail -n 2
find /var/tmp/cp-release-drill-s9-* -name 'overlay.qcow2' | wc -l
