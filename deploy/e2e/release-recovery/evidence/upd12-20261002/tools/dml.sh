#!/bin/bash
# usage: dml.sh LAB -> dml12.py on the lab's latest evidence run (read-only)
ev=$(ls -d /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/ | tail -1)
PYTHONDONTWRITEBYTECODE=1 python3 /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd12/dml12.py "$ev"
