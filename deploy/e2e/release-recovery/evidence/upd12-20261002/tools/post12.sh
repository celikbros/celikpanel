#!/bin/bash
# upd12: staged-folder summaries (host only, read-only on the evidence runs).
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd12
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd12-20261002"
export PYTHONDONTWRITEBYTECODE=1
python3 $P/summary12.py "$S" "$S" && echo summary-ok
python3 $P/texts12.py "$S" "$S/owner-texts.txt" && echo texts-ok
ls "$S"
