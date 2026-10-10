#!/bin/bash
# usage: live.sh LAB STEP_GLOB [all]  -> the checks of a section of a running or finished cell (read-only)
f=$(ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/$2/section.json 2>/dev/null | tail -n 1)
[ -n "$f" ] || { echo "no section file yet for $2"; ls /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/ | tail -n 5; exit 0; }
bash /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set3/checks.sh "$f" ${3:-}
