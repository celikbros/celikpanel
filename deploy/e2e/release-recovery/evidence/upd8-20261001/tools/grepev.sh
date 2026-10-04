#!/bin/bash
# usage: grepev.sh LAB [WIDTH] [LINES] [FILEGLOB]  -> read-only grep (pattern from pat.txt) over the cell evidence
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd8/pat.txt
cd $ev && grep -r -n -E -f <(tr -d '\r' < $P) --include="${4:-*}" . | cut -c1-${2:-400} | head -${3:-40}
