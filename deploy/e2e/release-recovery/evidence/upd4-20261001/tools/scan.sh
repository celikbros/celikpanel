#!/bin/bash
R=/var/tmp/cp-upd4-run
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd4run
S=$R/stage/upd4-20261001
python3 $P/scan4.py $S $S/secret-scan.txt
echo "--- installer logs naming the real origin"
for f in $S/*/run-*/host/current-worker-baseline-*.log; do printf '%s celikpanel.net=%s 185.95.=%s\n' "${f#$S/}" "$(grep -c 'celikpanel\.net' $f)" "$(grep -c '185\.95\.' $f)"; done | tee -a $S/secret-scan.txt
cp $S/secret-scan.txt $P/pulled/
