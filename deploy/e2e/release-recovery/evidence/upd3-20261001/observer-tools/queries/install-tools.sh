#!/bin/bash
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd3run
T=/var/tmp/cp-upd3-run/tools
mkdir -p $T
for f in cpinspect.py sidecar.sh bg.sh start-cell.sh st.sh; do install -m 0755 $P/$f $T/$f; done
sed -i 's#cd /var/tmp/cp-upd3-run/harness; ##' $T/bg.sh
sed -i "s#bash -c \"#bash -c \"cd /var/tmp/cp-upd3-run/harness; #" $T/bg.sh
grep -n harness $T/bg.sh
ls -la $T
