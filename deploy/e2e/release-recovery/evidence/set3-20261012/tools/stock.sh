#!/bin/bash
# set3: read-only stock-take of the run on the WSL host.
R=/var/tmp/cp-set3-run; L=$R/logs
date -u +%FT%TZ
echo "--- qemu"; pgrep -a qemu | cut -c1-140 || echo "no qemu process"
echo "--- jobs"; pgrep -af 'run-upd1|run-set2|run-set3|owner_update_trial|settings_writes|request_identity|build-upd1|build-dist|lab.py|go build|npm' | cut -c1-160 || echo "no job process"
echo "--- logs"; for f in $L/*.start; do n=$(basename $f .start); echo "$n start=$(cat $f) end=$(cat $L/$n.end 2>/dev/null) rc=$(cat $L/$n.rc 2>/dev/null)"; done
echo "--- build out tail"; tail -n 4 $L/build.out 2>/dev/null | cut -c1-300
echo "--- build err tail"; tail -n 4 $L/build.err 2>/dev/null | cut -c1-300
echo "--- run dir"; ls $R
echo "--- labs of this run"; ls -d /var/tmp/cp-release-drill-set3* /var/tmp/cp-release-drill-rid3* /var/tmp/cp-release-drill-upd14* 2>/dev/null || echo none
echo "--- builds"; ls /var/tmp/cp-upd1-build/ | tail -n 3
echo "--- staged evidence"; ls '/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/set3-20261012' 2>/dev/null || echo "no evidence folder yet"
free -m | sed -n 2p
