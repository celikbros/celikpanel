#!/bin/bash
# usage: runcell.sh SHORT CELL FIXTURE PREP ENROLL RUN_FLAGS...
SP=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/batch4
L=/var/tmp/cp-b4-0929/logs/$1
bash $SP/cell.sh "$@"
if grep -q 'not-run' $L/run-prepared.rc 2>/dev/null; then
  echo "NO-COLLECT (setup failed)" >> $L/driver.log
else
  bash $SP/collect.sh "$1" "$2"
fi
echo "ALLDONE $(date -u +%FT%TZ)" >> $L/driver.log
