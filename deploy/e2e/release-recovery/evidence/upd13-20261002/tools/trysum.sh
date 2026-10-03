#!/bin/bash
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
T=/var/tmp/cp-upd13-run/tmpout; mkdir -p $T
export PYTHONDONTWRITEBYTECODE=1
python3 $P/summary13.py "$S" $T > /dev/null && echo summary-ok
python3 $P/texts13.py "$S" $T/owner-texts.txt && echo texts-ok
python3 $P/cpsum13.py "$S" $T/changed-paths-table.md > /dev/null && echo cpsum-ok
wc -l $T/*
