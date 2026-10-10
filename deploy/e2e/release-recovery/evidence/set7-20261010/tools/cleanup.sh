#!/bin/bash
# set7: remove what this run created and no longer needs: the password copies kept for the scan, the loopback test
# directory, the hold stop file; list what is left.
set -u
R=/var/tmp/cp-set7-run
rm -rf -- $R/scanvalues $R/lptest
ls -la $R/secret 2>/dev/null | tail -n +4
echo "processes: qemu=$(pgrep -c qemu) hold=$(pgrep -fc 'job-hold.sh') set7driver=$(pgrep -fc set7_trial.py)"
du -sh $R /var/tmp/cp-release-drill-s7-* /var/tmp/cp-upd1-build/20261010t125013z /var/tmp/cp-upd1-build/20261010t125447z 2>/dev/null
ls -d /var/tmp/cp-pair-accept/dist/dc81296d* /var/tmp/cp-pair-accept/dist/2e9b30af* /var/tmp/cp-pair-accept/dist/57e9bfab* 2>/dev/null
find /var/tmp/cp-release-drill-s7-* -name 'overlay.qcow2' | wc -l
