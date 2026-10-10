#!/bin/bash
# set2 harness debugging session: restart the finished rid-ubuntu run-a lab (its evidence is already written) and run
# the sections the driver never reached. Output under /var/tmp/cp-set2-run/logs/debug-rid.*; not evidence.
export PYTHONDONTWRITEBYTECODE=1
cd /var/tmp/cp-set2-run/harness-dev
python3 deploy/e2e/release-recovery/lab.py start --work-root /var/tmp/cp-release-drill-rid-ub-a --execute
python3 deploy/e2e/release-recovery/lab.py status --work-root /var/tmp/cp-release-drill-rid-ub-a
python3 /mnt/c/Users/alice/AppData/Local/Temp/claude/c--CELIKBROS-PROJECTS-celikpanel/ab34f56e-94b5-4834-a9ea-4e8dbe057e7f/scratchpad/set2/debug_rid.py "$@"
