#!/bin/bash
# upd13: staged-folder summaries (host only, read-only on the evidence runs).
P=/mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/upd13
S="/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/release-recovery/evidence/upd13-20261002"
export PYTHONDONTWRITEBYTECODE=1
python3 $P/summary13.py "$S" "$S" > /dev/null && echo summary-ok
python3 $P/texts13.py "$S" "$S/owner-texts.txt" && echo texts-ok
python3 $P/dmltable13.py "$S" /var/tmp/cp-upd13-run/harness-h22 "$S/deferred-mail-timeline.txt" > /dev/null && echo dml-ok
python3 $P/cpsum13.py "$S" "$S/changed-paths-table.md" > /dev/null && echo cpsum-ok
python3 $P/cells13.py "$S" "$S/cells-table.md" > /dev/null && echo cells-ok
python3 $P/gaps13.py "$S" "$S/sampler-gaps.txt" > /dev/null && echo gaps-ok
find /var/tmp/cp-upd13-run -name __pycache__ -type d
ls "$S"
